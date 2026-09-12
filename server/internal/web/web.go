// Package web serves the built frontend from inside the binary.
//
// Embedding the SPA is what lets the controller ship as a single container: no
// sidecar nginx, no volume mount, no separate origin to configure CORS for.
package web

import (
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

// dist is populated by the frontend build stage of the Dockerfile. During local
// development it holds only a placeholder, which Handler detects.
//
//go:embed all:dist
var dist embed.FS

// Handler serves static assets with an SPA fallback: any path that is not a
// real file returns index.html so client-side routes survive a page refresh.
//
// devHint is shown when no build is embedded, which is the normal state when
// running `go run` against a separate Vite dev server.
func Handler(log *slog.Logger, devHint string) http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		log.Error("embedded frontend is unreadable", "error", err)
		return placeholder(devHint)
	}
	if _, err := fs.Stat(root, "index.html"); err != nil {
		log.Warn("no frontend build embedded; serving placeholder")
		return placeholder(devHint)
	}

	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		if _, err := fs.Stat(root, name); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				log.Error("stat embedded asset", "name", name, "error", err)
			}
			serveIndex(w, r, root, log)
			return
		}

		// Vite fingerprints everything under /assets, so those URLs can never
		// go stale. Anything else is served conservatively.
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

// serveIndex returns the SPA shell for unknown paths. It must never be cached,
// or a deploy would leave browsers pinned to a stale asset manifest.
func serveIndex(w http.ResponseWriter, r *http.Request, root fs.FS, log *slog.Logger) {
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		log.Error("read embedded index.html", "error", err)
		http.Error(w, "frontend unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		if _, err := w.Write(index); err != nil {
			log.Debug("write index.html", "error", err)
		}
	}
}

func placeholder(devHint string) http.Handler {
	body := []byte(`<!doctype html><html><head><meta charset="utf-8"><title>dockdeploy</title>` +
		`<style>body{font:15px/1.6 ui-sans-serif,system-ui,sans-serif;margin:4rem auto;max-width:38rem;padding:0 1.5rem;color:#e5e7eb;background:#0b0f14}` +
		`code{background:#1a2029;padding:.15rem .4rem;border-radius:4px}a{color:#60a5fa}</style></head><body>` +
		`<h1>No frontend build embedded</h1><p>The API is running. ` + devHint + `</p></body></html>`)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}
