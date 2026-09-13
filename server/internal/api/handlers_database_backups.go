package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DatabaseEngine string

const (
	EnginePostgres DatabaseEngine = "postgres"
	EngineMySQL    DatabaseEngine = "mysql"
	EngineMongoDB  DatabaseEngine = "mongo"
	EngineRedis    DatabaseEngine = "redis"
	EngineSQLite   DatabaseEngine = "sqlite"
)

type DatabaseExecutionMode string

const (
	ModeContainer DatabaseExecutionMode = "container"
	ModeHost      DatabaseExecutionMode = "host"
)

type DatabaseBackupFile struct {
	Filename     string         `json:"filename"`
	Path         string         `json:"path"`
	SizeBytes    int64          `json:"size_bytes"`
	Engine       DatabaseEngine `json:"engine"`
	DatabaseName string         `json:"database_name"`
	Target       string         `json:"target"`
	CreatedAt    time.Time      `json:"created_at"`
}

type DiscoveredDatabaseContainer struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Image  string         `json:"image"`
	Engine DatabaseEngine `json:"engine"`
	Status string         `json:"status"`
}

type listDatabaseBackupsResponse struct {
	BackupDir  string                        `json:"backup_dir"`
	Backups    []DatabaseBackupFile          `json:"backups"`
	Containers []DiscoveredDatabaseContainer `json:"containers"`
}

type createDatabaseBackupRequest struct {
	Engine        DatabaseEngine        `json:"engine"`
	Mode          DatabaseExecutionMode `json:"mode"` // "container" or "host"
	ContainerName string                `json:"container_name"`
	DatabaseName  string                `json:"database_name"`
	Username      string                `json:"username"`
	Password      string                `json:"password"`
	AuthDatabase  string                `json:"auth_database"` // for mongo
	SQLitePath    string                `json:"sqlite_path"`
	BackupDir     string                `json:"backup_dir"`
	SudoPassword  string                `json:"sudo_password"`
	SaveSudo      bool                  `json:"save_sudo"`
}

type createDatabaseBackupResponse struct {
	Success    bool                `json:"success"`
	BackupFile *DatabaseBackupFile `json:"backup_file,omitempty"`
	Stdout     string              `json:"stdout"`
	Stderr     string              `json:"stderr"`
	ExitCode   int                 `json:"exit_code"`
	DurationMs int64               `json:"duration_ms"`
}

type restoreDatabaseBackupRequest struct {
	Engine        DatabaseEngine        `json:"engine"`
	Mode          DatabaseExecutionMode `json:"mode"`
	ContainerName string                `json:"container_name"`
	DatabaseName  string                `json:"database_name"`
	Username      string                `json:"username"`
	Password      string                `json:"password"`
	AuthDatabase  string                `json:"auth_database"`
	SQLitePath    string                `json:"sqlite_path"`
	BackupPath    string                `json:"backup_path"`
	DropExisting  bool                  `json:"drop_existing"`
	SudoPassword  string                `json:"sudo_password"`
	SaveSudo      bool                  `json:"save_sudo"`
}

