package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/db"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/gitx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/secrets"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
)

func setupTestSQLiteStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "store_test.db")

	sqlDB, err := db.ConnectSQLite(ctx, dbPath, log)
	if err != nil {
		t.Fatalf("ConnectSQLite failed: %v", err)
	}

	if err := db.MigrateSQLite(ctx, sqlDB, log); err != nil {
		sqlDB.Close()
		t.Fatalf("MigrateSQLite failed: %v", err)
	}

	st := NewSQLite(sqlDB)
	t.Cleanup(func() {
		st.Close()
	})

	return st, sqlDB
}

func testSealer(t *testing.T) Sealer {
	t.Helper()
	var key [32]byte
	_, _ = rand.Read(key[:])
	sealer, err := secrets.NewSealer(key[:])
	if err != nil {
		t.Fatalf("NewSealer failed: %v", err)
	}
	return sealer
}

func TestSQLiteStore_UsersAndAuth(t *testing.T) {
	st, _ := setupTestSQLiteStore(t)
	ctx := context.Background()

	// 1. Initial user count
	count, err := st.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 users, got %d", count)
	}

	// 2. Create first admin
	admin, err := st.CreateFirstAdmin(ctx, "Admin@Example.com", "Admin User", "hash123")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	if admin.Email != "admin@example.com" {
		t.Errorf("expected normalized email, got %s", admin.Email)
	}
	if admin.Role != auth.RoleAdmin {
		t.Errorf("expected role admin, got %s", admin.Role)
	}

	// 3. Second CreateFirstAdmin should fail with conflict
	_, err = st.CreateFirstAdmin(ctx, "other@example.com", "Other", "hash456")
	if err != ErrConflict {
		t.Errorf("expected ErrConflict on duplicate first admin, got %v", err)
	}

	// 4. Create regular user
	member, err := st.CreateUser(ctx, "member@example.com", "Member User", "hash789", auth.RoleMember)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if member.Role != auth.RoleMember {
		t.Errorf("expected role member, got %s", member.Role)
	}

	// 5. UserByID and UserByEmail
	byID, err := st.UserByID(ctx, member.ID)
	if err != nil || byID.Email != member.Email {
		t.Fatalf("UserByID: got %v, err %v", byID, err)
	}
	byEmail, err := st.UserByEmail(ctx, "MEMBER@example.com")
	if err != nil || byEmail.ID != member.ID {
		t.Fatalf("UserByEmail: got %v, err %v", byEmail, err)
	}

	// 6. ListUsers and CountAdmins
	users, err := st.ListUsers(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("ListUsers: got %d users, err %v", len(users), err)
	}
	adminCount, err := st.CountAdmins(ctx)
	if err != nil || adminCount != 1 {
		t.Fatalf("CountAdmins: got %d, err %v", adminCount, err)
	}

	// 7. UpdateUser
	updated, err := st.UpdateUser(ctx, member.ID, "Updated Name", auth.RoleAdmin, UserActive)
	if err != nil || updated.Name != "Updated Name" || updated.Role != auth.RoleAdmin {
		t.Fatalf("UpdateUser failed: %v", err)
	}

	// 8. Touch login
	if err := st.TouchUserLogin(ctx, member.ID); err != nil {
		t.Fatalf("TouchUserLogin: %v", err)
	}
}

func TestSQLiteStore_SessionsAndTokens(t *testing.T) {
	st, _ := setupTestSQLiteStore(t)
	ctx := context.Background()

	admin, err := st.CreateFirstAdmin(ctx, "admin@example.com", "Admin", "hash")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}

	// Sessions
	tokenHash := sha256.Sum256([]byte("session-token-1"))
	session, err := st.CreateSession(ctx, admin.ID, tokenHash[:], time.Now().Add(24*time.Hour), "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.UserID != admin.ID {
		t.Errorf("session user ID mismatch")
	}

	sUser, err := st.SessionByTokenHash(ctx, tokenHash[:])
	if err != nil || sUser.User.ID != admin.ID {
		t.Fatalf("SessionByTokenHash: %v", err)
	}

	if err := st.TouchSession(ctx, session.ID, time.Now().Add(48*time.Hour)); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	sessions, err := st.ListSessions(ctx, admin.ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions: got %d, err %v", len(sessions), err)
	}

	// API Tokens
	apiHash := sha256.Sum256([]byte("api-token-1"))
	apiToken, err := st.CreateAPIToken(ctx, admin.ID, "ci-token", apiHash[:], "dock_ci", nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	tokenUser, err := st.APITokenByHash(ctx, apiHash[:])
	if err != nil || tokenUser.User.ID != admin.ID || tokenUser.Token.Name != "ci-token" {
		t.Fatalf("APITokenByHash: %v", err)
	}

	if err := st.TouchAPIToken(ctx, apiToken.ID); err != nil {
		t.Fatalf("TouchAPIToken: %v", err)
	}

	tokens, err := st.ListAPITokens(ctx, admin.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("ListAPITokens: got %d, err %v", len(tokens), err)
	}

	if err := st.DeleteAPIToken(ctx, apiToken.ID, admin.ID); err != nil {
		t.Fatalf("DeleteAPIToken: %v", err)
	}
}

