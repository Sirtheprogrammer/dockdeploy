import {
  Activity,
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Cpu,
  Flame,
  HardDrive,
  Layers,
  MemoryStick,
  RefreshCw,
  Server as ServerIcon,
  ShieldAlert,
  Sparkles,
  Terminal,
  Trash2,
  XCircle,
  Zap,
} from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  useDockerPrune,
  useServerMetrics,
  type DockerPruneResult,
  type HealthStatus,
  type Server,
} from '@/lib/servers'
import { formatBytes } from '@/lib/utils'

interface ServerMetricsViewProps {
  server: Server
  active?: boolean
  onLaunchTerminal?: () => void
}

function formatUptime(seconds: number | undefined | null): string {
  const secs = Number(seconds || 0)
  if (secs <= 0) return '0m'
  const days = Math.floor(secs / 86400)
  const hours = Math.floor((secs % 86400) / 3600)
  const minutes = Math.floor((secs % 3600) / 60)

  const parts = []
  if (days > 0) parts.push(`${days}d`)
  if (hours > 0 || days > 0) parts.push(`${hours}h`)
  parts.push(`${minutes}m`)
  return parts.join(' ')
}

function safeNumber(val: unknown, fallback = 0): number {
  const n = Number(val)
  return Number.isFinite(n) ? n : fallback
}

function HealthBadge({ status }: { status?: HealthStatus | null }) {
  switch (status) {
    case 'healthy':
      return (
        <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-2.5 py-1 text-xs font-medium text-emerald-500 border border-emerald-500/20">
          <CheckCircle2 className="size-3.5" />
          Healthy
        </span>
      )
    case 'warning':
      return (
        <span className="inline-flex items-center gap-1.5 rounded-full bg-amber-500/10 px-2.5 py-1 text-xs font-medium text-amber-500 border border-amber-500/20">
          <AlertTriangle className="size-3.5" />
          Warning
        </span>
      )
    case 'critical':
      return (
        <span className="inline-flex items-center gap-1.5 rounded-full bg-red-500/10 px-2.5 py-1 text-xs font-medium text-red-500 border border-red-500/20">
          <XCircle className="size-3.5" />
          Critical
        </span>
      )
    default:
      return (
        <span className="inline-flex items-center gap-1.5 rounded-full bg-zinc-500/10 px-2.5 py-1 text-xs font-medium text-zinc-400 border border-zinc-500/20">
          Unknown
        </span>
      )
  }
}

function ProgressBar({
  value,
  color = 'primary',
  className = '',
}: {
  value: number | undefined | null
  color?: 'primary' | 'warning' | 'danger'
  className?: string
}) {
  const val = safeNumber(value)
  const clamped = Math.min(Math.max(val, 0), 100)
  const barColors = {
    primary: clamped >= 90 ? 'bg-red-500' : clamped >= 80 ? 'bg-amber-500' : 'bg-primary',
    warning: 'bg-amber-500',
    danger: 'bg-red-500',
  }

  return (
    <div className={`h-2 w-full overflow-hidden rounded-full bg-secondary ${className}`}>
      <div
        className={`h-full transition-all duration-500 ease-out ${barColors[color]}`}
        style={{ width: `${clamped}%` }}
      />
    </div>
  )
}

