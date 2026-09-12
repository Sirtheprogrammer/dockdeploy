package servers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

type FileEntry struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	Mode        string    `json:"mode"`
	IsDir       bool      `json:"is_dir"`
	IsSymlink   bool      `json:"is_symlink"`
	ModTime     time.Time `json:"mod_time"`
	Permissions string    `json:"permissions"`
}

type DirectoryListing struct {
	Path       string      `json:"path"`
	Parent     string      `json:"parent"`
	Entries    []FileEntry `json:"entries"`
	TotalFiles int         `json:"total_files"`
	TotalDirs  int         `json:"total_dirs"`
	TotalBytes int64       `json:"total_bytes"`
}

type FileTransferResult struct {
	SourceServerID string `json:"source_server_id"`
	TargetServerID string `json:"target_server_id"`
	SourcePath     string `json:"source_path"`
	TargetPath     string `json:"target_path"`
	BytesCopied    int64  `json:"bytes_copied"`
	DurationMs     int64  `json:"duration_ms"`
}

// resolvePath canonicalizes paths and resolves user home ~ relative to server environment.
func (m *Manager) resolvePath(ctx context.Context, server *store.Server, path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		conn, err := m.Connect(ctx, server)
		if err != nil {
			return "/", err
		}
		defer conn.Release()

		res, err := conn.Run(ctx, `echo "$HOME"`)
		if err == nil && res.Ok() && res.Output() != "" {
			home := res.Output()
			if trimmed == "" || trimmed == "~" {
				return home, nil
			}
			return filepath.Clean(filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))), nil
		}
		return "/", nil
	}
	return filepath.Clean(trimmed), nil
}

// ListFiles lists directory contents with metadata, gracefully falling back to sudo
// commands for root-protected directories such as Docker volumes.
func (m *Manager) ListFiles(ctx context.Context, server *store.Server, rawPath string) (*DirectoryListing, error) {
	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return nil, err
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	// Try SFTP first
	sftpClient, sftpErr := sftp.NewClient(conn.Client())
	if sftpErr == nil {
		defer sftpClient.Close()

		stat, err := sftpClient.Stat(cleanPath)
		if err == nil && !stat.IsDir() {
			// Single file query
			entry := FileEntry{
				Name:        stat.Name(),
				Path:        cleanPath,
				Size:        stat.Size(),
				Mode:        stat.Mode().String(),
				IsDir:       false,
				IsSymlink:   stat.Mode()&os.ModeSymlink != 0,
				ModTime:     stat.ModTime().UTC(),
				Permissions: fmt.Sprintf("%04o", stat.Mode().Perm()),
			}
			parent := filepath.Dir(cleanPath)
			return &DirectoryListing{
				Path:       cleanPath,
				Parent:     parent,
				Entries:    []FileEntry{entry},
				TotalFiles: 1,
				TotalBytes: stat.Size(),
			}, nil
		}

		entries, err := sftpClient.ReadDir(cleanPath)
		if err == nil {
			parent := filepath.Dir(cleanPath)
			if cleanPath == "/" {
				parent = ""
			}
			listing := &DirectoryListing{
				Path:    cleanPath,
				Parent:  parent,
				Entries: make([]FileEntry, 0, len(entries)),
			}
			for _, fi := range entries {
				isSym := fi.Mode()&os.ModeSymlink != 0
				isDir := fi.IsDir()
				entry := FileEntry{
					Name:        fi.Name(),
					Path:        filepath.Join(cleanPath, fi.Name()),
					Size:        fi.Size(),
					Mode:        fi.Mode().String(),
					IsDir:       isDir,
					IsSymlink:   isSym,
					ModTime:     fi.ModTime().UTC(),
					Permissions: fmt.Sprintf("%04o", fi.Mode().Perm()),
				}
				if isDir {
					listing.TotalDirs++
				} else {
					listing.TotalFiles++
					listing.TotalBytes += fi.Size()
				}
				listing.Entries = append(listing.Entries, entry)
			}
			return listing, nil
		}
	}

	// Fallback using shell with sudo for restricted directories like /var/lib/docker/volumes
	script := fmt.Sprintf(`sudo sh -c '
TARGET="%s"
if [ ! -e "$TARGET" ]; then
  echo "ERR_NOT_FOUND"
  exit 1
fi
for f in "$TARGET"/* "$TARGET"/.*; do
  [ -e "$f" ] || continue
  BN=$(basename "$f")
  [ "$BN" = "." ] || [ "$BN" = ".." ] && continue
  stat -c "%%n|%%s|%%a|%%Y|%%F" "$f" 2>/dev/null
done
'`, escapeShell(cleanPath))

	res, err := conn.Run(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("list directory: %w", err)
	}
	if strings.Contains(res.Stdout, "ERR_NOT_FOUND") {
		return nil, os.ErrNotExist
	}

	parent := filepath.Dir(cleanPath)
	if cleanPath == "/" {
		parent = ""
	}
	listing := &DirectoryListing{
		Path:    cleanPath,
		Parent:  parent,
		Entries: make([]FileEntry, 0),
	}

	scanner := bufio.NewScanner(strings.NewReader(res.Stdout))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		fullPath := parts[0]
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		perm := parts[2]
		mtimeUnix, _ := strconv.ParseInt(parts[3], 10, 64)
		fileType := strings.ToLower(parts[4])

		isDir := strings.Contains(fileType, "directory")
		isSym := strings.Contains(fileType, "symbolic")

		entry := FileEntry{
			Name:        filepath.Base(fullPath),
			Path:        fullPath,
			Size:        size,
			Mode:        fileType,
			IsDir:       isDir,
			IsSymlink:   isSym,
			ModTime:     time.Unix(mtimeUnix, 0).UTC(),
			Permissions: perm,
		}
		if isDir {
			listing.TotalDirs++
		} else {
			listing.TotalFiles++
			listing.TotalBytes += size
		}
		listing.Entries = append(listing.Entries, entry)
	}

	return listing, nil
}

