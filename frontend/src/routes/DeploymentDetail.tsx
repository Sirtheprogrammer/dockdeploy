import {
  Check,
  CheckCircle2,
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  FileCode,
  Globe,
  Loader2,
  Plus,
  RefreshCw,
  Rocket,
  RotateCcw,
  Save,
  Shield,
  ShieldAlert,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'

import { AddDomainDialog } from '@/components/domains/AddDomainDialog'
import { ViewConfigDialog } from '@/components/domains/ViewConfigDialog'
import { RunLogViewer } from '@/components/deployments/RunLogViewer'
import { PageHeader } from '@/components/layout/PageHeader'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { DeploymentStatusBadge } from '@/routes/Deployments'
import {
  SOURCE_LABELS,
  activeRun,
  useCancelRun,
  useDeploy,
  useDeployment,
  useDeploymentEnv,
  useDeleteDeployment,
  useDeploymentWebhook,
  useRotateDeploymentWebhook,
  useRuns,
  useSetDeploymentEnv,
  type EnvVar,
  type Run,
} from '@/lib/deployments'
import {
  useDeleteDomain,
  useDomains,
  useIssueSSL,
  useSyncDomain,
  type Domain,
} from '@/lib/domains'
import { can, useSession } from '@/lib/session'
import { formatExpiration, formatRelative } from '@/lib/utils'

function RunStatusBadge({ status }: { status: Run['status'] }) {
  switch (status) {
    case 'succeeded':
      return <Badge variant="success">Succeeded</Badge>
    case 'failed':
      return <Badge variant="danger">Failed</Badge>
    case 'running':
      return <Badge variant="warning">Running</Badge>
    case 'queued':
      return <Badge variant="info">Queued</Badge>
    default:
      return <Badge variant="outline">Cancelled</Badge>
  }
}

function duration(run: Run): string {
  if (!run.started_at) return '—'
  const end = run.finished_at ? new Date(run.finished_at) : new Date()
  const seconds = Math.max(0, Math.round((end.getTime() - new Date(run.started_at).getTime()) / 1000))
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

export function DeploymentDetail() {
  const { deploymentID = '' } = useParams()
  const navigate = useNavigate()
  const { data: user } = useSession()

  const deployment = useDeployment(deploymentID)
  const [selectedRunID, setSelectedRunID] = useState<string | null>(null)

  const running = deployment.data?.status === 'deploying'
  const runs = useRuns(deploymentID, running)
  const deploy = useDeploy(deploymentID)
  const cancel = useCancelRun(deploymentID)
  const remove = useDeleteDeployment()

  const canDeploy = can(user, 'deployment:deploy')
  const canWrite = can(user, 'deployment:write')
  const canDelete = can(user, 'deployment:delete')
  const canDomainWrite = can(user, 'domain:write')

  if (deployment.isPending) {
    return (
      <div className="space-y-4 p-6">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-40" />
      </div>
    )
  }

  if (deployment.isError) {
    return (
      <div className="p-6">
        <Alert variant="danger">{deployment.error.message}</Alert>
        <Button asChild variant="outline" className="mt-4">
          <Link to="/deployments">Back to deployments</Link>
        </Button>
      </div>
    )
  }

  const live = activeRun(runs.data)
  // Default to whatever is happening now, falling back to the newest run, so
  // opening the page mid-build lands on the log that matters.
  const shownRun = runs.data?.find((r) => r.id === selectedRunID) ?? live ?? runs.data?.[0]

  return (
    <>
      <PageHeader
        title={deployment.data.name}
        description={SOURCE_LABELS[deployment.data.source_type]}
        actions={
          <>
            {canDeploy ? (
              <Button
                size="sm"
                disabled={deploy.isPending || Boolean(live)}
                onClick={() =>
                  deploy.mutate(undefined, { onSuccess: (run) => setSelectedRunID(run.id) })
                }
              >
                {deploy.isPending || live ? (
                  <Loader2 className="animate-spin" aria-hidden />
                ) : (
                  <Rocket aria-hidden />
                )}
                {live ? 'Deploying' : runs.data && runs.data.length > 0 ? 'Redeploy' : 'Deploy'}
              </Button>
            ) : null}
            {canDelete ? (
              <Button
                variant="outline"
                size="sm"
                className="text-destructive"
                disabled={remove.isPending}
                onClick={() => {
                  if (
                    confirm(
                      `Delete ${deployment.data.name}?\n\nThe container keeps running on the server. Only this deployment record and its history are removed.`,
                    )
                  ) {
                    remove.mutate(deploymentID, {
                      onSuccess: () => void navigate('/deployments'),
                    })
                  }
                }}
              >
                <Trash2 aria-hidden />
                Delete
              </Button>
            ) : null}
          </>
        }
      />

      <div className="space-y-6 p-6">
        {deploy.isError ? <Alert variant="danger">{deploy.error.message}</Alert> : null}
        {cancel.isError ? <Alert variant="danger">{cancel.error.message}</Alert> : null}

        <div className="flex flex-wrap items-center gap-3 text-sm">
          <DeploymentStatusBadge status={deployment.data.status} />
          <Link
            to={`/servers/${deployment.data.server_id}`}
            className="text-muted-foreground hover:text-foreground underline underline-offset-4"
          >
            {deployment.data.server_name ?? 'server'}
          </Link>
          {deployment.data.host_port ? (
            <span className="text-muted-foreground font-mono text-xs">
              127.0.0.1:{deployment.data.host_port} &rarr; {deployment.data.container_port}
            </span>
          ) : null}
        </div>

        {deployment.data.host_port ? (
          <Alert className="flex items-start gap-2 text-xs">
            <ExternalLink className="mt-0.5 size-3.5 shrink-0" aria-hidden />
            <span>
              This is published on loopback only, so it is not reachable from the internet yet.
              Add a domain to put nginx in front of it.
            </span>
          </Alert>
        ) : null}

        <Tabs defaultValue="activity">
          <TabsList>
            <TabsTrigger value="activity">Activity</TabsTrigger>
            <TabsTrigger value="environment">Environment</TabsTrigger>
            <TabsTrigger value="domains">Domains</TabsTrigger>
            <TabsTrigger value="webhook">Push to Deploy</TabsTrigger>
            <TabsTrigger value="config">Configuration</TabsTrigger>
          </TabsList>

          <TabsContent value="activity" className="space-y-4 pt-4">
            {shownRun ? (
              <div className="space-y-2">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <p className="text-sm font-medium">
                    Run #{shownRun.number}
                    <span className="text-muted-foreground ml-2 font-normal">
                      {shownRun.trigger} &middot; {duration(shownRun)}
                      {shownRun.commit_sha ? (
                        <> &middot; <span className="font-mono">{shownRun.commit_sha.slice(0, 8)}</span></>
                      ) : null}
                    </span>
                  </p>
                  {shownRun.status === 'queued' && canDeploy ? (
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={cancel.isPending}
                      onClick={() => cancel.mutate(shownRun.id)}
                    >
                      Cancel
                    </Button>
                  ) : null}
                </div>
                <RunLogViewer
                  key={shownRun.id}
                  deploymentID={deploymentID}
                  runID={shownRun.id}
                  initialStatus={shownRun.status}
                />
              </div>
            ) : (
              <Card>
                <CardContent className="text-muted-foreground py-10 text-center text-sm">
                  This has never been deployed.
                  {canDeploy ? ' Use Deploy above to build and run it.' : ''}
                </CardContent>
              </Card>
            )}

            {runs.data && runs.data.length > 0 ? (
              <Card>
                <CardHeader>
                  <CardTitle>History</CardTitle>
                </CardHeader>
                <CardContent className="px-0">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-16">Run</TableHead>
                        <TableHead>Status</TableHead>
                        <TableHead>Commit</TableHead>
                        <TableHead>Image</TableHead>
                        <TableHead>Took</TableHead>
                        <TableHead className="text-right">When</TableHead>
                        <TableHead className="w-24 text-right">Actions</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {runs.data.map((run) => (
                        <TableRow
                          key={run.id}
                          onClick={() => setSelectedRunID(run.id)}
                          className={
                            run.id === shownRun?.id ? 'bg-accent/40 cursor-pointer' : 'cursor-pointer'
                          }
                        >
                          <TableCell className="tabular font-medium">#{run.number}</TableCell>
                          <TableCell>
                            <RunStatusBadge status={run.status} />
                          </TableCell>
                          <TableCell className="font-mono text-xs">
                            {run.commit_sha ? run.commit_sha.slice(0, 8) : '—'}
                          </TableCell>
                          <TableCell className="text-muted-foreground max-w-[12rem] truncate font-mono text-xs">
                            {run.image_ref || '—'}
                          </TableCell>
                          <TableCell className="tabular text-sm">{duration(run)}</TableCell>
                          <TableCell className="text-muted-foreground text-right text-sm">
                            {formatRelative(run.queued_at)}
                          </TableCell>
                          <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                            {run.status === 'succeeded' && canDeploy ? (
                              <Button
                                variant="outline"
                                size="sm"
                                className="h-7 text-xs"
                                disabled={deploy.isPending || Boolean(live)}
                                onClick={() => {
                                  if (
                                    confirm(
                                      `Rollback to Run #${run.number}?\n\nThis will re-deploy image:\n${run.image_ref || '(captured from run #' + run.number + ')'}`,
                                    )
                                  ) {
                                    deploy.mutate(
                                      { rollback_run_id: run.id },
                                      { onSuccess: (newRun) => setSelectedRunID(newRun.id) },
                                    )
                                  }
                                }}
                              >
                                <RotateCcw className="size-3" aria-hidden />
                                Rollback
                              </Button>
                            ) : null}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
            ) : null}
          </TabsContent>

          <TabsContent value="environment" className="pt-4">
            <EnvironmentTab deploymentID={deploymentID} editable={canWrite} />
          </TabsContent>

          <TabsContent value="domains" className="pt-4">
            <DomainsTab
              deploymentID={deploymentID}
              serverID={deployment.data.server_id}
              canManage={canDomainWrite}
            />
          </TabsContent>

          <TabsContent value="webhook" className="pt-4">
            <WebhookTab deploymentID={deploymentID} />
          </TabsContent>

          <TabsContent value="config" className="pt-4">
            <Card className="max-w-3xl">
              <CardHeader>
                <CardTitle>Configuration</CardTitle>
                <CardDescription>Set when the deployment was created.</CardDescription>
              </CardHeader>
              <CardContent>
                <dl className="grid gap-x-6 gap-y-2.5 text-sm sm:grid-cols-[12rem_1fr]">
                  <dt className="text-muted-foreground">Source</dt>
                  <dd>{SOURCE_LABELS[deployment.data.source_type]}</dd>
                  {deployment.data.repo_url ? (
                    <>
                      <dt className="text-muted-foreground">Repository</dt>
                      <dd className="font-mono text-xs break-all">{deployment.data.repo_url}</dd>
                      <dt className="text-muted-foreground">Ref</dt>
                      <dd className="font-mono text-xs">{deployment.data.git_ref || 'default'}</dd>
                    </>
                  ) : null}
                  {deployment.data.image_ref ? (
                    <>
                      <dt className="text-muted-foreground">Image</dt>
                      <dd className="font-mono text-xs">{deployment.data.image_ref}</dd>
                    </>
                  ) : null}
                  {deployment.data.source_type === 'git_dockerfile' ? (
                    <>
                      <dt className="text-muted-foreground">Dockerfile</dt>
                      <dd className="font-mono text-xs">{deployment.data.dockerfile_path}</dd>
                      <dt className="text-muted-foreground">Build context</dt>
                      <dd className="font-mono text-xs">{deployment.data.build_context}</dd>
                    </>
                  ) : null}
                  <dt className="text-muted-foreground">Build strategy</dt>
                  <dd>
                    {deployment.data.build_strategy === 'registry'
                      ? 'Controller build + Registry push'
                      : 'Built on target server'}
                  </dd>
                  {deployment.data.build_strategy === 'registry' && deployment.data.image_name ? (
                    <>
                      <dt className="text-muted-foreground">Target image</dt>
                      <dd className="font-mono text-xs">{deployment.data.image_name}</dd>
                    </>
                  ) : null}
                  <dt className="text-muted-foreground">Working directory</dt>
                  <dd className="font-mono text-xs break-all">{deployment.data.workdir}</dd>
                  <dt className="text-muted-foreground">Container name</dt>
                  <dd className="font-mono text-xs">{deployment.data.slug}</dd>
                </dl>
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </div>
    </>
  )
}

/**
 * Domains tab displaying virtual hosts attached to this deployment.
 */
function DomainsTab({
  deploymentID,
  serverID,
  canManage,
}: {
  deploymentID: string
  serverID: string
  canManage: boolean
}) {
  const domainsQuery = useDomains({ deployment_id: deploymentID })
  const deleteMutation = useDeleteDomain()
  const issueSSLMutation = useIssueSSL()
  const syncMutation = useSyncDomain()

  const [configDomain, setConfigDomain] = useState<Domain | null>(null)

  const domains = domainsQuery.data ?? []

  if (domainsQuery.isLoading) {
    return (
      <div className="space-y-3 pt-4">
        <Skeleton className="h-10 w-48" />
        <Skeleton className="h-24 w-full" />
      </div>
    )
  }

  return (
    <div className="space-y-4 pt-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">Domain Routing</h3>
          <p className="text-muted-foreground text-xs">
            Nginx virtual hosts and SSL certificates routing external traffic to this container.
          </p>
        </div>
        {canManage ? (
          <AddDomainDialog
            defaultDeploymentID={deploymentID}
            defaultServerID={serverID}
            trigger={
              <Button size="sm">
                <Plus className="size-4" aria-hidden />
                Add Domain
              </Button>
            }
          />
        ) : null}
      </div>

      {domains.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-10 text-center">
            <Globe className="text-muted-foreground mb-3 size-8" aria-hidden />
            <p className="text-sm font-medium">No domains connected</p>
            <p className="text-muted-foreground mt-1 max-w-sm text-xs">
              Route traffic from a custom domain to this container by configuring an Nginx virtual host.
            </p>
            {canManage ? (
              <div className="mt-4">
                <AddDomainDialog
                  defaultDeploymentID={deploymentID}
                  defaultServerID={serverID}
                  trigger={
                    <Button size="sm" variant="outline">
                      <Plus className="size-4" aria-hidden />
                      Connect Domain
                    </Button>
                  }
                />
              </div>
            ) : null}
          </CardContent>
        </Card>
      ) : (
        <div className="overflow-hidden rounded-md border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Hostname</TableHead>
                <TableHead>Upstream</TableHead>
                <TableHead>SSL Certificate</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {domains.map((d) => {
                const isSyncing = syncMutation.isPending && syncMutation.variables === d.id
                const isIssuing = issueSSLMutation.isPending && issueSSLMutation.variables === d.id
                const isDeleting = deleteMutation.isPending && deleteMutation.variables === d.id

                return (
                  <TableRow key={d.id}>
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        <Globe className="text-muted-foreground size-4 shrink-0" aria-hidden />
                        <span>{d.hostname}</span>
                        <a
                          href={`${d.ssl_mode === 'letsencrypt' ? 'https' : 'http'}://${d.hostname}`}
                          target="_blank"
                          rel="noreferrer"
                          className="text-muted-foreground hover:text-foreground"
                          title="Open domain in browser"
                        >
                          <ExternalLink className="size-3" aria-hidden />
                        </a>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      127.0.0.1:{d.upstream_port}
                      {d.websocket ? (
                        <Badge variant="outline" className="ml-1.5 text-[10px] px-1 py-0">
                          WS
                        </Badge>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      {d.ssl_mode === 'letsencrypt' ? (
                        <div className="flex flex-col gap-0.5">
                          <div className="flex items-center gap-1.5 text-xs text-emerald-600 dark:text-emerald-400">
                            <ShieldCheck className="size-3.5" aria-hidden />
                            <span className="font-medium">Let's Encrypt</span>
                          </div>
                          {d.cert_expires_at ? (
                            <span className="text-muted-foreground text-[11px]">
                              {formatExpiration(d.cert_expires_at)}
                            </span>
                          ) : (
                            <span className="text-muted-foreground text-[11px]">Awaiting issuance</span>
                          )}
                        </div>
                      ) : (
                        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                          <Shield className="size-3.5" aria-hidden />
                          <span>HTTP Only</span>
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      {d.status === 'active' ? (
                        <Badge variant="success" className="flex w-fit items-center gap-1">
                          <CheckCircle2 className="h-3 w-3" />
                          Active
                        </Badge>
                      ) : d.status === 'error' ? (
                        <Badge variant="danger" title={d.status_message}>
                          Error
                        </Badge>
                      ) : (
                        <Badge variant="outline">Pending</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          title="View Nginx configuration"
                          onClick={() => setConfigDomain(d)}
                        >
                          <FileCode className="h-4 w-4 text-muted-foreground" />
                        </Button>
                        {canManage ? (
                          <>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              title="Sync Nginx configuration"
                              disabled={isSyncing}
                              onClick={() => syncMutation.mutate(d.id)}
                            >
                              {isSyncing ? (
                                <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                              ) : (
                                <RefreshCw className="h-4 w-4 text-muted-foreground" />
                              )}
                            </Button>
                            {d.ssl_mode === 'letsencrypt' ? (
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                title="Issue or renew SSL certificate"
                                disabled={isIssuing}
                                onClick={() => issueSSLMutation.mutate(d.id)}
                              >
                                {isIssuing ? (
                                  <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                                ) : (
                                  <ShieldAlert className="h-4 w-4 text-muted-foreground" />
                                )}
                              </Button>
                            ) : null}
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              className="text-muted-foreground hover:text-destructive"
                              title="Delete domain"
                              disabled={isDeleting}
                              onClick={() => {
                                if (
                                  confirm(
                                    `Delete domain "${d.hostname}"?\n\nThis will remove the Nginx virtual host from the server.`,
                                  )
                                ) {
                                  deleteMutation.mutate(d.id)
                                }
                              }}
                            >
                              {isDeleting ? (
                                <Loader2 className="h-4 w-4 animate-spin text-destructive" />
                              ) : (
                                <Trash2 className="h-4 w-4" />
                              )}
                            </Button>
                          </>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <ViewConfigDialog
        open={Boolean(configDomain)}
        onOpenChange={(open: boolean) => !open && setConfigDomain(null)}
        domain={configDomain}
      />
    </div>
  )
}

/**
 * Push to Deploy tab displaying webhook configuration and integration guides.
 */
function WebhookTab({ deploymentID }: { deploymentID: string }) {
  const webhook = useDeploymentWebhook(deploymentID)
  const rotateWebhook = useRotateDeploymentWebhook(deploymentID)
  const [showSecret, setShowSecret] = useState(false)
  const [copiedUrl, setCopiedUrl] = useState(false)
  const [copiedSecret, setCopiedSecret] = useState(false)

  const rawUrl = webhook.data?.webhook_url ?? ''
  let effectiveUrl = rawUrl
  if (rawUrl) {
    try {
      const parsed = new URL(rawUrl)
      if (
        (parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1') &&
        window.location.hostname !== 'localhost' &&
        window.location.hostname !== '127.0.0.1'
      ) {
        effectiveUrl = `${window.location.origin}${parsed.pathname}${parsed.search}`
      }
    } catch {
      effectiveUrl = rawUrl
    }
  }

  if (webhook.isPending) {
    return (
      <div className="space-y-4 pt-4 max-w-3xl">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }

  if (webhook.isError) {
    return (
      <div className="pt-4 max-w-3xl">
        <Alert variant="danger">{webhook.error.message}</Alert>
      </div>
    )
  }

  const { webhook_secret } = webhook.data

  const copyUrl = () => {
    void navigator.clipboard.writeText(effectiveUrl)
    setCopiedUrl(true)
    setTimeout(() => setCopiedUrl(false), 2000)
  }

  const copySecret = () => {
    void navigator.clipboard.writeText(webhook_secret)
    setCopiedSecret(true)
    setTimeout(() => setCopiedSecret(false), 2000)
  }

  const handleRotate = () => {
    if (
      window.confirm(
        'Rotate Webhook Secret?\n\nExisting CI/CD or git webhooks using the old secret will fail until updated with the new secret.',
      )
    ) {
      rotateWebhook.mutate()
    }
  }

  return (
    <div className="space-y-6 pt-4 max-w-3xl">
      <Card>
        <CardHeader>
          <CardTitle>Push to Deploy</CardTitle>
          <CardDescription>
            Trigger automatic builds and deployments when code is pushed to your Git repository or CI pipeline.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">Webhook URL</label>
            <div className="flex items-center gap-2">
              <Input
                readOnly
                value={effectiveUrl}
                className="font-mono text-xs bg-muted/50"
              />
              <Button variant="outline" size="sm" onClick={copyUrl}>
                {copiedUrl ? <Check className="size-4 text-emerald-500" /> : <Copy className="size-4" />}
                {copiedUrl ? 'Copied' : 'Copy'}
              </Button>
            </div>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">Webhook Secret</label>
            <div className="flex items-center gap-2">
              <Input
                readOnly
                type={showSecret ? 'text' : 'password'}
                value={webhook_secret}
                className="font-mono text-xs bg-muted/50"
              />
              <Button
                variant="outline"
                size="icon-sm"
                onClick={() => setShowSecret(!showSecret)}
                title={showSecret ? 'Hide secret' : 'Show secret'}
              >
                {showSecret ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
              </Button>
              <Button variant="outline" size="sm" onClick={copySecret}>
                {copiedSecret ? <Check className="size-4 text-emerald-500" /> : <Copy className="size-4" />}
                {copiedSecret ? 'Copied' : 'Copy'}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={handleRotate}
                disabled={rotateWebhook.isPending}
                title="Rotate secret"
              >
                {rotateWebhook.isPending ? <Loader2 className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
                Rotate
              </Button>
            </div>
            <p className="text-muted-foreground text-xs">
              Secret is required to authenticate webhook triggers. You can rotate it at any time if compromised.
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Setup Instructions</CardTitle>
          <CardDescription>
            Choose your platform below to configure automated webhook triggers.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6 text-sm">
          <div className="space-y-2">
            <h4 className="font-semibold text-sm">GitHub Webhooks</h4>
            <ol className="list-inside list-decimal text-muted-foreground space-y-1 text-xs leading-5">
              <li>Open your repository settings on GitHub and select <strong>Webhooks &rarr; Add webhook</strong>.</li>
              <li>Paste the <strong>Webhook URL</strong> into the <strong>Payload URL</strong> field.</li>
              <li>Set <strong>Content type</strong> to <code className="font-mono text-foreground">application/json</code>.</li>
              <li>Paste the <strong>Webhook Secret</strong> into the <strong>Secret</strong> field. dockdeploy automatically validates the HMAC-SHA256 signature (<code className="font-mono text-foreground">X-Hub-Signature-256</code>).</li>
              <li>Select <strong>Just the push event</strong> and click <strong>Add webhook</strong>.</li>
            </ol>
          </div>

          <div className="space-y-2 border-t pt-4">
            <h4 className="font-semibold text-sm">GitLab Webhooks</h4>
            <ol className="list-inside list-decimal text-muted-foreground space-y-1 text-xs leading-5">
              <li>Open your repository settings on GitLab and select <strong>Settings &rarr; Webhooks &rarr; Add new webhook</strong>.</li>
              <li>Paste the <strong>Webhook URL</strong> into the <strong>URL</strong> field.</li>
              <li>Paste the <strong>Webhook Secret</strong> into the <strong>Secret token</strong> field (<code className="font-mono text-foreground">X-Gitlab-Token</code>).</li>
              <li>Ensure <strong>Push events</strong> is checked and click <strong>Add webhook</strong>.</li>
            </ol>
          </div>

          <div className="space-y-2 border-t pt-4">
            <h4 className="font-semibold text-sm">Generic CI / cURL</h4>
            <p className="text-muted-foreground text-xs">
              Trigger a build from GitHub Actions, GitLab CI, Jenkins, or any shell script via Bearer token:
            </p>
            <div className="rounded bg-muted p-3 font-mono text-xs overflow-x-auto text-foreground">
              curl -X POST \<br />
              &nbsp;&nbsp;-H "Authorization: Bearer {webhook_secret}" \<br />
              &nbsp;&nbsp;{effectiveUrl}
            </div>
            <p className="text-muted-foreground text-xs">
              Or pass the token in a query parameter:
            </p>
            <div className="rounded bg-muted p-3 font-mono text-xs overflow-x-auto text-foreground">
              curl -X POST "{effectiveUrl}?token={webhook_secret}"
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

/**
 * Environment editor.
 *
 * Secret values are never sent back by the API, so they render as a marker
 * rather than an empty box that would look like the value had been lost. A
 * secret is only replaced when a new value is typed in its place.
 */
function EnvironmentTab({ deploymentID, editable }: { deploymentID: string; editable: boolean }) {
  const env = useDeploymentEnv(deploymentID)
  const save = useSetDeploymentEnv(deploymentID)
  const [draft, setDraft] = useState<string | null>(null)

  if (env.isPending) return <Skeleton className="h-40 max-w-3xl" />
  if (env.isError) return <Alert variant="danger">{env.error.message}</Alert>

  // Bound once so the closures below do not each have to re-narrow it.
  const current = env.data

  const SECRET_MARKER = '<kept>'

  const asText = (vars: EnvVar[]) =>
    vars
      .map((v) => `${v.key}=${v.is_secret ? SECRET_MARKER : v.value}`)
      .join('\n')

  const text = draft ?? asText(current)
  const dirty = draft !== null && draft !== asText(current)

  function onSave() {
    const existing = new Map(current.map((v) => [v.key, v]))
    const secretish = /(PASSWORD|SECRET|TOKEN|KEY|CREDENTIAL|PRIVATE|DSN|_URL$)/i

    const parsed: EnvVar[] = []
    for (const raw of text.split('\n')) {
      const line = raw.trim()
      if (line === '' || line.startsWith('#')) continue
      const index = line.indexOf('=')
      if (index <= 0) continue

      const key = line.slice(0, index).trim()
      const value = line.slice(index + 1).trim()
      const previous = existing.get(key)

      // The marker means "leave this one alone". The API cannot round-trip a
      // secret, so an unchanged one has to be re-sent from what we still hold.
      if (value === SECRET_MARKER && previous?.is_secret) {
        parsed.push({ key, value: '', is_secret: true })
        continue
      }
      parsed.push({ key, value, is_secret: previous?.is_secret ?? secretish.test(key) })
    }

    save.mutate(parsed, { onSuccess: () => setDraft(null) })
  }

  const unchangedSecrets = current.some((v) => v.is_secret)

  return (
    <Card className="max-w-3xl">
      <CardHeader>
        <CardTitle>Environment</CardTitle>
        <CardDescription>
          Written to a <code className="font-mono">.env</code> file with mode 0600 on the server
          and passed to the container. Changes apply on the next deploy.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {save.isError ? <Alert variant="danger">{save.error.message}</Alert> : null}
        {save.isSuccess && !dirty ? (
          <Alert variant="info" className="text-xs">
            Saved. Deploy to apply the change.
          </Alert>
        ) : null}

        <Textarea
          value={text}
          onChange={(e) => setDraft(e.target.value)}
          readOnly={!editable}
          className="min-h-48 font-mono text-xs"
          spellCheck={false}
        />

        {unchangedSecrets ? (
          <p className="text-muted-foreground text-xs">
            <code className="font-mono">{SECRET_MARKER}</code> stands for a stored secret. Leave it
            to keep the current value, or type a new one to replace it.
          </p>
        ) : null}

        {editable ? (
          <div className="flex items-center gap-3">
            <Button size="sm" disabled={!dirty || save.isPending} onClick={onSave}>
              {save.isPending ? <Loader2 className="animate-spin" aria-hidden /> : <Save aria-hidden />}
              Save
            </Button>
            {dirty ? (
              <Button variant="ghost" size="sm" onClick={() => setDraft(null)}>
                Discard
              </Button>
            ) : null}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
