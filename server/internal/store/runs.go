package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Deployment runs double as the job queue.
//
// Postgres is already here and already durable, so a separate broker would add
// an operational dependency for no benefit at this scale. Workers claim rows
// with SELECT ... FOR UPDATE SKIP LOCKED, which lets several run concurrently
// without ever handing the same run to two of them, and LISTEN/NOTIFY wakes
// them immediately instead of polling.

// RunChannel is the LISTEN/NOTIFY channel workers wait on.
const RunChannel = "deployment_runs"

// ErrNoRuns means the queue is empty. It is an ordinary outcome, not a fault.
var ErrNoRuns = errors.New("store: no queued runs")

type Run struct {
	ID           string     `db:"id"            json:"id"`
	DeploymentID string     `db:"deployment_id" json:"deployment_id"`
	Number       int        `db:"number"        json:"number"`
	Trigger      RunTrigger `db:"trigger"       json:"trigger"`
	Status       RunStatus  `db:"status"        json:"status"`

	CommitSHA string `db:"commit_sha" json:"commit_sha"`
	ImageRef  string `db:"image_ref"  json:"image_ref"`
	Error     string `db:"error"      json:"error"`

	TriggeredBy *string `db:"triggered_by" json:"triggered_by"`

	QueuedAt   time.Time  `db:"queued_at"   json:"queued_at"`
	StartedAt  *time.Time `db:"started_at"  json:"started_at"`
	FinishedAt *time.Time `db:"finished_at" json:"finished_at"`

	LockedAt *time.Time `db:"locked_at" json:"-"`
	LockedBy *string    `db:"locked_by" json:"-"`
}

// Duration reports how long a finished run took.
func (r *Run) Duration() time.Duration {
	if r.StartedAt == nil {
		return 0
	}
	end := time.Now()
	if r.FinishedAt != nil {
		end = *r.FinishedAt
	}
	return end.Sub(*r.StartedAt)
}

const runColumns = `id, deployment_id, number, trigger, status,
	commit_sha, image_ref, error, triggered_by,
	queued_at, started_at, finished_at, locked_at, locked_by`

// EnqueueRun creates a queued run and wakes a worker.
//
// The run number is allocated inside the transaction from the current maximum,
// so two concurrent deploys of the same application cannot both become run 8.
func (s *Store) EnqueueRun(ctx context.Context, deploymentID string, trigger RunTrigger, triggeredBy *string) (*Run, error) {
	var run *Run

	err := s.tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			INSERT INTO deployment_runs (deployment_id, number, trigger, triggered_by)
			VALUES (
				$1,
				(SELECT COALESCE(max(number), 0) + 1 FROM deployment_runs WHERE deployment_id = $1),
				$2, $3
			)
			RETURNING `+runColumns, deploymentID, trigger, triggeredBy)
		if err != nil {
			return wrap("store: enqueue run", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Run])
		if err != nil {
			return wrap("store: enqueue run", err)
		}

		// Notify inside the transaction: Postgres holds the notification until
		// commit, so a worker can never be woken for a run it cannot yet see.
		if _, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, RunChannel, created.ID); err != nil {
			return wrap("store: notify run", err)
		}

		run = &created
		return nil
	})

	return run, err
}

// ClaimRun takes the oldest queued run and marks it running.
//
// SKIP LOCKED is what makes this safe with several workers: a row already
// locked by another transaction is passed over rather than waited on, so
// workers never serialise behind each other.
func (s *Store) ClaimRun(ctx context.Context, workerID string) (*Run, error) {
	var run *Run

	err := s.tx(ctx, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `
			SELECT id FROM deployment_runs
			WHERE status = 'queued'
			ORDER BY queued_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1`).Scan(&id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoRuns
			}
			return wrap("store: claim run", err)
		}

		rows, err := tx.Query(ctx, `
			UPDATE deployment_runs
			SET status = 'running', started_at = now(), locked_at = now(), locked_by = $2
			WHERE id = $1
			RETURNING `+runColumns, id, workerID)
		if err != nil {
			return wrap("store: claim run", err)
		}
		claimed, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Run])
		if err != nil {
			return wrap("store: claim run", err)
		}
		run = &claimed
		return nil
	})

	return run, err
}

// ReleaseStaleRuns re-queues runs whose worker died mid-flight.
//
// Without this a crash during a deploy leaves a run marked running for ever,
// and the deployment stuck showing "deploying" with nothing working on it.
func (s *Store) ReleaseStaleRuns(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE deployment_runs
		SET status = 'failed',
		    error = 'The worker running this deploy stopped unexpectedly.',
		    finished_at = now()
		WHERE status = 'running' AND locked_at < now() - $1::interval`,
		olderThan.String())
	if err != nil {
		return 0, wrap("store: release stale runs", err)
	}
	return tag.RowsAffected(), nil
}

