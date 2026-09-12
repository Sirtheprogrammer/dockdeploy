import { useHealth } from '@/hooks/useHealth'
import { cn } from '@/lib/utils'

/**
 * Live status of the control plane itself, pinned to the bottom of the
 * sidebar. When Postgres is unreachable the API answers 503, which surfaces
 * here as "database unreachable" instead of a blank page somewhere else.
 */
export function HealthIndicator() {
  const { data, isPending, isError } = useHealth()

  const state = isPending
    ? { dot: 'bg-muted-foreground', label: 'Checking\u2026', detail: '' }
    : isError || data?.database !== 'ok'
      ? { dot: 'bg-destructive', label: 'Degraded', detail: 'database unreachable' }
      : { dot: 'bg-success', label: 'Connected', detail: data.version }

  return (
    <div className="flex items-center gap-2 px-1.5">
      <span className="relative flex size-2 shrink-0">
        {!isPending && !isError && data?.database === 'ok' ? (
          <span className="bg-success absolute inline-flex size-full animate-ping rounded-full opacity-60" />
        ) : null}
        <span className={cn('relative inline-flex size-2 rounded-full', state.dot)} />
      </span>
      <div className="min-w-0 leading-tight">
        <p className="text-xs font-medium">{state.label}</p>
        {state.detail ? (
          <p className="text-muted-foreground truncate font-mono text-[11px]">{state.detail}</p>
        ) : null}
      </div>
    </div>
  )
}
