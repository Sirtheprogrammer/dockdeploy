package api

import (
	"context"
	"net/http"
	"time"
)

type healthResponse struct {
	Status   string `json:"status"` // "ok" or "degraded"
	Version  string `json:"version"`
	Uptime   int64  `json:"uptime_seconds"`
	Database string `json:"database"` // "ok" or "unreachable"
}

// Version is stamped at build time via -ldflags.
var Version = "dev"

// handleHealth reports liveness plus the one dependency that matters.
//
// It answers 503 when Postgres is unreachable so container orchestrators and
// uptime checks see the outage instead of a cheerful 200.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	body := healthResponse{
		Status:   "ok",
		Version:  Version,
		Uptime:   int64(time.Since(s.Started).Seconds()),
		Database: "ok",
	}
	status := http.StatusOK

	if err := s.Store.Pool().Ping(ctx); err != nil {
		LoggerFrom(r.Context()).Warn("health check: database unreachable", "error", err)
		body.Status = "degraded"
		body.Database = "unreachable"
		status = http.StatusServiceUnavailable
	}

	return JSON(w, s.Log, status, body)
}
