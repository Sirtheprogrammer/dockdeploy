package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// SourceType is where a deployment gets its code or image.
type SourceType string

const (
	// SourceGitDockerfile builds an image from a Dockerfile in a repository.
	SourceGitDockerfile SourceType = "git_dockerfile"
	// SourceGitCompose runs a compose file from a repository.
	SourceGitCompose SourceType = "git_compose"
	// SourceRawCompose runs a compose file pasted into the dashboard.
	SourceRawCompose SourceType = "raw_compose"
	// SourceImage runs an existing image with no build step.
	SourceImage SourceType = "image"
)

func (s SourceType) Valid() bool {
	switch s {
	case SourceGitDockerfile, SourceGitCompose, SourceRawCompose, SourceImage:
		return true
	}
	return false
}

// NeedsGit reports whether a source requires cloning a repository.
func (s SourceType) NeedsGit() bool {
	return s == SourceGitDockerfile || s == SourceGitCompose
}

// UsesCompose reports whether the deployment is driven by a compose file
// rather than a single container.
func (s SourceType) UsesCompose() bool {
	return s == SourceGitCompose || s == SourceRawCompose
}

// BuildStrategy decides where the image is built.
type BuildStrategy string

const (
	// BuildRemote clones and builds on the target server. No registry needed,
	// but the build load lands on the machine running the app.
	BuildRemote BuildStrategy = "remote"
	// BuildRegistry builds on the controller, pushes, and the target pulls.
	// Keeps build load off the app server at the cost of registry setup.
	BuildRegistry BuildStrategy = "registry"
)

func (b BuildStrategy) Valid() bool { return b == BuildRemote || b == BuildRegistry }

type DeploymentStatus string

const (
	DeploymentNeverDeployed DeploymentStatus = "never_deployed"
	DeploymentDeploying     DeploymentStatus = "deploying"
	DeploymentRunning       DeploymentStatus = "running"
	DeploymentStopped       DeploymentStatus = "stopped"
	DeploymentFailed        DeploymentStatus = "failed"
)

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Terminal reports whether a run has finished, one way or another.
func (r RunStatus) Terminal() bool {
	return r == RunSucceeded || r == RunFailed || r == RunCancelled
}

type RunTrigger string

const (
	TriggerManual   RunTrigger = "manual"
	TriggerWebhook  RunTrigger = "webhook"
	TriggerAPI      RunTrigger = "api"
	TriggerRollback RunTrigger = "rollback"
)

