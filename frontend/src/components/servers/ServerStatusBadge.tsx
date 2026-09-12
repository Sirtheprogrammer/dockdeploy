import { Badge } from '@/components/ui/badge'
import type { ServerStatus } from '@/lib/servers'

const STATUS: Record<ServerStatus, { label: string; variant: 'success' | 'danger' | 'warning' | 'outline' }> = {
  online: { label: 'Online', variant: 'success' },
  offline: { label: 'Offline', variant: 'danger' },
  // Rejected credentials or, more seriously, a changed host key. Either way a
  // person has to look at it, so it reads as a warning rather than an outage.
  unauthorized: { label: 'Needs attention', variant: 'warning' },
  unknown: { label: 'Not checked', variant: 'outline' },
}

export function ServerStatusBadge({ status }: { status: ServerStatus }) {
  const { label, variant } = STATUS[status] ?? STATUS.unknown
  return <Badge variant={variant}>{label}</Badge>
}
