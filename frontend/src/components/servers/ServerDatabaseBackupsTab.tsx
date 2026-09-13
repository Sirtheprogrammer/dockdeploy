import {
  Archive,
  Database,
  HardDrive,
  Loader2,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { DatabaseBackupModal } from '@/components/servers/DatabaseBackupModal'
import { DatabaseRestoreModal } from '@/components/servers/DatabaseRestoreModal'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  useDatabaseBackups,
  useDeleteDatabaseBackup,
  type DatabaseBackupFile,
  type DatabaseEngine,
  type Server,
} from '@/lib/servers'
import { formatBytes, formatRelative } from '@/lib/utils'

interface ServerDatabaseBackupsTabProps {
  server: Server
  canWrite: boolean
}

const ENGINE_BADGES: Record<DatabaseEngine, { label: string; className: string }> = {
  postgres: { label: 'PostgreSQL', className: 'bg-sky-500/10 text-sky-400 border-sky-500/20' },
  mysql: { label: 'MySQL / MariaDB', className: 'bg-amber-500/10 text-amber-400 border-amber-500/20' },
  mongo: { label: 'MongoDB', className: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' },
  redis: { label: 'Redis', className: 'bg-rose-500/10 text-rose-400 border-rose-500/20' },
  sqlite: { label: 'SQLite', className: 'bg-purple-500/10 text-purple-400 border-purple-500/20' },
}

export function ServerDatabaseBackupsTab({ server, canWrite }: ServerDatabaseBackupsTabProps) {
  const [backupModalOpen, setBackupModalOpen] = useState(false)
  const [restoreModalOpen, setRestoreModalOpen] = useState(false)
  const [selectedBackupForRestore, setSelectedBackupForRestore] = useState<DatabaseBackupFile | null>(null)
  const [initialEngine, setInitialEngine] = useState<DatabaseEngine>('postgres')
  const [initialContainer, setInitialContainer] = useState('')
  const [search, setSearch] = useState('')
  const [engineFilter, setEngineFilter] = useState<string>('all')

  const { data, isPending, isError, error, refetch } = useDatabaseBackups(server.id)
  const deleteBackup = useDeleteDatabaseBackup(server.id)

  const backups = data?.backups ?? []
  const containers = data?.containers ?? []
  const backupDir = data?.backup_dir ?? '/var/backups/dockdeploy/databases'

  const filteredBackups = backups.filter((b) => {
    if (engineFilter !== 'all' && b.engine !== engineFilter) return false
    if (!search.trim()) return true
    const term = search.toLowerCase()
    return (
      b.filename.toLowerCase().includes(term) ||
      b.database_name.toLowerCase().includes(term) ||
      b.engine.toLowerCase().includes(term)
    )
  })

  const handleDelete = async (backup: DatabaseBackupFile) => {
    if (!confirm(`Are you sure you want to permanently delete backup file:\n${backup.filename}?`)) {
      return
    }

    try {
      await deleteBackup.mutateAsync({ path: backup.path })
      toast.success(`Backup ${backup.filename} deleted`)
      void refetch()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Delete failed'
      toast.error(msg)
    }
  }

  const handleQuickBackup = (eng: DatabaseEngine, containerName: string) => {
    setInitialEngine(eng)
    setInitialContainer(containerName)
    setBackupModalOpen(true)
  }

  const handleOpenRestore = (backup?: DatabaseBackupFile) => {
    setSelectedBackupForRestore(backup || null)
    setRestoreModalOpen(true)
  }

  return (
    <div className="space-y-6">
      {/* Header Overview Card */}
      <Card className="border-zinc-800 bg-zinc-950/60">
        <CardHeader className="pb-3">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
            <div>
              <CardTitle className="text-base text-zinc-100 flex items-center gap-2">
                <Database className="size-4 text-primary" />
                Database Backups & Disaster Recovery
              </CardTitle>
              <CardDescription className="text-xs text-zinc-400 mt-1">
                Multi-engine database tools supporting PostgreSQL, MySQL, MongoDB, Redis, and SQLite
              </CardDescription>
            </div>
            <div className="flex flex-wrap items-center gap-2 w-full sm:w-auto">
              <Button
                variant="outline"
                size="sm"
                onClick={() => void refetch()}
                className="h-8 text-xs border-zinc-800 text-zinc-300 hover:bg-zinc-900"
              >
                <RefreshCw className="mr-1.5 size-3.5" />
                Refresh
              </Button>
              {canWrite && (
                <>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => handleOpenRestore()}
                    className="h-8 text-xs border-zinc-800 text-zinc-300 hover:bg-zinc-900"
                  >
                    <RotateCcw className="mr-1.5 size-3.5 text-amber-400" />
                    Restore Database
                  </Button>
                  <Button
                    size="sm"
                    onClick={() => {
                      setInitialContainer('')
                      setBackupModalOpen(true)
                    }}
                    className="h-8 text-xs bg-primary hover:bg-primary/90 text-primary-foreground"
                  >
                    <Plus className="mr-1.5 size-3.5" />
                    Create Backup
                  </Button>
                </>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 pt-1 text-xs">
            <div className="rounded-lg border border-zinc-800/80 bg-zinc-900/40 p-3">
              <div className="text-zinc-500 font-medium">Backup Storage Location</div>
              <div className="mt-1 font-mono text-zinc-200 truncate" title={backupDir}>
                {backupDir}
              </div>
            </div>
            <div className="rounded-lg border border-zinc-800/80 bg-zinc-900/40 p-3">
              <div className="text-zinc-500 font-medium">Existing Backups</div>
              <div className="mt-1 text-sm font-semibold text-zinc-200">
                {backups.length} file{backups.length === 1 ? '' : 's'}{' '}
                <span className="text-xs font-normal text-zinc-500">
                  ({formatBytes(backups.reduce((acc, b) => acc + b.size_bytes, 0))})
                </span>
              </div>
            </div>
            <div className="rounded-lg border border-zinc-800/80 bg-zinc-900/40 p-3">
              <div className="text-zinc-500 font-medium">Active Database Containers</div>
              <div className="mt-1 text-sm font-semibold text-zinc-200">
                {containers.length} detected
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Discovered Containers Section */}
      {containers.length > 0 && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-400 flex items-center gap-1.5">
              <HardDrive className="size-3.5 text-zinc-500" />
              Detected Database Containers
            </h3>
            <span className="text-xs text-zinc-500">
              Ready for immediate snapshot or scheduled dump
            </span>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {containers.map((c) => {
              const badge = ENGINE_BADGES[c.engine] || {
                label: c.engine,
                className: 'bg-zinc-800 text-zinc-300',
              }
              return (
                <div
                  key={c.id}
                  className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-3.5 flex items-center justify-between gap-3"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-semibold text-sm text-zinc-200 truncate">
                        {c.name}
                      </span>
                      <Badge variant="outline" className={`text-[10px] px-1.5 py-0 ${badge.className}`}>
                        {badge.label}
                      </Badge>
                    </div>
                    <p className="text-xs font-mono text-zinc-500 truncate mt-0.5" title={c.image}>
                      {c.image}
                    </p>
                    <p className="text-[11px] text-zinc-400 mt-1">
                      Status: <span className="text-emerald-400">{c.status}</span>
                    </p>
                  </div>
                  {canWrite && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleQuickBackup(c.engine, c.name)}
                      className="shrink-0 h-8 text-xs border-zinc-700 hover:border-primary hover:text-primary"
                    >
                      <Archive className="mr-1.5 size-3" />
                      Backup
                    </Button>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* Backups List Section */}
      <div className="space-y-3">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <h3 className="text-xs font-semibold uppercase tracking-wider text-zinc-400 flex items-center gap-1.5">
            <Archive className="size-3.5 text-zinc-500" />
            Saved Database Backups
          </h3>

          <div className="flex items-center gap-2 w-full sm:w-auto">
            <div className="relative flex-1 sm:w-44">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-zinc-500" />
              <Input
                placeholder="Search backups..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="h-8 w-full sm:w-44 pl-8 bg-zinc-900 border-zinc-800 text-xs text-zinc-100"
              />
            </div>

            <select
              value={engineFilter}
              onChange={(e) => setEngineFilter(e.target.value)}
              className="h-8 shrink-0 rounded-md border border-zinc-800 bg-zinc-900 px-2 text-xs text-zinc-300 focus:outline-none"
            >
              <option value="all">All Engines</option>
              <option value="postgres">PostgreSQL</option>
              <option value="mysql">MySQL</option>
              <option value="mongo">MongoDB</option>
              <option value="redis">Redis</option>
              <option value="sqlite">SQLite</option>
            </select>
          </div>
        </div>

        {isPending ? (
          <div className="flex items-center justify-center p-12 text-zinc-500 text-xs gap-2">
            <Loader2 className="size-4 animate-spin" />
            Loading backups from server...
          </div>
        ) : isError ? (
          <Alert variant="danger" className="text-xs">
            Could not load backups: {error.message}
          </Alert>
        ) : filteredBackups.length === 0 ? (
          <div className="rounded-lg border border-dashed border-zinc-800 p-8 text-center">
            <Database className="mx-auto size-8 text-zinc-600 mb-2" />
            <p className="text-sm font-medium text-zinc-300">No database backups found</p>
            <p className="text-xs text-zinc-500 mt-1 max-w-sm mx-auto">
              Create your first atomic database backup or point to existing dumps in{' '}
              <code className="text-zinc-400 font-mono">{backupDir}</code>
            </p>
            {canWrite && (
              <Button
                size="sm"
                onClick={() => setBackupModalOpen(true)}
                className="mt-4 text-xs bg-primary hover:bg-primary/90 text-primary-foreground"
              >
                <Plus className="mr-1.5 size-3.5" />
                Create First Backup
              </Button>
            )}
          </div>
        ) : (
          <div className="rounded-lg border border-zinc-800 bg-zinc-950 overflow-hidden">
            {/* Mobile Card View (sm:hidden) */}
            <div className="divide-y divide-zinc-800/60 sm:hidden">
              {filteredBackups.map((b) => {
                const badge = ENGINE_BADGES[b.engine] || {
                  label: b.engine,
                  className: 'bg-zinc-800 text-zinc-300',
                }
                return (
                  <div key={b.path} className="p-3.5 space-y-2">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <Badge variant="outline" className={`text-[10px] px-2 py-0.5 ${badge.className}`}>
                          {badge.label}
                        </Badge>
                        <span className="font-semibold text-xs text-zinc-200">
                          {b.database_name}
                        </span>
                      </div>
                      <span className="text-xs text-zinc-300 font-mono font-medium">
                        {formatBytes(b.size_bytes)}
                      </span>
                    </div>

                    <div className="font-mono text-[11px] text-zinc-400 truncate" title={b.path}>
                      {b.filename}
                    </div>

                    <div className="flex items-center justify-between pt-1 border-t border-zinc-900 text-xs">
                      <span className="text-zinc-500 text-[11px]">
                        {formatRelative(b.created_at)}
                      </span>

                      {canWrite && (
                        <div className="flex items-center gap-1.5">
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleOpenRestore(b)}
                            className="h-7 px-2.5 text-xs border-zinc-700 hover:border-amber-500 hover:text-amber-400"
                          >
                            <RotateCcw className="mr-1 size-3 text-amber-400" />
                            Restore
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => handleDelete(b)}
                            className="h-7 px-2 text-xs text-zinc-500 hover:text-rose-400"
                          >
                            <Trash2 className="size-3" />
                          </Button>
                        </div>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>

            {/* Desktop Table View (hidden sm:block) */}
            <div className="hidden sm:block overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead className="border-b border-zinc-800 bg-zinc-900/50 text-zinc-400 font-medium">
                  <tr>
                    <th className="py-2.5 px-3">Engine</th>
                    <th className="py-2.5 px-3">Database / Target</th>
                    <th className="py-2.5 px-3">File</th>
                    <th className="py-2.5 px-3">Size</th>
                    <th className="py-2.5 px-3">Created</th>
                    <th className="py-2.5 px-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-zinc-800/60 font-mono">
                  {filteredBackups.map((b) => {
                    const badge = ENGINE_BADGES[b.engine] || {
                      label: b.engine,
                      className: 'bg-zinc-800 text-zinc-300',
                    }
                    return (
                      <tr key={b.path} className="hover:bg-zinc-900/30 transition-colors">
                        <td className="py-2.5 px-3 font-sans">
                          <Badge variant="outline" className={`text-[10px] px-2 py-0.5 ${badge.className}`}>
                            {badge.label}
                          </Badge>
                        </td>
                        <td className="py-2.5 px-3 font-sans font-medium text-zinc-200">
                          {b.database_name}
                        </td>
                        <td className="py-2.5 px-3 text-zinc-400 max-w-xs truncate" title={b.path}>
                          {b.filename}
                        </td>
                        <td className="py-2.5 px-3 text-zinc-300">
                          {formatBytes(b.size_bytes)}
                        </td>
                        <td className="py-2.5 px-3 text-zinc-400 font-sans">
                          {formatRelative(b.created_at)}
                        </td>
                        <td className="py-2.5 px-3 text-right space-x-1 font-sans">
                          {canWrite && (
                            <>
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => handleOpenRestore(b)}
                                className="h-7 px-2 text-xs border-zinc-700 hover:border-amber-500 hover:text-amber-400"
                              >
                                <RotateCcw className="mr-1 size-3 text-amber-400" />
                                Restore
                              </Button>
                              <Button
                                size="sm"
                                variant="ghost"
                                onClick={() => handleDelete(b)}
                                className="h-7 px-2 text-xs text-zinc-500 hover:text-rose-400"
                              >
                                <Trash2 className="size-3" />
                              </Button>
                            </>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>

      {/* Modals */}
      <DatabaseBackupModal
        server={server}
        containers={containers}
        defaultBackupDir={backupDir}
        open={backupModalOpen}
        onOpenChange={setBackupModalOpen}
        onSuccess={() => void refetch()}
        initialEngine={initialEngine}
        initialContainer={initialContainer}
      />

      <DatabaseRestoreModal
        server={server}
        containers={containers}
        open={restoreModalOpen}
        onOpenChange={setRestoreModalOpen}
        onSuccess={() => void refetch()}
        selectedBackup={selectedBackupForRestore}
      />
    </div>
  )
}
