import {
  AlertCircle,
  Archive,
  CheckCircle2,
  Database,
  Eye,
  EyeOff,
  Loader2,
} from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  useCreateDatabaseBackup,
  type CreateDatabaseBackupResult,
  type DatabaseEngine,
  type DatabaseExecutionMode,
  type DiscoveredDatabaseContainer,
  type Server,
} from '@/lib/servers'

interface DatabaseBackupModalProps {
  server: Server
  containers: DiscoveredDatabaseContainer[]
  defaultBackupDir: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess?: () => void
  initialEngine?: DatabaseEngine
  initialContainer?: string
}

const ENGINE_LABELS: Record<DatabaseEngine, { name: string; color: string; desc: string }> = {
  postgres: { name: 'PostgreSQL', color: 'bg-sky-500/10 text-sky-400 border-sky-500/20', desc: 'pg_dump / pg_dumpall (compressed sql.gz)' },
  mysql: { name: 'MySQL / MariaDB', color: 'bg-amber-500/10 text-amber-400 border-amber-500/20', desc: 'mysqldump with single-transaction (compressed sql.gz)' },
  mongo: { name: 'MongoDB', color: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20', desc: 'mongodump --archive --gzip' },
  redis: { name: 'Redis / KeyDB', color: 'bg-rose-500/10 text-rose-400 border-rose-500/20', desc: 'BGSAVE snapshot (dump.rdb)' },
  sqlite: { name: 'SQLite', color: 'bg-purple-500/10 text-purple-400 border-purple-500/20', desc: 'Online vacuum backup / atomic copy' },
}

interface DatabaseBackupFormProps {
  server: Server
  containers: DiscoveredDatabaseContainer[]
  defaultBackupDir: string
  initialEngine?: DatabaseEngine
  initialContainer?: string
  onClose: () => void
  onSuccess?: () => void
}

function DatabaseBackupForm({
  server,
  containers,
  defaultBackupDir,
  initialEngine = 'postgres',
  initialContainer = '',
  onClose,
  onSuccess,
}: DatabaseBackupFormProps) {
  const [engine, setEngine] = useState<DatabaseEngine>(initialEngine)
  const [mode, setMode] = useState<DatabaseExecutionMode>('container')
  const [containerName, setContainerName] = useState(initialContainer)
  const [databaseName, setDatabaseName] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [authDatabase, setAuthDatabase] = useState('admin')
  const [sqlitePath, setSqlitePath] = useState('')
  const [backupDir, setBackupDir] = useState(defaultBackupDir || '/var/backups/dockdeploy/databases')
  const [sudoPassword, setSudoPassword] = useState('')
  const [saveSudo, setSaveSudo] = useState(true)
  const [result, setResult] = useState<CreateDatabaseBackupResult | null>(null)

  const createBackup = useCreateDatabaseBackup(server.id)

  // Filter available containers for current engine
  const matchingContainers = containers.filter((c) => c.engine === engine)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()

    try {
      const res = await createBackup.mutateAsync({
        engine,
        mode,
        container_name: mode === 'container' ? containerName : undefined,
        database_name: databaseName.trim() || undefined,
        username: username.trim() || undefined,
        password: password.trim() || undefined,
        auth_database: engine === 'mongo' ? authDatabase.trim() : undefined,
        sqlite_path: engine === 'sqlite' ? sqlitePath.trim() : undefined,
        backup_dir: backupDir.trim() || undefined,
        sudo_password: sudoPassword.trim() || undefined,
        save_sudo: saveSudo,
      })
      setResult(res)
      if (res.success) {
        toast.success(`Backup created: ${res.backup_file?.filename ?? 'success'}`)
        onSuccess?.()
      } else {
        toast.error(`Backup exited with code ${res.exit_code}`)
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Backup failed'
      toast.error(msg)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
          <DialogHeader>
            <div className="flex items-center gap-2.5">
              <div className="flex size-9 items-center justify-center rounded-lg border border-primary/20 bg-primary/10 text-primary">
                <Database className="size-5" />
              </div>
              <div>
                <DialogTitle className="text-base text-zinc-100">
                  Create Database Backup
                </DialogTitle>
                <DialogDescription className="text-xs text-zinc-400">
                  Generate an atomic, compressed backup on{' '}
                  <span className="font-mono text-zinc-200">{server.name}</span>
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          {/* Engine Selection */}
          <div className="space-y-1.5">
            <Label className="text-xs font-medium text-zinc-300">Database Engine</Label>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
              {(Object.keys(ENGINE_LABELS) as DatabaseEngine[]).map((eng) => (
                <button
                  key={eng}
                  type="button"
                  onClick={() => {
                    setEngine(eng)
                    const matches = containers.filter((c) => c.engine === eng)
                    if (matches.length > 0 && matches[0]) {
                      setContainerName(matches[0].name)
                      setMode('container')
                    }
                  }}
                  className={`p-2.5 rounded-md border text-left transition-colors ${
                    engine === eng
                      ? 'border-primary bg-primary/10 text-primary-foreground'
                      : 'border-zinc-800 bg-zinc-900/40 text-zinc-400 hover:border-zinc-700'
                  }`}
                >
                  <div className="font-medium text-xs text-zinc-200 flex items-center justify-between">
                    {ENGINE_LABELS[eng].name}
                    {matchingContainers.some((c) => c.engine === eng) && (
                      <Badge variant="outline" className="text-[10px] px-1 py-0 border-zinc-700 text-zinc-400">
                        found
                      </Badge>
                    )}
                  </div>
                  <div className="text-[10px] text-zinc-500 mt-1 line-clamp-1">
                    {ENGINE_LABELS[eng].desc}
                  </div>
                </button>
              ))}
            </div>
          </div>

          {/* Mode & Target */}
          {engine !== 'sqlite' && (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label className="text-xs font-medium text-zinc-300">Environment</Label>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant={mode === 'container' ? 'secondary' : 'outline'}
                    onClick={() => setMode('container')}
                    className="flex-1 text-xs"
                  >
                    Docker Container
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant={mode === 'host' ? 'secondary' : 'outline'}
                    onClick={() => setMode('host')}
                    className="flex-1 text-xs"
                  >
                    Host Process
                  </Button>
                </div>
              </div>

              {mode === 'container' && (
                <div className="space-y-1.5">
                  <Label htmlFor="backup-container" className="text-xs font-medium text-zinc-300">
                    Target Container
                  </Label>
                  {matchingContainers.length > 0 ? (
                    <select
                      id="backup-container"
                      value={containerName}
                      onChange={(e) => setContainerName(e.target.value)}
                      className="w-full h-9 rounded-md border border-zinc-800 bg-zinc-900 px-3 py-1 text-xs text-zinc-100 focus:outline-none focus:ring-1 focus:ring-primary"
                    >
                      {matchingContainers.map((c) => (
                        <option key={c.id} value={c.name}>
                          {c.name} ({c.image})
                        </option>
                      ))}
                      <option value="custom">Other / custom name...</option>
                    </select>
                  ) : (
                    <Input
                      id="backup-container"
                      placeholder="e.g. postgres_db, my_mongo"
                      value={containerName}
                      onChange={(e) => setContainerName(e.target.value)}
                      className="h-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
                    />
                  )}
                </div>
              )}
            </div>
          )}

          {/* Database Details */}
          {engine === 'sqlite' ? (
            <div className="space-y-1.5">
              <Label htmlFor="sqlite-path" className="text-xs font-medium text-zinc-300">
                SQLite Database Path on Server
              </Label>
              <Input
                id="sqlite-path"
                placeholder="/var/lib/app/production.db or /root/data.sqlite"
                value={sqlitePath}
                onChange={(e) => setSqlitePath(e.target.value)}
                required
                className="h-9 bg-zinc-900 border-zinc-800 text-xs font-mono text-zinc-100"
              />
            </div>
          ) : (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="db-name" className="text-xs font-medium text-zinc-300">
                  Database Name {engine === 'redis' ? '(Optional)' : '(Leave blank for all)'}
                </Label>
                <Input
                  id="db-name"
                  placeholder={engine === 'redis' ? 'e.g. 0' : 'e.g. antenkayume_db, my_app'}
                  value={databaseName}
                  onChange={(e) => setDatabaseName(e.target.value)}
                  className="h-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="db-user" className="text-xs font-medium text-zinc-300">
                  Username (Optional)
                </Label>
                <Input
                  id="db-user"
                  placeholder={engine === 'postgres' ? 'postgres' : engine === 'mysql' ? 'root' : 'user'}
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  className="h-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="db-pass" className="text-xs font-medium text-zinc-300">
                  Password (if auth required)
                </Label>
                <div className="relative">
                  <Input
                    id="db-pass"
                    type={showPassword ? 'text' : 'password'}
                    placeholder="Database user password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="h-9 pr-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword(!showPassword)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-zinc-400 hover:text-zinc-200"
                  >
                    {showPassword ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                  </button>
                </div>
              </div>

              {engine === 'mongo' ? (
                <div className="space-y-1.5">
                  <Label htmlFor="auth-db" className="text-xs font-medium text-zinc-300">
                    Auth Database
                  </Label>
                  <Input
                    id="auth-db"
                    placeholder="admin"
                    value={authDatabase}
                    onChange={(e) => setAuthDatabase(e.target.value)}
                    className="h-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
                  />
                </div>
              ) : (
                <div className="space-y-1.5">
                  <Label htmlFor="backup-dir" className="text-xs font-medium text-zinc-300">
                    Backup Directory on Server
                  </Label>
                  <Input
                    id="backup-dir"
                    value={backupDir}
                    onChange={(e) => setBackupDir(e.target.value)}
                    className="h-9 bg-zinc-900 border-zinc-800 text-xs font-mono text-zinc-100"
                  />
                </div>
              )}
            </div>
          )}

          {/* Sudo password for elevation */}
          <div className="space-y-1.5">
            <Label htmlFor="sudo-pass" className="text-xs font-medium text-zinc-300">
              Sudo Password (if host elevation needed)
            </Label>
            <Input
              id="sudo-pass"
              type="password"
              placeholder={server.has_sudo_password ? '•••••••• (Stored password available)' : 'Enter sudo password if required'}
              value={sudoPassword}
              onChange={(e) => setSudoPassword(e.target.value)}
              className="h-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
            />
            {sudoPassword.trim() ? (
              <label className="flex items-center gap-2 text-xs text-zinc-400 cursor-pointer pt-1">
                <input
                  type="checkbox"
                  checked={saveSudo}
                  onChange={(e) => setSaveSudo(e.target.checked)}
                  className="rounded border-zinc-700 bg-zinc-900 text-primary focus:ring-0"
                />
                Save sudo password securely
              </label>
            ) : null}
          </div>

          {/* Results section */}
          {result && (
            <div className="space-y-2 border-t border-zinc-800 pt-3">
              <div className="flex items-center gap-2">
                {result.success ? (
                  <Badge variant="outline" className="border-emerald-500/30 bg-emerald-500/10 text-emerald-400 gap-1 text-xs">
                    <CheckCircle2 className="size-3" />
                    Backup Completed in {(result.duration_ms / 1000).toFixed(1)}s
                  </Badge>
                ) : (
                  <Badge variant="outline" className="border-rose-500/30 bg-rose-500/10 text-rose-400 gap-1 text-xs">
                    <AlertCircle className="size-3" />
                    Failed (Exit code: {result.exit_code})
                  </Badge>
                )}
                {result.backup_file && (
                  <span className="font-mono text-xs text-zinc-300 truncate">
                    {result.backup_file.filename} ({(result.backup_file.size_bytes / (1024 * 1024)).toFixed(2)} MB)
                  </span>
                )}
              </div>

              {(result.stdout || result.stderr) && (
                <div className="rounded-md border border-zinc-800 bg-black/80 p-3 font-mono text-[11px] max-h-36 overflow-y-auto whitespace-pre-wrap leading-relaxed">
                  {result.stdout && <div className="text-zinc-300">{result.stdout}</div>}
                  {result.stderr && <div className="text-rose-400">{result.stderr}</div>}
                </div>
              )}
            </div>
          )}

          <DialogFooter className="gap-2 sm:gap-0 pt-2 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              onClick={onClose}
              className="border-zinc-800 text-zinc-300 hover:bg-zinc-900"
            >
              Close
            </Button>
            <Button
              type="submit"
              disabled={createBackup.isPending}
              className="bg-primary hover:bg-primary/90 text-primary-foreground font-medium"
            >
              {createBackup.isPending ? (
                <>
                  <Loader2 className="mr-2 size-4 animate-spin" />
                  Running Backup...
                </>
              ) : (
                <>
                  <Archive className="mr-2 size-4" />
                  Run Backup Now
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
  )
}

export function DatabaseBackupModal({
  server,
  containers,
  defaultBackupDir,
  open,
  onOpenChange,
  onSuccess,
  initialEngine = 'postgres',
  initialContainer = '',
}: DatabaseBackupModalProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[calc(100vw-2rem)] sm:max-w-2xl max-h-[90vh] overflow-y-auto p-4 sm:p-6 bg-zinc-950 border-zinc-800 text-zinc-100">
        {open && (
          <DatabaseBackupForm
            key={`${initialEngine}-${initialContainer}`}
            server={server}
            containers={containers}
            defaultBackupDir={defaultBackupDir}
            initialEngine={initialEngine}
            initialContainer={initialContainer}
            onClose={() => onOpenChange(false)}
            onSuccess={onSuccess}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