type Deployment struct {
	ID       string `db:"id"        json:"id"`
	ServerID string `db:"server_id" json:"server_id"`
	Name     string `db:"name"      json:"name"`
	Slug     string `db:"slug"      json:"slug"`

	SourceType SourceType `db:"source_type" json:"source_type"`

	RepoURL         string  `db:"repo_url"          json:"repo_url"`
	GitRef          string  `db:"git_ref"           json:"git_ref"`
	GitCredentialID *string `db:"git_credential_id" json:"git_credential_id"`

	DockerfilePath string `db:"dockerfile_path" json:"dockerfile_path"`
	BuildContext   string `db:"build_context"   json:"build_context"`
	ComposePath    string `db:"compose_path"    json:"compose_path"`
	ComposeContent string `db:"compose_content" json:"compose_content"`
	ImageRef       string `db:"image_ref"       json:"image_ref"`

	BuildStrategy BuildStrategy `db:"build_strategy" json:"build_strategy"`
	RegistryID    *string       `db:"registry_id"    json:"registry_id"`
	ImageName     string        `db:"image_name"     json:"image_name"`

	Workdir       string `db:"workdir"        json:"workdir"`
	HostPort      *int   `db:"host_port"      json:"host_port"`
	ContainerPort int    `db:"container_port" json:"container_port"`

	WebhookSecretID *string `db:"webhook_secret_id" json:"-"`

	Status       DeploymentStatus `db:"status"         json:"status"`
	CurrentRunID *string          `db:"current_run_id" json:"current_run_id"`

	CreatedBy *string   `db:"created_by" json:"created_by"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

const deploymentColumns = `id, server_id, name, slug, source_type,
	repo_url, git_ref, git_credential_id,
	dockerfile_path, build_context, compose_path, compose_content, image_ref,
	build_strategy, registry_id, image_name,
	workdir, host_port, container_port, webhook_secret_id,
	status, current_run_id, created_by, created_at, updated_at`

// NewDeployment carries a deployment to be created, with the webhook secret
// still in plaintext.
type NewDeployment struct {
	ServerID   string
	Name       string
	Slug       string
	SourceType SourceType

	RepoURL         string
	GitRef          string
	GitCredentialID *string

	DockerfilePath string
	BuildContext   string
	ComposePath    string
	ComposeContent string
	ImageRef       string

	BuildStrategy BuildStrategy
	RegistryID    *string
	ImageName     string

	Workdir       string
	HostPort      *int
	ContainerPort int

	WebhookSecret string
	CreatedBy     string
}

func (s *Store) CreateDeployment(ctx context.Context, sealer Sealer, in NewDeployment) (*Deployment, error) {
	if s.sqlite != nil {
		return s.sqlite.CreateDeployment(ctx, sealer, in)
	}
	var deployment *Deployment

	err := s.tx(ctx, func(tx pgx.Tx) error {
		webhookID, err := insertSecret(ctx, tx, sealer, KindWebhookSecret, in.WebhookSecret)
		if err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
			INSERT INTO deployments (server_id, name, slug, source_type,
				repo_url, git_ref, git_credential_id,
				dockerfile_path, build_context, compose_path, compose_content, image_ref,
				build_strategy, registry_id, image_name,
				workdir, host_port, container_port, webhook_secret_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
			RETURNING `+deploymentColumns,
			in.ServerID, in.Name, in.Slug, in.SourceType,
			in.RepoURL, in.GitRef, in.GitCredentialID,
			in.DockerfilePath, in.BuildContext, in.ComposePath, in.ComposeContent, in.ImageRef,
			in.BuildStrategy, in.RegistryID, in.ImageName,
			in.Workdir, in.HostPort, in.ContainerPort, webhookID, nullable(in.CreatedBy))
		if err != nil {
			return wrap("store: create deployment", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Deployment])
		if err != nil {
			return wrap("store: create deployment", err)
		}
		deployment = &created
		return nil
	})

	return deployment, err
}

// ListDeployments returns deployments on servers the user can reach. The
// visibility rule lives in SQL for the same reason it does for servers: a
// handler that forgot it would leak another team's applications.
func (s *Store) ListDeployments(ctx context.Context, userID string, all bool) ([]Deployment, error) {
	if s.sqlite != nil {
		return s.sqlite.ListDeployments(ctx, userID, all)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+deploymentColumns+` FROM deployments
		WHERE $1 OR server_id IN (
			SELECT server_id FROM server_members WHERE user_id = $2::uuid
		)
		ORDER BY name`, all, userID)
	if err != nil {
		return nil, wrap("store: list deployments", err)
	}
	deployments, err := pgx.CollectRows(rows, pgx.RowToStructByName[Deployment])
	return deployments, wrap("store: list deployments", err)
}

func (s *Store) DeploymentByID(ctx context.Context, id string) (*Deployment, error) {
	if s.sqlite != nil {
		return s.sqlite.DeploymentByID(ctx, id)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+deploymentColumns+` FROM deployments WHERE id = $1`, id)
	if err != nil {
		return nil, wrap("store: deployment by id", err)
	}
	deployment, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Deployment])
	if err != nil {
		return nil, wrap("store: deployment by id", err)
	}
	return &deployment, nil
}

func (s *Store) UpdateDeploymentStatus(ctx context.Context, id string, status DeploymentStatus, runID *string) error {
	if s.sqlite != nil {
		return s.sqlite.UpdateDeploymentStatus(ctx, id, status, runID)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE deployments
		SET status = $2, current_run_id = COALESCE($3::uuid, current_run_id)
		WHERE id = $1`, id, status, runID)
	return wrap("store: update deployment status", err)
}

func (s *Store) SetDeploymentPort(ctx context.Context, id string, port int) error {
	if s.sqlite != nil {
		return s.sqlite.SetDeploymentPort(ctx, id, port)
	}
	_, err := s.pool.Exec(ctx, `UPDATE deployments SET host_port = $2 WHERE id = $1`, id, port)
	return wrap("store: set deployment port", err)
}

func (s *Store) DeleteDeployment(ctx context.Context, id string) error {
	if s.sqlite != nil {
		return s.sqlite.DeleteDeployment(ctx, id)
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		var webhookID *string
		var envSecrets []*string
		if err := tx.QueryRow(ctx,
			`SELECT webhook_secret_id FROM deployments WHERE id = $1`, id).Scan(&webhookID); err != nil {
			return wrap("store: delete deployment", err)
		}
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(array_agg(secret_id) FILTER (WHERE secret_id IS NOT NULL), '{}')
			 FROM deployment_env WHERE deployment_id = $1`, id).Scan(&envSecrets); err != nil {
			return wrap("store: delete deployment", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM deployments WHERE id = $1`, id); err != nil {
			return wrap("store: delete deployment", err)
		}

		// Secrets are referenced by the rows just removed, so they can only be
		// cleaned up afterwards.
		for _, secretID := range append(envSecrets, webhookID) {
			if secretID == nil {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM secrets WHERE id = $1`, *secretID); err != nil {
				return wrap("store: delete deployment secret", err)
			}
		}
		return nil
	})
}

