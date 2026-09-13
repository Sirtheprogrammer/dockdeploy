package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type DomainSSLMode string

const (
	DomainSSLNone        DomainSSLMode = "none"
	DomainSSLLetsEncrypt DomainSSLMode = "letsencrypt"
)

func (m DomainSSLMode) Valid() bool {
	return m == DomainSSLNone || m == DomainSSLLetsEncrypt
}

type DomainStatus string

const (
	DomainStatusPending DomainStatus = "pending"
	DomainStatusActive  DomainStatus = "active"
	DomainStatusError   DomainStatus = "error"
)

func (s DomainStatus) Valid() bool {
	return s == DomainStatusPending || s == DomainStatusActive || s == DomainStatusError
}

type Domain struct {
	ID             string        `db:"id"              json:"id"`
	DeploymentID   *string       `db:"deployment_id"   json:"deployment_id"`
	ServerID       string        `db:"server_id"       json:"server_id"`
	Hostname       string        `db:"hostname"        json:"hostname"`
	UpstreamPort   int           `db:"upstream_port"   json:"upstream_port"`
	SSLMode        DomainSSLMode `db:"ssl_mode"        json:"ssl_mode"`
	WebSocket      bool          `db:"websocket"       json:"websocket"`
	ConfigRendered string        `db:"config_rendered" json:"config_rendered"`
	CertExpiresAt  *time.Time    `db:"cert_expires_at" json:"cert_expires_at"`
	Status         DomainStatus  `db:"status"          json:"status"`
	StatusMessage  string        `db:"status_message"  json:"status_message"`
	CreatedBy      *string       `db:"created_by"      json:"created_by"`
	CreatedAt      time.Time     `db:"created_at"      json:"created_at"`
	UpdatedAt      time.Time     `db:"updated_at"      json:"updated_at"`
}

const domainColumns = `id, deployment_id, server_id, hostname, upstream_port,
	ssl_mode, websocket, config_rendered, cert_expires_at, status, status_message,
	created_by, created_at, updated_at`

type NewDomain struct {
	DeploymentID   *string
	ServerID       string
	Hostname       string
	UpstreamPort   int
	SSLMode        DomainSSLMode
	WebSocket      bool
	ConfigRendered string
	Status         DomainStatus
	StatusMessage  string
	CreatedBy      string
}

