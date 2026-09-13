import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Eye,
  EyeOff,
  Loader2,
  RotateCcw,
} from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { Alert } from '@/components/ui/alert'
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
  useRestoreDatabaseBackup,
  type DatabaseBackupFile,
  type DatabaseEngine,
  type DatabaseExecutionMode,
  type DiscoveredDatabaseContainer,
  type RestoreDatabaseBackupResult,
  type Server,
} from '@/lib/servers'

interface DatabaseRestoreModalProps {
  server: Server
  containers: DiscoveredDatabaseContainer[]
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess?: () => void
  selectedBackup?: DatabaseBackupFile | null
}

const ENGINE_LABELS: Record<DatabaseEngine, { name: string; desc: string }> = {
  postgres: { name: 'PostgreSQL', desc: 'psql streaming decompression into target DB' },
  mysql: { name: 'MySQL / MariaDB', desc: 'mysql client streaming restore' },
  mongo: { name: 'MongoDB', desc: 'mongorestore --archive --gzip' },
  redis: { name: 'Redis', desc: 'Safe container snapshot replacement & restart' },
  sqlite: { name: 'SQLite', desc: 'Atomic file restore with pre-restore .bak safety copy' },
}

interface DatabaseRestoreFormProps {
  server: Server
  containers: DiscoveredDatabaseContainer[]
  selectedBackup?: DatabaseBackupFile | null
  onClose: () => void
  onSuccess?: () => void
}

