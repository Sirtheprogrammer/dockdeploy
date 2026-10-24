import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Copy,
  Download,
  Globe,
  HardDrive,
  Loader2,
  Play,
  RefreshCw,
  Rocket,
  Terminal,
} from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import type { SafeguardAction } from '@/lib/ai'
import { api } from '@/lib/api'
import { can, useSession } from '@/lib/session'
import { cn } from '@/lib/utils'

interface SafeguardCardProps {
  action: SafeguardAction
  onExecuted?: () => void
}

interface CommandResult {
  exitCode?: number
  stdout?: string
  stderr?: string
  message?: string
}

export function SafeguardCard({ action, onExecuted }: SafeguardCardProps) {
  const { data: user } = useSession()
  const [expanded, setExpanded] = useState(false)
  const [running, setRunning] = useState(false)
  const [completed, setCompleted] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [cmdResult, setCmdResult] = useState<CommandResult | null>(null)

  const hasPermission = can(user, action.permission as Parameters<typeof can>[1])

  const dangerColors = {
    low: 'border-emerald-500/30 bg-emerald-500/5 text-emerald-600 dark:text-emerald-400',
    medium: 'border-amber-500/30 bg-amber-500/5 text-amber-600 dark:text-amber-400',
    high: 'border-rose-500/30 bg-rose-500/5 text-rose-600 dark:text-rose-400',
  }

  const dangerBadgeVariant: Record<'low' | 'medium' | 'high', 'success' | 'warning' | 'danger'> = {
    low: 'success',
    medium: 'warning',
    high: 'danger',
  }

  async function handleExecute() {
    if (!hasPermission) {
      toast.error(`Permission denied: you need '${action.permission}' to run this action.`)
      return
    }

    setRunning(true)
    setError(null)
    setCmdResult(null)

    try {
      const serverId = action.server_id || (action.payload?.server_id as string)

      if (action.type.startsWith('container:')) {
        const op = action.type.replace('container:', '')
        const containerId = action.resource_id || (action.payload?.container_id as string)

        if (!serverId || !containerId) {
          throw new Error('Missing server_id or container_id for container operation.')
        }

        await api.post(`/servers/${serverId}/containers/${containerId}/actions`, {
          action: op,
        })
        toast.success(`Container operation '${op}' executed successfully!`)
      } else if (action.type === 'server:exec_root') {
        const command = (action.payload?.command as string) || ''
        if (!serverId) throw new Error('Missing server_id for command execution.')
        if (!command) throw new Error('No command specified in action payload.')

        const resp = await api.post<{ exit_code: number; stdout: string; stderr: string }>(
          `/servers/${serverId}/exec-root`,
          { command },
        )
        setCmdResult({
          exitCode: resp.exit_code,
          stdout: resp.stdout,
          stderr: resp.stderr,
        })
        if (resp.exit_code === 0) {
          toast.success(`Root command executed successfully!`)
        } else {
          toast.warning(`Command finished with exit code ${resp.exit_code}`)
        }
      } else if (action.type === 'server:probe') {
        if (!serverId) throw new Error('Missing server_id for probe.')
        await api.post(`/servers/${serverId}/probe`, {})
        toast.success('Server connection and Docker probe completed successfully!')
      } else if (action.type === 'server:install_docker') {
        if (!serverId) throw new Error('Missing server_id for Docker installation.')
        await api.post(`/servers/${serverId}/install-docker`, {})
        toast.success('Docker installation script started on server!')
      } else if (action.type === 'server:backup' || action.type === 'database:backup') {
        if (!serverId) throw new Error('Missing server_id for database backup.')
        await api.post(`/servers/${serverId}/database-backups`, {})
        toast.success('Database backup initiated successfully!')
      } else if (action.type === 'deployment:deploy') {
        const depId = action.resource_id || (action.payload?.deployment_id as string)
        if (!depId) throw new Error('Missing deployment_id.')
        await api.post(`/deployments/${depId}/runs`, {})
        toast.success('Deployment run triggered successfully!')
      } else if (action.type === 'domain:sync') {
        const domainId = action.resource_id || (action.payload?.domain_id as string)
        if (!domainId) throw new Error('Missing domain_id.')
        await api.post(`/domains/${domainId}/sync`, {})
        toast.success('Domain virtual host synced successfully!')
      } else if (action.type === 'domain:ssl') {
        const domainId = action.resource_id || (action.payload?.domain_id as string)
        if (!domainId) throw new Error('Missing domain_id.')
        await api.post(`/domains/${domainId}/ssl`, {})
        toast.success('Let\'s Encrypt SSL certificate issue/renewal started!')
      } else {
        toast.info(`Action ${action.type} confirmed. Review payload for details.`)
      }

      setCompleted(true)
      onExecuted?.()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Action execution failed.'
      setError(msg)
      toast.error(msg)
    } finally {
      setRunning(false)
    }
  }

  function handleCopy() {
    navigator.clipboard.writeText(JSON.stringify(action, null, 2))
    toast.success('Action details copied to clipboard')
  }

  // Get action icon
  function getActionIcon() {
    if (action.type.startsWith('container:')) return <Terminal className="size-4.5" />
    if (action.type === 'server:exec_root') return <Terminal className="size-4.5 text-amber-500" />
    if (action.type === 'server:probe') return <RefreshCw className="size-4.5 text-sky-400" />
    if (action.type === 'server:install_docker') return <Download className="size-4.5 text-blue-400" />
    if (action.type === 'server:backup' || action.type === 'database:backup') return <HardDrive className="size-4.5 text-emerald-400" />
    if (action.type === 'deployment:deploy') return <Rocket className="size-4.5 text-emerald-400" />
    if (action.type.startsWith('domain:')) return <Globe className="size-4.5 text-cyan-400" />
    return <Play className="size-4.5" />
  }

  return (
    <Card
      className={cn(
        'my-3 overflow-hidden border p-3.5 shadow-sm transition-all',
        dangerColors[action.danger_level || 'low'],
      )}
    >
      <div className="flex items-start justify-between gap-2.5">
        <div className="flex items-start gap-2.5 min-w-0">
          <div className="mt-0.5 shrink-0 flex items-center justify-center rounded-lg bg-background/60 p-1.5 border border-border/50">
            {getActionIcon()}
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-foreground text-sm font-semibold tracking-tight">
                {action.title || 'Safeguarded Action Proposed'}
              </span>
              <Badge
                variant={dangerBadgeVariant[action.danger_level || 'low']}
                className="text-[10px] uppercase tracking-wider"
              >
                {action.danger_level || 'low'} danger
              </Badge>
              <Badge variant="outline" className="text-[10px] font-mono">
                req: {action.permission}
              </Badge>
              <Badge variant="outline" className="text-[10px] font-mono text-muted-foreground">
                type: {action.type}
              </Badge>
            </div>
            <p className="text-muted-foreground mt-1 text-xs leading-relaxed">
              {action.description}
            </p>
          </div>
        </div>

        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0 text-muted-foreground hover:text-foreground"
          onClick={() => setExpanded(!expanded)}
          title={expanded ? 'Collapse details' : 'Expand details'}
        >
          {expanded ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
        </Button>
      </div>

      {expanded && action.payload && (
        <div className="mt-3 border-t pt-2.5">
          <span className="text-muted-foreground block text-[11px] font-medium">Proposed Parameters:</span>
          <pre className="scrollbar-thin bg-background/80 mt-1 max-h-48 overflow-x-auto rounded p-2.5 text-[11px] font-mono text-zinc-300 border border-border/40">
            {JSON.stringify(action.payload, null, 2)}
          </pre>
        </div>
      )}

      {/* Execution Output (e.g. root command stdout / stderr) */}
      {cmdResult && (
        <div className="mt-3 border-t pt-2.5 space-y-1.5">
          <div className="flex items-center justify-between text-[11px] font-mono">
            <span className="text-muted-foreground">Execution Output:</span>
            <Badge variant={cmdResult.exitCode === 0 ? 'success' : 'danger'} className="text-[10px] py-0">
              exit code {cmdResult.exitCode}
            </Badge>
          </div>
          <pre className="scrollbar-thin bg-zinc-950 p-2.5 rounded text-[11px] font-mono text-zinc-200 overflow-x-auto max-h-56 leading-relaxed border border-zinc-800">
            {cmdResult.stdout && <span>{cmdResult.stdout}</span>}
            {cmdResult.stderr && <span className="text-rose-400 font-semibold">{cmdResult.stderr}</span>}
            {!cmdResult.stdout && !cmdResult.stderr && <span className="text-zinc-500">(No output returned)</span>}
          </pre>
        </div>
      )}

      {error && (
        <div className="text-destructive mt-2 text-xs flex items-center gap-1.5 font-medium">
          <AlertTriangle className="size-3.5 shrink-0" />
          <span>Execution error: {error}</span>
        </div>
      )}

      <div className="mt-3 flex items-center justify-between gap-2 border-t pt-2.5">
        <Button variant="ghost" size="sm" onClick={handleCopy} className="h-7 gap-1 text-xs text-muted-foreground hover:text-foreground">
          <Copy className="size-3" />
          Copy Action JSON
        </Button>

        <div className="flex items-center gap-2">
          {!hasPermission && (
            <span className="text-muted-foreground text-[11px]">
              Missing permission: <code>{action.permission}</code>
            </span>
          )}

          {completed ? (
            <div className="flex items-center gap-1.5">
              <Badge variant="success" className="gap-1">
                <CheckCircle2 className="size-3.5" />
                Executed Successfully
              </Badge>
              <Button
                size="sm"
                variant="ghost"
                onClick={handleExecute}
                disabled={running}
                className="h-6 text-[11px] px-2 text-muted-foreground hover:text-foreground"
                title="Run again"
              >
                Rerun
              </Button>
            </div>
          ) : (
            <Button
              size="sm"
              variant={action.danger_level === 'high' ? 'destructive' : 'default'}
              disabled={running || !hasPermission}
              onClick={handleExecute}
              className="h-7 gap-1.5 text-xs font-medium cursor-pointer"
            >
              {running ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Play className="size-3 fill-current" />
              )}
              Approve & Run
            </Button>
          )}
        </div>
      </div>
    </Card>
  )
}
