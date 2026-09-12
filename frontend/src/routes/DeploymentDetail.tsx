import { ExternalLink, Loader2, Rocket, Save, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'

import { RunLogViewer } from '@/components/deployments/RunLogViewer'
import { PageHeader } from '@/components/layout/PageHeader'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
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
  useRuns,
  useSetDeploymentEnv,
  type EnvVar,
  type Run,
} from '@/lib/deployments'
import { can, useSession } from '@/lib/session'
import { formatRelative } from '@/lib/utils'

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
                {live ? 'Deploying' : 'Deploy'}
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
                          <TableCell className="text-muted-foreground truncate font-mono text-xs">
                            {run.image_ref || '—'}
                          </TableCell>
                          <TableCell className="tabular text-sm">{duration(run)}</TableCell>
                          <TableCell className="text-muted-foreground text-right text-sm">
                            {formatRelative(run.queued_at)}
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
                  <dd>Built on the server</dd>
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
