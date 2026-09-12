package store_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/db"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/secrets"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// These tests need a real Postgres, because the behaviour under test lives in
// SQL and a transaction. Each run creates a throwaway database and drops it
// afterwards, so it never touches development data.
//
//	DOCKDEPLOY_TEST_DATABASE_URL='postgres://dockdeploy:<pw>@localhost:5432/dockdeploy?sslmode=disable' \
//	  go test ./internal/store/ -v

func testStore(t *testing.T) (*store.Store, store.Sealer) {
	t.Helper()

	adminURL := os.Getenv("DOCKDEPLOY_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("set DOCKDEPLOY_TEST_DATABASE_URL to run store tests")
	}

	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)

	admin, err := db.Connect(ctx, adminURL, log)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	name := fmt.Sprintf("dockdeploy_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatalf("create test database: %v", err)
	}

	testURL := replaceDatabase(adminURL, name)
	pool, err := db.Connect(ctx, testURL, log)
	if err != nil {
		admin.Close()
		t.Fatalf("connect to test database: %v", err)
	}
	if err := db.Migrate(ctx, pool, log); err != nil {
		pool.Close()
		admin.Close()
		t.Fatalf("migrate: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		// Terminate stragglers first; Postgres refuses to drop a database
		// that still has a connection open.
		dropCtx := context.WithoutCancel(ctx)
		_, _ = admin.Exec(dropCtx,
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
		if _, err := admin.Exec(dropCtx, "DROP DATABASE IF EXISTS "+name); err != nil {
			t.Logf("could not drop %s: %v", name, err)
		}
		admin.Close()
	})

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("read key: %v", err)
	}
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatalf("sealer: %v", err)
	}

	return store.New(pool), sealer
}

func replaceDatabase(url, name string) string {
	// postgres://user:pw@host:port/dbname?params
	slash := strings.LastIndex(url, "/")
	if slash < 0 {
		return url
	}
	rest := url[slash+1:]
	if q := strings.Index(rest, "?"); q >= 0 {
		return url[:slash+1] + name + rest[q:]
	}
	return url[:slash+1] + name
}

