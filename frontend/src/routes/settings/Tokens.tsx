import { KeyRound, Loader2, Plus, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { FieldError } from '@/components/AuthLayout'
import { CopyButton } from '@/components/CopyButton'
import { EmptyState } from '@/components/EmptyState'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useCreateToken, useDeleteToken, useTokens, type CreatedToken } from '@/lib/admin'
import { ROLE_LABELS, useSession } from '@/lib/session'
import { formatRelative } from '@/lib/utils'

const EXPIRY_OPTIONS = [
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
  { value: '365', label: '1 year' },
  { value: '0', label: 'Never' },
]

function CreateTokenDialog() {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [expiry, setExpiry] = useState('90')
  const [created, setCreated] = useState<CreatedToken | null>(null)

  const create = useCreateToken()

  function reset() {
    setName('')
    setExpiry('90')
    setCreated(null)
    create.reset()
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    create.mutate({ name, expires_in_days: Number(expiry) }, { onSuccess: setCreated })
  }

  const fieldErrors = create.error?.fields ?? {}
  const formError = create.error && Object.keys(fieldErrors).length === 0 ? create.error.message : null

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) reset()
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus aria-hidden />
          New token
        </Button>
      </DialogTrigger>

      <DialogContent>
        {created ? (
          <>
            <DialogHeader>
              <DialogTitle>Copy your token now</DialogTitle>
              <DialogDescription>
                Only a hash of this token is stored. Once you close this dialog there is no way to
                see it again.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-2">
              <Label htmlFor="new-token">Token</Label>
              <div className="flex gap-2">
                <Input
                  id="new-token"
                  readOnly
                  value={created.token}
                  className="font-mono text-xs"
                  onFocus={(e) => e.currentTarget.select()}
                />
                <CopyButton value={created.token} />
              </div>
            </div>

            <Alert variant="info" className="text-xs">
              Send it as{' '}
              <code className="font-mono">Authorization: Bearer &lt;token&gt;</code>. It carries your
              own permissions, so it can do exactly what you can and nothing more.
            </Alert>

            <DialogFooter>
              <Button onClick={() => setOpen(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={onSubmit} noValidate className="grid gap-4">
            <DialogHeader>
              <DialogTitle>New API token</DialogTitle>
              <DialogDescription>
                For scripts and CI pipelines that need to trigger deployments without a browser.
              </DialogDescription>
            </DialogHeader>

            {formError ? <Alert variant="danger">{formError}</Alert> : null}

            <div className="space-y-1.5">
              <Label htmlFor="token-name">Name</Label>
              <Input
                id="token-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="github-actions"
                required
                autoFocus
                aria-invalid={Boolean(fieldErrors.name)}
              />
              <FieldError message={fieldErrors.name} />
              <p className="text-muted-foreground text-xs">
                So you can tell which token to revoke later.
              </p>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="token-expiry">Expires</Label>
              <Select value={expiry} onValueChange={setExpiry}>
                <SelectTrigger id="token-expiry">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {EXPIRY_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldError message={fieldErrors.expires_in_days} />
            </div>

            <DialogFooter>
              <Button type="submit" disabled={create.isPending || !name}>
                {create.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
                Create token
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

export function Tokens() {
  const { data: user } = useSession()
  const { data: tokens, isPending } = useTokens()
  const remove = useDeleteToken()

  return (
    <div className="max-w-4xl">
      <Card>
        <CardHeader className="flex-row items-start justify-between gap-2">
          <div className="space-y-1">
            <CardTitle>API tokens</CardTitle>
            <CardDescription>
              Tokens act as you and inherit your{' '}
              {user ? ROLE_LABELS[user.role].toLowerCase() : ''} permissions. Revoking one takes
              effect immediately.
            </CardDescription>
          </div>
          <CreateTokenDialog />
        </CardHeader>

        <CardContent className="px-0">
          {remove.isError ? (
            <Alert variant="danger" className="mx-5 mb-3">
              {remove.error.message}
            </Alert>
          ) : null}

          {!isPending && !tokens?.length ? (
            <EmptyState
              icon={KeyRound}
              title="No tokens yet"
              description="Create one to let a CI pipeline or script trigger deployments on your behalf."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Token</TableHead>
                  <TableHead>Last used</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="w-10" />
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

                {tokens?.map((token) => {
                  const expired = token.expires_at && new Date(token.expires_at) < new Date()
                  return (
                    <TableRow key={token.id}>
                      <TableCell className="font-medium">{token.name}</TableCell>
                      <TableCell>
                        <code className="text-muted-foreground text-xs">
                          {token.prefix}
                          &hellip;
                        </code>
                      </TableCell>
                      <TableCell className="text-muted-foreground text-sm">
                        {token.last_used_at ? formatRelative(token.last_used_at) : 'Never'}
                      </TableCell>
                      <TableCell className="text-sm">
                        {!token.expires_at ? (
                          <span className="text-muted-foreground">Never</span>
                        ) : expired ? (
                          <Badge variant="danger">Expired</Badge>
                        ) : (
                          <span className="text-muted-foreground">
                            {formatRelative(token.expires_at)}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          className="text-muted-foreground hover:text-destructive"
                          aria-label={`Revoke ${token.name}`}
                          disabled={remove.isPending}
                          onClick={() => {
                            if (confirm(`Revoke "${token.name}"? Anything using it stops working.`)) {
                              remove.mutate(token.id)
                            }
                          }}
                        >
                          <Trash2 aria-hidden />
                        </Button>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