type restoreDatabaseBackupResponse struct {
	Success    bool   `json:"success"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

type deleteDatabaseBackupRequest struct {
	Path         string `json:"path"`
	SudoPassword string `json:"sudo_password"`
	SaveSudo     bool   `json:"save_sudo"`
}

const defaultDatabaseBackupDir = "/var/backups/dockdeploy/databases"

var safeIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

func sanitizeIdentifier(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "db"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func (s *Server) handleListDatabaseBackups(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	backupDir := strings.TrimSpace(r.URL.Query().Get("backup_dir"))
	if backupDir == "" {
		backupDir = defaultDatabaseBackupDir
	}

	conn, err := s.Servers.Connect(r.Context(), server)
	if err != nil {
		return Internal(fmt.Errorf("connect to server: %w", err))
	}
	defer conn.Release()

	// 1. Discover database containers if Docker is available
	var dbContainers []DiscoveredDatabaseContainer
	session, sErr := s.Servers.Session(r.Context(), server)
	if sErr == nil {
		defer session.Close()
		containers, cErr := session.Docker.ListContainers(r.Context())
		if cErr == nil {
			for _, c := range containers {
				imgLower := strings.ToLower(c.Image)
				var eng DatabaseEngine
				switch {
				case strings.Contains(imgLower, "postgres") || strings.Contains(imgLower, "timescale"):
					eng = EnginePostgres
				case strings.Contains(imgLower, "mysql") || strings.Contains(imgLower, "mariadb") || strings.Contains(imgLower, "percona"):
					eng = EngineMySQL
				case strings.Contains(imgLower, "mongo"):
					eng = EngineMongoDB
				case strings.Contains(imgLower, "redis") || strings.Contains(imgLower, "keydb"):
					eng = EngineRedis
				}

				if eng != "" {
					dbContainers = append(dbContainers, DiscoveredDatabaseContainer{
						ID:     c.ID,
						Name:   c.Name,
						Image:  c.Image,
						Engine: eng,
						Status: c.Status,
					})
				}
			}
		}
	}

	// 2. Discover existing backup files in backupDir
	listScript := fmt.Sprintf(`mkdir -p %s && find %s -maxdepth 1 -type f -exec stat -c "%%n|%%s|%%Y" {} + 2>/dev/null || true`,
		shellQuote(backupDir), shellQuote(backupDir))

	res, runErr := conn.Run(r.Context(), listScript)
	var backupFiles []DatabaseBackupFile
	if runErr == nil && res.Ok() {
		lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "|")
			if len(parts) < 3 {
				continue
			}
			filePath := parts[0]
			size, _ := strconv.ParseInt(parts[1], 10, 64)
			mtimeSec, _ := strconv.ParseInt(parts[2], 10, 64)
			createdAt := time.Unix(mtimeSec, 0)
			filename := filepath.Base(filePath)

			// Infer engine from filename: <engine>_<dbname_or_target>_<date>_<time>.<ext>
			var eng DatabaseEngine
			dbName := "database"
			lower := strings.ToLower(filename)
			switch {
			case strings.HasPrefix(lower, "postgres"):
				eng = EnginePostgres
			case strings.HasPrefix(lower, "mysql") || strings.HasPrefix(lower, "mariadb"):
				eng = EngineMySQL
			case strings.HasPrefix(lower, "mongo"):
				eng = EngineMongoDB
			case strings.HasPrefix(lower, "redis"):
				eng = EngineRedis
			case strings.HasPrefix(lower, "sqlite"):
				eng = EngineSQLite
			default:
				eng = EnginePostgres
			}

			// Parse database name from middle if possible
			subParts := strings.Split(filename, "_")
			if len(subParts) >= 3 {
				dbName = strings.Join(subParts[1:len(subParts)-2], "_")
			}

			backupFiles = append(backupFiles, DatabaseBackupFile{
				Filename:     filename,
				Path:         filePath,
				SizeBytes:    size,
				Engine:       eng,
				DatabaseName: dbName,
				CreatedAt:    createdAt,
			})
		}
	}

	// Sort backups newest first
	for i := 0; i < len(backupFiles); i++ {
		for j := i + 1; j < len(backupFiles); j++ {
			if backupFiles[i].CreatedAt.Before(backupFiles[j].CreatedAt) {
				backupFiles[i], backupFiles[j] = backupFiles[j], backupFiles[i]
			}
		}
	}

	if backupFiles == nil {
		backupFiles = []DatabaseBackupFile{}
	}
	if dbContainers == nil {
		dbContainers = []DiscoveredDatabaseContainer{}
	}

	return JSON(w, s.Log, http.StatusOK, listDatabaseBackupsResponse{
		BackupDir:  backupDir,
		Backups:    backupFiles,
		Containers: dbContainers,
	})
}

func (s *Server) handleCreateDatabaseBackup(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req createDatabaseBackupRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	if req.Engine == "" {
		f.add("engine", "Database engine is required.")
	}
	if req.Mode == "" {
		req.Mode = ModeContainer
	}
	if req.Mode == ModeContainer && req.ContainerName == "" && req.Engine != EngineSQLite {
		f.add("container_name", "Container name is required for container mode.")
	}
	if req.Engine == EngineSQLite && req.SQLitePath == "" {
		f.add("sqlite_path", "SQLite database file path is required.")
	}
	if err := f.err(); err != nil {
		return err
	}

	backupDir := strings.TrimSpace(req.BackupDir)
	if backupDir == "" {
		backupDir = defaultDatabaseBackupDir
	}

	conn, err := s.Servers.Connect(r.Context(), server)
	if err != nil {
		return Internal(fmt.Errorf("connect to server: %w", err))
	}
	defer conn.Release()

	// Timeout for taking database backup (up to 15 minutes)
	backupCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Minute)
	defer cancel()

	start := time.Now()
	timestamp := time.Now().UTC().Format("2006-01-02_150405")

	var ext string
	switch req.Engine {
	case EnginePostgres, EngineMySQL:
		ext = "sql.gz"
	case EngineMongoDB:
		ext = "archive.gz"
	case EngineRedis:
		ext = "rdb"
	case EngineSQLite:
		ext = "db"
	default:
		ext = "bak"
	}

	targetName := req.DatabaseName
	if targetName == "" {
		if req.Engine == EngineSQLite {
			targetName = filepath.Base(req.SQLitePath)
		} else if req.ContainerName != "" {
			targetName = req.ContainerName
		} else {
			targetName = "all"
		}
	}
	safeTarget := sanitizeIdentifier(targetName)
	filename := fmt.Sprintf("%s_%s_%s.%s", req.Engine, safeTarget, timestamp, ext)
	destPath := filepath.Join(backupDir, filename)

	// Build shell command per engine & mode
	var backupCmd string

	switch req.Engine {
	case EnginePostgres:
		user := req.Username
		if user == "" {
			user = "postgres"
		}
		pgPassEnv := ""
		if req.Password != "" {
			pgPassEnv = fmt.Sprintf("PGPASSWORD=%s ", shellQuote(req.Password))
		}

		if req.Mode == ModeContainer {
			if req.DatabaseName == "" || req.DatabaseName == "all" {
				backupCmd = fmt.Sprintf(`docker exec -i %s env %spg_dumpall -U %s | gzip -9 > %s`,
					shellQuote(req.ContainerName), pgPassEnv, shellQuote(user), shellQuote(destPath))
			} else {
				backupCmd = fmt.Sprintf(`docker exec -i %s env %spg_dump -U %s -d %s -F p --clean --if-exists | gzip -9 > %s`,
					shellQuote(req.ContainerName), pgPassEnv, shellQuote(user), shellQuote(req.DatabaseName), shellQuote(destPath))
			}
		} else {
			if req.DatabaseName == "" || req.DatabaseName == "all" {
				backupCmd = fmt.Sprintf(`env %spg_dumpall -U %s | gzip -9 > %s`,
					pgPassEnv, shellQuote(user), shellQuote(destPath))
			} else {
				backupCmd = fmt.Sprintf(`env %spg_dump -U %s -d %s -F p --clean --if-exists | gzip -9 > %s`,
					pgPassEnv, shellQuote(user), shellQuote(req.DatabaseName), shellQuote(destPath))
			}
		}

	case EngineMySQL:
		user := req.Username
		if user == "" {
			user = "root"
		}
		passFlag := ""
		if req.Password != "" {
			passFlag = fmt.Sprintf("-p%s", shellQuote(req.Password))
		}

		if req.Mode == ModeContainer {
			if req.DatabaseName == "" || req.DatabaseName == "all" {
				backupCmd = fmt.Sprintf(`docker exec -i %s mysqldump --single-transaction --quick --routines --triggers --all-databases -u%s %s | gzip -9 > %s`,
					shellQuote(req.ContainerName), shellQuote(user), passFlag, shellQuote(destPath))
			} else {
				backupCmd = fmt.Sprintf(`docker exec -i %s mysqldump --single-transaction --quick --routines --triggers -u%s %s %s | gzip -9 > %s`,
					shellQuote(req.ContainerName), shellQuote(user), passFlag, shellQuote(req.DatabaseName), shellQuote(destPath))
			}
		} else {
			if req.DatabaseName == "" || req.DatabaseName == "all" {
				backupCmd = fmt.Sprintf(`mysqldump --single-transaction --quick --routines --triggers --all-databases -u%s %s | gzip -9 > %s`,
					shellQuote(user), passFlag, shellQuote(destPath))
			} else {
				backupCmd = fmt.Sprintf(`mysqldump --single-transaction --quick --routines --triggers -u%s %s %s | gzip -9 > %s`,
					shellQuote(user), passFlag, shellQuote(req.DatabaseName), shellQuote(destPath))
			}
		}

	case EngineMongoDB:
		authFlags := ""
		if req.Username != "" {
			authFlags += fmt.Sprintf(" -u %s", shellQuote(req.Username))
		}
		if req.Password != "" {
			authFlags += fmt.Sprintf(" -p %s", shellQuote(req.Password))
		}
		if req.AuthDatabase != "" {
			authFlags += fmt.Sprintf(" --authenticationDatabase %s", shellQuote(req.AuthDatabase))
		}
		dbFlag := ""
		if req.DatabaseName != "" && req.DatabaseName != "all" {
			dbFlag = fmt.Sprintf(" --db %s", shellQuote(req.DatabaseName))
		}

		if req.Mode == ModeContainer {
			backupCmd = fmt.Sprintf(`docker exec -i %s mongodump --archive --gzip%s%s > %s`,
				shellQuote(req.ContainerName), dbFlag, authFlags, shellQuote(destPath))
		} else {
			backupCmd = fmt.Sprintf(`mongodump --archive=%s --gzip%s%s`,
				shellQuote(destPath), dbFlag, authFlags)
		}

	case EngineRedis:
		authFlag := ""
		if req.Password != "" {
			authFlag = fmt.Sprintf(" -a %s", shellQuote(req.Password))
		}

		if req.Mode == ModeContainer {
			backupCmd = fmt.Sprintf(`docker exec -i %s redis-cli%s bgsave && sleep 2 && docker cp %s:/data/dump.rdb %s`,
				shellQuote(req.ContainerName), authFlag, shellQuote(req.ContainerName), shellQuote(destPath))
		} else {
			backupCmd = fmt.Sprintf(`redis-cli%s bgsave && sleep 2 && cp /var/lib/redis/dump.rdb %s`,
				authFlag, shellQuote(destPath))
		}

	case EngineSQLite:
		sqliteFile := req.SQLitePath
		backupCmd = fmt.Sprintf(`sqlite3 %s ".backup '%s'" 2>/dev/null || cp -f %s %s`,
			shellQuote(sqliteFile), shellQuote(destPath), shellQuote(sqliteFile), shellQuote(destPath))
	}

	// Ensure destination directory exists and run elevated
	fullScript := fmt.Sprintf(`mkdir -p %s && chmod 700 %s && %s`,
		shellQuote(backupDir), shellQuote(backupDir), backupCmd)

	res, runErr := s.runElevated(backupCtx, server, conn, fullScript, req.SudoPassword, req.SaveSudo)
	duration := time.Since(start).Milliseconds()

	if runErr != nil {
		var fErr *Error
		if errors.As(runErr, &fErr) {
			return runErr
		}
		return Internal(runErr)
	}

	AuditResource(r.Context(), "servers", server.ID)
	AuditMeta(r.Context(), "action", "create_database_backup")
	AuditMeta(r.Context(), "engine", string(req.Engine))
	AuditMeta(r.Context(), "dest", destPath)
	AuditMeta(r.Context(), "success", res.Ok())

	var backupFile *DatabaseBackupFile
	if res.Ok() {
		// Read created file size
		sizeRes, _ := conn.Run(r.Context(), fmt.Sprintf("stat -c '%%s' %s 2>/dev/null", shellQuote(destPath)))
		size, _ := strconv.ParseInt(strings.TrimSpace(sizeRes.Stdout), 10, 64)

		backupFile = &DatabaseBackupFile{
			Filename:     filename,
			Path:         destPath,
			SizeBytes:    size,
			Engine:       req.Engine,
			DatabaseName: targetName,
			CreatedAt:    time.Now().UTC(),
		}
	}

	return JSON(w, s.Log, http.StatusOK, createDatabaseBackupResponse{
		Success:    res.Ok(),
		BackupFile: backupFile,
		Stdout:     res.Stdout,
		Stderr:     res.Stderr,
		ExitCode:   res.ExitCode,
		DurationMs: duration,
	})
}

func (s *Server) handleRestoreDatabaseBackup(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req restoreDatabaseBackupRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	if req.Engine == "" {
		f.add("engine", "Database engine is required.")
	}
	if req.BackupPath == "" {
		f.add("backup_path", "Backup file path on server is required.")
	}
	if req.Mode == "" {
		req.Mode = ModeContainer
	}
	if req.Mode == ModeContainer && req.ContainerName == "" && req.Engine != EngineSQLite {
		f.add("container_name", "Container name is required for container mode.")
	}
	if req.Engine == EngineSQLite && req.SQLitePath == "" {
		f.add("sqlite_path", "SQLite database path is required.")
	}
	if err := f.err(); err != nil {
		return err
	}

	conn, err := s.Servers.Connect(r.Context(), server)
	if err != nil {
		return Internal(fmt.Errorf("connect to server: %w", err))
	}
	defer conn.Release()

	// Verify backup file exists
	checkRes, _ := conn.Run(r.Context(), fmt.Sprintf("test -f %s && echo 'EXISTS'", shellQuote(req.BackupPath)))
	if !strings.Contains(checkRes.Stdout, "EXISTS") {
		return Invalid(fields{"backup_path": fmt.Sprintf("Backup file '%s' does not exist on the server.", req.BackupPath)})
	}

	// Timeout for restoring database (up to 20 minutes)
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Minute)
	defer cancel()

	start := time.Now()
	var restoreCmd string

	switch req.Engine {
	case EnginePostgres:
		user := req.Username
		if user == "" {
			user = "postgres"
		}
		pgPassEnv := ""
		if req.Password != "" {
			pgPassEnv = fmt.Sprintf("PGPASSWORD=%s ", shellQuote(req.Password))
		}
		db := req.DatabaseName
		if db == "" {
			db = "postgres"
		}

		if req.Mode == ModeContainer {
			restoreCmd = fmt.Sprintf(`gunzip -c %s | docker exec -i %s env %spsql -U %s -d %s`,
				shellQuote(req.BackupPath), shellQuote(req.ContainerName), pgPassEnv, shellQuote(user), shellQuote(db))
		} else {
			restoreCmd = fmt.Sprintf(`gunzip -c %s | env %spsql -U %s -d %s`,
				shellQuote(req.BackupPath), pgPassEnv, shellQuote(user), shellQuote(db))
		}

	case EngineMySQL:
		user := req.Username
		if user == "" {
			user = "root"
		}
		passFlag := ""
		if req.Password != "" {
			passFlag = fmt.Sprintf("-p%s", shellQuote(req.Password))
		}
		db := req.DatabaseName

		if req.Mode == ModeContainer {
			if db != "" {
				restoreCmd = fmt.Sprintf(`gunzip -c %s | docker exec -i %s mysql -u%s %s %s`,
					shellQuote(req.BackupPath), shellQuote(req.ContainerName), shellQuote(user), passFlag, shellQuote(db))
			} else {
				restoreCmd = fmt.Sprintf(`gunzip -c %s | docker exec -i %s mysql -u%s %s`,
					shellQuote(req.BackupPath), shellQuote(req.ContainerName), shellQuote(user), passFlag)
			}
		} else {
			if db != "" {
				restoreCmd = fmt.Sprintf(`gunzip -c %s | mysql -u%s %s %s`,
					shellQuote(req.BackupPath), shellQuote(user), passFlag, shellQuote(db))
			} else {
				restoreCmd = fmt.Sprintf(`gunzip -c %s | mysql -u%s %s`,
					shellQuote(req.BackupPath), shellQuote(user), passFlag)
			}
		}

	case EngineMongoDB:
		authFlags := ""
		if req.Username != "" {
			authFlags += fmt.Sprintf(" -u %s", shellQuote(req.Username))
		}
		if req.Password != "" {
			authFlags += fmt.Sprintf(" -p %s", shellQuote(req.Password))
		}
		if req.AuthDatabase != "" {
			authFlags += fmt.Sprintf(" --authenticationDatabase %s", shellQuote(req.AuthDatabase))
		}
		dropFlag := ""
		if req.DropExisting {
			dropFlag = " --drop"
		}
		dbFlag := ""
		if req.DatabaseName != "" && req.DatabaseName != "all" {
			dbFlag = fmt.Sprintf(" --nsInclude=%s.*", shellQuote(req.DatabaseName))
		}

		if req.Mode == ModeContainer {
			restoreCmd = fmt.Sprintf(`docker exec -i %s mongorestore --archive --gzip%s%s%s < %s`,
				shellQuote(req.ContainerName), dropFlag, dbFlag, authFlags, shellQuote(req.BackupPath))
		} else {
			restoreCmd = fmt.Sprintf(`mongorestore --archive=%s --gzip%s%s%s`,
				shellQuote(req.BackupPath), dropFlag, dbFlag, authFlags)
		}

	case EngineRedis:
		if req.Mode == ModeContainer {
			restoreCmd = fmt.Sprintf(`docker stop %s && docker cp %s %s:/data/dump.rdb && docker start %s`,
				shellQuote(req.ContainerName), shellQuote(req.BackupPath), shellQuote(req.ContainerName), shellQuote(req.ContainerName))
		} else {
			restoreCmd = fmt.Sprintf(`(systemctl stop redis || systemctl stop redis-server || true) && cp -f %s /var/lib/redis/dump.rdb && (chown redis:redis /var/lib/redis/dump.rdb 2>/dev/null || true) && (systemctl start redis || systemctl start redis-server || true)`,
				shellQuote(req.BackupPath))
		}

	case EngineSQLite:
		sqliteFile := req.SQLitePath
		// Make safety pre-restore backup first
		restoreCmd = fmt.Sprintf(`cp -f %s %s.pre-restore.bak 2>/dev/null || true; cp -f %s %s`,
			shellQuote(sqliteFile), shellQuote(sqliteFile), shellQuote(req.BackupPath), shellQuote(sqliteFile))
	}

	res, runErr := s.runElevated(restoreCtx, server, conn, restoreCmd, req.SudoPassword, req.SaveSudo)
	duration := time.Since(start).Milliseconds()

	if runErr != nil {
		var fErr *Error
		if errors.As(runErr, &fErr) {
			return runErr
		}
		return Internal(runErr)
	}

	AuditResource(r.Context(), "servers", server.ID)
	AuditMeta(r.Context(), "action", "restore_database_backup")
	AuditMeta(r.Context(), "engine", string(req.Engine))
	AuditMeta(r.Context(), "backup_path", req.BackupPath)
	AuditMeta(r.Context(), "success", res.Ok())

	return JSON(w, s.Log, http.StatusOK, restoreDatabaseBackupResponse{
		Success:    res.Ok(),
		Stdout:     res.Stdout,
		Stderr:     res.Stderr,
		ExitCode:   res.ExitCode,
		DurationMs: duration,
	})
}

func (s *Server) handleDeleteDatabaseBackup(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req deleteDatabaseBackupRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return Invalid(fields{"path": "Backup path is required."})
	}

	// Security guard: prevent deleting files outside /var/backups
	cleanPath := filepath.Clean(path)
	if !strings.HasPrefix(cleanPath, "/var/backups/") && !strings.Contains(cleanPath, "dockdeploy") {
		return Invalid(fields{"path": "Deletion is only allowed for files inside the backup directory."})
	}

	conn, err := s.Servers.Connect(r.Context(), server)
	if err != nil {
		return Internal(fmt.Errorf("connect to server: %w", err))
	}
	defer conn.Release()

	cmd := fmt.Sprintf("rm -f %s", shellQuote(cleanPath))
	res, runErr := s.runElevated(r.Context(), server, conn, cmd, req.SudoPassword, req.SaveSudo)
	if runErr != nil {
		var fErr *Error
		if errors.As(runErr, &fErr) {
			return runErr
		}
		return Internal(runErr)
	}

	AuditResource(r.Context(), "servers", server.ID)
	AuditMeta(r.Context(), "action", "delete_database_backup")
	AuditMeta(r.Context(), "path", cleanPath)

	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"deleted": cleanPath,
		"success": res.Ok(),
	})
}
