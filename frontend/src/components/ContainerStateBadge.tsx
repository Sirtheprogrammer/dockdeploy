import { Badge } from '@/components/ui/badge'

/**
 * Docker state vocabulary: created, running, paused, restarting, removing,
 * exited, dead. Only running is green; everything else is something the
 * operator may need to look at.
 */
export function ContainerStateBadge({ state, health }: { state: string; health?: string }) {
  if (state === 'running') {
    if (health?.includes('unhealthy')) return <Badge variant="danger">Unhealthy</Badge>
    if (health?.includes('starting')) return <Badge variant="warning">Starting</Badge>
    return <Badge variant="success">Running</Badge>
  }
  if (state === 'restarting') return <Badge variant="warning">Restarting</Badge>
  if (state === 'paused') return <Badge variant="warning">Paused</Badge>
  if (state === 'dead') return <Badge variant="danger">Dead</Badge>
  if (state === 'exited') return <Badge variant="outline">Exited</Badge>
  return <Badge variant="outline">{state}</Badge>
}
