import { AlertTriangle, CheckCircle2, ChevronDown, ChevronUp, Copy, Loader2, Play, ShieldAlert, ShieldCheck } from 'lucide-react'
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

export function SafeguardCard({ action, onExecuted }: SafeguardCardProps) {
  const { data: user } = useSession()
  const [expanded, setExpanded] = useState(false)
  const [running, setRunning] = useState(false)
  const [completed, setCompleted] = useState(false)
  const [error, setError] = useState<string | null>(null)

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

    try {
      if (action.type.startsWith('container:')) {
        const op = action.type.replace('container:', '')
        const serverId = action.server_id || (action.payload?.server_id as string)
        const containerId = action.resource_id || (action.payload?.container_id as string)

        if (!serverId || !containerId) {
          throw new Error('Missing server_id or container_id for container operation.')
        }

        await api.post(`/servers/${serverId}/containers/${containerId}/actions`, {
          action: op,
        })
        toast.success(`Container operation '${op}' executed successfully!`)
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
      } else {
        toast.info(`Action ${action.type} confirmed. Review payload for manual execution.`)
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

  return (
    <Card className={cn('my-3 overflow-hidden border p-3.5 shadow-sm transition-all', dangerColors[action.danger_level || 'low'])}>
      <div className="flex items-start justify-between gap-2.5">
        <div className="flex items-start gap-2.5 min-w-0">
          <div className="mt-0.5 shrink-0">
            {action.danger_level === 'high' ? (
              <ShieldAlert className="size-5 text-rose-500" />
            ) : action.danger_level === 'medium' ? (
              <AlertTriangle className="size-5 text-amber-500" />
            ) : (
              <ShieldCheck className="size-5 text-emerald-500" />
            )}
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-foreground text-sm font-semibold tracking-tight">
                {action.title || 'Safeguarded Action Proposed'}
              </span>
              <Badge variant={dangerBadgeVariant[action.danger_level || 'low']} className="text-[10px] uppercase tracking-wider">
                {action.danger_level || 'low'} danger
              </Badge>
              <Badge variant="outline" className="text-[10px]">
                req: {action.permission}
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
          className="size-7 shrink-0"
          onClick={() => setExpanded(!expanded)}
          title={expanded ? 'Collapse details' : 'Expand details'}
        >
          {expanded ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
        </Button>
      </div>

      {expanded && action.payload && (
        <div className="mt-3 border-t pt-2.5">
          <span className="text-muted-foreground block text-[11px] font-medium">Proposed Parameters:</span>
          <pre className="scrollbar-thin bg-background/80 mt-1 max-h-36 overflow-x-auto rounded p-2 text-[11px] font-mono text-zinc-300">
            {JSON.stringify(action.payload, null, 2)}
          </pre>
        </div>
      )}

      {error && (
        <div className="text-destructive mt-2 text-xs">
          Execution error: {error}
        </div>
      )}

      <div className="mt-3 flex items-center justify-between gap-2 border-t pt-2.5">
        <Button variant="ghost" size="sm" onClick={handleCopy} className="h-7 gap-1 text-xs">
          <Copy className="size-3" />
          Copy
        </Button>

        <div className="flex items-center gap-2">
          {!hasPermission && (
            <span className="text-muted-foreground text-[11px]">
              Requires permission: <code>{action.permission}</code>
            </span>
          )}

          {completed ? (
            <Badge variant="success" className="gap-1">
              <CheckCircle2 className="size-3.5" />
              Executed
            </Badge>
          ) : (
            <Button
              size="sm"
              variant={action.danger_level === 'high' ? 'destructive' : 'default'}
              disabled={running || !hasPermission}
              onClick={handleExecute}
              className="h-7 gap-1.5 text-xs font-medium"
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
