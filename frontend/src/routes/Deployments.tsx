import { Rocket, Server as ServerIcon } from 'lucide-react'
import { Link } from 'react-router'

import { EmptyState } from '@/components/EmptyState'
import { NewDeploymentDialog } from '@/components/deployments/NewDeploymentDialog'
import { PageHeader } from '@/components/layout/PageHeader'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { SOURCE_SHORT, useDeployments, type DeploymentStatus } from '@/lib/deployments'
import { can, useSession } from '@/lib/session'
import { useServers } from '@/lib/servers'
import { formatRelative } from '@/lib/utils'

const STATUS: Record<DeploymentStatus, { label: string; variant: 'success' | 'danger' | 'warning' | 'outline' }> = {
  running: { label: 'Running', variant: 'success' },
  deploying: { label: 'Deploying', variant: 'warning' },
  failed: { label: 'Failed', variant: 'danger' },
  stopped: { label: 'Stopped', variant: 'outline' },
  never_deployed: { label: 'Not deployed', variant: 'outline' },
}

export function DeploymentStatusBadge({ status }: { status: DeploymentStatus }) {
  const { label, variant } = STATUS[status] ?? STATUS.never_deployed
  return <Badge variant={variant}>{label}</Badge>
}

export function Deployments() {
  const { data: user } = useSession()
  const { data: deployments, isPending, isError, error } = useDeployments()
  const servers = useServers()

  const canCreate = can(user, 'deployment:write')
  const hasServers = (servers.data?.length ?? 0) > 0

  return (
    <>
      <PageHeader
        title="Deployments"
        description="Applications built and run from a git repository or a compose file."
        actions={canCreate && hasServers ? <NewDeploymentDialog /> : null}
      />

      <div className="p-6">
        {isError ? <Alert variant="danger">{error.message}</Alert> : null}

        {isPending ? (
          <div className="space-y-3">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-20" />
            ))}
          </div>
        ) : null}

        {/* A deployment needs somewhere to run, so an instance with no servers
            is pointed at that step rather than at a form it cannot complete. */}
        {deployments && deployments.length === 0 && !hasServers ? (
          <EmptyState
            icon={ServerIcon}
            title="Connect a server first"
            description="Deployments run on a machine you own. Add one over SSH and it becomes available here."
            action={
              <Button asChild>
                <Link to="/servers">Go to servers</Link>
              </Button>
            }
          />
        ) : null}

        {deployments && deployments.length === 0 && hasServers ? (
          <EmptyState
            icon={Rocket}
            title="No deployments yet"
            description="Point dockdeploy at a git repository or paste a compose file. It clones and builds on the server, then runs the result."
            action={canCreate ? <NewDeploymentDialog /> : undefined}
          />
        ) : null}

        {deployments && deployments.length > 0 ? (
          <div className="space-y-3">
            {deployments.map((deployment) => (
              <Link
                key={deployment.id}
                to={`/deployments/${deployment.id}`}
                className="bg-card hover:border-primary/40 focus-visible:outline-ring flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4 transition-colors focus-visible:outline-2"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <p className="truncate text-sm font-medium">{deployment.name}</p>
                    <DeploymentStatusBadge status={deployment.status} />
                  </div>
                  <p className="text-muted-foreground mt-0.5 truncate font-mono text-xs">
                    {deployment.repo_url || deployment.image_ref || deployment.slug}
                  </p>
                </div>

                <dl className="text-muted-foreground flex shrink-0 items-center gap-6 text-xs">
                  <div>
                    <dt className="mb-0.5">Server</dt>
                    <dd className="text-foreground">{deployment.server_name ?? '—'}</dd>
                  </div>
                  <div>
                    <dt className="mb-0.5">Source</dt>
                    <dd className="text-foreground">{SOURCE_SHORT[deployment.source_type]}</dd>
                  </div>
                  <div>
                    <dt className="mb-0.5">Port</dt>
                    <dd className="text-foreground tabular">
                      {deployment.host_port ?? '—'}
                    </dd>
                  </div>
                  <div>
                    <dt className="mb-0.5">Updated</dt>
                    <dd className="text-foreground">{formatRelative(deployment.updated_at)}</dd>
                  </div>
                </dl>
              </Link>
            ))}
          </div>
        ) : null}
      </div>
    </>
  )
}
