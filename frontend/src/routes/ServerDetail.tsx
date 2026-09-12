import {
  Box,
  HardDrive,
  Layers,
  Loader2,
  Network,
  RefreshCw,
  ScrollText,
  Settings2,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'

import { ContainerStateBadge } from '@/components/ContainerStateBadge'
import { EmptyState } from '@/components/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { CapabilityList } from '@/components/servers/CapabilityList'
import { LogViewer } from '@/components/servers/LogViewer'
import { ServerStatusBadge } from '@/components/servers/ServerStatusBadge'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { can, useSession } from '@/lib/session'
import {
  capabilitiesOf,
  useContainerAction,
  useContainers,
  useDeleteServer,
  useDockerInfo,
  useImages,
  useNetworks,
  useProbeServer,
  useServer,
  useVolumes,
  type Container,
} from '@/lib/servers'
import { formatBytes, formatRelative } from '@/lib/utils'

export function ServerDetail() {
  const { serverID = '' } = useParams()
  const navigate = useNavigate()
  const { data: user } = useSession()

  const [tab, setTab] = useState('containers')
  const [logsFor, setLogsFor] = useState<Container | null>(null)

  const server = useServer(serverID)
  const info = useDockerInfo(serverID)
  const probe = useProbeServer(serverID)
  const remove = useDeleteServer()

  const canOperate = can(user, 'container:operate')
  const canDelete = can(user, 'server:delete')

  if (server.isPending) {
    return (
      <div className="space-y-4 p-6">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-40" />
      </div>
    )
  }

  if (server.isError) {
    return (
      <div className="p-6">
        <Alert variant="danger">{server.error.message}</Alert>
        <Button asChild variant="outline" className="mt-4">
          <Link to="/servers">Back to servers</Link>
        </Button>
      </div>
    )
  }

  const caps = capabilitiesOf(server.data)

  return (
    <>
      <PageHeader
        title={server.data.name}
        description={`${server.data.username}@${server.data.host}${server.data.port !== 22 ? `:${server.data.port}` : ''}`}
        actions={
          <>
            <Button variant="outline" size="sm" disabled={probe.isPending} onClick={() => probe.mutate()}>
              {probe.isPending ? (
                <Loader2 className="animate-spin" aria-hidden />
              ) : (
                <RefreshCw aria-hidden />
              )}
              Re-check
            </Button>
            {canDelete ? (
              <Button
                variant="outline"
                size="sm"
                className="text-destructive"
                disabled={remove.isPending}
                onClick={() => {
                  if (
                    confirm(
                      `Remove ${server.data.name} from dockdeploy?\n\nThe server and its containers are left untouched. Only the stored connection and credentials are deleted.`,
                    )
                  ) {
                    remove.mutate(serverID, { onSuccess: () => void navigate('/servers') })
                  }
                }}
              >
                <Trash2 aria-hidden />
                Remove
              </Button>
            ) : null}
          </>
        }
      />

      <div className="space-y-6 p-6">
        <div className="flex flex-wrap items-center gap-3">
          <ServerStatusBadge status={server.data.status} />
          {info.data ? (
            <span className="text-muted-foreground text-sm">
              Docker {info.data.server_version} &middot; {info.data.cpus} CPU &middot;{' '}
              {formatBytes(info.data.memory_bytes)} &middot; {info.data.containers_running}/
              {info.data.containers} running
            </span>
          ) : null}
          {server.data.last_seen_at ? (
            <span className="text-muted-foreground text-sm">
              seen {formatRelative(server.data.last_seen_at)}
            </span>
          ) : null}
        </div>

        {server.data.status_message ? (
          <Alert variant={server.data.status === 'online' ? 'warning' : 'danger'}>
            {server.data.status_message}
          </Alert>
        ) : null}

        <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="containers">Containers</TabsTrigger>
            <TabsTrigger value="images">Images</TabsTrigger>
            <TabsTrigger value="volumes">Volumes</TabsTrigger>
            <TabsTrigger value="networks">Networks</TabsTrigger>
            <TabsTrigger value="system">System</TabsTrigger>
          </TabsList>

          <TabsContent value="containers" className="pt-4">
            <ContainersTab
              serverID={serverID}
              canOperate={canOperate}
              onShowLogs={setLogsFor}
            />
          </TabsContent>
          <TabsContent value="images" className="pt-4">
            <ImagesTab serverID={serverID} active={tab === 'images'} />
          </TabsContent>
          <TabsContent value="volumes" className="pt-4">
            <VolumesTab serverID={serverID} active={tab === 'volumes'} />
          </TabsContent>
          <TabsContent value="networks" className="pt-4">
            <NetworksTab serverID={serverID} active={tab === 'networks'} />
          </TabsContent>
          <TabsContent value="system" className="pt-4">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Settings2 className="size-4" aria-hidden />
                  What this server supports
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <CapabilityList capabilities={caps} />
                <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[10rem_1fr]">
                  <dt className="text-muted-foreground">Host key</dt>
                  <dd className="font-mono text-xs break-all">{server.data.host_key_fingerprint}</dd>
                  <dt className="text-muted-foreground">Docker socket</dt>
                  <dd className="font-mono text-xs">{server.data.docker_socket}</dd>
                  <dt className="text-muted-foreground">Authentication</dt>
                  <dd className="text-xs">
                    {server.data.auth_method === 'key' ? 'Private key' : 'Password'}
                  </dd>
                </dl>
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </div>

      <Dialog open={logsFor !== null} onOpenChange={(open) => !open && setLogsFor(null)}>
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <ScrollText className="size-4" aria-hidden />
              {logsFor?.name}
            </DialogTitle>
            <DialogDescription className="font-mono text-xs">{logsFor?.image}</DialogDescription>
          </DialogHeader>
          {/* Keyed so switching containers remounts with an empty buffer
              instead of appending to the previous container's lines. */}
          {logsFor ? (
            <LogViewer key={logsFor.id} serverID={serverID} containerID={logsFor.id} />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  )
}

function ContainersTab({
  serverID,
  canOperate,
  onShowLogs,
}: {
  serverID: string
  canOperate: boolean
  onShowLogs: (container: Container) => void
}) {
  const { data: containers, isPending, isError, error } = useContainers(serverID)
  const action = useContainerAction(serverID)
  const [busy, setBusy] = useState<string | null>(null)

  if (isError) return <Alert variant="danger">{error.message}</Alert>
  if (isPending) return <Skeleton className="h-48" />
  if (containers.length === 0) {
    return (
      <EmptyState
        icon={Box}
        title="No containers on this server"
        description="Nothing is running here yet. Create a deployment to put something on it."
      />
    )
  }

  function run(container: Container, verb: 'start' | 'stop' | 'restart') {
    setBusy(container.id)
    action.mutate({ containerID: container.id, action: verb }, { onSettled: () => setBusy(null) })
  }

  return (
    <div className="space-y-3">
      {action.isError ? <Alert variant="danger">{action.error.message}</Alert> : null}

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Container</TableHead>
              <TableHead>State</TableHead>
              <TableHead>Ports</TableHead>
              <TableHead className="w-56 text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {containers.map((container) => (
              <TableRow key={container.id}>
                <TableCell>
                  <p className="text-sm font-medium">{container.name}</p>
                  <p className="text-muted-foreground truncate font-mono text-xs">
                    {container.image}
                  </p>
                </TableCell>
                <TableCell>
                  <ContainerStateBadge state={container.state} health={container.health} />
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {container.ports.filter((p) => p.host_port).length === 0 ? (
                    <span className="text-muted-foreground">—</span>
                  ) : (
                    container.ports
                      .filter((p) => p.host_port)
                      .map((p) => `${p.host_port}:${p.container_port}`)
                      .join(' ')
                  )}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex items-center justify-end gap-1">
                    <Button variant="ghost" size="sm" onClick={() => onShowLogs(container)}>
                      Logs
                    </Button>
                    {canOperate ? (
                      <>
                        {container.state === 'running' ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={busy === container.id}
                            onClick={() => run(container, 'stop')}
                          >
                            Stop
                          </Button>
                        ) : (
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={busy === container.id}
                            onClick={() => run(container, 'start')}
                          >
                            Start
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={busy === container.id}
                          onClick={() => run(container, 'restart')}
                        >
                          {busy === container.id ? (
                            <Loader2 className="animate-spin" aria-hidden />
                          ) : (
                            'Restart'
                          )}
                        </Button>
                      </>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function ImagesTab({ serverID, active }: { serverID: string; active: boolean }) {
  const { data: images, isPending, isError, error } = useImages(serverID, active)
  if (isError) return <Alert variant="danger">{error.message}</Alert>
  if (isPending) return <Skeleton className="h-40" />
  if (images.length === 0) {
    return <EmptyState icon={Layers} title="No images" description="This server has no images pulled." />
  }

  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Tag</TableHead>
            <TableHead>Size</TableHead>
            <TableHead className="text-right">Created</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {images.map((image) => (
            <TableRow key={image.Id}>
              <TableCell className="font-mono text-xs">
                {image.RepoTags?.filter((t) => t !== '<none>:<none>').join(', ') || (
                  <span className="text-muted-foreground">untagged</span>
                )}
              </TableCell>
              <TableCell className="tabular text-sm">{formatBytes(image.Size)}</TableCell>
              <TableCell className="text-muted-foreground text-right text-sm">
                {formatRelative(image.Created * 1000)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function VolumesTab({ serverID, active }: { serverID: string; active: boolean }) {
  const { data: volumes, isPending, isError, error } = useVolumes(serverID, active)
  if (isError) return <Alert variant="danger">{error.message}</Alert>
  if (isPending) return <Skeleton className="h-40" />
  if (volumes.length === 0) {
    return <EmptyState icon={HardDrive} title="No volumes" description="This server has no named volumes." />
  }

  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Driver</TableHead>
            <TableHead>Mountpoint</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {volumes.map((volume) => (
            <TableRow key={volume.Name}>
              <TableCell className="font-mono text-xs">{volume.Name}</TableCell>
              <TableCell className="text-sm">{volume.Driver}</TableCell>
              <TableCell className="text-muted-foreground truncate font-mono text-xs">
                {volume.Mountpoint}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function NetworksTab({ serverID, active }: { serverID: string; active: boolean }) {
  const { data: networks, isPending, isError, error } = useNetworks(serverID, active)
  if (isError) return <Alert variant="danger">{error.message}</Alert>
  if (isPending) return <Skeleton className="h-40" />
  if (networks.length === 0) {
    return <EmptyState icon={Network} title="No networks" description="This server has no Docker networks." />
  }

  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Driver</TableHead>
            <TableHead>Scope</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {networks.map((network) => (
            <TableRow key={network.Id}>
              <TableCell className="font-mono text-xs">{network.Name}</TableCell>
              <TableCell className="text-sm">{network.Driver}</TableCell>
              <TableCell className="text-muted-foreground text-sm">{network.Scope}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