// FinishRun records the outcome and updates the parent deployment.
func (s *Store) FinishRun(ctx context.Context, runID string, status RunStatus, imageRef, commitSHA, failure string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		var deploymentID string
		err := tx.QueryRow(ctx, `
			UPDATE deployment_runs
			SET status = $2, image_ref = COALESCE(NULLIF($3, ''), image_ref),
			    commit_sha = COALESCE(NULLIF($4, ''), commit_sha),
			    error = $5, finished_at = now()
			WHERE id = $1
			RETURNING deployment_id`, runID, status, imageRef, commitSHA, failure).Scan(&deploymentID)
		if err != nil {
			return wrap("store: finish run", err)
		}

		deploymentStatus := DeploymentRunning
		if status != RunSucceeded {
			deploymentStatus = DeploymentFailed
		}

		if _, err := tx.Exec(ctx,
			`UPDATE deployments SET status = $2, current_run_id = $3 WHERE id = $1`,
			deploymentID, deploymentStatus, runID); err != nil {
			return wrap("store: finish run", err)
		}

		// Wake anyone waiting on this run so the UI settles immediately rather
		// than on the next poll.
		_, err = tx.Exec(ctx, `SELECT pg_notify($1, $2)`, RunChannel, runID)
		return wrap("store: notify run", err)
	})
}

func (s *Store) RunByID(ctx context.Context, id string) (*Run, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+runColumns+` FROM deployment_runs WHERE id = $1`, id)
	if err != nil {
		return nil, wrap("store: run by id", err)
	}
	run, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Run])
	if err != nil {
		return nil, wrap("store: run by id", err)
	}
	return &run, nil
}

func (s *Store) ListRuns(ctx context.Context, deploymentID string, limit int) ([]Run, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+runColumns+` FROM deployment_runs
		WHERE deployment_id = $1 ORDER BY number DESC LIMIT $2`, deploymentID, limit)
	if err != nil {
		return nil, wrap("store: list runs", err)
	}
	runs, err := pgx.CollectRows(rows, pgx.RowToStructByName[Run])
	return runs, wrap("store: list runs", err)
}

// LastSuccessfulRun finds the run to roll back to.
func (s *Store) LastSuccessfulRun(ctx context.Context, deploymentID, excludeRunID string) (*Run, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+runColumns+` FROM deployment_runs
		WHERE deployment_id = $1 AND status = 'succeeded'
		  AND image_ref <> '' AND ($2 = '' OR id <> $2::uuid)
		ORDER BY number DESC LIMIT 1`, deploymentID, excludeRunID)
	if err != nil {
		return nil, wrap("store: last successful run", err)
	}
	run, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Run])
	if err != nil {
		return nil, wrap("store: last successful run", err)
	}
	return &run, nil
}

func (s *Store) CancelRun(ctx context.Context, runID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE deployment_runs
		SET status = 'cancelled', finished_at = now()
		WHERE id = $1 AND status = 'queued'`, runID)
	if err != nil {
		return wrap("store: cancel run", err)
	}
	if tag.RowsAffected() == 0 {
		// Already started or already finished. Cancelling a running deploy
		// mid-build would leave the server in an unknown state, so it is not
		// offered.
		return ErrNotFound
	}
	return nil
}

// --- run logs ------------------------------------------------------------

type LogEntry struct {
	Seq    int64     `db:"seq"    json:"seq"`
	Stream string    `db:"stream" json:"stream"`
	Line   string    `db:"line"   json:"line"`
	TS     time.Time `db:"ts"     json:"ts"`
}

// AppendLogs persists a batch of output lines.
//
// Batching matters: a docker build emits thousands of lines, and one INSERT
// per line would make the database the bottleneck in the build.
func (s *Store) AppendLogs(ctx context.Context, runID string, startSeq int64, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for i, entry := range entries {
		batch.Queue(`
			INSERT INTO deployment_logs (run_id, seq, stream, line)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (run_id, seq) DO NOTHING`,
			runID, startSeq+int64(i), entry.Stream, entry.Line)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()
	for range entries {
		if _, err := results.Exec(); err != nil {
			return wrap("store: append logs", err)
		}
	}
	return nil
}

// ListLogs replays stored output, so a client that reloads mid-build sees
// everything from the start before attaching to the live stream.
func (s *Store) ListLogs(ctx context.Context, runID string, afterSeq int64, limit int) ([]LogEntry, error) {
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	rows, err := s.pool.Query(ctx, `
		SELECT seq, stream, line, ts FROM deployment_logs
		WHERE run_id = $1 AND seq > $2
		ORDER BY seq LIMIT $3`, runID, afterSeq, limit)
	if err != nil {
		return nil, wrap("store: list logs", err)
	}
	entries, err := pgx.CollectRows(rows, pgx.RowToStructByName[LogEntry])
	return entries, wrap("store: list logs", err)
}

// PruneRunHistory keeps the most recent runs per deployment and deletes the
// rest, so log volume does not grow without bound.
func (s *Store) PruneRunHistory(ctx context.Context, keepPerDeployment int) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM deployment_runs r
		WHERE r.status <> 'running'
		  AND r.id NOT IN (SELECT current_run_id FROM deployments WHERE current_run_id IS NOT NULL)
		  AND r.number <= (
			SELECT max(number) - $1 FROM deployment_runs WHERE deployment_id = r.deployment_id
		)`, keepPerDeployment)
	if err != nil {
		return 0, wrap("store: prune runs", err)
	}
	return tag.RowsAffected(), nil
}
