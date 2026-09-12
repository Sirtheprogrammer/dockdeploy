import {
  Activity,
  ArrowUpRight,
  Database,
  Globe,
  Layers,
  Server,
  Timer,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router'

import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useHealth } from '@/hooks/useHealth'
import { useDeployments } from '@/lib/deployments'
import { useDomains } from '@/lib/domains'
import { useServers } from '@/lib/servers'

function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m ${seconds % 60}s`
}

function Stat({
  icon: Icon,
  label,
  value,
  pending,
}: {
  icon: LucideIcon
  label: string
  value: ReactNode
  pending: boolean
}) {
  return (
    <Card className="overflow-hidden">
      <CardHeader className="flex-row items-center justify-between gap-2 pb-2">
        <CardTitle className="text-muted-foreground font-normal">{label}</CardTitle>
        <span className="bg-muted text-muted-foreground flex size-8 items-center justify-center rounded-lg">
          <Icon className="size-4 shrink-0" aria-hidden />
        </span>
      </CardHeader>
      <CardContent>
        {pending ? <Skeleton className="h-6 w-24" /> : <div className="text-xl font-semibold">{value}</div>}
      </CardContent>
    </Card>
  )
}

export function Overview() {
  const { data, isPending, isError } = useHealth()
  const serversQuery = useServers()
  const deploymentsQuery = useDeployments()
  const domainsQuery = useDomains()

  const reachable = !isError && data !== undefined
  const servers = serversQuery.data ?? []
  const deployments = deploymentsQuery.data ?? []
  const domains = domainsQuery.data ?? []

  const runningDeployments = deployments.filter((d) => d.status === 'running').length
  const activeDomains = domains.filter((d) => d.status === 'active').length

  return (
    <>
      <PageHeader
        title="Overview"
        description="Health of this control plane and everything it manages."
      />

      <div className="space-y-6 p-6 lg:p-8">
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <Stat
            icon={Activity}
            label="Control plane"
            pending={isPending}
            value={
              reachable && data.status === 'ok' ? (
                <Badge variant="success">Healthy</Badge>
              ) : (
                <Badge variant="danger">Degraded</Badge>
              )
            }
          />
          <Stat
            icon={Database}
            label="Database"
            pending={isPending}
            value={
              reachable && data.database === 'ok' ? (
                <Badge variant="success">Connected</Badge>
              ) : (
                <Badge variant="danger">Unreachable</Badge>
              )
            }
          />
          <Stat
            icon={Timer}
            label="Uptime"
            pending={isPending}
            value={<span className="tabular">{reachable ? formatUptime(data.uptime_seconds) : '\u2014'}</span>}
          />
          <Stat
            icon={Server}
            label="Version"
            pending={isPending}
            value={<span className="font-mono text-base">{reachable ? data.version : '\u2014'}</span>}
          />
        </div>

        <div>
          <h2 className="text-base font-semibold mb-3">Managed Resources</h2>
          <div className="grid gap-4 sm:grid-cols-3">
            <Card>
              <CardHeader className="flex-row items-center justify-between pb-2">
                <CardTitle className="text-sm font-medium">Servers</CardTitle>
                <Server className="size-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                {serversQuery.isLoading ? (
                  <Skeleton className="h-8 w-16" />
                ) : (
                  <div className="text-2xl font-bold">{servers.length}</div>
                )}
                <p className="text-xs text-muted-foreground mt-1">
                  Connected via agentless SSH
                </p>
                <div className="mt-4">
                  <Button asChild variant="outline" size="sm" className="w-full">
                    <Link to="/servers" className="flex items-center justify-center gap-1">
                      Manage servers <ArrowUpRight className="size-3.5" />
                    </Link>
                  </Button>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex-row items-center justify-between pb-2">
                <CardTitle className="text-sm font-medium">Deployments</CardTitle>
                <Layers className="size-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                {deploymentsQuery.isLoading ? (
                  <Skeleton className="h-8 w-16" />
                ) : (
                  <div className="text-2xl font-bold">
                    {deployments.length}
                    {deployments.length > 0 ? (
                      <span className="text-xs font-normal text-muted-foreground ml-2">
                        ({runningDeployments} running)
                      </span>
                    ) : null}
                  </div>
                )}
                <p className="text-xs text-muted-foreground mt-1">
                  Git repositories & Docker compose apps
                </p>
                <div className="mt-4">
                  <Button asChild variant="outline" size="sm" className="w-full">
                    <Link to="/deployments" className="flex items-center justify-center gap-1">
                      View deployments <ArrowUpRight className="size-3.5" />
                    </Link>
                  </Button>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex-row items-center justify-between pb-2">
                <CardTitle className="text-sm font-medium">Domains</CardTitle>
                <Globe className="size-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                {domainsQuery.isLoading ? (
                  <Skeleton className="h-8 w-16" />
                ) : (
                  <div className="text-2xl font-bold">
                    {domains.length}
                    {domains.length > 0 ? (
                      <span className="text-xs font-normal text-muted-foreground ml-2">
                        ({activeDomains} active)
                      </span>
                    ) : null}
                  </div>
                )}
                <p className="text-xs text-muted-foreground mt-1">
                  Nginx virtual hosts & Let's Encrypt TLS
                </p>
                <div className="mt-4">
                  <Button asChild variant="outline" size="sm" className="w-full">
                    <Link to="/domains" className="flex items-center justify-center gap-1">
                      Configure domains <ArrowUpRight className="size-3.5" />
                    </Link>
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>
        </div>

        {servers.length === 0 && !serversQuery.isLoading ? (
          <Card className="border-primary/20 bg-primary/[0.035]">
            <CardHeader>
              <CardTitle>Getting started</CardTitle>
            </CardHeader>
            <CardContent className="text-muted-foreground space-y-4 text-sm leading-6">
              <p>
                This instance is running but has nothing to manage yet. Connect a server over SSH and
                dockdeploy will discover the containers already running on it.
              </p>
              <ol className="text-foreground/80 list-inside list-decimal space-y-2">
                <li>Add a server with its SSH credentials and verify the host key.</li>
                <li>Review the containers dockdeploy finds there.</li>
                <li>Create a deployment from a git repository or a compose file.</li>
                <li>Point a domain at it and issue a certificate.</li>
              </ol>
            </CardContent>
          </Card>
        ) : null}
      </div>
    </>
  )
}
