#!/usr/bin/env bash
# ==============================================================================
# Database Backup & Recovery Utility
# Engines supported: PostgreSQL, MySQL/MariaDB, MongoDB, Redis, SQLite
# Modes: Container (Docker) & Host Native
# ==============================================================================
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

DEFAULT_BACKUP_DIR="/var/backups/dockdeploy/databases"
BACKUP_DIR="${BACKUP_DIR:-$DEFAULT_BACKUP_DIR}"

usage() {
    cat <<EOF
Usage: $(basename "$0") <command> [options]

Commands:
  backup          Create a new database backup
  restore         Restore a database from a backup archive
  list            List all available database backups
  prune           Remove backups older than N days

Global Options:
  -d, --dir <path>          Backup directory (default: ${BACKUP_DIR})
  -h, --help                Show this help message

Backup Options:
  -e, --engine <engine>     Database engine: postgres, mysql, mongo, redis, sqlite
  -m, --mode <mode>         Execution mode: container (default) or host
  -c, --container <name>    Container name or ID (required for container mode)
  -n, --database <dbname>   Database name (default: postgres for pg, all for mysql/mongo)
  -u, --user <username>     Database user (default: postgres/root/admin)
  -p, --password <pass>     Database password (or set via environment variable)
  --auth-db <authdb>        MongoDB authentication database (default: admin)
  --sqlite-path <path>      Path to SQLite database file on server
  --all                     Dump all databases (Postgres pg_dumpall, MySQL --all-databases)

Restore Options:
  -f, --file <path>         Path to backup file to restore
  -e, --engine <engine>     Database engine (auto-detected from filename if omitted)
  -m, --mode <mode>         Execution mode: container (default) or host
  -c, --container <name>    Target container name (for container mode)
  -n, --database <dbname>   Target database name
  -u, --user <username>     Database user
  -p, --password <pass>     Database password
  --auth-db <authdb>        MongoDB authentication database
  --sqlite-path <path>      Target path for SQLite database
  --drop                    (MongoDB) Drop existing collections before restoring
  --force                   Bypass confirmation prompts

Prune Options:
  --retention-days <days>   Keep backups within last N days (default: 30)

Examples:
  # Backup PostgreSQL container:
  $(basename "$0") backup -e postgres -c my-postgres-container -n mydb -u postgres

  # Backup MySQL container:
  $(basename "$0") backup -e mysql -c my-mysql-container -n mydb -u root -p secret

  # Backup MongoDB container:
  $(basename "$0") backup -e mongo -c my-mongo-container --auth-db admin -u admin -p secret

  # Backup Redis container:
  $(basename "$0") backup -e redis -c my-redis-container

  # Backup SQLite database:
  $(basename "$0") backup -e sqlite --sqlite-path /app/data/prod.db

  # List backups:
  $(basename "$0") list

  # Restore PostgreSQL backup:
  $(basename "$0") restore -f /var/backups/dockdeploy/databases/postgres_mydb_2026-09-13.sql.gz -c my-postgres-container -n mydb -u postgres --force
EOF
    exit 0
}

ensure_backup_dir() {
    if [[ ! -d "$BACKUP_DIR" ]]; then
        if mkdir -p "$BACKUP_DIR" 2>/dev/null; then
            chmod 700 "$BACKUP_DIR" 2>/dev/null || true
            return 0
        fi
        if [[ $EUID -eq 0 ]]; then
            mkdir -p "$BACKUP_DIR"
            chmod 700 "$BACKUP_DIR"
        elif command -v sudo >/dev/null 2>&1; then
            sudo mkdir -p "$BACKUP_DIR"
            sudo chmod 700 "$BACKUP_DIR"
            sudo chown "$(id -u):$(id -g)" "$BACKUP_DIR" 2>/dev/null || true
        else
            echo -e "${RED}Failed to create backup directory: ${BACKUP_DIR}${NC}" >&2
            exit 1
        fi
    fi
}