function DatabaseRestoreForm({
  server,
  containers,
  selectedBackup,
  onClose,
  onSuccess,
}: DatabaseRestoreFormProps) {
  const initialEngine: DatabaseEngine = selectedBackup?.engine ?? 'postgres'
  const matchingInitial = containers.find((c) => c.engine === initialEngine)

  const [engine, setEngine] = useState<DatabaseEngine>(initialEngine)
  const [mode, setMode] = useState<DatabaseExecutionMode>('container')
  const [containerName, setContainerName] = useState(matchingInitial ? matchingInitial.name : '')
  const [databaseName, setDatabaseName] = useState(
    selectedBackup?.database_name && selectedBackup.database_name !== 'database'
      ? selectedBackup.database_name
      : ''
  )
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [authDatabase, setAuthDatabase] = useState('admin')
  const [sqlitePath, setSqlitePath] = useState('')
  const [backupPath, setBackupPath] = useState(selectedBackup?.path ?? '')
  const [dropExisting, setDropExisting] = useState(true)
  const [confirmed, setConfirmed] = useState(false)
  const [sudoPassword, setSudoPassword] = useState('')
  const [saveSudo, setSaveSudo] = useState(true)
  const [result, setResult] = useState<RestoreDatabaseBackupResult | null>(null)

  const restoreBackup = useRestoreDatabaseBackup(server.id)

  const matchingContainers = containers.filter((c) => c.engine === engine)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!confirmed) {
      toast.error('Please confirm that you understand existing data will be overwritten')
      return
    }

    try {
      const res = await restoreBackup.mutateAsync({
        engine,
        mode,
        container_name: mode === 'container' ? containerName : undefined,
        database_name: databaseName.trim() || undefined,
        username: username.trim() || undefined,
        password: password.trim() || undefined,
        auth_database: engine === 'mongo' ? authDatabase.trim() : undefined,
        sqlite_path: engine === 'sqlite' ? sqlitePath.trim() : undefined,
        backup_path: backupPath.trim(),
        drop_existing: dropExisting,
        sudo_password: sudoPassword.trim() || undefined,
        save_sudo: saveSudo,
      })
      setResult(res)
      if (res.success) {
        toast.success('Database restored successfully!')
        onSuccess?.()
      } else {
        toast.error(`Database restore failed with exit code ${res.exit_code}`)
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Restore failed'
      toast.error(msg)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <DialogHeader>
        <div className="flex items-center gap-2.5">
          <div className="flex size-9 items-center justify-center rounded-lg border border-amber-500/20 bg-amber-500/10 text-amber-400">
            <RotateCcw className="size-5" />
          </div>
          <div>
            <DialogTitle className="text-base text-zinc-100">
              Restore Database from Backup
            </DialogTitle>
            <DialogDescription className="text-xs text-zinc-400">
              Restore backup data to target database on{' '}
              <span className="font-mono text-zinc-200">{server.name}</span>
            </DialogDescription>
          </div>
        </div>
      </DialogHeader>

          {/* Critical Warning */}
          <Alert variant="warning" className="text-xs flex gap-2 border-amber-500/30 bg-amber-500/10 text-amber-200">
            <AlertTriangle className="size-4 shrink-0 mt-0.5 text-amber-400" />
            <div>
              <p className="font-medium">Data Overwrite Warning</p>
              <p className="text-amber-300/80 text-[11px] mt-0.5">
                Restoring will overwrite existing tables, documents, or keys in the target database.
                Ensure you have a recent backup before proceeding.
              </p>
            </div>
          </Alert>

          {/* Backup File Path */}
          <div className="space-y-1.5">
            <Label htmlFor="restore-path" className="text-xs font-medium text-zinc-300">
              Backup File Path on Server
            </Label>
            <Input
              id="restore-path"
              placeholder="/var/backups/dockdeploy/databases/postgres_db_2026-09-13.sql.gz"
              value={backupPath}
              onChange={(e) => setBackupPath(e.target.value)}
              required
              className="h-9 bg-zinc-900 border-zinc-800 text-xs font-mono text-zinc-100"
            />
          </div>

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
                      ? 'border-amber-500 bg-amber-500/10 text-amber-200'
                      : 'border-zinc-800 bg-zinc-900/40 text-zinc-400 hover:border-zinc-700'
                  }`}
                >
                  <div className="font-medium text-xs text-zinc-200">
                    {ENGINE_LABELS[eng].name}
                  </div>
                  <div className="text-[10px] text-zinc-500 mt-0.5 line-clamp-1">
                    {ENGINE_LABELS[eng].desc}
                  </div>
                </button>
              ))}
            </div>
          </div>

          {/* Mode & Target */}
          {engine !== 'sqlite' && (
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label className="text-xs font-medium text-zinc-300">Target Environment</Label>
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
                  <Label htmlFor="restore-container" className="text-xs font-medium text-zinc-300">
                    Target Container
                  </Label>
                  {matchingContainers.length > 0 ? (
                    <select
                      id="restore-container"
                      value={containerName}
                      onChange={(e) => setContainerName(e.target.value)}
                      className="w-full h-9 rounded-md border border-zinc-800 bg-zinc-900 px-3 py-1 text-xs text-zinc-100 focus:outline-none focus:ring-1 focus:ring-amber-500"
                    >
                      {matchingContainers.map((c) => (
                        <option key={c.id} value={c.name}>
                          {c.name} ({c.image})
                        </option>
                      ))}
                      <option value="custom">Custom container name...</option>
                    </select>
                  ) : (
                    <Input
                      id="restore-container"
                      placeholder="e.g. postgres_db"
                      value={containerName}
                      onChange={(e) => setContainerName(e.target.value)}
                      className="h-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
                    />
                  )}
                </div>
              )}
            </div>
          )}

          {/* Database Name & Credentials */}
          {engine === 'sqlite' ? (
            <div className="space-y-1.5">
              <Label htmlFor="restore-sqlite" className="text-xs font-medium text-zinc-300">
                Target SQLite Database Path
              </Label>
              <Input
                id="restore-sqlite"
                placeholder="/var/lib/app/production.db"
                value={sqlitePath}
                onChange={(e) => setSqlitePath(e.target.value)}
                required
                className="h-9 bg-zinc-900 border-zinc-800 text-xs font-mono text-zinc-100"
              />
              <p className="text-[11px] text-zinc-500">
                A pre-restore copy will automatically be saved to <code className="text-zinc-400">.pre-restore.bak</code>
              </p>
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="restore-dbname" className="text-xs font-medium text-zinc-300">
                  Target Database Name
                </Label>
                <Input
                  id="restore-dbname"
                  placeholder="e.g. antenkayume_db, my_app"
                  value={databaseName}
                  onChange={(e) => setDatabaseName(e.target.value)}
                  className="h-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="restore-user" className="text-xs font-medium text-zinc-300">
                  Username (Optional)
                </Label>
                <Input
                  id="restore-user"
                  placeholder={engine === 'postgres' ? 'postgres' : engine === 'mysql' ? 'root' : 'user'}
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  className="h-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="restore-pass" className="text-xs font-medium text-zinc-300">
                  Password (if required)
                </Label>
                <div className="relative">
                  <Input
                    id="restore-pass"
                    type={showPassword ? 'text' : 'password'}
                    placeholder="Database password"
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

              {engine === 'mongo' && (
                <div className="space-y-1.5">
                  <Label htmlFor="restore-authdb" className="text-xs font-medium text-zinc-300">
                    Auth Database
                  </Label>
                  <Input
                    id="restore-authdb"
                    placeholder="admin"
                    value={authDatabase}
                    onChange={(e) => setAuthDatabase(e.target.value)}
                    className="h-9 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
                  />
                </div>
              )}
            </div>
          )}

          {/* Options */}
          {engine === 'mongo' && (
            <label className="flex items-center gap-2 text-xs text-zinc-300 cursor-pointer">
              <input
                type="checkbox"
                checked={dropExisting}
                onChange={(e) => setDropExisting(e.target.checked)}
                className="rounded border-zinc-700 bg-zinc-900 text-amber-500 focus:ring-0"
              />
              Drop existing collections before restoring (<code className="text-amber-400">--drop</code>)
            </label>
          )}

          {/* Confirmation Checkbox */}
          <div className="rounded-md border border-amber-500/20 bg-amber-500/5 p-3">
            <label className="flex items-start gap-2.5 text-xs text-zinc-300 cursor-pointer">
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(e) => setConfirmed(e.target.checked)}
                className="mt-0.5 rounded border-amber-500/40 bg-zinc-900 text-amber-500 focus:ring-0"
              />
              <span className="leading-tight">
                I understand this operation will write directly to the target database and may overwrite existing records.
              </span>
            </label>
          </div>

          {/* Sudo password */}
          <div className="space-y-1.5">
            <Label htmlFor="restore-sudo" className="text-xs font-medium text-zinc-300">
              Sudo Password (if elevation required)
            </Label>
            <Input
              id="restore-sudo"
              type="password"
              placeholder={server.has_sudo_password ? '•••••••• (Stored password available)' : 'Enter sudo password if needed'}
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
                  className="rounded border-zinc-700 bg-zinc-900 text-amber-500 focus:ring-0"
                />
                Save sudo password securely on server record
              </label>
            ) : null}
          </div>

          {/* Result */}
          {result && (
            <div className="space-y-2 border-t border-zinc-800 pt-3">
              <div className="flex items-center gap-2">
                {result.success ? (
                  <Badge variant="outline" className="border-emerald-500/30 bg-emerald-500/10 text-emerald-400 gap-1 text-xs">
                    <CheckCircle2 className="size-3" />
                    Restore Succeeded in {(result.duration_ms / 1000).toFixed(1)}s
                  </Badge>
                ) : (
                  <Badge variant="outline" className="border-rose-500/30 bg-rose-500/10 text-rose-400 gap-1 text-xs">
                    <AlertCircle className="size-3" />
                    Failed (Exit code: {result.exit_code})
                  </Badge>
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
              disabled={restoreBackup.isPending || !confirmed}
              className="bg-amber-600 hover:bg-amber-500 text-white font-medium"
            >
              {restoreBackup.isPending ? (
                <>
                  <Loader2 className="mr-2 size-4 animate-spin" />
                  Restoring Database...
                </>
              ) : (
                <>
                  <RotateCcw className="mr-2 size-4" />
                  Restore Database
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
  )
}

export function DatabaseRestoreModal({
  server,
  containers,
  open,
  onOpenChange,
  onSuccess,
  selectedBackup,
}: DatabaseRestoreModalProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto p-6 bg-zinc-950 border-zinc-800 text-zinc-100">
        {open && (
          <DatabaseRestoreForm
            key={selectedBackup?.path ?? 'restore-form'}
            server={server}
            containers={containers}
            selectedBackup={selectedBackup}
            onClose={() => onOpenChange(false)}
            onSuccess={onSuccess}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
