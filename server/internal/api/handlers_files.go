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
	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

func (s *Server) extractSudo(r *http.Request, serverID string) string {
	pass := r.Header.Get("X-Sudo-Password")
	if pass == "" {
		pass = r.URL.Query().Get("sudo_password")
	}
	saveSudo := r.Header.Get("X-Save-Sudo") == "true" || r.URL.Query().Get("save_sudo") == "true"
	if saveSudo && pass != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, serverID, pass)
	}
	return pass
}

// handleListFiles returns a directory listing for a given path on the server.
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	path := r.URL.Query().Get("path")
	sudoPass := s.extractSudo(r, server.ID)

	listing, err := s.Servers.ListFiles(r.Context(), server, path, sudoPass)
	if err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to access this root-protected path."})
		}
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

	sudoPass := s.extractSudo(r, server.ID)
	reader, size, name, err := s.Servers.DownloadFile(r.Context(), server, path, sudoPass)
	if err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to download this protected file."})
		}
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

	sudoPass := s.extractSudo(r, server.ID)
	reader, archiveName, err := s.Servers.DownloadArchive(r.Context(), server, path, sudoPass)
	if err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to archive this protected directory."})
		}
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

// handleGetFileContent reads text content and metadata for in-browser editing.
func (s *Server) handleGetFileContent(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		return BadRequest("Query parameter 'path' is required.")
	}

	sudoPass := s.extractSudo(r, server.ID)
	content, err := s.Servers.ReadFileContent(r.Context(), server, path, 5*1024*1024, sudoPass)
	if err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to read this protected file."})
		}
		if errors.Is(err, os.ErrNotExist) {
			return NotFound("File not found: %s", path)
		}
		return BadRequest("Failed to read file: %v", err).WithCause(err)
	}

	return JSON(w, s.Log, http.StatusOK, content)
}

type saveFileRequest struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	SudoPassword string `json:"sudo_password"`
	SaveSudo     bool   `json:"save_sudo"`
}

// handleSaveFileContent writes edited text content back to the server.
func (s *Server) handleSaveFileContent(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req saveFileRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	path := f.required("path", req.Path, 1, 1024)
	if err := f.err(); err != nil {
		return err
	}

	sudoPass := req.SudoPassword
	if sudoPass == "" {
		sudoPass = s.extractSudo(r, server.ID)
	}
	if req.SaveSudo && req.SudoPassword != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
	}

	if err := s.Servers.WriteFileContent(r.Context(), server, path, req.Content, sudoPass); err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to save this protected file."})
		}
		return BadRequest("Failed to save file: %v", err).WithCause(err)
	}

	AuditMeta(r.Context(), "server_id", server.ID)
	AuditMeta(r.Context(), "path", path)
	AuditMeta(r.Context(), "bytes_written", len(req.Content))

	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"path":  path,
		"size":  len(req.Content),
		"saved": true,
	})
}

type mkdirRequest struct {
	Path         string `json:"path"`
	SudoPassword string `json:"sudo_password"`
	SaveSudo     bool   `json:"save_sudo"`
}

func (s *Server) handleCreateDirectory(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req mkdirRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	path := f.required("path", req.Path, 1, 1024)
	if err := f.err(); err != nil {
		return err
	}

	sudoPass := req.SudoPassword
	if sudoPass == "" {
		sudoPass = s.extractSudo(r, server.ID)
	}
	if req.SaveSudo && req.SudoPassword != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
	}

	if err := s.Servers.CreateDirectory(r.Context(), server, path, sudoPass); err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to create this directory."})
		}
		return BadRequest("Failed to create directory: %v", err).WithCause(err)
	}

	return JSON(w, s.Log, http.StatusCreated, map[string]any{"path": path, "created": true})
}