// AllocateHostPort reserves a free loopback port for a deployment.
//
// Two sources are consulted, and both are needed. The database knows what this
// platform has handed out. inUse comes from the server itself and covers what
// the database cannot know: deleting a deployment removes its record but
// leaves its container running and still holding the port, and containers the
// platform never created may sit in the range too.
func (s *Store) AllocateHostPort(ctx context.Context, serverID string, min, max int, inUse []int) (int, error) {
	if s.sqlite != nil {
		return s.sqlite.AllocateHostPort(ctx, serverID, min, max, inUse)
	}
	var port *int
	err := s.pool.QueryRow(ctx, `
		SELECT candidate FROM generate_series($2::int, $3::int) AS candidate
		WHERE candidate NOT IN (
			SELECT host_port FROM deployments
			WHERE server_id = $1 AND host_port IS NOT NULL
		)
		AND NOT (candidate = ANY($4::int[]))
		ORDER BY candidate
		LIMIT 1`, serverID, min, max, inUse).Scan(&port)
	if err != nil {
		return 0, wrap("store: allocate port", err)
	}
	if port == nil {
		return 0, fmt.Errorf("store: every port between %d and %d is already in use on this server", min, max)
	}
	return *port, nil
}

// --- environment ---------------------------------------------------------

type EnvVar struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

// SetDeploymentEnv replaces the whole environment in one transaction.
//
// Replacing wholesale rather than patching keys means the stored set always
// matches what the user last saw, with no way to leave a removed variable
// behind.
//
// One case is not a replacement: a secret submitted with an empty value means
// "keep the stored one". The API never returns a secret value, so a client
// editing an unrelated variable has nothing to send back for it. Without this,
// saving the form would encrypt an empty string over the real credential and
// the next deploy would start the application with a blank password.
func (s *Store) SetDeploymentEnv(ctx context.Context, sealer Sealer, deploymentID string, vars []EnvVar) error {
	if s.sqlite != nil {
		return s.sqlite.SetDeploymentEnv(ctx, sealer, deploymentID, vars)
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		// Existing secrets by key, so the ones being kept can be carried over
		// rather than deleted and rewritten.
		existing := map[string]string{}
		rows, err := tx.Query(ctx, `
			SELECT key, secret_id FROM deployment_env
			WHERE deployment_id = $1 AND secret_id IS NOT NULL`, deploymentID)
		if err != nil {
			return wrap("store: read env secrets", err)
		}
		for rows.Next() {
			var key, secretID string
			if err := rows.Scan(&key, &secretID); err != nil {
				rows.Close()
				return wrap("store: read env secrets", err)
			}
			existing[key] = secretID
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return wrap("store: read env secrets", err)
		}

		keep := map[string]string{}
		for _, v := range vars {
			if v.IsSecret && v.Value == "" {
				if secretID, ok := existing[v.Key]; ok {
					keep[v.Key] = secretID
				}
			}
		}

		if _, err := tx.Exec(ctx,
			`DELETE FROM deployment_env WHERE deployment_id = $1`, deploymentID); err != nil {
			return wrap("store: clear env", err)
		}

		// Only secrets that are genuinely going away are deleted. Deleting a
		// kept one first and re-inserting it would lose the value.
		for key, secretID := range existing {
			if _, kept := keep[key]; kept {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM secrets WHERE id = $1`, secretID); err != nil {
				return wrap("store: delete env secret", err)
			}
		}

		for _, v := range vars {
			if !v.IsSecret {
				if _, err := tx.Exec(ctx, `
					INSERT INTO deployment_env (deployment_id, key, value, is_secret)
					VALUES ($1, $2, $3, false)`, deploymentID, v.Key, v.Value); err != nil {
					return wrap("store: insert env", err)
				}
				continue
			}

			secretID, kept := keep[v.Key]
			if !kept {
				created, err := insertSecret(ctx, tx, sealer, KindDeploymentEnv, v.Value)
				if err != nil {
					return err
				}
				secretID = *created
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO deployment_env (deployment_id, key, secret_id, is_secret)
				VALUES ($1, $2, $3, true)`, deploymentID, v.Key, secretID); err != nil {
				return wrap("store: insert env", err)
			}
		}
		return nil
	})
}