// DownloadFile opens a stream to download a single file from the server.
func (m *Manager) DownloadFile(ctx context.Context, server *store.Server, rawPath string) (io.ReadCloser, int64, string, error) {
	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return nil, 0, "", err
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, 0, "", err
	}

	sftpClient, sftpErr := sftp.NewClient(conn.Client())
	if sftpErr == nil {
		fi, statErr := sftpClient.Stat(cleanPath)
		if statErr == nil && !fi.IsDir() {
			file, openErr := sftpClient.Open(cleanPath)
			if openErr == nil {
				reader := &sftpReadCloser{
					file:   file,
					client: sftpClient,
					conn:   conn,
				}
				return reader, fi.Size(), fi.Name(), nil
			}
		}
		sftpClient.Close()
	}

	// Fallback: stream using sudo cat via SSH session
	session, err := conn.Client().NewSession()
	if err != nil {
		conn.Release()
		return nil, 0, "", fmt.Errorf("ssh session: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		conn.Release()
		return nil, 0, "", fmt.Errorf("stdout pipe: %w", err)
	}

	cmd := fmt.Sprintf(`sudo cat "%s"`, escapeShell(cleanPath))
	if err := session.Start(cmd); err != nil {
		session.Close()
		conn.Release()
		return nil, 0, "", fmt.Errorf("start cat: %w", err)
	}

	reader := &execReadCloser{
		reader:  stdout,
		session: session,
		conn:    conn,
	}
	return reader, -1, filepath.Base(cleanPath), nil
}

// DownloadArchive streams a tar.gz archive of any directory (or volume) directly from the server.
func (m *Manager) DownloadArchive(ctx context.Context, server *store.Server, rawPath string) (io.ReadCloser, string, error) {
	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return nil, "", err
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, "", err
	}

	session, err := conn.Client().NewSession()
	if err != nil {
		conn.Release()
		return nil, "", fmt.Errorf("ssh session: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		conn.Release()
		return nil, "", fmt.Errorf("stdout pipe: %w", err)
	}

	parent := filepath.Dir(cleanPath)
	base := filepath.Base(cleanPath)
	if cleanPath == "/" {
		parent = "/"
		base = "."
	}

	cmd := fmt.Sprintf(`sudo tar -czf - -C "%s" "%s"`, escapeShell(parent), escapeShell(base))
	if err := session.Start(cmd); err != nil {
		session.Close()
		conn.Release()
		return nil, "", fmt.Errorf("start tar: %w", err)
	}

	archiveName := base + ".tar.gz"
	if base == "." {
		archiveName = server.Name + "_root.tar.gz"
	}

	reader := &execReadCloser{
		reader:  stdout,
		session: session,
		conn:    conn,
	}
	return reader, archiveName, nil
}

// UploadFile writes a file from an incoming stream to the target path on the server.
func (m *Manager) UploadFile(ctx context.Context, server *store.Server, targetPath string, r io.Reader, size int64, mode uint32) error {
	cleanPath, err := m.resolvePath(ctx, server, targetPath)
	if err != nil {
		return err
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return err
	}
	defer conn.Release()

	sftpClient, err := sftp.NewClient(conn.Client())
	if err == nil {
		defer sftpClient.Close()
		dir := filepath.Dir(cleanPath)
		_ = sftpClient.MkdirAll(dir)

		f, err := sftpClient.Create(cleanPath)
		if err == nil {
			defer f.Close()
			if mode > 0 {
				_ = f.Chmod(os.FileMode(mode))
			}
			_, copyErr := io.Copy(f, r)
			return copyErr
		}
	}

	// Sudo tee fallback
	session, err := conn.Client().NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	dir := filepath.Dir(cleanPath)
	cmd := fmt.Sprintf(`sudo mkdir -p "%s" && sudo tee "%s" >/dev/null`, escapeShell(dir), escapeShell(cleanPath))
	if err := session.Start(cmd); err != nil {
		return fmt.Errorf("start upload: %w", err)
	}

	_, copyErr := io.Copy(stdin, r)
	_ = stdin.Close()
	waitErr := session.Wait()

	if copyErr != nil {
		return copyErr
	}
	return waitErr
}

