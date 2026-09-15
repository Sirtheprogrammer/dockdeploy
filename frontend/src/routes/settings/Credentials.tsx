import { KeyRound, Loader2, Plus, Sparkles, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { FieldError } from '@/components/AuthLayout'
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
import { Textarea } from '@/components/ui/textarea'
import {
  useCreateGitCredential,
  useCreateRegistry,
  useDeleteGitCredential,
  useDeleteRegistry,
  useGitCredentials,
  useRegistries,
} from '@/lib/deployments'
import { useAutoDetectAllServers, useServers } from '@/lib/servers'
import { formatRelative } from '@/lib/utils'

function AddGitCredentialDialog() {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [kind, setKind] = useState<'token' | 'ssh_key'>('token')
  const [username, setUsername] = useState('')
  const [secret, setSecret] = useState('')

  const create = useCreateGitCredential()

  function reset() {
    setName('')
    setKind('token')
    setUsername('')
    setSecret('')
    create.reset()
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    create.mutate(
      { name, kind, username, secret },
      {
        onSuccess: () => {
          setOpen(false)
          reset()
        },
      },
    )
  }

  const errors = create.error?.fields ?? {}
  const formError = create.error && Object.keys(errors).length === 0 ? create.error.message : null

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
          Add credential
        </Button>
      </DialogTrigger>

      <DialogContent>
        <form onSubmit={onSubmit} noValidate className="grid gap-4">
          <DialogHeader>
            <DialogTitle>Git credential</DialogTitle>
            <DialogDescription>
              For private repositories. Encrypted before storage and never shown again.
            </DialogDescription>
          </DialogHeader>

          {formError ? <Alert variant="danger">{formError}</Alert> : null}

          <div className="space-y-1.5">
            <Label htmlFor="cred-name">Name</Label>
            <Input
              id="cred-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="github-deploy"
              required
              autoFocus
              aria-invalid={Boolean(errors.name)}
            />
            <FieldError message={errors.name} />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="cred-kind">Type</Label>
            <Select value={kind} onValueChange={(v) => setKind(v as 'token' | 'ssh_key')}>
              <SelectTrigger id="cred-kind">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="token">Access token (https)</SelectItem>
                <SelectItem value="ssh_key">SSH deploy key (git@host)</SelectItem>
              </SelectContent>
            </Select>
            <FieldError message={errors.kind} />
          </div>

          {kind === 'token' ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="cred-user">Username</Label>
                <Input
                  id="cred-user"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="your-username"
                />
                <p className="text-muted-foreground text-xs">
                  GitHub ignores this. GitLab wants <code className="font-mono">oauth2</code>;
                  Bitbucket wants your account name.
                </p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="cred-secret">Token</Label>
                <Input
                  id="cred-secret"
                  type="password"
                  value={secret}
                  onChange={(e) => setSecret(e.target.value)}
                  autoComplete="off"
                  required
                  aria-invalid={Boolean(errors.secret)}
                />
                <FieldError message={errors.secret} />
              </div>
            </>
          ) : (
            <div className="space-y-1.5">
              <Label htmlFor="cred-key">Private key</Label>
              <Textarea
                id="cred-key"
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                className="min-h-32 font-mono text-xs"
                spellCheck={false}
                required
                aria-invalid={Boolean(errors.secret)}
              />
              <FieldError message={errors.secret} />
              <p className="text-muted-foreground text-xs">
                The server doing the cloning needs an ssh client installed.
              </p>
            </div>
          )}

          <DialogFooter>
            <Button type="submit" disabled={create.isPending || !name || !secret}>
              {create.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
              Save credential
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function AddRegistryDialog() {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')

  const create = useCreateRegistry()

  function reset() {
    setName('')
    setUrl('')
    setUsername('')
    setPassword('')
    create.reset()
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    create.mutate(
      { name, url, username, password },
      {
        onSuccess: () => {
          setOpen(false)
          reset()
        },
      },
    )
  }

  const errors = create.error?.fields ?? {}
  const formError = create.error && Object.keys(errors).length === 0 ? create.error.message : null

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) reset()
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          <Plus aria-hidden />
          Add registry
        </Button>
      </DialogTrigger>

      <DialogContent>
        <form onSubmit={onSubmit} noValidate className="grid gap-4">
          <DialogHeader>
            <DialogTitle>Container registry</DialogTitle>
            <DialogDescription>
              Used for pulling private images. Pushing from here is not available yet.
            </DialogDescription>
          </DialogHeader>

          {formError ? <Alert variant="danger">{formError}</Alert> : null}

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="reg-name">Name</Label>
              <Input
                id="reg-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="ghcr"
                required
                autoFocus
                aria-invalid={Boolean(errors.name)}
              />
              <FieldError message={errors.name} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="reg-url">Registry URL</Label>
              <Input
                id="reg-url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="ghcr.io"
                className="font-mono text-xs"
                required
                aria-invalid={Boolean(errors.url)}
              />
              <FieldError message={errors.url} />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="reg-user">Username</Label>
            <Input
              id="reg-user"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              aria-invalid={Boolean(errors.username)}
            />
            <FieldError message={errors.username} />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="reg-pass">Password or token</Label>
            <Input
              id="reg-pass"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="off"
              required
              aria-invalid={Boolean(errors.password)}
            />
            <FieldError message={errors.password} />
          </div>

          <DialogFooter>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
              Save registry
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function Credentials() {
  const gitCredentials = useGitCredentials()
  const registries = useRegistries()
  const removeCredential = useDeleteGitCredential()
  const removeRegistry = useDeleteRegistry()
  const servers = useServers()
  const autoDetectAll = useAutoDetectAllServers()

  const hasServers = (servers.data?.length ?? 0) > 0

  return (
    <div className="grid max-w-4xl min-w-0 gap-4">
      <Card>
        <CardHeader className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
          <div className="space-y-1">
            <CardTitle>Git credentials</CardTitle>
            <CardDescription>
              Tokens and deploy keys for cloning private repositories. Secrets are encrypted with
              AES-256-GCM and are never returned by the API.
            </CardDescription>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {hasServers ? (
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
                Scan Servers
              </Button>
            ) : null}
            <AddGitCredentialDialog />
          </div>
        </CardHeader>
        <CardContent className="px-0">
          {removeCredential.isError ? (
            <Alert variant="danger" className="mx-5 mb-3">
              {removeCredential.error.message}
            </Alert>
          ) : null}

          {gitCredentials.data?.length === 0 ? (
            <EmptyState
              icon={KeyRound}
              title="No git credentials"
              description="Public repositories need none. Add one to deploy from a private repository."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Username</TableHead>
                  <TableHead>Added</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {gitCredentials.data?.map((credential) => (
                  <TableRow key={credential.id}>
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-1.5">
                        <span>{credential.name}</span>
                        {credential.name.startsWith('Server ') ? (
                          <Badge
                            variant="outline"
                            className="border-emerald-500/30 text-[10px] text-emerald-500 font-normal"
                          >
                            Auto-saved
                          </Badge>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">
                        {credential.kind === 'token' ? 'Token' : 'SSH key'}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground font-mono text-xs">
                      {credential.username || '—'}
                    </TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {formatRelative(credential.created_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="text-muted-foreground hover:text-destructive"
                        aria-label={`Delete ${credential.name}`}
                        disabled={removeCredential.isPending}
                        onClick={() => {
                          if (
                            confirm(
                              `Delete "${credential.name}"? Deployments using it will fail to clone until another is chosen.`,
                            )
                          ) {
                            removeCredential.mutate(credential.id)
                          }
                        }}
                      >
                        <Trash2 aria-hidden />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
          <div className="space-y-1">
            <CardTitle>Registries</CardTitle>
            <CardDescription>Logins for private container registries.</CardDescription>
          </div>
          <AddRegistryDialog />
        </CardHeader>
        <CardContent className="px-0">
          {registries.data?.length === 0 ? (
            <p className="text-muted-foreground px-5 py-6 text-center text-sm">
              No registries configured.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>URL</TableHead>
                  <TableHead>Username</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {registries.data?.map((registry) => (
                  <TableRow key={registry.id}>
                    <TableCell className="font-medium">{registry.name}</TableCell>
                    <TableCell className="font-mono text-xs">{registry.url}</TableCell>
                    <TableCell className="text-muted-foreground font-mono text-xs">
                      {registry.username}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="text-muted-foreground hover:text-destructive"
                        aria-label={`Delete ${registry.name}`}
                        disabled={removeRegistry.isPending}
                        onClick={() => removeRegistry.mutate(registry.id)}
                      >
                        <Trash2 aria-hidden />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
