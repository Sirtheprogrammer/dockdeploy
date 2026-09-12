package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// AuditEntry records one mutating request. Servers and deployments are shared,
// destructive surfaces, so the log is written for every write regardless of
// whether it succeeded.
type AuditEntry struct {
	ID           int64           `db:"id"            json:"id"`
	UserID       *string         `db:"user_id"       json:"user_id"`
	ActorEmail   string          `db:"actor_email"   json:"actor_email"`
	Action       string          `db:"action"        json:"action"`
	ResourceType string          `db:"resource_type" json:"resource_type"`
	ResourceID   string          `db:"resource_id"   json:"resource_id"`
	Status       int             `db:"status"        json:"status"`
	IP           string          `db:"ip"            json:"ip"`
	Meta         json.RawMessage `db:"meta"          json:"meta"`
	CreatedAt    time.Time       `db:"created_at"    json:"created_at"`
}

const auditColumns = `id, user_id, actor_email, action, resource_type, resource_id, status, ip, meta, created_at`

// WriteAudit appends an entry. Callers pass a nil userID for actions taken
// before authentication, such as a failed sign-in.
func (s *Store) WriteAudit(ctx context.Context, e AuditEntry) error {
	if s.sqlite != nil {
		return s.sqlite.WriteAudit(ctx, e)
	}
	meta := e.Meta
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (user_id, actor_email, action, resource_type, resource_id, status, ip, meta)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.UserID, e.ActorEmail, e.Action, e.ResourceType, e.ResourceID, e.Status, e.IP, meta)
	return wrap("store: write audit", err)
}

type AuditFilter struct {
	UserID       string
	ResourceType string
	ResourceID   string
	Limit        int
	Before       *time.Time
}

// ListAudit returns entries newest first, paginated by timestamp so new writes
// do not shift the pages under a reader.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEntry, error) {
	if s.sqlite != nil {
		return s.sqlite.ListAudit(ctx, f)
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+auditColumns+` FROM audit_log
		WHERE ($1 = '' OR user_id = $1::uuid)
		  AND ($2 = '' OR resource_type = $2)
		  AND ($3 = '' OR resource_id = $3)
		  AND ($4::timestamptz IS NULL OR created_at < $4)
		ORDER BY created_at DESC, id DESC
		LIMIT $5`,
		f.UserID, f.ResourceType, f.ResourceID, f.Before, limit)
	if err != nil {
		return nil, wrap("store: list audit", err)
	}
	entries, err := pgx.CollectRows(rows, pgx.RowToStructByName[AuditEntry])
	return entries, wrap("store: list audit", err)
}

// PurgeOldAuditLogs removes audit entries older than the retention period.
func (s *Store) PurgeOldAuditLogs(ctx context.Context, olderThan time.Duration) (int64, error) {
	if s.sqlite != nil {
		return s.sqlite.PurgeOldAuditLogs(ctx, olderThan)
	}
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.pool.Exec(ctx, `DELETE FROM audit_log WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, wrap("store: purge audit logs", err)
	}
	return tag.RowsAffected(), nil
}

