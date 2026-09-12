import { ScrollText } from 'lucide-react'
import { useState } from 'react'

import { EmptyState } from '@/components/EmptyState'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useAudit, type AuditEntry } from '@/lib/admin'
import { formatRelative } from '@/lib/utils'

/** Failures are the interesting rows here, so status drives the only colour. */
function StatusBadge({ status }: { status: number }) {
  if (status >= 500) return <Badge variant="danger">{status}</Badge>
  if (status === 401 || status === 403) return <Badge variant="warning">{status}</Badge>
  if (status >= 400) return <Badge variant="outline">{status}</Badge>
  return <Badge variant="success">{status}</Badge>
}

/** Turns "POST /api/users/{userID}" into something readable at a glance. */
function describe(entry: AuditEntry): string {
  const [method = '', pattern = ''] = entry.action.split(' ')
  const path = pattern.replace(/^\/api\//, '').replace(/\/\{[^}]+\}/g, '')
  const verb =
    method === 'POST'
      ? 'created'
      : method === 'DELETE'
        ? 'deleted'
        : method === 'PATCH' || method === 'PUT'
          ? 'updated'
          : method.toLowerCase()
  return `${verb} ${path}`
}

function MetaCell({ meta }: { meta: Record<string, unknown> }) {
  const entries = Object.entries(meta ?? {}).filter(([key]) => key !== 'user_id')
  if (entries.length === 0) return <span className="text-muted-foreground">&mdash;</span>
  return (
    <span className="text-muted-foreground font-mono text-xs">
      {entries.map(([key, value]) => `${key}=${String(value)}`).join(' ')}
    </span>
  )
}

export function Audit() {
  const [limit, setLimit] = useState(50)
  const { data, isPending, isError, error } = useAudit(limit)

  return (
    <div className="max-w-5xl">
      <Card>
        <CardHeader>
          <CardTitle>Audit log</CardTitle>
          <CardDescription>
            Every action that changed something, successful or refused. Sign-ins and rejected
            requests are recorded too, so a misconfigured role or a probing token is visible here.
          </CardDescription>
        </CardHeader>

        <CardContent className="px-0">
          {isError ? (
            <Alert variant="danger" className="mx-5 mb-3">
              {error.message}
            </Alert>
          ) : null}

          {!isPending && !data?.entries.length ? (
            <EmptyState
              icon={ScrollText}
              title="Nothing recorded yet"
              description="Actions appear here as soon as anyone changes something on this instance."
            />
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-16">Status</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead>Actor</TableHead>
                    <TableHead>Details</TableHead>
                    <TableHead className="text-right">When</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {isPending ? (
                    <TableRow>
                      <TableCell colSpan={5} className="text-muted-foreground py-6 text-center">
                        Loading.
                      </TableCell>
                    </TableRow>
                  ) : null}

                  {data?.entries.map((entry) => (
                    <TableRow key={entry.id}>
                      <TableCell>
                        <StatusBadge status={entry.status} />
                      </TableCell>
                      <TableCell>
                        <p className="text-sm">{describe(entry)}</p>
                        <p className="text-muted-foreground font-mono text-xs">{entry.action}</p>
                      </TableCell>
                      <TableCell>
                        <p className="font-mono text-xs">
                          {entry.actor_email || <span className="text-muted-foreground">anonymous</span>}
                        </p>
                        <p className="text-muted-foreground font-mono text-xs">{entry.ip}</p>
                      </TableCell>
                      <TableCell>
                        <MetaCell meta={entry.meta} />
                      </TableCell>
                      <TableCell className="text-muted-foreground text-right text-sm whitespace-nowrap">
                        <time dateTime={entry.created_at} title={new Date(entry.created_at).toLocaleString()}>
                          {formatRelative(entry.created_at)}
                        </time>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {/* The API returns a cursor only when a full page came back, so
                  its presence is exactly the signal that more exist. */}
              {data?.before ? (
                <div className="flex justify-center border-t pt-4">
                  <Button variant="outline" size="sm" onClick={() => setLimit((n) => n + 50)}>
                    Show more
                  </Button>
                </div>
              ) : null}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