cmd_list() {
    ensure_backup_dir
    echo -e "${BLUE}==> Backups in: ${CYAN}${BACKUP_DIR}${NC}"
    if [[ ! -d "$BACKUP_DIR" ]] || [[ -z "$(ls -A "$BACKUP_DIR" 2>/dev/null)" ]]; then
        echo -e "${YELLOW}No database backups found.${NC}"
        return 0
    fi

    printf "%-40s %-12s %-20s\n" "FILENAME" "SIZE" "MODIFIED"
    printf "%-40s %-12s %-20s\n" "----------------------------------------" "------------" "--------------------"
    
    for file in "$BACKUP_DIR"/*; do
        [[ -f "$file" ]] || continue
        local fname
        fname=$(basename "$file")
        local size
        size=$(ls -lh "$file" | awk '{print $5}')
        local mod
        mod=$(date -r "$file" "+%Y-%m-%d %H:%M" 2>/dev/null || stat -c "%y" "$file" 2>/dev/null | cut -d'.' -f1 || echo "unknown")
        printf "%-42s %-10s %-20s\n" "$fname" "$size" "$mod"
    done
}

cmd_prune() {
    ensure_backup_dir
    local days="${RETENTION_DAYS:-30}"
    echo -e "${BLUE}==> Pruning backups older than ${days} days in ${BACKUP_DIR}...${NC}"
    local count
    count=$(find "$BACKUP_DIR" -maxdepth 1 -type f -mtime "+$days" | wc -l)
    if [[ "$count" -eq 0 ]]; then
        echo -e "${GREEN}No obsolete backups found.${NC}"
        return 0
    fi

    find "$BACKUP_DIR" -maxdepth 1 -type f -mtime "+$days" -exec rm -v {} \;
    echo -e "${GREEN}Pruned $count old backup file(s).${NC}"
}

cmd_backup() {
    ensure_backup_dir
    local timestamp
    timestamp=$(date +"%Y%m%d_%H%M%S")

    case "$ENGINE" in
        postgres)
            local target_name="${DB_NAME:-all}"
            local filename="postgres_${target_name}_${timestamp}.sql.gz"
            local filepath="${BACKUP_DIR}/${filename}"
            local pguser="${DB_USER:-postgres}"

            echo -e "${BLUE}==> Starting PostgreSQL backup...${NC}"
            if [[ "$MODE" == "container" ]]; then
                [[ -z "$CONTAINER" ]] && { echo -e "${RED}Error: --container required for container mode${NC}" >&2; exit 1; }
                local env_pass=()
                [[ -n "$DB_PASS" ]] && env_pass=(-e "PGPASSWORD=${DB_PASS}")

                if [[ "$ALL_DATABASES" == true ]] || [[ -z "${DB_NAME:-}" ]]; then
                    docker exec -i "${env_pass[@]}" "$CONTAINER" pg_dumpall -U "$pguser" | gzip > "$filepath"
                else
                    docker exec -i "${env_pass[@]}" "$CONTAINER" pg_dump -U "$pguser" "$DB_NAME" | gzip > "$filepath"
                fi
            else
                local env_pass=()
                [[ -n "$DB_PASS" ]] && env_pass=(PGPASSWORD="$DB_PASS")

                if [[ "$ALL_DATABASES" == true ]] || [[ -z "${DB_NAME:-}" ]]; then
                    env "${env_pass[@]}" pg_dumpall -U "$pguser" | gzip > "$filepath"
                else
                    env "${env_pass[@]}" pg_dump -U "$pguser" "$DB_NAME" | gzip > "$filepath"
                fi
            fi
            ;;

        mysql)
            local target_name="${DB_NAME:-all}"
            local filename="mysql_${target_name}_${timestamp}.sql.gz"
            local filepath="${BACKUP_DIR}/${filename}"
            local myuser="${DB_USER:-root}"

            echo -e "${BLUE}==> Starting MySQL/MariaDB backup...${NC}"
            local pass_flag=()
            [[ -n "$DB_PASS" ]] && pass_flag=("-p${DB_PASS}")

            if [[ "$MODE" == "container" ]]; then
                [[ -z "$CONTAINER" ]] && { echo -e "${RED}Error: --container required for container mode${NC}" >&2; exit 1; }
                if [[ "$ALL_DATABASES" == true ]] || [[ -z "${DB_NAME:-}" ]]; then
                    docker exec -i "$CONTAINER" mysqldump -u "$myuser" "${pass_flag[@]}" --all-databases --single-transaction --quick | gzip > "$filepath"
                else
                    docker exec -i "$CONTAINER" mysqldump -u "$myuser" "${pass_flag[@]}" "$DB_NAME" --single-transaction --quick | gzip > "$filepath"
                fi
            else
                if [[ "$ALL_DATABASES" == true ]] || [[ -z "${DB_NAME:-}" ]]; then
                    mysqldump -u "$myuser" "${pass_flag[@]}" --all-databases --single-transaction --quick | gzip > "$filepath"
                else
                    mysqldump -u "$myuser" "${pass_flag[@]}" "$DB_NAME" --single-transaction --quick | gzip > "$filepath"
                fi
            fi
            ;;

        mongo)
            local target_name="${DB_NAME:-all}"
            local filename="mongo_${target_name}_${timestamp}.archive.gz"
            local filepath="${BACKUP_DIR}/${filename}"
            local auth_args=()
            [[ -n "${DB_USER:-}" ]] && auth_args+=(--username "$DB_USER")
            [[ -n "${DB_PASS:-}" ]] && auth_args+=(--password "$DB_PASS")
            auth_args+=(--authenticationDatabase "${AUTH_DB:-admin}")

            local db_args=()
            [[ -n "${DB_NAME:-}" ]] && db_args+=(--db "$DB_NAME")

            echo -e "${BLUE}==> Starting MongoDB backup...${NC}"
            if [[ "$MODE" == "container" ]]; then
                [[ -z "$CONTAINER" ]] && { echo -e "${RED}Error: --container required for container mode${NC}" >&2; exit 1; }
                docker exec -i "$CONTAINER" mongodump "${auth_args[@]}" "${db_args[@]}" --archive --gzip > "$filepath"
            else
                mongodump "${auth_args[@]}" "${db_args[@]}" --archive --gzip > "$filepath"
            fi
            ;;

        redis)
            local filename="redis_${CONTAINER:-host}_${timestamp}.rdb"
            local filepath="${BACKUP_DIR}/${filename}"

            echo -e "${BLUE}==> Starting Redis backup (triggering BGSAVE)...${NC}"
            local cli_auth=()
            [[ -n "$DB_PASS" ]] && cli_auth=(-a "$DB_PASS")

            if [[ "$MODE" == "container" ]]; then
                [[ -z "$CONTAINER" ]] && { echo -e "${RED}Error: --container required for container mode${NC}" >&2; exit 1; }
                
                # Get last save timestamp
                local last_save
                last_save=$(docker exec "$CONTAINER" redis-cli "${cli_auth[@]}" LASTSAVE 2>/dev/null | tr -d '\r\n')
                docker exec "$CONTAINER" redis-cli "${cli_auth[@]}" BGSAVE >/dev/null

                # Wait up to 30 seconds for background save to finish
                local retries=0
                while [[ $retries -lt 30 ]]; do
                    sleep 1
                    local current_save
                    current_save=$(docker exec "$CONTAINER" redis-cli "${cli_auth[@]}" LASTSAVE 2>/dev/null | tr -d '\r\n')
                    if [[ "$current_save" -gt "$last_save" ]]; then
                        break
                    fi
                    ((retries++))
                done

                # Determine rdb path inside container
                local rdb_dir
                rdb_dir=$(docker exec "$CONTAINER" redis-cli "${cli_auth[@]}" config get dir 2>/dev/null | tail -n1 | tr -d '\r\n')
                local rdb_file
                rdb_file=$(docker exec "$CONTAINER" redis-cli "${cli_auth[@]}" config get dbfilename 2>/dev/null | tail -n1 | tr -d '\r\n')
                [[ -z "$rdb_dir" ]] && rdb_dir="/data"
                [[ -z "$rdb_file" ]] && rdb_file="dump.rdb"

                docker cp "${CONTAINER}:${rdb_dir}/${rdb_file}" "$filepath"
            else
                redis-cli "${cli_auth[@]}" bgsave
                sleep 2
                cp /var/lib/redis/dump.rdb "$filepath"
            fi
            ;;

        sqlite)
            [[ -z "$SQLITE_PATH" ]] && { echo -e "${RED}Error: --sqlite-path required for sqlite backup${NC}" >&2; exit 1; }
            [[ ! -f "$SQLITE_PATH" ]] && { echo -e "${RED}Error: SQLite file not found: $SQLITE_PATH${NC}" >&2; exit 1; }
            local base_name
            base_name=$(basename "$SQLITE_PATH")
            local filename="sqlite_${base_name}_${timestamp}.db"
            local filepath="${BACKUP_DIR}/${filename}"

            echo -e "${BLUE}==> Starting SQLite atomic backup...${NC}"
            if command -v sqlite3 >/dev/null 2>&1; then
                sqlite3 "$SQLITE_PATH" ".backup '$filepath'"
            else
                cp --preserve=all "$SQLITE_PATH" "$filepath"
            fi
            ;;

        *)
            echo -e "${RED}Unsupported engine: ${ENGINE}. Choose from: postgres, mysql, mongo, redis, sqlite${NC}" >&2
            exit 1
            ;;
    esac

    local size
    size=$(ls -lh "$filepath" | awk '{print $5}')
    echo -e "${GREEN}✓ Backup created successfully!${NC}"
    echo -e "  Path: ${CYAN}${filepath}${NC}"
    echo -e "  Size: ${YELLOW}${size}${NC}"
}

cmd_restore() {
    [[ -z "$RESTORE_FILE" ]] && { echo -e "${RED}Error: --file <path> is required for restore${NC}" >&2; exit 1; }
    [[ ! -f "$RESTORE_FILE" ]] && { echo -e "${RED}Error: Backup file not found: $RESTORE_FILE${NC}" >&2; exit 1; }

    # Auto-detect engine if omitted
    if [[ -z "$ENGINE" ]]; then
        local base
        base=$(basename "$RESTORE_FILE")
        if [[ "$base" =~ ^postgres_ ]] || [[ "$base" == *".sql.gz"* && "$base" =~ postgres ]]; then
            ENGINE="postgres"
        elif [[ "$base" =~ ^mysql_ ]] || [[ "$base" == *".sql.gz"* && "$base" =~ mysql ]]; then
            ENGINE="mysql"
        elif [[ "$base" =~ ^mongo_ ]] || [[ "$base" == *".archive.gz"* ]]; then
            ENGINE="mongo"
        elif [[ "$base" =~ ^redis_ ]] || [[ "$base" == *".rdb"* ]]; then
            ENGINE="redis"
        elif [[ "$base" =~ ^sqlite_ ]] || [[ "$base" == *".db"* ]]; then
            ENGINE="sqlite"
        else
            echo -e "${RED}Could not auto-detect database engine from filename. Specify with -e <engine>${NC}" >&2
            exit 1
        fi
        echo -e "${BLUE}[info] Auto-detected engine: ${CYAN}${ENGINE}${NC}"
    fi

    if [[ "$FORCE" != true ]]; then
        echo -e "${RED}WARNING: Restoring will overwrite or modify existing data in the target database!${NC}"
        read -r -p "Are you sure you want to proceed? [y/N] " response
        if [[ ! "$response" =~ ^([yY][eE][sS]|[yY])$ ]]; then
            echo "Restore aborted by user."
            exit 0
        fi
    fi

    echo -e "${BLUE}==> Restoring from: ${CYAN}${RESTORE_FILE}${NC} (Engine: ${ENGINE})"

    case "$ENGINE" in
        postgres)
            local pguser="${DB_USER:-postgres}"
            local dbname="${DB_NAME:-postgres}"
            local env_pass=()
            [[ -n "$DB_PASS" ]] && env_pass=(-e "PGPASSWORD=${DB_PASS}")

            if [[ "$MODE" == "container" ]]; then
                [[ -z "$CONTAINER" ]] && { echo -e "${RED}Error: --container required for container mode${NC}" >&2; exit 1; }
                gunzip -c "$RESTORE_FILE" | docker exec -i "${env_pass[@]}" "$CONTAINER" psql -U "$pguser" -d "$dbname"
            else
                local local_env=()
                [[ -n "$DB_PASS" ]] && local_env=(PGPASSWORD="$DB_PASS")
                gunzip -c "$RESTORE_FILE" | env "${local_env[@]}" psql -U "$pguser" -d "$dbname"
            fi
            ;;

        mysql)
            local myuser="${DB_USER:-root}"
            local pass_flag=()
            [[ -n "$DB_PASS" ]] && pass_flag=("-p${DB_PASS}")
            local db_target=()
            [[ -n "${DB_NAME:-}" ]] && db_target=("$DB_NAME")

            if [[ "$MODE" == "container" ]]; then
                [[ -z "$CONTAINER" ]] && { echo -e "${RED}Error: --container required for container mode${NC}" >&2; exit 1; }
                gunzip -c "$RESTORE_FILE" | docker exec -i "$CONTAINER" mysql -u "$myuser" "${pass_flag[@]}" "${db_target[@]}"
            else
                gunzip -c "$RESTORE_FILE" | mysql -u "$myuser" "${pass_flag[@]}" "${db_target[@]}"
            fi
            ;;

        mongo)
            local auth_args=()
            [[ -n "${DB_USER:-}" ]] && auth_args+=(--username "$DB_USER")
            [[ -n "${DB_PASS:-}" ]] && auth_args+=(--password "$DB_PASS")
            auth_args+=(--authenticationDatabase "${AUTH_DB:-admin}")

            local drop_flag=()
            [[ "$DROP_EXISTING" == true ]] && drop_flag=(--drop)

            local db_args=()
            [[ -n "${DB_NAME:-}" ]] && db_args+=(--nsInclude="${DB_NAME}.*")

            if [[ "$MODE" == "container" ]]; then
                [[ -z "$CONTAINER" ]] && { echo -e "${RED}Error: --container required for container mode${NC}" >&2; exit 1; }
                docker exec -i "$CONTAINER" mongorestore "${auth_args[@]}" "${drop_flag[@]}" "${db_args[@]}" --archive --gzip < "$RESTORE_FILE"
            else
                mongorestore "${auth_args[@]}" "${drop_flag[@]}" "${db_args[@]}" --archive --gzip < "$RESTORE_FILE"
            fi
            ;;

        redis)
            if [[ "$MODE" == "container" ]]; then
                [[ -z "$CONTAINER" ]] && { echo -e "${RED}Error: --container required for container mode${NC}" >&2; exit 1; }
                echo -e "Stopping Redis container temporarily to replace RDB file..."
                docker stop "$CONTAINER" >/dev/null
                docker cp "$RESTORE_FILE" "${CONTAINER}:/data/dump.rdb"
                docker start "$CONTAINER" >/dev/null
            else
                systemctl stop redis-server || systemctl stop redis
                cp "$RESTORE_FILE" /var/lib/redis/dump.rdb
                chown redis:redis /var/lib/redis/dump.rdb
                systemctl start redis-server || systemctl start redis
            fi
            ;;

        sqlite)
            [[ -z "$SQLITE_PATH" ]] && { echo -e "${RED}Error: --sqlite-path required for sqlite restore${NC}" >&2; exit 1; }
            if [[ -f "$SQLITE_PATH" ]]; then
                echo -e "Creating safety backup of target before overwrite: ${SQLITE_PATH}.pre-restore.bak"
                cp --preserve=all "$SQLITE_PATH" "${SQLITE_PATH}.pre-restore.bak"
            fi
            cp --preserve=all "$RESTORE_FILE" "$SQLITE_PATH"
            ;;

        *)
            echo -e "${RED}Unsupported engine: ${ENGINE}${NC}" >&2
            exit 1
            ;;
    esac

    echo -e "${GREEN}✓ Database restore completed successfully!${NC}"
}

# Parse Subcommand
if [[ $# -eq 0 ]]; then
    usage
fi

COMMAND="$1"
shift

ENGINE=""
MODE="container"
CONTAINER=""
DB_NAME=""
DB_USER=""
DB_PASS=""
AUTH_DB="admin"
SQLITE_PATH=""
RESTORE_FILE=""
DROP_EXISTING=false
FORCE=false
ALL_DATABASES=false
RETENTION_DAYS=30

while [[ $# -gt 0 ]]; do
    case "$1" in
        -d|--dir)
            BACKUP_DIR="$2"
            shift 2
            ;;
        -e|--engine)
            ENGINE="$2"
            shift 2
            ;;
        -m|--mode)
            MODE="$2"
            shift 2
            ;;
        -c|--container)
            CONTAINER="$2"
            shift 2
            ;;
        -n|--database)
            DB_NAME="$2"
            shift 2
            ;;
        -u|--user)
            DB_USER="$2"
            shift 2
            ;;
        -p|--password)
            DB_PASS="$2"
            shift 2
            ;;
        --auth-db)
            AUTH_DB="$2"
            shift 2
            ;;
        --sqlite-path)
            SQLITE_PATH="$2"
            shift 2
            ;;
        -f|--file)
            RESTORE_FILE="$2"
            shift 2
            ;;
        --drop)
            DROP_EXISTING=true
            shift
            ;;
        --force)
            FORCE=true
            shift
            ;;
        --all)
            ALL_DATABASES=true
            shift
            ;;
        --retention-days)
            RETENTION_DAYS="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}" >&2
            usage
            ;;
    esac
done

case "$COMMAND" in
    list)
        cmd_list
        ;;
    prune)
        cmd_prune
        ;;
    backup)
        cmd_backup
        ;;
    restore)
        cmd_restore
        ;;
    help|--help|-h)
        usage
        ;;
    *)
        echo -e "${RED}Unknown command: $COMMAND${NC}" >&2
        usage
        ;;
esac
