package servers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

var ErrSudoPasswordRequired = errors.New("sudo_password_required")

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

type FileContent struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	Permissions string    `json:"permissions"`
	ModTime     time.Time `json:"mod_time"`
	IsBinary    bool      `json:"is_binary"`
	Content     string    `json:"content"`
}

type StorageItem struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SizeBytes int64  `json:"size_bytes"`
	HumanSize string `json:"human_size"`
	SizeHuman string `json:"size_human"`
	IsDir     bool   `json:"is_dir"`
	ModTime   string `json:"mod_time"`
}

type StorageScanResult struct {
	TargetPath     string        `json:"target_path"`
	MinSizeMB      int           `json:"min_size_mb"`
	ScannedAt      string        `json:"scanned_at"`
	TopDirectories []StorageItem `json:"top_directories"`
	LargestFiles   []StorageItem `json:"largest_files"`
	TopLargeDirs   []StorageItem `json:"top_large_dirs"`
	TopLargeFiles  []StorageItem `json:"top_large_files"`
}

// ShellQuote quotes a string safely for inclusion in POSIX shell command lines.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func shellQuote(s string) string {
	return ShellQuote(s)
}

func (m *Manager) runSudo(ctx context.Context, server *store.Server, conn *sshx.Conn, sudoPasswordOverride, cmd string) (sshx.Result, error) {
	var caps sshx.Capabilities
	if len(server.Capabilities) > 0 {
		_ = json.Unmarshal(server.Capabilities, &caps)
	}

	sudoPassword := sudoPasswordOverride
	if sudoPassword == "" {
		if cred, err := m.store.ServerCredential(ctx, m.sealer, server); err == nil && cred.SudoPassword != "" {
			sudoPassword = cred.SudoPassword
		}
	}

	var fullCmd string
	if caps.SudoMode == sshx.SudoRoot || (conn.IsLocal() && os.Geteuid() == 0) {
		fullCmd = cmd
	} else if sudoPassword != "" {
		fullCmd = fmt.Sprintf("printf '%%s\\n' %s | sudo -S -p '' sh -c %s", shellQuote(sudoPassword), shellQuote(cmd))
	} else if caps.SudoMode == sshx.SudoNoPassword {
		fullCmd = "sudo -n sh -c " + shellQuote(cmd)
	} else {
		return sshx.Result{ExitCode: 1}, ErrSudoPasswordRequired
	}

	res, err := conn.Run(ctx, fullCmd)
	if err != nil && res.ExitCode == 0 {
		return res, err
	}
	if !res.Ok() {
		errLower := strings.ToLower(res.Stderr)
		if strings.Contains(errLower, "password is required") ||
			strings.Contains(errLower, "no tty present") ||
			strings.Contains(errLower, "incorrect password") ||
			strings.Contains(errLower, "try again") {
			return res, ErrSudoPasswordRequired
		}
	}
	return res, nil
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
func (m *Manager) ListFiles(ctx context.Context, server *store.Server, rawPath string, sudoPasswords ...string) (*DirectoryListing, error) {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}

	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return nil, err
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	if conn.IsLocal() {
		entries, err := os.ReadDir(cleanPath)
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
			for _, de := range entries {
				fi, err := de.Info()
				if err != nil {
					continue
				}
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

	// Try SFTP first if remote
	if conn.Client() != nil {
		sftpClient, sftpErr := sftp.NewClient(conn.Client())
		if sftpErr == nil {
			defer sftpClient.Close()

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
	}

	// Fallback using elevated shell with sudo for restricted directories like /var/lib/docker/volumes
	script := fmt.Sprintf(`TARGET=%s
if [ ! -e "$TARGET" ]; then
  echo "ERR_NOT_FOUND"
  exit 1
fi
if [ -f "$TARGET" ]; then
  stat -c "%%n|%%s|%%a|%%Y|%%F" "$TARGET" 2>/dev/null
  exit 0
fi
for f in "$TARGET"/* "$TARGET"/.*; do
  [ -e "$f" ] || continue
  BN=$(basename "$f")
  [ "$BN" = "." ] || [ "$BN" = ".." ] && continue
  stat -c "%%n|%%s|%%a|%%Y|%%F" "$f" 2>/dev/null
done
`, shellQuote(cleanPath))

	res, err := m.runSudo(ctx, server, conn, sudoPassword, script)
	if err != nil {
		return nil, err
	}
	if !res.Ok() {
		if strings.Contains(res.Stdout, "ERR_NOT_FOUND") {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("list directory failed: %s", res.Stderr)
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
		if line == "" || line == "ERR_NOT_FOUND" {
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
func (m *Manager) DownloadFile(ctx context.Context, server *store.Server, rawPath string, sudoPasswords ...string) (io.ReadCloser, int64, string, error) {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}

	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return nil, 0, "", err
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, 0, "", err
	}

	if conn.IsLocal() {
		f, err := os.Open(cleanPath)
		if err == nil {
			fi, err := f.Stat()
			if err == nil {
				return f, fi.Size(), fi.Name(), nil
			}
			f.Close()
		}
	}

	if conn.Client() != nil {
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
	}

	// Fallback: stream using sudo cat via SSH session
	if conn.Client() != nil {
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

		var cmd string
		if sudoPassword != "" {
			cmd = fmt.Sprintf("printf '%%s\\n' %s | sudo -S -p '' cat %s", shellQuote(sudoPassword), shellQuote(cleanPath))
		} else {
			cmd = fmt.Sprintf("sudo cat %s", shellQuote(cleanPath))
		}

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

	conn.Release()
	return nil, 0, "", errors.New("no client available")
}

// ReadFileContent reads the text content of a file up to maxBytes, detecting whether it's binary.
func (m *Manager) ReadFileContent(ctx context.Context, server *store.Server, rawPath string, maxBytes int64, sudoPasswords ...string) (*FileContent, error) {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}

	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024 // 5 MB default limit
	}

	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return nil, err
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	var modTime time.Time
	var perms string
	var size int64 = -1
	var buf []byte

	if conn.IsLocal() {
		fi, statErr := os.Stat(cleanPath)
		if statErr == nil && !fi.IsDir() {
			if fi.Size() > maxBytes {
				return nil, fmt.Errorf("file size (%d bytes) exceeds editor limit (%d bytes)", fi.Size(), maxBytes)
			}
			data, err := os.ReadFile(cleanPath)
			if err == nil {
				buf = data
				size = fi.Size()
				modTime = fi.ModTime().UTC()
				perms = fmt.Sprintf("%04o", fi.Mode().Perm())
			}
		}
	} else if conn.Client() != nil {
		sftpClient, sftpErr := sftp.NewClient(conn.Client())
		if sftpErr == nil {
			defer sftpClient.Close()
			fi, statErr := sftpClient.Stat(cleanPath)
			if statErr == nil && !fi.IsDir() {
				if fi.Size() > maxBytes {
					return nil, fmt.Errorf("file size (%d bytes) exceeds editor limit (%d bytes)", fi.Size(), maxBytes)
				}
				f, openErr := sftpClient.Open(cleanPath)
				if openErr == nil {
					defer f.Close()
					buf, err = io.ReadAll(io.LimitReader(f, maxBytes+1))
					if err == nil {
						size = fi.Size()
						modTime = fi.ModTime().UTC()
						perms = fmt.Sprintf("%04o", fi.Mode().Perm())
					}
				}
			}
		}
	}

	// Fallback via elevated shell/sudo
	if buf == nil {
		statCmd := fmt.Sprintf(`stat -c "%%s|%%a|%%Y" %s 2>/dev/null`, shellQuote(cleanPath))
		if statRes, err := m.runSudo(ctx, server, conn, sudoPassword, statCmd); err == nil && statRes.Ok() {
			parts := strings.Split(strings.TrimSpace(statRes.Output()), "|")
			if len(parts) == 3 {
				size, _ = strconv.ParseInt(parts[0], 10, 64)
				perms = parts[1]
				mtimeUnix, _ := strconv.ParseInt(parts[2], 10, 64)
				modTime = time.Unix(mtimeUnix, 0).UTC()
			}
		}

		if size > maxBytes {
			return nil, fmt.Errorf("file size (%d bytes) exceeds editor limit (%d bytes)", size, maxBytes)
		}

		catCmd := fmt.Sprintf(`cat %s`, shellQuote(cleanPath))
		res, err := m.runSudo(ctx, server, conn, sudoPassword, catCmd)
		if err != nil {
			return nil, err
		}
		if !res.Ok() {
			return nil, fmt.Errorf("read file failed: %s", res.Stderr)
		}
		buf = []byte(res.Stdout)
	}

	if int64(len(buf)) > maxBytes {
		return nil, fmt.Errorf("file size exceeds editor limit (%d bytes)", maxBytes)
	}

	isBinary := false
	checkLen := len(buf)
	if checkLen > 1024 {
		checkLen = 1024
	}
	for i := 0; i < checkLen; i++ {
		if buf[i] == 0 {
			isBinary = true
			break
		}
	}

	content := ""
	if !isBinary {
		content = string(buf)
	}

	if size < 0 {
		size = int64(len(buf))
	}
	if perms == "" {
		perms = "0644"
	}
	if modTime.IsZero() {
		modTime = time.Now().UTC()
	}

	return &FileContent{
		Name:        filepath.Base(cleanPath),
		Path:        cleanPath,
		Size:        size,
		Permissions: perms,
		ModTime:     modTime,
		IsBinary:    isBinary,
		Content:     content,
	}, nil
}

// WriteFileContent writes a string to a file on the server, supporting elevated sudo write.
func (m *Manager) WriteFileContent(ctx context.Context, server *store.Server, rawPath string, content string, sudoPasswords ...string) error {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}

	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return err
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return err
	}
	defer conn.Release()

	if conn.IsLocal() {
		dir := filepath.Dir(cleanPath)
		_ = os.MkdirAll(dir, 0755)
		if err := os.WriteFile(cleanPath, []byte(content), 0644); err == nil {
			return nil
		}
	} else if conn.Client() != nil {
		if sftpClient, err := sftp.NewClient(conn.Client()); err == nil {
			defer sftpClient.Close()
			dir := filepath.Dir(cleanPath)
			_ = sftpClient.MkdirAll(dir)
			if f, err := sftpClient.Create(cleanPath); err == nil {
				_, copyErr := io.WriteString(f, content)
				_ = f.Close()
				if copyErr == nil {
					return nil
				}
			}
		}
	}

	// Sudo tee write fallback
	dir := filepath.Dir(cleanPath)
	writeScript := fmt.Sprintf("mkdir -p %s && cat > %s << 'DOCKDEPLOY_EOF'\n%s\nDOCKDEPLOY_EOF",
		shellQuote(dir), shellQuote(cleanPath), content)

	res, err := m.runSudo(ctx, server, conn, sudoPassword, writeScript)
	if err != nil {
		return err
	}
	if !res.Ok() {
		return fmt.Errorf("sudo write failed: %s", res.Stderr)
	}
	return nil
}

// CreateDirectory creates a directory path on the server with sudo fallback.
func (m *Manager) CreateDirectory(ctx context.Context, server *store.Server, rawPath string, sudoPasswords ...string) error {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}
	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return err
	}
	conn, err := m.Connect(ctx, server)
	if err != nil {
		return err
	}
	defer conn.Release()

	if conn.IsLocal() {
		if err := os.MkdirAll(cleanPath, 0755); err == nil {
			return nil
		}
	} else if conn.Client() != nil {
		if sftpClient, err := sftp.NewClient(conn.Client()); err == nil {
			defer sftpClient.Close()
			if err := sftpClient.MkdirAll(cleanPath); err == nil {
				return nil
			}
		}
	}

	res, err := m.runSudo(ctx, server, conn, sudoPassword, "mkdir -p "+shellQuote(cleanPath))
	if err != nil {
		return err
	}
	if !res.Ok() {
		return fmt.Errorf("create directory failed: %s", res.Stderr)
	}
	return nil
}