// seedDeployment creates the minimum rows a deployment needs.
func seedDeployment(t *testing.T, db *store.Store, sealer store.Sealer) *store.Deployment {
	t.Helper()
	ctx := t.Context()

	server, err := db.CreateServer(ctx, sealer, store.NewServer{
		Name: "test-server", Host: "example.invalid", Port: 22, Username: "root",
		AuthMethod: sshx.AuthPassword, Password: "irrelevant",
		HostKeyFingerprint: "SHA256:test",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	deployment, err := db.CreateDeployment(ctx, sealer, store.NewDeployment{
		ServerID: server.ID, Name: "Test App", Slug: "test-app",
		SourceType: store.SourceImage, ImageRef: "nginx:alpine",
		BuildStrategy: store.BuildRemote, Workdir: "apps/test-app",
		ContainerPort: 80, WebhookSecret: "webhook-secret-value",
	})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	return deployment
}

// A secret submitted with an empty value means "keep the stored one".
//
// This is the shape every save from the dashboard takes: the API never returns
// a secret value, so a client editing an unrelated variable has nothing to
// send back for it. Getting this wrong encrypts an empty string over the real
// credential, and the next deploy starts the application with a blank
// password -- silently, and with no way to recover the old value.
func TestSetDeploymentEnvKeepsUntouchedSecrets(t *testing.T) {
	db, sealer := testStore(t)
	deployment := seedDeployment(t, db, sealer)
	ctx := t.Context()

	const secretValue = "the-real-database-password"

	if err := db.SetDeploymentEnv(ctx, sealer, deployment.ID, []store.EnvVar{
		{Key: "LOG_LEVEL", Value: "debug", IsSecret: false},
		{Key: "DB_PASSWORD", Value: secretValue, IsSecret: true},
	}); err != nil {
		t.Fatalf("initial set: %v", err)
	}

	// Save again, changing only the plain variable and sending the secret back
	// empty, exactly as the dashboard does.
	if err := db.SetDeploymentEnv(ctx, sealer, deployment.ID, []store.EnvVar{
		{Key: "LOG_LEVEL", Value: "info", IsSecret: false},
		{Key: "DB_PASSWORD", Value: "", IsSecret: true},
	}); err != nil {
		t.Fatalf("second set: %v", err)
	}

	resolved, err := db.ResolveDeploymentEnv(ctx, sealer, deployment.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if got := resolved["DB_PASSWORD"]; got != secretValue {
		t.Errorf("DB_PASSWORD = %q, want the original secret preserved", got)
	}
	if got := resolved["LOG_LEVEL"]; got != "info" {
		t.Errorf("LOG_LEVEL = %q, want it updated to info", got)
	}
}

func TestSetDeploymentEnvReplacesSecretWhenGivenOne(t *testing.T) {
	db, sealer := testStore(t)
	deployment := seedDeployment(t, db, sealer)
	ctx := t.Context()

	if err := db.SetDeploymentEnv(ctx, sealer, deployment.ID, []store.EnvVar{
		{Key: "TOKEN", Value: "first-value-here", IsSecret: true},
	}); err != nil {
		t.Fatalf("initial set: %v", err)
	}
	if err := db.SetDeploymentEnv(ctx, sealer, deployment.ID, []store.EnvVar{
		{Key: "TOKEN", Value: "second-value-here", IsSecret: true},
	}); err != nil {
		t.Fatalf("second set: %v", err)
	}

	resolved, err := db.ResolveDeploymentEnv(ctx, sealer, deployment.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := resolved["TOKEN"]; got != "second-value-here" {
		t.Errorf("TOKEN = %q, want the new value", got)
	}
}

// Removing a variable must delete its secret too, or the secrets table
// accumulates rows nothing references and nothing can clean up.
func TestSetDeploymentEnvDropsRemovedSecrets(t *testing.T) {
	db, sealer := testStore(t)
	deployment := seedDeployment(t, db, sealer)
	ctx := t.Context()

	if err := db.SetDeploymentEnv(ctx, sealer, deployment.ID, []store.EnvVar{
		{Key: "KEEP_ME", Value: "kept-secret-value", IsSecret: true},
		{Key: "DROP_ME", Value: "dropped-secret-value", IsSecret: true},
	}); err != nil {
		t.Fatalf("initial set: %v", err)
	}

	before := countSecrets(t, db)

	if err := db.SetDeploymentEnv(ctx, sealer, deployment.ID, []store.EnvVar{
		{Key: "KEEP_ME", Value: "", IsSecret: true},
	}); err != nil {
		t.Fatalf("second set: %v", err)
	}

	if after := countSecrets(t, db); after != before-1 {
		t.Errorf("secret count = %d, want %d (one removed variable should drop one secret)", after, before-1)
	}

	resolved, err := db.ResolveDeploymentEnv(ctx, sealer, deployment.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := resolved["KEEP_ME"]; got != "kept-secret-value" {
		t.Errorf("KEEP_ME = %q, want it preserved", got)
	}
	if _, present := resolved["DROP_ME"]; present {
		t.Error("DROP_ME is still present after removal")
	}
}

// Secret values must never come back from the read path, even to their owner.
func TestListDeploymentEnvMasksSecrets(t *testing.T) {
	db, sealer := testStore(t)
	deployment := seedDeployment(t, db, sealer)
	ctx := t.Context()

	if err := db.SetDeploymentEnv(ctx, sealer, deployment.ID, []store.EnvVar{
		{Key: "PLAIN", Value: "visible", IsSecret: false},
		{Key: "HIDDEN", Value: "must-not-be-returned", IsSecret: true},
	}); err != nil {
		t.Fatalf("set: %v", err)
	}

	listed, err := db.ListDeploymentEnv(ctx, deployment.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	for _, v := range listed {
		switch v.Key {
		case "PLAIN":
			if v.Value != "visible" {
				t.Errorf("PLAIN = %q, want visible", v.Value)
			}
		case "HIDDEN":
			if v.Value != "" {
				t.Errorf("HIDDEN leaked its value: %q", v.Value)
			}
			if !v.IsSecret {
				t.Error("HIDDEN is not flagged as a secret")
			}
		}
	}
}

func countSecrets(t *testing.T, db *store.Store) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(t.Context(), `SELECT count(*) FROM secrets`).Scan(&n); err != nil {
		t.Fatalf("count secrets: %v", err)
	}
	return n
}