func (s *Store) CreateDomain(ctx context.Context, in NewDomain) (*Domain, error) {
	if s.sqlite != nil {
		return s.sqlite.CreateDomain(ctx, in)
	}
	if in.SSLMode == "" {
		in.SSLMode = DomainSSLNone
	}
	if in.Status == "" {
		in.Status = DomainStatusPending
	}
	in.Hostname = strings.ToLower(strings.TrimSpace(in.Hostname))

	row := s.pool.QueryRow(ctx, `
		INSERT INTO domains (
			deployment_id, server_id, hostname, upstream_port,
			ssl_mode, websocket, config_rendered, status, status_message, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+domainColumns,
		in.DeploymentID, in.ServerID, in.Hostname, in.UpstreamPort,
		in.SSLMode, in.WebSocket, in.ConfigRendered, in.Status, in.StatusMessage, nullable(in.CreatedBy),
	)
	var d Domain
	if err := row.Scan(
		&d.ID, &d.DeploymentID, &d.ServerID, &d.Hostname, &d.UpstreamPort,
		&d.SSLMode, &d.WebSocket, &d.ConfigRendered, &d.CertExpiresAt, &d.Status,
		&d.StatusMessage, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, wrap("store: create domain", err)
	}
	return &d, nil
}


func (s *Store) DomainByID(ctx context.Context, id string) (*Domain, error) {
	if s.sqlite != nil {
		return s.sqlite.DomainByID(ctx, id)
	}
	row := s.pool.QueryRow(ctx, `SELECT `+domainColumns+` FROM domains WHERE id = $1`, id)
	var d Domain
	if err := row.Scan(
		&d.ID, &d.DeploymentID, &d.ServerID, &d.Hostname, &d.UpstreamPort,
		&d.SSLMode, &d.WebSocket, &d.ConfigRendered, &d.CertExpiresAt, &d.Status,
		&d.StatusMessage, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, wrap("store: domain by id", err)
	}
	return &d, nil
}

func (s *Store) DomainByHostname(ctx context.Context, hostname string) (*Domain, error) {
	if s.sqlite != nil {
		return s.sqlite.DomainByHostname(ctx, hostname)
	}
	row := s.pool.QueryRow(ctx, `SELECT `+domainColumns+` FROM domains WHERE hostname = $1`, strings.ToLower(strings.TrimSpace(hostname)))
	var d Domain
	if err := row.Scan(
		&d.ID, &d.DeploymentID, &d.ServerID, &d.Hostname, &d.UpstreamPort,
		&d.SSLMode, &d.WebSocket, &d.ConfigRendered, &d.CertExpiresAt, &d.Status,
		&d.StatusMessage, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, wrap("store: domain by hostname", err)
	}
	return &d, nil
}

type DomainFilter struct {
	ServerID     *string
	DeploymentID *string
}

func (s *Store) ListDomains(ctx context.Context, filter DomainFilter) ([]Domain, error) {
	if s.sqlite != nil {
		return s.sqlite.ListDomains(ctx, filter)
	}
	query := `SELECT ` + domainColumns + ` FROM domains WHERE 1=1`
	var args []any
	argIdx := 1

	if filter.ServerID != nil && *filter.ServerID != "" {
		query += fmt.Sprintf(` AND server_id = $%d`, argIdx)
		args = append(args, *filter.ServerID)
		argIdx++
	}
	if filter.DeploymentID != nil && *filter.DeploymentID != "" {
		query += fmt.Sprintf(` AND deployment_id = $%d`, argIdx)
		args = append(args, *filter.DeploymentID)
		argIdx++
	}
	query += ` ORDER BY hostname ASC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, wrap("store: list domains", err)
	}
	defer rows.Close()

	var domains []Domain
	for rows.Next() {
		var d Domain
		if err := rows.Scan(
			&d.ID, &d.DeploymentID, &d.ServerID, &d.Hostname, &d.UpstreamPort,
			&d.SSLMode, &d.WebSocket, &d.ConfigRendered, &d.CertExpiresAt, &d.Status,
			&d.StatusMessage, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, wrap("store: scan domain", err)
		}
		domains = append(domains, d)
	}
	return domains, rows.Err()
}

func (s *Store) ListDomainsByServer(ctx context.Context, serverID string) ([]Domain, error) {
	return s.ListDomains(ctx, DomainFilter{ServerID: &serverID})
}

func (s *Store) ListDomainsByDeployment(ctx context.Context, deploymentID string) ([]Domain, error) {
	return s.ListDomains(ctx, DomainFilter{DeploymentID: &deploymentID})
}

type UpdateDomainParams struct {
	DeploymentID   *string
	ConfigRendered *string
	CertExpiresAt  *time.Time
	Status         DomainStatus
	StatusMessage  string
}

func (s *Store) UpdateDomainStatus(ctx context.Context, id string, params UpdateDomainParams) error {
	if s.sqlite != nil {
		return s.sqlite.UpdateDomainStatus(ctx, id, params)
	}
	query := `UPDATE domains SET status = $1, status_message = $2`
	args := []any{params.Status, params.StatusMessage}
	idx := 3

	if params.DeploymentID != nil {
		query += fmt.Sprintf(`, deployment_id = $%d`, idx)
		args = append(args, nullable(*params.DeploymentID))
		idx++
	}
	if params.ConfigRendered != nil {
		query += fmt.Sprintf(`, config_rendered = $%d`, idx)
		args = append(args, *params.ConfigRendered)
		idx++
	}
	if params.CertExpiresAt != nil {
		query += fmt.Sprintf(`, cert_expires_at = $%d`, idx)
		args = append(args, *params.CertExpiresAt)
		idx++
	}

	query += fmt.Sprintf(` WHERE id = $%d`, idx)
	args = append(args, id)

	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return wrap("store: update domain status", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteDomain(ctx context.Context, id string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteDomain(ctx, id)
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM domains WHERE id = $1`, id)
	if err != nil {
		return wrap("store: delete domain", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