// DeletePath removes a file or directory recursively on the server with sudo fallback.
func (m *Manager) DeletePath(ctx context.Context, server *store.Server, rawPath string, sudoPasswords ...string) error {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}
	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return err
	}
	if cleanPath == "/" || cleanPath == "" {
		return errors.New("cannot delete root filesystem")
	}
	conn, err := m.Connect(ctx, server)
	if err != nil {
		return err
	}
	defer conn.Release()

	if conn.IsLocal() {
		if err := os.RemoveAll(cleanPath); err == nil {
			return nil
		}
	} else if conn.Client() != nil {
		if sftpClient, err := sftp.NewClient(conn.Client()); err == nil {
			defer sftpClient.Close()
			if err := sftpClient.RemoveAll(cleanPath); err == nil {
				return nil
			}
		}
	}

	res, err := m.runSudo(ctx, server, conn, sudoPassword, "rm -rf "+shellQuote(cleanPath))
	if err != nil {
		return err
	}
	if !res.Ok() {
		return fmt.Errorf("delete failed: %s", res.Stderr)
	}
	return nil
}

// CopyPath copies a file or directory recursively on the server.
func (m *Manager) CopyPath(ctx context.Context, server *store.Server, srcPath, dstPath string, sudoPasswords ...string) error {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}
	cleanSrc, err := m.resolvePath(ctx, server, srcPath)
	if err != nil {
		return err
	}
	cleanDst, err := m.resolvePath(ctx, server, dstPath)
	if err != nil {
		return err
	}
	conn, err := m.Connect(ctx, server)
	if err != nil {
		return err
	}
	defer conn.Release()

	cmd := fmt.Sprintf("cp -a %s %s", shellQuote(cleanSrc), shellQuote(cleanDst))
	res, err := m.runSudo(ctx, server, conn, sudoPassword, cmd)
	if err != nil {
		return err
	}
	if !res.Ok() {
		return fmt.Errorf("copy failed: %s", res.Stderr)
	}
	return nil
}