export function ServerMetricsView({ server, active = true, onLaunchTerminal }: ServerMetricsViewProps) {
  const [autoRefresh, setAutoRefresh] = useState(true)

  // Docker Cleanup Modal State
  const [pruneTarget, setPruneTarget] = useState<'images' | 'containers' | 'volumes' | 'buildcache' | 'all' | null>(null)
  const [pruneAllImages, setPruneAllImages] = useState(false)
  const [pruneResult, setPruneResult] = useState<DockerPruneResult | null>(null)

  const query = useServerMetrics(server.id, {
    enabled: active,
    refetchInterval: autoRefresh && active ? 5000 : false,
  })

  const pruneMutation = useDockerPrune(server.id)

  function handlePruneConfirm() {
    if (!pruneTarget) return

    pruneMutation.mutate(
      {
        target: pruneTarget,
        all_images: pruneAllImages || pruneTarget === 'all',
      },
      {
        onSuccess: (res) => {
          setPruneResult(res)
          toast.success(
            `Pruned ${res.target}: reclaimed ${formatBytes(res.space_reclaimed)} of disk space!`,
          )
          void query.refetch()
        },
        onError: (err) => {
          toast.error(`Prune failed: ${err.message}`)
        },
      },
    )
  }

  if (query.isPending) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-28 w-full" />
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Skeleton className="h-40" />
          <Skeleton className="h-40" />
          <Skeleton className="h-40" />
          <Skeleton className="h-40" />
        </div>
      </div>
    )
  }

  if (query.isError) {
    return (
      <Alert variant="danger">
        <div className="space-y-2">
          <p className="font-semibold">Failed to collect server metrics</p>
          <p className="text-xs">{query.error?.message || 'Could not reach server metrics endpoint.'}</p>
          <Button variant="outline" size="sm" onClick={() => void query.refetch()} className="mt-2">
            <RefreshCw className="mr-1.5 size-3.5" />
            Try again
          </Button>
        </div>
      </Alert>
    )
  }

  const metrics = query.data

  const cpu = metrics.cpu ?? { usage_percent: 0, cores: 1, load1: 0, load5: 0, load15: 0 }
  const memory = metrics.memory ?? { total_bytes: 0, used_bytes: 0, available_bytes: 0, used_percent: 0, swap_total_bytes: 0, swap_used_bytes: 0, swap_used_percent: 0 }
  const disk = metrics.disk ?? { filesystem: '', mount: '/', total_bytes: 0, used_bytes: 0, free_bytes: 0, used_percent: 0 }
  const docker = metrics.docker ?? { status: 'unavailable', server_version: '', containers_total: 0, containers_running: 0, containers_stopped: 0, images_count: 0 }
  const system = metrics.system ?? { hostname: server.name, os: '', kernel: '', uptime_seconds: 0, server_time: '' }
  const troubleshooting = metrics.troubleshooting ?? {}
  const health: HealthStatus = metrics.health ?? 'healthy'
  const healthIssues: string[] = Array.isArray(metrics.health_issues) ? metrics.health_issues : []

  return (
    <div className="space-y-6">
      {/* Top Banner: Health status & System overview */}
      <Card className="overflow-hidden border-border/80 shadow-sm">
        <div className="flex flex-col gap-4 p-5 sm:flex-row sm:items-center sm:justify-between border-b bg-muted/20">
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-2">
              <ServerIcon className="size-5 text-muted-foreground" aria-hidden />
              <h3 className="text-base font-semibold">{system.hostname || server.name}</h3>
            </div>
            <HealthBadge status={health} />
            <span className="text-muted-foreground text-xs font-mono">
              Uptime: {formatUptime(system.uptime_seconds)}
            </span>
            {system.os ? (
              <span className="text-muted-foreground text-xs">
                &middot; {system.os}
              </span>
            ) : null}
          </div>

          <div className="flex items-center gap-2">
            <Button
              variant={autoRefresh ? 'secondary' : 'outline'}
              size="sm"
              className="text-xs h-8"
              onClick={() => setAutoRefresh(!autoRefresh)}
            >
              <Activity className={`mr-1.5 size-3.5 ${autoRefresh ? 'text-emerald-500 animate-pulse' : ''}`} />
              {autoRefresh ? 'Live (5s)' : 'Auto-refresh off'}
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="text-xs h-8"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <RefreshCw className={`size-3.5 ${query.isFetching ? 'animate-spin' : ''}`} />
              <span className="sr-only sm:not-sr-only sm:ml-1.5">Refresh</span>
            </Button>
          </div>
        </div>

        {healthIssues.length > 0 ? (
          <div className="border-b bg-amber-500/5 px-5 py-3">
            <div className="space-y-1">
              <p className="text-xs font-medium text-amber-500 flex items-center gap-1.5">
                <AlertTriangle className="size-3.5 shrink-0" />
                Detected Issues & Recommendations:
              </p>
              <ul className="list-disc list-inside space-y-0.5 text-xs text-muted-foreground pl-1">
                {healthIssues.map((issue, idx) => (
                  <li key={idx}>{issue}</li>
                ))}
              </ul>
            </div>
          </div>
        ) : null}

        <CardContent className="pt-4 grid gap-x-6 gap-y-2 text-xs sm:grid-cols-2 lg:grid-cols-4 text-muted-foreground">
          <div>
            <span className="font-medium text-foreground">Kernel: </span>
            <span className="font-mono">{system.kernel || '—'}</span>
          </div>
          <div>
            <span className="font-medium text-foreground">Docker Engine: </span>
            <span className="font-mono">{docker.server_version || 'Unavailable'}</span>
          </div>
          <div>
            <span className="font-medium text-foreground">Active Containers: </span>
            <span>{safeNumber(docker.containers_running)} running / {safeNumber(docker.containers_total)} total</span>
          </div>
          <div>
            <span className="font-medium text-foreground">Metrics Timestamp: </span>
            <span>{metrics.collected_at ? new Date(metrics.collected_at).toLocaleTimeString() : '—'}</span>
          </div>
        </CardContent>
      </Card>

      {/* Metrics Cards Grid */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {/* CPU & Load */}
        <Card className="shadow-sm">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center justify-between text-muted-foreground">
              <span>CPU Usage</span>
              <Cpu className="size-4 text-primary" aria-hidden />
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex items-baseline justify-between">
              <span className="text-2xl font-bold tracking-tight">{safeNumber(cpu.usage_percent).toFixed(1)}%</span>
              <span className="text-xs text-muted-foreground">{safeNumber(cpu.cores, 1)} CPU {safeNumber(cpu.cores, 1) === 1 ? 'core' : 'cores'}</span>
            </div>
            <ProgressBar value={cpu.usage_percent} />
            <div className="pt-1 border-t text-[11px] text-muted-foreground flex justify-between font-mono">
              <span>Load:</span>
              <span>{safeNumber(cpu.load1).toFixed(2)} (1m) &middot; {safeNumber(cpu.load5).toFixed(2)} (5m) &middot; {safeNumber(cpu.load15).toFixed(2)} (15m)</span>
            </div>
          </CardContent>
        </Card>

        {/* Memory Utilization */}
        <Card className="shadow-sm">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center justify-between text-muted-foreground">
              <span>Memory (RAM)</span>
              <MemoryStick className="size-4 text-primary" aria-hidden />
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex items-baseline justify-between">
              <span className="text-2xl font-bold tracking-tight">{safeNumber(memory.used_percent).toFixed(1)}%</span>
              <span className="text-xs text-muted-foreground">
                {formatBytes(safeNumber(memory.used_bytes))} / {formatBytes(safeNumber(memory.total_bytes))}
              </span>
            </div>
            <ProgressBar value={memory.used_percent} />
            <div className="pt-1 border-t text-[11px] text-muted-foreground flex justify-between font-mono">
              <span>Available: {formatBytes(safeNumber(memory.available_bytes))}</span>
              {safeNumber(memory.swap_total_bytes) > 0 ? (
                <span>Swap: {safeNumber(memory.swap_used_percent).toFixed(1)}%</span>
              ) : null}
            </div>
          </CardContent>
        </Card>

        {/* Disk Storage */}
        <Card className="shadow-sm">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center justify-between text-muted-foreground">
              <span>Disk Storage (/)</span>
              <HardDrive className="size-4 text-primary" aria-hidden />
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex items-baseline justify-between">
              <span className="text-2xl font-bold tracking-tight">{safeNumber(disk.used_percent).toFixed(1)}%</span>
              <span className="text-xs text-muted-foreground">
                {formatBytes(safeNumber(disk.used_bytes))} / {formatBytes(safeNumber(disk.total_bytes))}
              </span>
            </div>
            <ProgressBar value={disk.used_percent} />
            <div className="pt-1 border-t text-[11px] text-muted-foreground flex justify-between font-mono truncate">
              <span>Mount: {disk.mount || '/'}</span>
              <span>Free: {formatBytes(safeNumber(disk.free_bytes))}</span>
            </div>
          </CardContent>
        </Card>

        {/* Docker Engine */}
        <Card className="shadow-sm">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center justify-between text-muted-foreground">
              <span>Docker Daemon</span>
              <Layers className="size-4 text-primary" aria-hidden />
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex items-baseline justify-between">
              <span className="text-2xl font-bold tracking-tight">
                {docker.status === 'running' ? (
                  <span className="text-emerald-500">Online</span>
                ) : (
                  <span className="text-destructive">Offline</span>
                )}
              </span>
              <span className="text-xs text-muted-foreground font-mono">{docker.server_version || 'v—'}</span>
            </div>
            <div className="h-2 w-full overflow-hidden rounded-full bg-secondary">
              <div
                className={`h-full ${docker.status === 'running' ? 'bg-emerald-500' : 'bg-red-500'}`}
                style={{ width: docker.status === 'running' ? '100%' : '0%' }}
              />
            </div>
            <div className="pt-1 border-t text-[11px] text-muted-foreground flex justify-between font-mono">
              <span>{safeNumber(docker.containers_running)} active</span>
              <span>{safeNumber(docker.containers_stopped)} stopped</span>
              <span>{safeNumber(docker.images_count)} images</span>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Docker Storage Cleanup Bar */}
      <Card className="border-border/80 shadow-sm">
        <CardHeader className="pb-3">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
            <div>
              <CardTitle className="text-sm font-semibold flex items-center gap-2">
                <Trash2 className="size-4 text-destructive" />
                Docker Storage & System Cleanup
              </CardTitle>
              <CardDescription className="text-xs">
                Free up disk space on this server by pruning dangling images, stopped containers, build caches, and unused volumes.
              </CardDescription>
            </div>
            <Badge variant="outline" className="w-fit text-xs font-mono">
              {safeNumber(docker.containers_stopped)} stopped &middot; {safeNumber(docker.images_count)} images
            </Badge>
          </div>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap items-center gap-2 pt-1">
            <Button
              variant="outline"
              size="sm"
              className="text-xs h-8"
              onClick={() => {
                setPruneTarget('images')
                setPruneAllImages(false)
                setPruneResult(null)
              }}
              title="Delete untagged and dangling Docker images"
            >
              <Layers className="size-3.5 mr-1.5 text-amber-500" />
              Prune Stale Images
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="text-xs h-8"
              onClick={() => {
                setPruneTarget('containers')
                setPruneResult(null)
              }}
              title="Delete all stopped containers that are no longer running"
            >
              <XCircle className="size-3.5 mr-1.5 text-red-400" />
              Delete Stopped Containers
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="text-xs h-8"
              onClick={() => {
                setPruneTarget('buildcache')
                setPruneResult(null)
              }}
              title="Clear Docker build cache to reclaim disk space"
            >
              <Flame className="size-3.5 mr-1.5 text-orange-500" />
              Clear Build Cache
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="text-xs h-8"
              onClick={() => {
                setPruneTarget('volumes')
                setPruneResult(null)
              }}
              title="Delete unattached Docker storage volumes"
            >
              <HardDrive className="size-3.5 mr-1.5 text-primary" />
              Prune Unused Volumes
            </Button>
            <Button
              variant="default"
              size="sm"
              className="text-xs h-8 ml-auto"
              onClick={() => {
                setPruneTarget('all')
                setPruneAllImages(true)
                setPruneResult(null)
              }}
              title="Run full docker system prune to reclaim maximum storage"
            >
              <Sparkles className="size-3.5 mr-1.5" />
              Full Docker Cleanup
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Troubleshooting & Fast Diagnostics Tabs */}
      <Card className="border-border/80 shadow-sm">
        <CardHeader className="pb-3 border-b bg-muted/10">
          <CardTitle className="text-sm font-semibold flex items-center gap-2">
            <Zap className="size-4 text-amber-500" />
            Fast Troubleshooting & System Diagnostics
          </CardTitle>
          <CardDescription className="text-xs">
            Inspect top resource-consuming processes, disk partition saturation, active network ports, and systemd/OOM errors.
          </CardDescription>
        </CardHeader>
        <CardContent className="pt-4">
          <Tabs defaultValue="processes" className="w-full">
            <TabsList className="grid w-full grid-cols-4 mb-4">
              <TabsTrigger value="processes" className="text-xs">
                Top Processes
              </TabsTrigger>
              <TabsTrigger value="partitions" className="text-xs">
                Disk Partitions ({(troubleshooting.partitions ?? []).length})
              </TabsTrigger>
              <TabsTrigger value="ports" className="text-xs">
                Open Ports ({(troubleshooting.listening_sockets ?? []).length})
              </TabsTrigger>
              <TabsTrigger value="health" className="text-xs">
                Alerts & OOM ({((troubleshooting.failed_systemd_units ?? []).length) + ((troubleshooting.oom_errors ?? []).length)})
              </TabsTrigger>
            </TabsList>

            {/* TAB 1: Top Processes */}
            <TabsContent value="processes" className="space-y-4">
              <div className="grid gap-4 md:grid-cols-2">
                {/* Top CPU */}
                <div className="space-y-2">
                  <h4 className="text-xs font-semibold text-muted-foreground flex items-center gap-1.5">
                    <Cpu className="size-3.5 text-primary" />
                    Top CPU Processes
                  </h4>
                  <div className="rounded-md border">
                    <Table>
                      <TableHeader>
                        <TableRow className="text-[11px]">
                          <TableHead>PID</TableHead>
                          <TableHead>User</TableHead>
                          <TableHead>CPU %</TableHead>
                          <TableHead>Command</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {(troubleshooting.top_processes_cpu ?? []).length === 0 ? (
                          <TableRow>
                            <TableCell colSpan={4} className="text-center text-xs text-muted-foreground py-4">
                              No process data available.
                            </TableCell>
                          </TableRow>
                        ) : (
                          troubleshooting.top_processes_cpu?.map((p) => (
                            <TableRow key={`cpu-${p.pid}-${p.command}`} className="font-mono text-xs">
                              <TableCell className="text-muted-foreground">{p.pid}</TableCell>
                              <TableCell>{p.user}</TableCell>
                              <TableCell className="font-semibold text-primary">{p.cpu.toFixed(1)}%</TableCell>
                              <TableCell className="truncate max-w-[140px]" title={p.command}>
                                {p.command}
                              </TableCell>
                            </TableRow>
                          ))
                        )}
                      </TableBody>
                    </Table>
                  </div>
                </div>

                {/* Top Memory */}
                <div className="space-y-2">
                  <h4 className="text-xs font-semibold text-muted-foreground flex items-center gap-1.5">
                    <MemoryStick className="size-3.5 text-primary" />
                    Top Memory Processes
                  </h4>
                  <div className="rounded-md border">
                    <Table>
                      <TableHeader>
                        <TableRow className="text-[11px]">
                          <TableHead>PID</TableHead>
                          <TableHead>User</TableHead>
                          <TableHead>RAM %</TableHead>
                          <TableHead>Command</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {(troubleshooting.top_processes_mem ?? []).length === 0 ? (
                          <TableRow>
                            <TableCell colSpan={4} className="text-center text-xs text-muted-foreground py-4">
                              No process data available.
                            </TableCell>
                          </TableRow>
                        ) : (
                          troubleshooting.top_processes_mem?.map((p) => (
                            <TableRow key={`mem-${p.pid}-${p.command}`} className="font-mono text-xs">
                              <TableCell className="text-muted-foreground">{p.pid}</TableCell>
                              <TableCell>{p.user}</TableCell>
                              <TableCell className="font-semibold text-amber-500">{p.memory.toFixed(1)}%</TableCell>
                              <TableCell className="truncate max-w-[140px]" title={p.command}>
                                {p.command}
                              </TableCell>
                            </TableRow>
                          ))
                        )}
                      </TableBody>
                    </Table>
                  </div>
                </div>
              </div>
            </TabsContent>

            {/* TAB 2: Partitions */}
            <TabsContent value="partitions" className="space-y-3">
              {(troubleshooting.partitions ?? []).length === 0 ? (
                <p className="text-xs text-muted-foreground py-4 text-center">No partition details available.</p>
              ) : (
                <div className="space-y-3">
                  {troubleshooting.partitions?.map((part) => (
                    <div key={part.mount} className="p-3 rounded-md border bg-muted/10 space-y-2">
                      <div className="flex items-center justify-between text-xs font-mono">
                        <span className="font-semibold text-foreground">{part.mount} ({part.filesystem})</span>
                        <span className="text-muted-foreground">
                          {formatBytes(part.used_bytes)} / {formatBytes(part.total_bytes)} ({part.used_percent.toFixed(1)}%)
                        </span>
                      </div>
                      <ProgressBar value={part.used_percent} />
                      <div className="text-[11px] text-muted-foreground flex justify-between font-mono">
                        <span>Free: {formatBytes(part.free_bytes)}</span>
                        <span>Used: {formatBytes(part.used_bytes)}</span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </TabsContent>

            {/* TAB 3: Open Ports & Sockets */}
            <TabsContent value="ports" className="space-y-3">
              <div className="flex items-center justify-between text-xs text-muted-foreground pb-1">
                <span>Active Listening TCP/UDP Ports</span>
                {troubleshooting.tcp_connection_count ? (
                  <Badge variant="outline" className="font-mono text-[11px]">
                    {troubleshooting.tcp_connection_count} Active TCP Connections
                  </Badge>
                ) : null}
              </div>
              <div className="rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow className="text-[11px]">
                      <TableHead>Protocol</TableHead>
                      <TableHead>Local Address & Port</TableHead>
                      <TableHead>Process / Daemon</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(troubleshooting.listening_sockets ?? []).length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={3} className="text-center text-xs text-muted-foreground py-4">
                          No open port information returned.
                        </TableCell>
                      </TableRow>
                    ) : (
                      troubleshooting.listening_sockets?.map((s, idx) => (
                        <TableRow key={`port-${idx}-${s.local}`} className="font-mono text-xs">
                          <TableCell className="uppercase text-muted-foreground font-semibold text-[11px]">
                            {s.protocol}
                          </TableCell>
                          <TableCell className="font-semibold text-foreground">{s.local}</TableCell>
                          <TableCell className="text-primary truncate max-w-[200px]">
                            {s.process || 'system'}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            </TabsContent>

            {/* TAB 4: Alerts & OOM */}
            <TabsContent value="health" className="space-y-4">
              {/* Failed Systemd Units */}
              <div className="space-y-2">
                <h4 className="text-xs font-semibold flex items-center gap-1.5 text-muted-foreground">
                  <ShieldAlert className="size-3.5 text-amber-500" />
                  Failed Systemd Services ({troubleshooting.failed_systemd_units?.length || 0})
                </h4>
                {(troubleshooting.failed_systemd_units ?? []).length === 0 ? (
                  <div className="p-3 rounded-md border border-emerald-500/20 bg-emerald-500/5 text-xs text-emerald-500 flex items-center gap-2">
                    <CheckCircle2 className="size-4 shrink-0" />
                    <span>No failed systemd services detected. All services operating normally.</span>
                  </div>
                ) : (
                  <div className="space-y-1.5">
                    {troubleshooting.failed_systemd_units?.map((unit, idx) => (
                      <div key={idx} className="p-2.5 rounded border border-amber-500/30 bg-amber-500/10 font-mono text-xs text-amber-600 dark:text-amber-400">
                        {unit}
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {/* OOM Killer Logs */}
              <div className="space-y-2">
                <h4 className="text-xs font-semibold flex items-center gap-1.5 text-muted-foreground">
                  <AlertCircle className="size-3.5 text-destructive" />
                  Out-Of-Memory (OOM) Killer Events ({troubleshooting.oom_errors?.length || 0})
                </h4>
                {(troubleshooting.oom_errors ?? []).length === 0 ? (
                  <div className="p-3 rounded-md border border-emerald-500/20 bg-emerald-500/5 text-xs text-emerald-500 flex items-center gap-2">
                    <CheckCircle2 className="size-4 shrink-0" />
                    <span>No recent Out-Of-Memory (OOM) kills detected in kernel dmesg.</span>
                  </div>
                ) : (
                  <div className="space-y-1.5">
                    {troubleshooting.oom_errors?.map((oom, idx) => (
                      <div key={idx} className="p-2.5 rounded border border-destructive/30 bg-destructive/10 font-mono text-xs text-destructive">
                        {oom}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      {/* Terminal launch shortcut */}
      {onLaunchTerminal ? (
        <div className="rounded-lg border border-dashed p-4 flex flex-col sm:flex-row items-center justify-between gap-3 bg-card/40">
          <div className="space-y-0.5 text-center sm:text-left">
            <p className="text-sm font-medium">Need to troubleshoot or inspect system processes directly?</p>
            <p className="text-xs text-muted-foreground">
              Launch an interactive terminal session over SSH to run shell diagnostics.
            </p>
          </div>
          <Button onClick={onLaunchTerminal} size="sm" variant="outline" className="shrink-0 gap-1.5">
            <Terminal className="size-3.5 text-sky-400" />
            Launch Terminal
          </Button>
        </div>
      ) : null}

      {/* Docker Cleanup Confirmation Modal */}
      <Dialog open={Boolean(pruneTarget)} onOpenChange={(open) => !open && setPruneTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Trash2 className="size-5 text-destructive" />
              Confirm Docker Cleanup
            </DialogTitle>
            <DialogDescription>
              {pruneTarget === 'images' && 'Remove dangling and unreferenced Docker images to reclaim disk space.'}
              {pruneTarget === 'containers' && 'Permanently delete all stopped Docker containers on this server.'}
              {pruneTarget === 'volumes' && 'Delete all unused Docker storage volumes not connected to any container.'}
              {pruneTarget === 'buildcache' && 'Clear all cached layers from previous docker builds.'}
              {pruneTarget === 'all' && 'Run a full system prune: removes stopped containers, unused networks, unreferenced images, and build cache.'}
            </DialogDescription>
          </DialogHeader>

          {pruneResult ? (
            <div className="space-y-3 py-2">
              <Alert variant="info">
                <div className="space-y-1 text-xs">
                  <div className="font-semibold text-sm">Cleanup Completed!</div>
                  <p>Reclaimed: <strong>{formatBytes(pruneResult.space_reclaimed)}</strong></p>
                  <p>Images deleted: {pruneResult.images_deleted}</p>
                  <p>Containers removed: {pruneResult.containers_deleted}</p>
                  <p>Volumes removed: {pruneResult.volumes_deleted}</p>
                  <p>Build cache entries cleared: {pruneResult.build_cache_deleted}</p>
                </div>
              </Alert>

              <DialogFooter>
                <Button onClick={() => setPruneTarget(null)}>
                  Done
                </Button>
              </DialogFooter>
            </div>
          ) : (
            <div className="space-y-4">
              <p className="text-xs text-muted-foreground">
                Target server: <strong>{server.name}</strong> ({server.host})
              </p>

              {pruneTarget === 'images' && (
                <div className="flex items-center space-x-2 pt-1">
                  <input
                    id="prune-all-img"
                    type="checkbox"
                    checked={pruneAllImages}
                    onChange={(e) => setPruneAllImages(e.target.checked)}
                    className="size-4 rounded border-input text-primary focus:ring-primary"
                  />
                  <label htmlFor="prune-all-img" className="text-xs cursor-pointer">
                    Also delete unused images (not just dangling untagged ones)
                  </label>
                </div>
              )}

              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setPruneTarget(null)}
                  disabled={pruneMutation.isPending}
                >
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  onClick={handlePruneConfirm}
                  disabled={pruneMutation.isPending}
                >
                  {pruneMutation.isPending ? 'Cleaning up...' : 'Confirm Cleanup'}
                </Button>
              </DialogFooter>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