func TestSQLiteStore_ServersAndDeployments(t *testing.T) {
	st, _ := setupTestSQLiteStore(t)
	sealer := testSealer(t)
	ctx := context.Background()

	admin, err := st.CreateFirstAdmin(ctx, "admin@example.com", "Admin", "hash")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}

	// Create Server
	srv, err := st.CreateServer(ctx, sealer, NewServer{
		Name:               "prod-host",
		Host:               "192.168.1.10",
		Port:               22,
		Username:           "deploy",
		AuthMethod:         sshx.AuthPassword,
		Password:           "secretpass",
		HostKeyFingerprint: "SHA256:abc123fingerprint",
		CreatedBy:          admin.ID,
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if srv.Name != "prod-host" {
		t.Errorf("server name mismatch")
	}

	// ServerCredential decryption
	cred, err := st.ServerCredential(ctx, sealer, srv)
	if err != nil {
		t.Fatalf("ServerCredential: %v", err)
	}
	if cred.Credential.Password != "secretpass" {
		t.Errorf("credential password mismatch: got %s", cred.Credential.Password)
	}

	// Server status update
	err = st.UpdateServerStatus(ctx, srv.ID, ServerOnline, "reachable", &sshx.Capabilities{DockerVersion: "24.0.5"})
	if err != nil {
		t.Fatalf("UpdateServerStatus: %v", err)
	}

	// Create Deployment
	port := 8080
	dep, err := st.CreateDeployment(ctx, sealer, NewDeployment{
		ServerID:       srv.ID,
		Name:           "my-web-app",
		Slug:           "my-web-app",
		SourceType:     SourceImage,
		BuildStrategy:  BuildRemote,
		ImageRef:       "nginx:alpine",
		ContainerPort:  80,
		HostPort:       &port,
		WebhookSecret:  "hooksecret123",
		CreatedBy:      admin.ID,
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if dep.Name != "my-web-app" {
		t.Errorf("deployment name mismatch")
	}

	// Webhook secret recovery
	whSecret, err := st.DeploymentWebhookSecret(ctx, sealer, dep)
	if err != nil || whSecret != "hooksecret123" {
		t.Fatalf("DeploymentWebhookSecret: got %s, err %v", whSecret, err)
	}

	// Environment Variables
	err = st.SetDeploymentEnv(ctx, sealer, dep.ID, []EnvVar{
		{Key: "PORT", Value: "8080", IsSecret: false},
		{Key: "DB_PASS", Value: "supersecret", IsSecret: true},
	})
	if err != nil {
		t.Fatalf("SetDeploymentEnv: %v", err)
	}

	listedEnv, err := st.ListDeploymentEnv(ctx, dep.ID)
	if err != nil || len(listedEnv) != 2 {
		t.Fatalf("ListDeploymentEnv: got %d, err %v", len(listedEnv), err)
	}
	// Verify secret values masked on List
	for _, env := range listedEnv {
		if env.Key == "DB_PASS" && env.Value != "" {
			t.Errorf("expected secret env value to be masked, got %s", env.Value)
		}
	}

	resolvedEnv, err := st.ResolveDeploymentEnv(ctx, sealer, dep.ID)
	if err != nil || resolvedEnv["DB_PASS"] != "supersecret" {
		t.Fatalf("ResolveDeploymentEnv: DB_PASS = %s, err %v", resolvedEnv["DB_PASS"], err)
	}
}

func TestSQLiteStore_CredentialsAndRegistries(t *testing.T) {
	st, _ := setupTestSQLiteStore(t)
	sealer := testSealer(t)
	ctx := context.Background()

	// Registry
	reg, err := st.CreateRegistry(ctx, sealer, "ghcr", "ghcr.io", "bot", "token123", "")
	if err != nil {
		t.Fatalf("CreateRegistry: %v", err)
	}
	pw, err := st.RegistryPassword(ctx, sealer, reg)
	if err != nil || pw != "token123" {
		t.Fatalf("RegistryPassword: got %s, err %v", pw, err)
	}
	regs, err := st.ListRegistries(ctx)
	if err != nil || len(regs) != 1 {
		t.Fatalf("ListRegistries: got %d, err %v", len(regs), err)
	}
	if err := st.DeleteRegistry(ctx, reg.ID); err != nil {
		t.Fatalf("DeleteRegistry: %v", err)
	}

	// Git Credential
	gitCred, err := st.CreateGitCredential(ctx, sealer, "github-ci", gitx.KindToken, "git", "ghp_secret", "")
	if err != nil {
		t.Fatalf("CreateGitCredential: %v", err)
	}
	resolvedGit, err := st.ResolveGitCredential(ctx, sealer, &gitCred.ID)
	if err != nil || resolvedGit.Secret != "ghp_secret" {
		t.Fatalf("ResolveGitCredential: got %v, err %v", resolvedGit, err)
	}
	if err := st.DeleteGitCredential(ctx, gitCred.ID); err != nil {
		t.Fatalf("DeleteGitCredential: %v", err)
	}
}

func TestSQLiteStore_RunsAndPubSub(t *testing.T) {
	st, _ := setupTestSQLiteStore(t)
	sealer := testSealer(t)
	ctx := context.Background()

	srv, err := st.CreateServer(ctx, sealer, NewServer{
		Name:               "server-1",
		Host:               "localhost",
		Port:               22,
		Username:           "root",
		AuthMethod:         sshx.AuthPassword,
		Password:           "pw",
		HostKeyFingerprint: "SHA256:fingerprint",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	dep, err := st.CreateDeployment(ctx, sealer, NewDeployment{
		ServerID:      srv.ID,
		Name:          "app-1",
		Slug:          "app-1",
		SourceType:    SourceImage,
		BuildStrategy: BuildRemote,
		ImageRef:      "alpine:latest",
		ContainerPort: 80,
		WebhookSecret: "sec",
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}

	// Subscribe to runs
	ch, cancel, err := st.SubscribeRuns(ctx)
	if err != nil {
		t.Fatalf("SubscribeRuns: %v", err)
	}
	defer cancel()

	// Enqueue run
	run, err := st.EnqueueRunWithParams(ctx, EnqueueRunParams{
		DeploymentID: dep.ID,
		Trigger:      TriggerManual,
	})
	if err != nil {
		t.Fatalf("EnqueueRunWithParams: %v", err)
	}
	if run.Status != RunQueued {
		t.Errorf("expected status queued, got %s", run.Status)
	}

	// Verify pubsub notified
	select {
	case <-ch:
		// success
	case <-time.After(1 * time.Second):
		t.Errorf("timeout waiting for pubsub notification on EnqueueRun")
	}

	// Claim run
	claimed, err := st.ClaimRun(ctx, "worker-1")
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	if claimed.ID != run.ID {
		t.Errorf("claimed run ID mismatch: got %s, want %s", claimed.ID, run.ID)
	}
	if claimed.Status != RunRunning {
		t.Errorf("expected claimed run to be running, got %s", claimed.Status)
	}

	// Append and list logs
	err = st.AppendLogs(ctx, run.ID, 0, []LogEntry{
		{Stream: "stdout", Line: "Step 1/2: Preparing build"},
		{Stream: "stdout", Line: "Step 2/2: Finished"},
	})
	if err != nil {
		t.Fatalf("AppendLogs: %v", err)
	}

	logs, err := st.ListLogs(ctx, run.ID, -1, 100)
	if err != nil || len(logs) != 2 {
		t.Fatalf("ListLogs: got %d logs, err %v", len(logs), err)
	}
	if logs[0].Line != "Step 1/2: Preparing build" {
		t.Errorf("log line mismatch")
	}

	// Finish run
	if err := st.FinishRun(ctx, run.ID, RunSucceeded, "alpine:latest", "sha123", ""); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	finished, err := st.RunByID(ctx, run.ID)
	if err != nil || finished.Status != RunSucceeded {
		t.Fatalf("RunByID after finish: %v", err)
	}
}

func TestSQLiteStore_Domains(t *testing.T) {
	st, _ := setupTestSQLiteStore(t)
	sealer := testSealer(t)
	ctx := context.Background()

	srv, err := st.CreateServer(ctx, sealer, NewServer{
		Name:               "server-1",
		Host:               "localhost",
		Port:               22,
		Username:           "root",
		AuthMethod:         sshx.AuthPassword,
		Password:           "pw",
		HostKeyFingerprint: "SHA256:fingerprint",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	dom, err := st.CreateDomain(ctx, NewDomain{
		ServerID:     srv.ID,
		Hostname:     "App.Example.Com",
		UpstreamPort: 8080,
		SSLMode:      DomainSSLLetsEncrypt,
		WebSocket:    true,
	})
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if dom.Hostname != "app.example.com" {
		t.Errorf("expected lowercase hostname, got %s", dom.Hostname)
	}

	byHost, err := st.DomainByHostname(ctx, "app.example.com")
	if err != nil || byHost.ID != dom.ID {
		t.Fatalf("DomainByHostname: %v", err)
	}

	err = st.UpdateDomainStatus(ctx, dom.ID, UpdateDomainParams{
		Status:        DomainStatusActive,
		StatusMessage: "SSL certificate acquired",
	})
	if err != nil {
		t.Fatalf("UpdateDomainStatus: %v", err)
	}

	if err := st.DeleteDomain(ctx, dom.ID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
}
