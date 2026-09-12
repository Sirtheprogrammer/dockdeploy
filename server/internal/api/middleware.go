package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type contextKey string

const loggerKey contextKey = "logger"

// LoggerFrom returns the request-scoped logger, which carries the request id so
// every line emitted while handling one request can be correlated.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return log
	}
	return slog.Default()
}

// requestLogger attaches a request-scoped logger and records one line per
// completed request.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			entry := log.With(
				"request_id", middleware.GetReqID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
			)
			ctx := context.WithValue(r.Context(), loggerKey, entry)

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				// Health checks fire constantly and would drown out real
				// traffic at info level.
				level := slog.LevelInfo
				if r.URL.Path == "/api/health" {
					level = slog.LevelDebug
				}
				entry.Log(ctx, level, "request",
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"duration_ms", time.Since(start).Milliseconds(),
					"remote", r.RemoteAddr,
				)
			}()

			next.ServeHTTP(ww, r.WithContext(ctx))
		})
	}
}

// recoverer turns a panic into a logged 500 rather than a dropped connection.
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// A client disconnecting mid-write is normal, not a bug.
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				LoggerFrom(r.Context()).Error("panic recovered",
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				writeJSON(w, log, http.StatusInternalServerError, map[string]any{
					"error": &Error{Code: CodeInternal, Message: "Something went wrong on our end."},
				})
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// securityHeaders sets defaults appropriate for an API plus a same-origin SPA.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