// TransferFiles streams a file or directory from sourceServer directly to targetServer over SSH tar pipe.
func (m *Manager) TransferFiles(ctx context.Context, srcServer *store.Server, dstServer *store.Server, srcPath string, dstPath string) (*FileTransferResult, error) {
	cleanSrc, err := m.resolvePath(ctx, srcServer, srcPath)
	if err != nil {
		return nil, fmt.Errorf("source path: %w", err)
	}
	cleanDst, err := m.resolvePath(ctx, dstServer, dstPath)
	if err != nil {
		return nil, fmt.Errorf("target path: %w", err)
	}

	srcConn, err := m.Connect(ctx, srcServer)
	if err != nil {
		return nil, fmt.Errorf("connect source %s: %w", srcServer.Name, err)
	}
	defer srcConn.Release()

	dstConn, err := m.Connect(ctx, dstServer)
	if err != nil {
		return nil, fmt.Errorf("connect target %s: %w", dstServer.Name, err)
	}
	defer dstConn.Release()

	srcSession, err := srcConn.Client().NewSession()
	if err != nil {
		return nil, fmt.Errorf("source session: %w", err)
	}
	defer srcSession.Close()

	dstSession, err := dstConn.Client().NewSession()
	if err != nil {
		return nil, fmt.Errorf("destination session: %w", err)
	}
	defer dstSession.Close()

	srcStdout, err := srcSession.StdoutPipe()
	if err != nil {
		return nil, err
	}

	dstStdin, err := dstSession.StdinPipe()
	if err != nil {
		return nil, err
	}

	srcParent := filepath.Dir(cleanSrc)
	srcBase := filepath.Base(cleanSrc)
	if cleanSrc == "/" {
		srcParent = "/"
		srcBase = "."
	}

	srcCmd := fmt.Sprintf(`sudo tar -czf - -C "%s" "%s"`, escapeShell(srcParent), escapeShell(srcBase))
	dstCmd := fmt.Sprintf(`sudo mkdir -p "%s" && sudo tar -xzf - -C "%s"`, escapeShell(cleanDst), escapeShell(cleanDst))

	start := time.Now()
	if err := srcSession.Start(srcCmd); err != nil {
		return nil, fmt.Errorf("start source archive: %w", err)
	}

	if err := dstSession.Start(dstCmd); err != nil {
		return nil, fmt.Errorf("start target extract: %w", err)
	}

	var bytesCopied int64
	countingReader := &countingReader{reader: srcStdout, count: &bytesCopied}

	copyDone := make(chan error, 1)
	go func() {
		_, cErr := io.Copy(dstStdin, countingReader)
		_ = dstStdin.Close()
		copyDone <- cErr
	}()

	var copyErr, srcErr, dstErr error
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case copyErr = <-copyDone:
	}

	srcErr = srcSession.Wait()
	dstErr = dstSession.Wait()

	duration := time.Since(start).Milliseconds()

	if copyErr != nil {
		return nil, fmt.Errorf("transfer pipe error: %w", copyErr)
	}
	if srcErr != nil {
		return nil, fmt.Errorf("source error: %w", srcErr)
	}
	if dstErr != nil {
		return nil, fmt.Errorf("target error: %w", dstErr)
	}

	return &FileTransferResult{
		SourceServerID: srcServer.ID,
		TargetServerID: dstServer.ID,
		SourcePath:     cleanSrc,
		TargetPath:     cleanDst,
		BytesCopied:    atomic.LoadInt64(&bytesCopied),
		DurationMs:     duration,
	}, nil
}

type countingReader struct {
	reader io.Reader
	count  *int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.reader.Read(p)
	if n > 0 {
		atomic.AddInt64(cr.count, int64(n))
	}
	return n, err
}

type sftpReadCloser struct {
	file   *sftp.File
	client *sftp.Client
	conn   *sshx.Conn
}

func (r *sftpReadCloser) Read(p []byte) (int, error) {
	return r.file.Read(p)
}

func (r *sftpReadCloser) Close() error {
	var errs []error
	if err := r.file.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := r.client.Close(); err != nil {
		errs = append(errs, err)
	}
	r.conn.Release()
	return errors.Join(errs...)
}

type execReadCloser struct {
	reader  io.Reader
	session *ssh.Session
	conn    *sshx.Conn
}

func (r *execReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *execReadCloser) Close() error {
	var errs []error
	if err := r.session.Close(); err != nil {
		errs = append(errs, err)
	}
	r.conn.Release()
	return errors.Join(errs...)
}

func escapeShell(arg string) string {
	return strings.ReplaceAll(arg, `"`, `\"`)
}