// ListDeploymentEnv returns variables with secret values masked, for display.
func (s *Store) ListDeploymentEnv(ctx context.Context, deploymentID string) ([]EnvVar, error) {
	if s.sqlite != nil {
		return s.sqlite.ListDeploymentEnv(ctx, deploymentID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT key, value, is_secret FROM deployment_env
		WHERE deployment_id = $1 ORDER BY key`, deploymentID)
	if err != nil {
		return nil, wrap("store: list env", err)
	}
	defer rows.Close()

	var out []EnvVar
	for rows.Next() {
		var v EnvVar
		if err := rows.Scan(&v.Key, &v.Value, &v.IsSecret); err != nil {
			return nil, wrap("store: list env", err)
		}
		// Secret values are never returned, not even to the owner. The only
		// way to change one is to set a new value.
		if v.IsSecret {
			v.Value = ""
		}
		out = append(out, v)
	}
	return out, wrap("store: list env", rows.Err())
}

// ResolveDeploymentEnv decrypts the environment for writing to the server.
// The result is written to a 0600 .env file and must never be logged.
func (s *Store) ResolveDeploymentEnv(ctx context.Context, sealer Sealer, deploymentID string) (map[string]string, error) {
	if s.sqlite != nil {
		return s.sqlite.ResolveDeploymentEnv(ctx, sealer, deploymentID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT key, value, secret_id, is_secret FROM deployment_env
		WHERE deployment_id = $1 ORDER BY key`, deploymentID)
	if err != nil {
		return nil, wrap("store: resolve env", err)
	}
	defer rows.Close()

	type entry struct {
		key      string
		value    string
		secretID *string
		isSecret bool
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.key, &e.value, &e.secretID, &e.isSecret); err != nil {
			return nil, wrap("store: resolve env", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("store: resolve env", err)
	}

	// Decrypt after the rows are closed: openSecret borrows another connection
	// from the pool, and holding an open cursor while doing so can deadlock a
	// small pool.
	resolved := make(map[string]string, len(entries))
	for _, e := range entries {
		if !e.isSecret {
			resolved[e.key] = e.value
			continue
		}
		plaintext, err := s.openSecret(ctx, sealer, e.secretID)
		if err != nil {
			return nil, err
		}
		resolved[e.key] = plaintext
	}
	return resolved, nil
}

// SecretEnvValues returns just the sensitive values, so log output can be
// scrubbed of them before it is persisted or streamed.
func (s *Store) SecretEnvValues(ctx context.Context, sealer Sealer, deploymentID string) ([]string, error) {
	if s.sqlite != nil {
		return s.sqlite.SecretEnvValues(ctx, sealer, deploymentID)
	}
	env, err := s.ResolveDeploymentEnv(ctx, sealer, deploymentID)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT key FROM deployment_env WHERE deployment_id = $1 AND is_secret`, deploymentID)
	if err != nil {
		return nil, wrap("store: secret env keys", err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, wrap("store: secret env keys", err)
		}
		if value := env[key]; value != "" {
			values = append(values, value)
		}
	}
	return values, wrap("store: secret env keys", rows.Err())
}

// DeploymentWebhookSecret decrypts the webhook secret for push-to-deploy triggers.
func (s *Store) DeploymentWebhookSecret(ctx context.Context, sealer Sealer, deployment *Deployment) (string, error) {
	if s.sqlite != nil {
		return s.sqlite.DeploymentWebhookSecret(ctx, sealer, deployment)
	}
	if deployment.WebhookSecretID == nil {
		return "", errors.New("deployment has no webhook secret")
	}
	return s.openSecret(ctx, sealer, deployment.WebhookSecretID)
}
