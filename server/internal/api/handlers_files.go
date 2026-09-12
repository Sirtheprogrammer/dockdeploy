package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// handleListFiles returns a directory listing for a given path on the server.
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	path := r.URL.Query().Get("path")
	listing, err := s.Servers.ListFiles(r.Context(), server, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NotFound("Path not found: %s", path)
		}
		return BadRequest("Failed to list path: %v", err).WithCause(err)
	}

	return JSON(w, s.Log, http.StatusOK, listing)
}

// handleDownloadFile streams a single file from the server to the client.
func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		return BadRequest("Query parameter 'path' is required.")
	}

	reader, size, name, err := s.Servers.DownloadFile(r.Context(), server, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NotFound("File not found: %s", path)
		}
		return BadRequest("Failed to download file: %v", err).WithCause(err)
	}
	defer reader.Close()

	cleanName := filepath.Base(name)
	if cleanName == "" || cleanName == "." || cleanName == "/" {
		cleanName = "download"
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", cleanName))
	w.Header().Set("Content-Type", "application/octet-stream")
	if size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}

	_, _ = io.Copy(w, reader)
	return nil
}

// handleDownloadArchive creates and streams a tar.gz archive of any directory or volume.
func (s *Server) handleDownloadArchive(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		return BadRequest("Query parameter 'path' is required.")
	}

	reader, archiveName, err := s.Servers.DownloadArchive(r.Context(), server, path)
	if err != nil {
		return BadRequest("Failed to create archive: %v", err).WithCause(err)
	}
	defer reader.Close()

	cleanArchive := filepath.Base(archiveName)
	if cleanArchive == "" || cleanArchive == "." {
		cleanArchive = "archive.tar.gz"
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", cleanArchive))
	w.Header().Set("Content-Type", "application/gzip")

	_, _ = io.Copy(w, reader)
	return nil
}

// handleUploadFile accepts a multipart file and writes it to target path on the server.
func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	// 128 MB max memory buffer for multipart parts before temporary files on disk
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		return BadRequest("Failed to parse multipart upload: %v", err).WithCause(err)
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return BadRequest("Form field 'file' is required.")
	}
	defer file.Close()

	targetPath := strings.TrimSpace(r.FormValue("target_path"))
	if targetPath == "" {
		targetDir := strings.TrimSpace(r.FormValue("dir"))
		if targetDir == "" {
			targetDir = strings.TrimSpace(r.URL.Query().Get("dir"))
		}
		if targetDir == "" {
			targetDir = "."
		}
		targetPath = filepath.Join(targetDir, filepath.Base(header.Filename))
	}

	if err := s.Servers.UploadFile(r.Context(), server, targetPath, file, header.Size, 0644); err != nil {
		return BadRequest("Failed to upload file: %v", err).WithCause(err)
	}

	AuditMeta(r.Context(), "server_id", server.ID)
	AuditMeta(r.Context(), "path", targetPath)

	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"path":   targetPath,
		"size":   header.Size,
		"status": "uploaded",
	})
}

type transferRequest struct {
	SourcePath     string `json:"source_path"`
	TargetServerID string `json:"target_server_id"`
	TargetPath     string `json:"target_path"`
}

// handleTransferFile streams a file or directory directly from source server to target server.
func (s *Server) handleTransferFile(w http.ResponseWriter, r *http.Request) error {
	srcServer, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req transferRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	srcPath := f.required("source_path", req.SourcePath, 1, 1024)
	targetID := f.required("target_server_id", req.TargetServerID, 1, 128)
	targetPath := f.required("target_path", req.TargetPath, 1, 1024)
	if err := f.err(); err != nil {
		return err
	}

	actor := MustIdentity(r.Context())
	dstServer, err := s.Store.ServerByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("Target server not found.")
		}
		return Internal(err)
	}

	allowed, err := s.Store.CanAccessServer(r.Context(), targetID, actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}
	if !allowed {
		return NotFound("Target server not found.")
	}

	result, err := s.Servers.TransferFiles(r.Context(), srcServer, dstServer, srcPath, targetPath)
	if err != nil {
		return BadRequest("File transfer failed: %v", err).WithCause(err)
	}

	AuditMeta(r.Context(), "source_server_id", srcServer.ID)
	AuditMeta(r.Context(), "target_server_id", targetID)
	AuditMeta(r.Context(), "source_path", srcPath)
	AuditMeta(r.Context(), "target_path", targetPath)

	return JSON(w, s.Log, http.StatusOK, result)
}
