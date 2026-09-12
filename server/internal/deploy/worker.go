package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

const (
	// staleAfter is how long a claimed run may go without finishing before it
	// is assumed dead. Generous, because a large image build is legitimately
	// slow and killing a working deploy is worse than a late cleanup.
	staleAfter = 2 * time.Hour
	// sweepEvery bounds how often stale runs are reclaimed. It also acts as a
	// safety net if a NOTIFY is ever missed.
	sweepEvery = time.Minute
	// keepRuns is how much history each deployment retains.
	keepRuns = 30
)

// Worker drains the run queue.
//
// Deploys are minutes long, so they cannot ride on an HTTP request: the user
// would have to keep the tab open and a refresh would orphan the build. Runs
// are rows, a worker claims them, and the browser watches from the side.
type Worker struct {
	store       *store.Store
	engine      *Engine
	log         *slog.Logger
	concurrency int
	id          string
}

func NewWorker(db *store.Store, engine *Engine, log *slog.Logger, concurrency int) *Worker {
	if concurrency < 1 {
		concurrency = 2
	}

	// Identifies which process holds a run, so a stuck deploy can be traced
	// back to a specific container.
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}

	return &Worker{
		store:       db,
		engine:      engine,
		log:         log,
		concurrency: concurrency,
		id:          fmt.Sprintf("%s/%d", host, os.Getpid()),
	}
}

// Run drains the queue until the context is cancelled.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info("deploy worker started", "worker", w.id, "concurrency", w.concurrency)

	// Anything left running from a previous process is orphaned by definition:
	// this process holds no connection to it and nothing will ever finish it.
	if released, err := w.store.ReleaseStaleRuns(ctx, 0); err != nil {
		w.log.Warn("release runs from a previous process", "error", err)
	} else if released > 0 {
		w.log.Info("released orphaned runs from a previous process", "count", released)
	}

	// wake carries a nudge from either a NOTIFY or the periodic sweep.
	wake := make(chan struct{}, 1)

	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		w.listen(ctx, wake)
	}()

	group.Add(1)
	go func() {
		defer group.Done()
		w.sweep(ctx, wake)
	}()

	// A bounded pool: several deploys may run at once, but not so many that
	// they starve the machine or open an SSH connection per queued run.
	slots := make(chan struct{}, w.concurrency)
	var running sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			w.log.Info("deploy worker draining", "worker", w.id)
			running.Wait()
			group.Wait()
			return
		default:
		}

		run, err := w.store.ClaimRun(ctx, w.id)
		switch {
		case errors.Is(err, store.ErrNoRuns):
			// Nothing to do. Wait to be woken rather than spinning.
			select {
			case <-ctx.Done():
			case <-wake:
			}
			continue
		case err != nil:
			if ctx.Err() != nil {
				continue
			}
			w.log.Error("claim run", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			continue
		}

		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			continue
		}

		running.Add(1)
		go func(run *store.Run) {
			defer running.Done()
			defer func() { <-slots }()

			w.log.Info("deploy started", "run", run.ID, "deployment", run.DeploymentID, "number", run.Number)
			w.engine.Execute(ctx, run)
			w.log.Info("deploy finished", "run", run.ID, "number", run.Number)

			// A finished run frees a slot, and there may be more queued.
			select {
			case wake <- struct{}{}:
			default:
			}
		}(run)
	}
}

// listen waits for pg_notify and nudges the claim loop.
//
// A dedicated connection is required: LISTEN is session state, and a pooled
// connection handed to someone else would take the subscription with it.
func (w *Worker) listen(ctx context.Context, wake chan<- struct{}) {
	for ctx.Err() == nil {
		if err := w.listenOnce(ctx, wake); err != nil && ctx.Err() == nil {
			w.log.Warn("run notifications interrupted, reconnecting", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (w *Worker) listenOnce(ctx context.Context, wake chan<- struct{}) error {
	conn, err := w.store.Pool().Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire listener connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+store.RunChannel); err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return err
		}
		select {
		case wake <- struct{}{}:
		default:
			// A nudge is already pending; one is as good as many.
		}
	}
}

// sweep reclaims dead runs and prunes history on a timer.
func (w *Worker) sweep(ctx context.Context, wake chan<- struct{}) {
	ticker := time.NewTicker(sweepEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if released, err := w.store.ReleaseStaleRuns(ctx, staleAfter); err != nil {
			if ctx.Err() == nil {
				w.log.Warn("release stale runs", "error", err)
			}
		} else if released > 0 {
			w.log.Warn("failed runs whose worker stopped responding", "count", released)
		}

		if pruned, err := w.store.PruneRunHistory(ctx, keepRuns); err != nil {
			if ctx.Err() == nil {
				w.log.Warn("prune run history", "error", err)
			}
		} else if pruned > 0 {
			w.log.Debug("pruned old runs", "count", pruned)
		}

		// Also acts as a backstop: if a NOTIFY were ever missed, the queue
		// still drains within one sweep interval.
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
