import { AlertTriangle, Check, Fingerprint, Loader2, Plus, ShieldCheck } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'

import { FieldError } from '@/components/AuthLayout'
import { CapabilityList } from '@/components/servers/CapabilityList'
import { DiscoverySummaryView } from '@/components/servers/AutoDetectDialog'
import { InstallDockerDialog } from '@/components/servers/InstallDockerDialog'
import { Alert } from '@/components/ui/alert'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  useCreateServer,
  useFingerprint,
  type AuthMethod,
  type Capabilities,
  type CreateServerResult,
} from '@/lib/servers'
import { cn } from '@/lib/utils'

type Step = 'address' | 'verify' | 'credentials' | 'result'

export function AddServerDialog() {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [step, setStep] = useState<Step>('address')

  const [connType, setConnType] = useState<'ssh' | 'local'>('ssh')
  const [dockerSocket, setDockerSocket] = useState('/var/run/docker.sock')
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('22')
  const [username, setUsername] = useState('root')
  const [method, setMethod] = useState<AuthMethod>('key')
  const [password, setPassword] = useState('')
  const [privateKey, setPrivateKey] = useState('')
  const [passphrase, setPassphrase] = useState('')

  const [fingerprint, setFingerprint] = useState('')
  const [keyType, setKeyType] = useState('')
  const [result, setResult] = useState<CreateServerResult | null>(null)
  const [installDockerOpen, setInstallDockerOpen] = useState(false)

  const probe = useFingerprint()
  const create = useCreateServer()

  function reset() {
    setStep('address')
    setConnType('ssh')
    setDockerSocket('/var/run/docker.sock')
    setName('')
    setHost('')
    setPort('22')
    setUsername('root')
    setMethod('key')
    setPassword('')
    setPrivateKey('')
    setPassphrase('')
    setFingerprint('')
    setKeyType('')
    setResult(null)
    setInstallDockerOpen(false)
    probe.reset()
    create.reset()
  }

  function onConnectLocal(event: FormEvent) {
    event.preventDefault()
    create.mutate(
      {
        name: name.trim() || 'Local Docker',
        host: 'localhost',
        port: 0,
        username: 'local',
        auth_method: 'local',
        docker_socket: dockerSocket.trim() || '/var/run/docker.sock',
        host_key_fingerprint: 'local',
      },
      {
        onSuccess: (created) => {
          setResult(created)
          setStep('result')
        },
      },
    )
  }

  function onLookup(event: FormEvent) {
    event.preventDefault()
    probe.mutate(
      { host: host.trim(), port: Number(port) || 22 },
      {
        onSuccess: ({ fingerprint, key_type }) => {
          setFingerprint(fingerprint)
          setKeyType(key_type)
          setStep('verify')
        },
      },
    )
  }

  function onCreate(event: FormEvent) {
    event.preventDefault()
    create.mutate(
      {
        name: name.trim(),
        host: host.trim(),
        port: Number(port) || 22,
        username: username.trim(),
        auth_method: method,
        password: method === 'password' ? password : undefined,
        private_key: method === 'key' ? privateKey : undefined,
        passphrase: method === 'key' && passphrase ? passphrase : undefined,
        host_key_fingerprint: fingerprint,
      },
      {
        onSuccess: (created) => {
          setResult(created)
          setStep('result')
        },
      },
    )
  }

  const lookupErrors = probe.error?.fields ?? {}
  const createErrors = create.error?.fields ?? {}
  const createFormError =
    create.error && Object.keys(createErrors).length === 0 ? create.error.message : null

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) reset()
      }}
    >
      <DialogTrigger asChild>
        <Button>
          <Plus aria-hidden />
          Add server
        </Button>
      </DialogTrigger>

      <DialogContent className="max-w-xl">
        {step === 'address' ? (
          <div className="grid gap-4">
            <DialogHeader>
              <DialogTitle>Add a server</DialogTitle>
              <DialogDescription>
                Connect to a remote server over SSH, or manage your local Docker daemon directly.
              </DialogDescription>
            </DialogHeader>

            <div className="bg-muted flex rounded-lg p-1 text-sm font-medium">
              <button
                type="button"
                onClick={() => setConnType('ssh')}
                className={cn(
                  'flex-1 rounded-md py-1.5 transition-colors',
                  connType === 'ssh'
                    ? 'bg-background text-foreground shadow-xs'
                    : 'text-muted-foreground hover:text-foreground',
                )}
              >
                Remote Server (SSH)
              </button>
              <button
                type="button"
                onClick={() => {
                  setConnType('local')
                  if (!name) setName('Local Docker')
                }}
                className={cn(
                  'flex-1 rounded-md py-1.5 transition-colors',
                  connType === 'local'
                    ? 'bg-background text-foreground shadow-xs'
                    : 'text-muted-foreground hover:text-foreground',
                )}
              >
                Local Docker (Zero SSH)
              </button>
            </div>

            {connType === 'local' ? (
              <form onSubmit={onConnectLocal} noValidate className="grid gap-4">
                {createFormError ? <Alert variant="danger">{createFormError}</Alert> : null}

                <div className="space-y-1.5">
                  <Label htmlFor="local-server-name">Server name</Label>
                  <Input
                    id="local-server-name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Local Docker"
                    required
                    autoFocus
                    aria-invalid={Boolean(createErrors.name)}
                  />
                  <FieldError message={createErrors.name} />
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="local-docker-socket">Docker socket path</Label>
                  <Input
                    id="local-docker-socket"
                    value={dockerSocket}
                    onChange={(e) => setDockerSocket(e.target.value)}
                    placeholder="/var/run/docker.sock"
                    required
                    aria-invalid={Boolean(createErrors.docker_socket)}
                  />
                  <FieldError message={createErrors.docker_socket} />
                  <p className="text-muted-foreground text-xs">
                    Connects directly to your local Docker engine via Unix domain socket without needing an SSH server or credentials.
                  </p>
                </div>

                <DialogFooter>
                  <Button type="submit" disabled={create.isPending || !name.trim()}>
                    {create.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
                    Connect Local Docker
                  </Button>
                </DialogFooter>
              </form>
            ) : (
              <form onSubmit={onLookup} noValidate className="grid gap-4">
                {probe.error && Object.keys(lookupErrors).length === 0 ? (
                  <Alert variant="danger">{probe.error.message}</Alert>
                ) : null}

                <div className="grid gap-4 sm:grid-cols-[1fr_7rem]">
                  <div className="space-y-1.5">
                    <Label htmlFor="server-host">Host</Label>
                    <Input
                      id="server-host"
                      value={host}
                      onChange={(e) => setHost(e.target.value)}
                      placeholder="192.0.2.10 or deploy.example.com"
                      required
                      autoFocus
                      aria-invalid={Boolean(lookupErrors.host)}
                    />
                    <FieldError message={lookupErrors.host} />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="server-port">SSH port</Label>
                    <Input
                      id="server-port"
                      value={port}
                      onChange={(e) => setPort(e.target.value)}
                      inputMode="numeric"
                      aria-invalid={Boolean(lookupErrors.port)}
                    />
                    <FieldError message={lookupErrors.port} />
                  </div>
                </div>

                <DialogFooter>
                  <Button type="submit" disabled={probe.isPending || !host.trim()}>
                    {probe.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
                    Continue
                  </Button>
                </DialogFooter>
              </form>
            )}
          </div>
        ) : null}

        {step === 'verify' ? (
          <div className="grid gap-4">
            <DialogHeader>
              <DialogTitle>Verify the host key</DialogTitle>
              <DialogDescription>
                Check this fingerprint matches the server before continuing. dockdeploy pins it and
                will refuse to connect if it ever changes.
              </DialogDescription>
            </DialogHeader>

            <div className="bg-muted/40 rounded-md border p-4">
              <div className="text-muted-foreground mb-2 flex items-center gap-2 text-xs">
                <Fingerprint className="size-3.5" aria-hidden />
                {keyType}
              </div>
              <code className="text-sm break-all">{fingerprint}</code>
            </div>

            <Alert variant="warning" className="text-xs leading-relaxed">
              Confirm it on the server itself with:
              <code className="mt-1.5 block font-mono">
                ssh-keygen -lf /etc/ssh/ssh_host_{keyType.includes('ed25519') ? 'ed25519' : 'ecdsa'}
                _key.pub
              </code>
              Your credentials have not been sent anywhere yet.
            </Alert>

            <DialogFooter>
              <Button variant="outline" onClick={() => setStep('address')}>
                Back
              </Button>
              <Button onClick={() => setStep('credentials')}>
                <ShieldCheck aria-hidden />
                This matches
              </Button>
            </DialogFooter>
          </div>
        ) : null}

        {step === 'credentials' ? (
          <form onSubmit={onCreate} noValidate className="grid gap-4">
            <DialogHeader>
              <DialogTitle>Sign in to the server</DialogTitle>
              <DialogDescription>
                Credentials are encrypted before they are stored and are never shown again.
              </DialogDescription>
            </DialogHeader>

            {createFormError ? <Alert variant="danger">{createFormError}</Alert> : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="server-name">Name</Label>
                <Input
                  id="server-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="production-01"
                  required
                  autoFocus
                  aria-invalid={Boolean(createErrors.name)}
                />
                <FieldError message={createErrors.name} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="server-user">SSH user</Label>
                <Input
                  id="server-user"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  required
                  aria-invalid={Boolean(createErrors.username)}
                />
                <FieldError message={createErrors.username} />
              </div>
            </div>

            <div className="space-y-1.5">
              <Label>Authentication</Label>
              <div className="flex gap-2">
                {(['key', 'password'] as AuthMethod[]).map((option) => (
                  <button
                    key={option}
                    type="button"
                    onClick={() => setMethod(option)}
                    className={cn(
                      'flex-1 rounded-md border px-3 py-2 text-sm transition-colors',
                      method === option
                        ? 'border-primary bg-primary/10 text-foreground'
                        : 'text-muted-foreground hover:bg-accent/50',
                    )}
                  >
                    {option === 'key' ? 'Private key' : 'Password'}
                  </button>
                ))}
              </div>
            </div>

            {method === 'key' ? (
              <>
                <div className="space-y-1.5">
                  <Label htmlFor="server-key">Private key</Label>
                  <Textarea
                    id="server-key"
                    value={privateKey}
                    onChange={(e) => setPrivateKey(e.target.value)}
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                    className="min-h-28 font-mono text-xs"
                    spellCheck={false}
                    aria-invalid={Boolean(createErrors.private_key)}
                  />
                  <FieldError message={createErrors.private_key} />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="server-passphrase">Key passphrase (if any)</Label>
                  <Input
                    id="server-passphrase"
                    type="password"
                    value={passphrase}
                    onChange={(e) => setPassphrase(e.target.value)}
                    autoComplete="off"
                  />
                </div>
              </>
            ) : (
              <div className="space-y-1.5">
                <Label htmlFor="server-password">Password</Label>
                <Input
                  id="server-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="off"
                  aria-invalid={Boolean(createErrors.password)}
                />
                <FieldError message={createErrors.password} />
              </div>
            )}

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setStep('verify')}>
                Back
              </Button>
              <Button type="submit" disabled={create.isPending}>
                {create.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
                Connect and add
              </Button>
            </DialogFooter>
          </form>
        ) : null}

        {step === 'result' && result ? (
          <div className="grid gap-4">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                {result.server.status === 'online' ? (
                  <Check className="text-success size-4" aria-hidden />
                ) : (
                  <AlertTriangle className="text-warning size-4" aria-hidden />
                )}
                {result.server.name} added
              </DialogTitle>
              <DialogDescription>Here is what dockdeploy found on it.</DialogDescription>
            </DialogHeader>

            <CapabilityList
              capabilities={result.capabilities as Capabilities | null}
              onInstallDocker={() => setInstallDockerOpen(true)}
            />

            {result.discovery ? (
              <div className="border-t pt-3">
                <p className="mb-2 text-xs font-semibold text-foreground">Auto-Detected Resources</p>
                <DiscoverySummaryView report={result.discovery} />
              </div>
            ) : null}

            <InstallDockerDialog
              server={result.server}
              open={installDockerOpen}
              onOpenChange={setInstallDockerOpen}
              onSuccess={() => {
                setOpen(false)
                void navigate(`/servers/${result.server.id}`)
              }}
            />

            <DialogFooter>
              <Button variant="outline" onClick={reset}>
                Add another
              </Button>
              <Button
                onClick={() => {
                  setOpen(false)
                  void navigate(`/servers/${result.server.id}`)
                }}
              >
                Open server
              </Button>
            </DialogFooter>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