type fileOpRequest struct {
	Path         string `json:"path"`
	SourcePath   string `json:"source_path"`
	TargetPath   string `json:"target_path"`
	Mode         string `json:"mode"`
	SudoPassword string `json:"sudo_password"`
	SaveSudo     bool   `json:"save_sudo"`
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	sudoPass := s.extractSudo(r, server.ID)

	if path == "" {
		var req fileOpRequest
		if err := DecodeJSON(w, r, &req); err == nil {
			path = req.Path
			if req.SudoPassword != "" {
				sudoPass = req.SudoPassword
			}
			if req.SaveSudo && req.SudoPassword != "" {
				_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
			}
		}
	}

	if path == "" || path == "/" {
		return BadRequest("A valid non-root path is required to delete.")
	}

	if err := s.Servers.DeletePath(r.Context(), server, path, sudoPass); err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to delete this item."})
		}
		return BadRequest("Failed to delete path: %v", err).WithCause(err)
	}

	AuditMeta(r.Context(), "server_id", server.ID)
	AuditMeta(r.Context(), "deleted_path", path)

	return JSON(w, s.Log, http.StatusOK, map[string]any{"path": path, "deleted": true})
}

func (s *Server) handleCopyFile(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req fileOpRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	src := f.required("source_path", req.SourcePath, 1, 1024)
	dst := f.required("target_path", req.TargetPath, 1, 1024)
	if err := f.err(); err != nil {
		return err
	}

	sudoPass := req.SudoPassword
	if sudoPass == "" {
		sudoPass = s.extractSudo(r, server.ID)
	}
	if req.SaveSudo && req.SudoPassword != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
	}

	if err := s.Servers.CopyPath(r.Context(), server, src, dst, sudoPass); err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to copy this file/folder."})
		}
		return BadRequest("Failed to copy path: %v", err).WithCause(err)
	}

	return JSON(w, s.Log, http.StatusOK, map[string]any{"source": src, "target": dst, "copied": true})
}

func (s *Server) handleMoveFile(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req fileOpRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	src := f.required("source_path", req.SourcePath, 1, 1024)
	dst := f.required("target_path", req.TargetPath, 1, 1024)
	if err := f.err(); err != nil {
		return err
	}

	sudoPass := req.SudoPassword
	if sudoPass == "" {
		sudoPass = s.extractSudo(r, server.ID)
	}
	if req.SaveSudo && req.SudoPassword != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
	}

	if err := s.Servers.MovePath(r.Context(), server, src, dst, sudoPass); err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to move/rename this item."})
		}
		return BadRequest("Failed to move/rename path: %v", err).WithCause(err)
	}

	return JSON(w, s.Log, http.StatusOK, map[string]any{"source": src, "target": dst, "moved": true})
}

func (s *Server) handleChmodFile(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req fileOpRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	path := f.required("path", req.Path, 1, 1024)
	mode := f.required("mode", req.Mode, 1, 10)
	if err := f.err(); err != nil {
		return err
	}

	sudoPass := req.SudoPassword
	if sudoPass == "" {
		sudoPass = s.extractSudo(r, server.ID)
	}
	if req.SaveSudo && req.SudoPassword != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
	}

	if err := s.Servers.ChmodPath(r.Context(), server, path, mode, sudoPass); err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to chmod this item."})
		}
		return BadRequest("Failed to chmod path: %v", err).WithCause(err)
	}

	return JSON(w, s.Log, http.StatusOK, map[string]any{"path": path, "mode": mode, "updated": true})
}

func (s *Server) handleScanStorage(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	minSizeMB := 10
	if m := r.URL.Query().Get("min_size_mb"); m != "" {
		if parsed, err := strconv.Atoi(m); err == nil && parsed > 0 {
			minSizeMB = parsed
		}
	}

	sudoPass := s.extractSudo(r, server.ID)
	result, err := s.Servers.ScanStorage(r.Context(), server, path, minSizeMB, sudoPass)
	if err != nil {
		if errors.Is(err, servers.ErrSudoPasswordRequired) {
			return Invalid(fields{"sudo_password": "Sudo password is required to scan storage on this path."})
		}
		return BadRequest("Failed to scan storage: %v", err).WithCause(err)
	}

	return JSON(w, s.Log, http.StatusOK, result)
}
