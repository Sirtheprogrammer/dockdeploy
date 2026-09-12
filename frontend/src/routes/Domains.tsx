import {
  AlertCircle,
  CheckCircle2,
  Clock,
  ExternalLink,
  FileCode,
  Globe,
  Loader2,
  RefreshCw,
  Search,
  Shield,
  ShieldAlert,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'

import { EmptyState } from '@/components/EmptyState'
import { AddDomainDialog } from '@/components/domains/AddDomainDialog'
import { ViewConfigDialog } from '@/components/domains/ViewConfigDialog'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  useDeleteDomain,
  useDomains,
  useIssueSSL,
  useSyncDomain,
  type Domain,
} from '@/lib/domains'
import { useServers } from '@/lib/servers'
import { formatExpiration } from '@/lib/utils'

export function Domains() {
  const [search, setSearch] = useState('')
  const [selectedServer, setSelectedServer] = useState<string>('all')
  const [configDomain, setConfigDomain] = useState<Domain | null>(null)

  const domainsQuery = useDomains(
    selectedServer !== 'all' ? { server_id: selectedServer } : undefined,
  )
  const serversQuery = useServers()
  const deleteMutation = useDeleteDomain()
  const issueSSLMutation = useIssueSSL()
  const syncMutation = useSyncDomain()

  const domains = domainsQuery.data ?? []
  const filteredDomains = domains.filter((d) =>
    d.hostname.toLowerCase().includes(search.toLowerCase().trim()),
  )

  return (
    <>
      <PageHeader
        title="Domains"
        description="Nginx virtual hosts and automated Let's Encrypt TLS certificates on your servers."
        actions={<AddDomainDialog />}
      />

      <div className="space-y-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="relative max-w-sm flex-1">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Filter domains..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-9 text-sm"
            />
          </div>

          <div className="flex items-center gap-2">
            <Select value={selectedServer} onValueChange={setSelectedServer}>
              <SelectTrigger className="w-[180px]">
                <SelectValue placeholder="All servers" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All servers</SelectItem>
                {serversQuery.data?.map((s) => (
                  <SelectItem key={s.id} value={s.id}>
                    {s.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        {domainsQuery.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
          </div>
        ) : domains.length === 0 ? (
          <EmptyState
            icon={Globe}
            title="No domains configured"
            description="Point your domains at services running on your servers. dockdeploy writes an Nginx virtual host, validates syntax, and issues Let's Encrypt certificates."
            action={<AddDomainDialog />}
          />
        ) : filteredDomains.length === 0 ? (
          <div className="rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground">
            No domains matched your filter.
          </div>
        ) : (
          <div className="overflow-hidden rounded-md border bg-card">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Hostname</TableHead>
                  <TableHead>Server</TableHead>
                  <TableHead>Deployment</TableHead>
                  <TableHead>Upstream</TableHead>
                  <TableHead>SSL Certificate</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredDomains.map((domain) => {
                  const isSyncing = syncMutation.isPending && syncMutation.variables === domain.id
                  const isIssuing =
                    issueSSLMutation.isPending && issueSSLMutation.variables === domain.id
                  const isDeleting =
                    deleteMutation.isPending && deleteMutation.variables === domain.id

                  return (
                    <TableRow key={domain.id}>
                      <TableCell className="font-medium">
                        <div className="flex items-center gap-2">
                          <Globe className="h-4 w-4 text-muted-foreground" />
                          <span className="font-mono text-xs">{domain.hostname}</span>
                          <a
                            href={`${domain.ssl_mode === 'letsencrypt' ? 'https' : 'http'}://${domain.hostname}`}
                            target="_blank"
                            rel="noreferrer"
                            className="text-muted-foreground hover:text-foreground"
                          >
                            <ExternalLink className="h-3 w-3" />
                          </a>
                        </div>
                      </TableCell>

                      <TableCell>
                        <Link
                          to={`/servers/${domain.server_id}`}
                          className="text-muted-foreground hover:text-foreground underline underline-offset-4 text-xs"
                        >
                          {domain.server_name || domain.server_id.slice(0, 8)}
                        </Link>
                      </TableCell>

                      <TableCell>
                        {domain.deployment_id ? (
                          <Link
                            to={`/deployments/${domain.deployment_id}`}
                            className="text-muted-foreground hover:text-foreground underline underline-offset-4 text-xs"
                          >
                            {domain.deployment_name || 'Deployment'}
                          </Link>
                        ) : (
                          <span className="text-muted-foreground text-xs">—</span>
                        )}
                      </TableCell>

                      <TableCell className="font-mono text-xs">
                        127.0.0.1:{domain.upstream_port}
                        {domain.websocket ? (
                          <Badge variant="outline" className="ml-1.5 text-[10px] px-1 py-0">
                            WS
                          </Badge>
                        ) : null}
                      </TableCell>

                      <TableCell>
                        {domain.ssl_mode === 'letsencrypt' ? (
                          <div className="flex items-center gap-1.5 text-xs">
                            {domain.cert_expires_at ? (
                              <>
                                <ShieldCheck className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
                                <span className="text-muted-foreground">
                                  {formatExpiration(domain.cert_expires_at)}
                                </span>
                              </>
                            ) : domain.status === 'error' ? (
                              <>
                                <ShieldAlert className="h-4 w-4 text-destructive" />
                                <span className="text-destructive">Issuance failed</span>
                              </>
                            ) : (
                              <>
                                <Clock className="h-4 w-4 text-amber-500" />
                                <span>Pending issuance</span>
                              </>
                            )}
                          </div>
                        ) : (
                          <span className="text-muted-foreground text-xs flex items-center gap-1">
                            <Shield className="h-3.5 w-3.5 opacity-40" />
                            None (HTTP)
                          </span>
                        )}
                      </TableCell>

                      <TableCell>
                        {domain.status === 'active' ? (
                          <Badge variant="success" className="flex w-fit items-center gap-1">
                            <CheckCircle2 className="h-3 w-3" />
                            Active
                          </Badge>
                        ) : domain.status === 'error' ? (
                          <div className="flex items-center gap-1.5" title={domain.status_message}>
                            <Badge variant="danger" className="flex w-fit items-center gap-1">
                              <AlertCircle className="h-3 w-3" />
                              Error
                            </Badge>
                            {domain.status_message ? (
                              <span className="text-xs text-destructive truncate max-w-[120px]">
                                {domain.status_message}
                              </span>
                            ) : null}
                          </div>
                        ) : (
                          <Badge variant="outline" className="flex w-fit items-center gap-1">
                            <Loader2 className="h-3 w-3 animate-spin" />
                            Pending
                          </Badge>
                        )}
                      </TableCell>

                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            title="View Nginx configuration"
                            onClick={() => setConfigDomain(domain)}
                          >
                            <FileCode className="h-4 w-4 text-muted-foreground" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            title="Sync Nginx configuration"
                            disabled={isSyncing}
                            onClick={() => syncMutation.mutate(domain.id)}
                          >
                            {isSyncing ? (
                              <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                            ) : (
                              <RefreshCw className="h-4 w-4 text-muted-foreground" />
                            )}
                          </Button>
                          {domain.ssl_mode === 'letsencrypt' ? (
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              title="Issue or renew SSL certificate"
                              disabled={isIssuing}
                              onClick={() => issueSSLMutation.mutate(domain.id)}
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
                                  `Delete domain "${domain.hostname}"?\n\nThis will remove the Nginx virtual host configuration from the server and stop routing traffic for this domain.`,
                                )
                              ) {
                                deleteMutation.mutate(domain.id)
                              }
                            }}
                          >
                            {isDeleting ? (
                              <Loader2 className="h-4 w-4 animate-spin text-destructive" />
                            ) : (
                              <Trash2 className="h-4 w-4" />
                            )}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </div>

      <ViewConfigDialog
        domain={configDomain}
        open={Boolean(configDomain)}
        onOpenChange={(open: boolean) => !open && setConfigDomain(null)}
      />
    </>
  )
}
