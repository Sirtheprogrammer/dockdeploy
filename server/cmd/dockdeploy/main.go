// Command dockdeploy is the control plane for the deployment platform: it
// serves the API and dashboard, and drives Docker on registered servers over
// SSH.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/api"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/config"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/db"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/deploy"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/secrets"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg)
	slog.SetDefault(log)
	log.Info("starting dockdeploy",
		"version", api.Version,
		"env", cfg.Env,
		"port", cfg.Port,
		"app_url", cfg.AppURL,
	)

	// Interrupt cancels startup as well as serving, so a slow database does not
	// leave the process unkillable.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sealer, err := secrets.NewSealer(cfg.EncryptionKey)
	if err != nil {
		return err
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool, log); err != nil {
		return err
	}

	db := store.New(pool)
	go purgeExpiredSessions(ctx, db, log)

	// One SSH connection per managed server, shared by every request that
	// needs it and closed on shutdown.
	sshPool := sshx.NewPool(log)
	defer sshPool.Close()

	// The deploy engine and its worker. Runs are rows in Postgres, so a build
	// survives a page refresh and is picked up again if this process restarts.
	deployHub := deploy.NewHub()
	serverManager := servers.NewManager(db, sealer, sshPool, log)
	engine := deploy.NewEngine(db, sealer, serverManager, deployHub, log, deploy.Config{
		PortMin:    cfg.DeployPortMin,
		PortMax:    cfg.DeployPortMax,
		RemoteRoot: cfg.DeployRemoteRoot,
	})

	worker := deploy.NewWorker(db, engine, log, cfg.DeployConcurrency)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx)
	}()

	srv := &api.Server{
		Config:  cfg,
		Log:     log,
		Store:   db,
		Sealer:  sealer,
		Hasher:  auth.NewHasher(cfg.SessionSecret),
		Servers: serverManager,
		Deploys: engine,
		Started: time.Now(),
		SPA:     web.Handler(log, `Run <code>npm run dev</code> in <code>frontend/</code> and open the Vite URL, or build the image to embed the dashboard.`),
	}

	handler, err := srv.Routes()
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: log streaming and container terminals hold a
		// response open indefinitely by design.
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
		stop() // restore default signal handling: a second Ctrl-C kills us now
		log.Info("shutting down", "grace", cfg.ShutdownGrace)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	// Let in-flight deploys finish writing their outcome before exiting, so a
	// restart does not leave a run stuck in "running".
	select {
	case <-workerDone:
	case <-shutdownCtx.Done():
		log.Warn("deploy worker did not drain before the shutdown deadline")
	}

	log.Info("stopped")
	return nil
}

// purgeExpiredSessions keeps the sessions table from growing without bound.
// Expired rows are already unusable -- SessionByTokenHash filters them in SQL
// -- so this is housekeeping, not a security control.
func purgeExpiredSessions(ctx context.Context, db *store.Store, log *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		removed, err := db.PurgeExpiredSessions(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Warn("purge expired sessions", "error", err)
		case removed > 0:
			log.Debug("purged expired sessions", "count", removed)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.IsProduction() {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
