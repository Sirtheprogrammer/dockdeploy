import { Globe, KeyRound, Loader2, RefreshCw, Rocket, Sparkles } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'

import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { useAutoDetectServer, type DiscoveryReport } from '@/lib/servers'

export function DiscoverySummaryView({ report }: { report: DiscoveryReport }) {
  const domainsCount = report.domains.length
  const deploymentsCount = report.deployments.length
  const credsCount = report.credentials.length

  return (
    <div className="space-y-4 text-xs">
      {/* Metrics Row */}
      <div className="grid grid-cols-3 gap-2 text-center">
        <div className="bg-muted/40 rounded-lg border p-2.5">
          <div className="text-muted-foreground flex items-center justify-center gap-1 font-medium">
            <Globe className="size-3.5 text-blue-500" />
            <span>Domains</span>
          </div>
          <p className="mt-1 text-lg font-bold">{domainsCount}</p>
        </div>
        <div className="bg-muted/40 rounded-lg border p-2.5">
          <div className="text-muted-foreground flex items-center justify-center gap-1 font-medium">
            <Rocket className="size-3.5 text-amber-500" />
            <span>Deployments</span>
          </div>
          <p className="mt-1 text-lg font-bold">{deploymentsCount}</p>
        </div>
        <div className="bg-muted/40 rounded-lg border p-2.5">
          <div className="text-muted-foreground flex items-center justify-center gap-1 font-medium">
            <KeyRound className="size-3.5 text-emerald-500" />
            <span>Git Creds</span>
          </div>
          <p className="mt-1 text-lg font-bold">{credsCount}</p>
        </div>
      </div>

      {report.errors && report.errors.length > 0 ? (
        <Alert variant="warning">
          <ul className="list-inside list-disc space-y-1">
            {report.errors.map((err, i) => (
              <li key={i}>{err}</li>
            ))}
          </ul>
        </Alert>
      ) : null}

      {/* Discovered Domains */}
      {domainsCount > 0 ? (
        <div className="rounded-lg border p-3">
          <div className="mb-2 flex items-center justify-between">
            <span className="flex items-center gap-1.5 font-semibold text-foreground">
              <Globe className="size-3.5 text-blue-500" />
              Nginx Virtual Hosts ({domainsCount})
            </span>
            <Button variant="ghost" size="sm" asChild className="h-6 px-2 text-[11px]">
              <Link to="/domains">View in Domains &rarr;</Link>
            </Button>
          </div>
          <div className="divide-y text-muted-foreground">
            {report.domains.map((d) => (
              <div key={d.id} className="flex items-center justify-between py-1.5">
                <div className="min-w-0 pr-2">
                  <span className="font-mono font-medium text-foreground">{d.hostname}</span>
                  <span className="ml-2 text-[10px] text-muted-foreground">
                    &rarr; port {d.upstream_port}
                  </span>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  {d.ssl_mode === 'letsencrypt' ? (
                    <Badge variant="outline" className="border-blue-500/30 text-[10px] text-blue-500">
                      Let's Encrypt
                    </Badge>
                  ) : null}
                  <Badge
                    variant={d.action === 'created' ? 'success' : 'outline'}
                    className="text-[10px] capitalize"
                  >
                    {d.action}
                  </Badge>
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}

      {/* Discovered Deployments */}
      {deploymentsCount > 0 ? (
        <div className="rounded-lg border p-3">
          <div className="mb-2 flex items-center justify-between">
            <span className="flex items-center gap-1.5 font-semibold text-foreground">
              <Rocket className="size-3.5 text-amber-500" />
              Docker Deployments ({deploymentsCount})
            </span>
            <Button variant="ghost" size="sm" asChild className="h-6 px-2 text-[11px]">
              <Link to="/deployments">View in Deployments &rarr;</Link>
            </Button>
          </div>
          <div className="divide-y text-muted-foreground">
            {report.deployments.map((dep) => (
              <div key={dep.id} className="flex items-center justify-between py-1.5">
                <div className="min-w-0 pr-2">
                  <span className="font-mono font-medium text-foreground">{dep.name}</span>
                  <span className="ml-2 text-[10px] text-muted-foreground">
                    ({dep.source_type.replace('_', ' ')})
                  </span>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  <Badge
                    variant={dep.status === 'running' ? 'success' : 'outline'}
                    className="text-[10px] capitalize"
                  >
                    {dep.status}
                  </Badge>
                  <Badge
                    variant={dep.action === 'created' ? 'success' : 'outline'}
                    className="text-[10px] capitalize"
                  >
                    {dep.action}
                  </Badge>
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}

      {/* Discovered Git Credentials */}
      {credsCount > 0 ? (
        <div className="rounded-lg border p-3">
          <div className="mb-2 flex items-center justify-between">
            <span className="flex items-center gap-1.5 font-semibold text-foreground">
              <KeyRound className="size-3.5 text-emerald-500" />
              Auto-Saved Git Credentials ({credsCount})
            </span>
            <Button variant="ghost" size="sm" asChild className="h-6 px-2 text-[11px]">
              <Link to="/settings/credentials">View in Credentials &rarr;</Link>
            </Button>
          </div>
          <div className="divide-y text-muted-foreground">
            {report.credentials.map((c) => (
              <div key={c.id} className="flex items-center justify-between py-1.5">
                <div className="min-w-0 pr-2">
                  <p className="truncate font-medium text-foreground">{c.name}</p>
                  <p className="text-[10px] text-muted-foreground">
                    User: {c.username || 'git'} &bull; Type: {c.kind}
                  </p>
                </div>
                <Badge
                  variant={c.action === 'saved' ? 'success' : 'outline'}
                  className="shrink-0 text-[10px] capitalize"
                >
                  {c.action === 'saved' ? 'Autosaved' : 'Stored'}
                </Badge>
              </div>
            ))}
          </div>
        </div>
      ) : null}

      {domainsCount === 0 && deploymentsCount === 0 && credsCount === 0 ? (
        <p className="text-muted-foreground text-center py-4">
          No external Nginx virtual hosts, standalone containers, or git credentials found on this server.
        </p>
      ) : null}
    </div>
  )
}

export function AutoDetectDialog({
  serverID,
  serverName,
  trigger,
}: {
  serverID: string
  serverName: string
  trigger?: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [report, setReport] = useState<DiscoveryReport | null>(null)
  const autoDetect = useAutoDetectServer()

  function runDiscovery() {
    autoDetect.mutate(serverID, {
      onSuccess: (res) => {
        setReport(res.discovery)
      },
    })
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next && !report && !autoDetect.isPending) {
          runDiscovery()
        }
      }}
    >
      <DialogTrigger asChild>
        {trigger ?? (
          <Button variant="outline" size="sm">
            <Sparkles className="size-3.5 text-blue-500" aria-hidden />
            Auto-Detect Resources
          </Button>
        )}
      </DialogTrigger>

      <DialogContent className="max-w-xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Sparkles className="size-4 text-blue-500" aria-hidden />
            Auto-Detect Resources: {serverName}
          </DialogTitle>
          <DialogDescription>
            Scanning for live Nginx virtual hosts, Docker containers, and git credentials.
          </DialogDescription>
        </DialogHeader>

        {autoDetect.isPending ? (
          <div className="flex flex-col items-center justify-center py-10 text-center">
            <Loader2 className="size-8 animate-spin text-primary mb-3" />
            <p className="text-sm font-medium">Scanning server configuration...</p>
            <p className="text-muted-foreground text-xs mt-1">
              Inspecting Nginx configs, Docker containers, and SSH/git credentials.
            </p>
          </div>
        ) : null}

        {autoDetect.isError ? (
          <Alert variant="danger">
            {autoDetect.error?.message || 'Failed to auto-detect resources on server.'}
          </Alert>
        ) : null}

        {report && !autoDetect.isPending ? <DiscoverySummaryView report={report} /> : null}

        <DialogFooter className="gap-2 sm:justify-between">
          <Button
            variant="outline"
            size="sm"
            disabled={autoDetect.isPending}
            onClick={runDiscovery}
          >
            {autoDetect.isPending ? (
              <Loader2 className="animate-spin size-3.5" />
            ) : (
              <RefreshCw className="size-3.5" />
            )}
            Scan Again
          </Button>
          <Button size="sm" onClick={() => setOpen(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