// MovePath moves or renames a file or directory on the server.
func (m *Manager) MovePath(ctx context.Context, server *store.Server, srcPath, dstPath string, sudoPasswords ...string) error {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}
	cleanSrc, err := m.resolvePath(ctx, server, srcPath)
	if err != nil {
		return err
	}
	cleanDst, err := m.resolvePath(ctx, server, dstPath)
	if err != nil {
		return err
	}
	conn, err := m.Connect(ctx, server)
	if err != nil {
		return err
	}
	defer conn.Release()

	cmd := fmt.Sprintf("mv %s %s", shellQuote(cleanSrc), shellQuote(cleanDst))
	res, err := m.runSudo(ctx, server, conn, sudoPassword, cmd)
	if err != nil {
		return err
	}
	if !res.Ok() {
		return fmt.Errorf("move failed: %s", res.Stderr)
	}
	return nil
}

// ChmodPath changes the file permission mode.
func (m *Manager) ChmodPath(ctx context.Context, server *store.Server, rawPath, mode string, sudoPasswords ...string) error {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}
	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return err
	}
	conn, err := m.Connect(ctx, server)
	if err != nil {
		return err
	}
	defer conn.Release()

	cmd := fmt.Sprintf("chmod %s %s", shellQuote(mode), shellQuote(cleanPath))
	res, err := m.runSudo(ctx, server, conn, sudoPassword, cmd)
	if err != nil {
		return err
	}
	if !res.Ok() {
		return fmt.Errorf("chmod failed: %s", res.Stderr)
	}
	return nil
}

