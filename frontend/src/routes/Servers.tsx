import { Loader2, Server as ServerIcon, Sparkles } from 'lucide-react'
import { Link } from 'react-router'

import { EmptyState } from '@/components/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { AddServerDialog } from '@/components/servers/AddServerDialog'
import { AutoDetectDialog } from '@/components/servers/AutoDetectDialog'
import { ServerStatusBadge } from '@/components/servers/ServerStatusBadge'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { can, useSession } from '@/lib/session'
import { capabilitiesOf, useAutoDetectAllServers, useServers } from '@/lib/servers'
import { formatRelative } from '@/lib/utils'

export function Servers() {
  const { data: user } = useSession()
  const { data: servers, isPending, isError, error } = useServers()
  const autoDetectAll = useAutoDetectAllServers()
  const canAdd = can(user, 'server:write')

  return (
    <>
      <PageHeader
        title="Servers"
        description="Machines dockdeploy manages over SSH."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {servers && servers.length > 0 && canAdd ? (
              <Button
                variant="outline"
                size="sm"
                disabled={autoDetectAll.isPending}
                onClick={() => autoDetectAll.mutate()}
              >
                {autoDetectAll.isPending ? (
                  <Loader2 className="animate-spin size-3.5" />
                ) : (
                  <Sparkles className="size-3.5 text-blue-500" />
                )}
                Auto-Detect All
              </Button>
            ) : null}
            {canAdd ? <AddServerDialog /> : null}
          </div>
        }
      />

      <div className="p-4 sm:p-6 lg:p-8">
        {isError ? <Alert variant="danger">{error.message}</Alert> : null}

        {autoDetectAll.isSuccess ? (
          <Alert variant="default" className="mb-4 border-blue-500/30 bg-blue-500/10 text-blue-500">
            Auto-detection completed across all servers! Synced all discovered domains, deployments, and git credentials.
          </Alert>
        ) : null}

        {isPending ? (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-36" />
            ))}
          </div>
        ) : null}

        {servers && servers.length === 0 ? (
          <EmptyState
            icon={ServerIcon}
            title="No servers connected"
            description={
              canAdd
                ? 'Add a machine with its SSH credentials and dockdeploy will connect to its Docker daemon and list what is already running there. Nothing is installed on the server.'
                : 'No servers have been shared with you yet. Ask an administrator for access.'
            }
            action={canAdd ? <AddServerDialog /> : undefined}
          />
        ) : null}

        {servers && servers.length > 0 ? (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {servers.map((server) => {
              const caps = capabilitiesOf(server)
              return (
                <Link
                  key={server.id}
                  to={`/servers/${server.id}`}
                  className="bg-card hover:border-primary/50 hover:shadow-primary/5 focus-visible:outline-ring group block rounded-xl border p-5 shadow-sm transition-all hover:-translate-y-0.5 hover:shadow-md focus-visible:outline-2"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="group-hover:text-primary truncate text-sm font-semibold transition-colors">{server.name}</p>
                      <p className="text-muted-foreground truncate font-mono text-xs">
                        {server.username}@{server.host}
                        {server.port !== 22 ? `:${server.port}` : ''}
                      </p>
                    </div>
                    <ServerStatusBadge status={server.status} />
                  </div>

                  <dl className="text-muted-foreground mt-5 space-y-2 border-t pt-4 text-xs">
                    <div className="flex justify-between gap-2">
                      <dt>Docker</dt>
                      <dd className="font-mono">{caps?.docker_version || '—'}</dd>
                    </div>
                    <div className="flex justify-between gap-2">
                      <dt>Last seen</dt>
                      <dd>{server.last_seen_at ? formatRelative(server.last_seen_at) : 'never'}</dd>
                    </div>
                  </dl>

                  <div className="mt-4 flex items-center justify-between border-t pt-3" onClick={(e) => e.stopPropagation()}>
                    <span className="text-[11px] text-muted-foreground">Managed over SSH</span>
                    <AutoDetectDialog
                      serverID={server.id}
                      serverName={server.name}
                      trigger={
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-6 px-2 text-[11px] text-blue-500 hover:text-blue-600 hover:bg-blue-500/10"
                          onClick={(e) => {
                            e.preventDefault()
                            e.stopPropagation()
                          }}
                        >
                          <Sparkles className="mr-1 size-3" />
                          Auto-Detect
                        </Button>
                      }
                    />
                  </div>

                  {server.status_message ? (
                    <p className="text-warning mt-3 line-clamp-2 text-xs">{server.status_message}</p>
                  ) : null}
                </Link>
              )
            })}
          </div>
        ) : null}
      </div>
    </>
  )
}
