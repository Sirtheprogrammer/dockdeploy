import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Cpu,
  HardDrive,
  Layers,
  MemoryStick,
  RefreshCw,
  Server as ServerIcon,
  XCircle,
} from 'lucide-react'
import { useState } from 'react'

import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useServerMetrics, type HealthStatus, type Server } from '@/lib/servers'
import { formatBytes } from '@/lib/utils'

interface ServerMetricsViewProps {
  server: Server
  onLaunchTerminal?: () => void
}

function formatUptime(seconds: number): string {
  if (seconds <= 0) return '0m'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)

  const parts = []
  if (days > 0) parts.push(`${days}d`)
  if (hours > 0 || days > 0) parts.push(`${hours}h`)
  parts.push(`${minutes}m`)
  return parts.join(' ')
}

function HealthBadge({ status }: { status: HealthStatus }) {
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
  }
}

function ProgressBar({
  value,
  color = 'primary',
  className = '',
}: {
  value: number
  color?: 'primary' | 'warning' | 'danger'
  className?: string
}) {
  const clamped = Math.min(Math.max(value, 0), 100)
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

export function ServerMetricsView({ server, onLaunchTerminal }: ServerMetricsViewProps) {
  const [autoRefresh, setAutoRefresh] = useState(true)
  const { data: metrics, isPending, isError, error, isFetching, refetch } = useServerMetrics(server.id, {
    refetchInterval: autoRefresh ? 5000 : false,
  })

  if (isPending) {
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

  if (isError) {
    return (
      <Alert variant="danger">
        <div className="space-y-2">
          <p className="font-semibold">Failed to collect server metrics</p>
          <p className="text-xs">{error.message}</p>
          <Button variant="outline" size="sm" onClick={() => refetch()} className="mt-2">
            <RefreshCw className="mr-1.5 size-3.5" />
            Try again
          </Button>
        </div>
      </Alert>
    )
  }

  const { cpu, memory, disk, docker, system, health, health_issues } = metrics

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
              disabled={isFetching}
              onClick={() => refetch()}
            >
              <RefreshCw className={`size-3.5 ${isFetching ? 'animate-spin' : ''}`} />
              <span className="sr-only sm:not-sr-only sm:ml-1.5">Refresh</span>
            </Button>
          </div>
        </div>

        {health_issues.length > 0 ? (
          <div className="border-b bg-amber-500/5 px-5 py-3">
            <div className="space-y-1">
              <p className="text-xs font-medium text-amber-500 flex items-center gap-1.5">
                <AlertTriangle className="size-3.5 shrink-0" />
                Detected Issues & Recommendations:
              </p>
              <ul className="list-disc list-inside space-y-0.5 text-xs text-muted-foreground pl-1">
                {health_issues.map((issue, idx) => (
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
            <span>{docker.containers_running} running / {docker.containers_total} total</span>
          </div>
          <div>
            <span className="font-medium text-foreground">Metrics Timestamp: </span>
            <span>{new Date(metrics.collected_at).toLocaleTimeString()}</span>
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
              <span className="text-2xl font-bold tracking-tight">{cpu.usage_percent}%</span>
              <span className="text-xs text-muted-foreground">{cpu.cores} CPU {cpu.cores === 1 ? 'core' : 'cores'}</span>
            </div>
            <ProgressBar value={cpu.usage_percent} />
            <div className="pt-1 border-t text-[11px] text-muted-foreground flex justify-between font-mono">
              <span>Load:</span>
              <span>{cpu.load1.toFixed(2)} (1m) &middot; {cpu.load5.toFixed(2)} (5m) &middot; {cpu.load15.toFixed(2)} (15m)</span>
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
              <span className="text-2xl font-bold tracking-tight">{memory.used_percent}%</span>
              <span className="text-xs text-muted-foreground">
                {formatBytes(memory.used_bytes)} / {formatBytes(memory.total_bytes)}
              </span>
            </div>
            <ProgressBar value={memory.used_percent} />
            <div className="pt-1 border-t text-[11px] text-muted-foreground flex justify-between font-mono">
              <span>Available: {formatBytes(memory.available_bytes)}</span>
              {memory.swap_total_bytes > 0 ? (
                <span>Swap: {memory.swap_used_percent}%</span>
              ) : null}
            </div>
          </CardContent>
        </Card>

        {/* Disk Storage */}
        <Card className="shadow-sm">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium flex items-center justify-between text-muted-foreground">
              <span>Disk Storage</span>
              <HardDrive className="size-4 text-primary" aria-hidden />
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex items-baseline justify-between">
              <span className="text-2xl font-bold tracking-tight">{disk.used_percent}%</span>
              <span className="text-xs text-muted-foreground">
                {formatBytes(disk.used_bytes)} / {formatBytes(disk.total_bytes)}
              </span>
            </div>
            <ProgressBar value={disk.used_percent} />
            <div className="pt-1 border-t text-[11px] text-muted-foreground flex justify-between font-mono truncate">
              <span>Mount: {disk.mount}</span>
              <span>Free: {formatBytes(disk.free_bytes)}</span>
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
              <span>{docker.containers_running} active</span>
              <span>{docker.containers_stopped} stopped</span>
              <span>{docker.images_count} images</span>
            </div>
          </CardContent>
        </Card>
      </div>

      {onLaunchTerminal ? (
        <div className="rounded-lg border border-dashed p-4 flex flex-col sm:flex-row items-center justify-between gap-3 bg-card/40">
          <div className="space-y-0.5 text-center sm:text-left">
            <p className="text-sm font-medium">Need to troubleshoot or inspect system processes directly?</p>
            <p className="text-xs text-muted-foreground">
              Launch an interactive terminal session over SSH to run shell diagnostics.
            </p>
          </div>
          <Button onClick={onLaunchTerminal} size="sm" variant="outline" className="shrink-0 gap-1.5">
            <Activity className="size-3.5 text-sky-400" />
            Launch Terminal
          </Button>
        </div>
      ) : null}
    </div>
  )
}