// ScanStorage discovers the largest directories and files consuming disk storage on the server.
func (m *Manager) ScanStorage(ctx context.Context, server *store.Server, rawPath string, minSizeMB int, sudoPasswords ...string) (*StorageScanResult, error) {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}
	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return nil, err
	}
	if minSizeMB <= 0 {
		minSizeMB = 10
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	script := fmt.Sprintf(`TARGET=%s
echo "===DIRS==="
du -k -d 2 "$TARGET" 2>/dev/null | sort -rn | head -n 25
echo "===FILES==="
find "$TARGET" -xdev -type f -size +%dM -printf "%%s|%%TY-%%Tm-%%Td %%TH:%%TM|%%p\n" 2>/dev/null | sort -rn | head -n 35
`, shellQuote(cleanPath), minSizeMB)

	res, err := m.runSudo(ctx, server, conn, sudoPassword, script)
	if err != nil {
		return nil, err
	}

	result := &StorageScanResult{
		TargetPath:     cleanPath,
		MinSizeMB:      minSizeMB,
		ScannedAt:      time.Now().UTC().Format(time.RFC3339),
		TopDirectories: make([]StorageItem, 0),
		LargestFiles:   make([]StorageItem, 0),
		TopLargeDirs:   make([]StorageItem, 0),
		TopLargeFiles:  make([]StorageItem, 0),
	}

	scanner := bufio.NewScanner(strings.NewReader(res.Stdout))
	section := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "===DIRS===" {
			section = "dirs"
			continue
		} else if line == "===FILES===" {
			section = "files"
			continue
		}
		if line == "" {
			continue
		}

		if section == "dirs" {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[0], 10, 64)
				dirPath := strings.Join(fields[1:], " ")
				b := kb * 1024
				item := StorageItem{
					Path:      dirPath,
					Name:      filepath.Base(dirPath),
					Size:      b,
					SizeBytes: b,
					HumanSize: formatBytes(b),
					SizeHuman: formatBytes(b),
					IsDir:     true,
				}
				result.TopDirectories = append(result.TopDirectories, item)
				result.TopLargeDirs = append(result.TopLargeDirs, item)
			}
		} else if section == "files" {
			parts := strings.SplitN(line, "|", 3)
			if len(parts) >= 3 {
				b, _ := strconv.ParseInt(parts[0], 10, 64)
				mtime := parts[1]
				filePath := parts[2]
				item := StorageItem{
					Path:      filePath,
					Name:      filepath.Base(filePath),
					Size:      b,
					SizeBytes: b,
					HumanSize: formatBytes(b),
					SizeHuman: formatBytes(b),
					IsDir:     false,
					ModTime:   mtime,
				}
				result.LargestFiles = append(result.LargestFiles, item)
				result.TopLargeFiles = append(result.TopLargeFiles, item)
			}
		}
	}

	return result, nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// DownloadArchive streams a tar.gz archive of any directory (or volume) directly from the server.
func (m *Manager) DownloadArchive(ctx context.Context, server *store.Server, rawPath string, sudoPasswords ...string) (io.ReadCloser, string, error) {
	sudoPassword := ""
	if len(sudoPasswords) > 0 {
		sudoPassword = sudoPasswords[0]
	}

	cleanPath, err := m.resolvePath(ctx, server, rawPath)
	if err != nil {
		return nil, "", err
	}

	conn, err := m.Connect(ctx, server)
	if err != nil {
		return nil, "", err
	}

	parent := filepath.Dir(cleanPath)
	base := filepath.Base(cleanPath)
	if cleanPath == "/" {
		parent = "/"
		base = "."
	}

	archiveName := base + ".tar.gz"
	if base == "." {
		archiveName = server.Name + "_root.tar.gz"
	}

	if conn.IsLocal() {
		cmd := exec.CommandContext(ctx, "tar", "-czf", "-", "-C", parent, base)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			conn.Release()
			return nil, "", fmt.Errorf("tar stdout: %w", err)
		}
		if err := cmd.Start(); err != nil {
			conn.Release()
			return nil, "", fmt.Errorf("start tar: %w", err)
		}
		reader := &localCmdReadCloser{
			reader: stdout,
			cmd:    cmd,
		}
		return reader, archiveName, nil
	}

	if conn.Client() != nil {
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

		var cmd string
		if sudoPassword != "" {
			cmd = fmt.Sprintf("printf '%%s\\n' %s | sudo -S -p '' tar -czf - -C %s %s",
				shellQuote(sudoPassword), shellQuote(parent), shellQuote(base))
		} else {
			cmd = fmt.Sprintf("sudo tar -czf - -C %s %s", shellQuote(parent), shellQuote(base))
		}

		if err := session.Start(cmd); err != nil {
			session.Close()
			conn.Release()
			return nil, "", fmt.Errorf("start tar: %w", err)
		}

		reader := &execReadCloser{
			reader:  stdout,
			session: session,
			conn:    conn,
		}
		return reader, archiveName, nil
	}

	conn.Release()
	return nil, "", errors.New("no client available")
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

	if conn.IsLocal() {
		dir := filepath.Dir(cleanPath)
		_ = os.MkdirAll(dir, 0755)
		perm := os.FileMode(mode)
		if perm == 0 {
			perm = 0644
		}
		f, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
		if err != nil {
			return err
		}
		defer f.Close()
		_, copyErr := io.Copy(f, r)
		return copyErr
	}

	if conn.Client() != nil {
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
		cmd := fmt.Sprintf("sudo mkdir -p %s && sudo tee %s >/dev/null", shellQuote(dir), shellQuote(cleanPath))
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

	return errors.New("no client available")
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

	if srcConn.Client() == nil || dstConn.Client() == nil {
		return nil, errors.New("cross-server transfer requires remote SSH on both servers")
	}

	srcSession, err := srcConn.Client().NewSession()
	if err != nil {
		return nil, fmt.Errorf("source session: %w", err)
	}
	defer srcSession.Close()

	dstSession, err := dstConn.Client().NewSession()
	if err != nil {
		return nil, fmt.Errorf("target session: %w", err)
	}
	defer dstSession.Close()

	srcStdout, err := srcSession.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("source stdout: %w", err)
	}

	dstStdin, err := dstSession.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("target stdin: %w", err)
	}

	srcParent := filepath.Dir(cleanSrc)
	srcBase := filepath.Base(cleanSrc)
	if cleanSrc == "/" {
		srcParent = "/"
		srcBase = "."
	}

	srcCmd := fmt.Sprintf("sudo tar -czf - -C %s %s", shellQuote(srcParent), shellQuote(srcBase))
	dstCmd := fmt.Sprintf("sudo mkdir -p %s && sudo tar -xzf - -C %s", shellQuote(cleanDst), shellQuote(cleanDst))

	start := time.Now()

	if err := dstSession.Start(dstCmd); err != nil {
		return nil, fmt.Errorf("start target tar: %w", err)
	}
	if err := srcSession.Start(srcCmd); err != nil {
		_ = dstStdin.Close()
		return nil, fmt.Errorf("start source tar: %w", err)
	}

	copiedBytes, copyErr := io.Copy(dstStdin, srcStdout)
	_ = dstStdin.Close()

	srcWaitErr := srcSession.Wait()
	dstWaitErr := dstSession.Wait()

	duration := time.Since(start).Milliseconds()

	if copyErr != nil {
		return nil, fmt.Errorf("pipe transfer: %w", copyErr)
	}
	if srcWaitErr != nil {
		return nil, fmt.Errorf("source tar failed: %w", srcWaitErr)
	}
	if dstWaitErr != nil {
		return nil, fmt.Errorf("target extraction failed: %w", dstWaitErr)
	}

	return &FileTransferResult{
		SourceServerID: srcServer.ID,
		TargetServerID: dstServer.ID,
		SourcePath:     cleanSrc,
		TargetPath:     cleanDst,
		BytesCopied:    copiedBytes,
		DurationMs:     duration,
	}, nil
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

type localCmdReadCloser struct {
	reader io.Reader
	cmd    *exec.Cmd
}

func (r *localCmdReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *localCmdReadCloser) Close() error {
	return r.cmd.Wait()
}
