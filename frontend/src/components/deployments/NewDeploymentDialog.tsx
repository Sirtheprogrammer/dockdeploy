import { FileCode, GitBranch, Layers, Loader2, Package, Plus } from 'lucide-react'
import { useState, type FormEvent, type ReactNode } from 'react'
import { useNavigate } from 'react-router'

import { FieldError } from '@/components/AuthLayout'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import {
  useCreateDeployment,
  useGitCredentials,
  type EnvVar,
  type SourceType,
} from '@/lib/deployments'
import { capabilitiesOf, useServers } from '@/lib/servers'
import { cn } from '@/lib/utils'

const SOURCES: { value: SourceType; label: string; hint: string; icon: typeof GitBranch }[] = [
  {
    value: 'git_dockerfile',
    label: 'Git repository',
    hint: 'Clone and build a Dockerfile on the server.',
    icon: GitBranch,
  },
  {
    value: 'git_compose',
    label: 'Git + compose',
    hint: 'Clone and run a compose file from the repository.',
    icon: FileCode,
  },
  {
    value: 'raw_compose',
    label: 'Compose file',
    hint: 'Paste a compose file. No repository needed.',
    icon: Layers,
  },
  {
    value: 'image',
    label: 'Existing image',
    hint: 'Pull and run an image that is already published.',
    icon: Package,
  },
]

const SAMPLE_COMPOSE = `services:
  web:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "127.0.0.1:8081:80"
`

export function NewDeploymentDialog({ trigger }: { trigger?: ReactNode }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)

  const [serverID, setServerID] = useState('')
  const [name, setName] = useState('')
  const [source, setSource] = useState<SourceType>('git_dockerfile')
  const [repoURL, setRepoURL] = useState('')
  const [gitRef, setGitRef] = useState('main')
  const [credentialID, setCredentialID] = useState('')
  const [dockerfilePath, setDockerfilePath] = useState('Dockerfile')
  const [buildContext, setBuildContext] = useState('.')
  const [composePath, setComposePath] = useState('docker-compose.yml')
  const [composeContent, setComposeContent] = useState(SAMPLE_COMPOSE)
  const [imageRef, setImageRef] = useState('')
  const [containerPort, setContainerPort] = useState('80')
  const [envText, setEnvText] = useState('')

  const servers = useServers()
  const credentials = useGitCredentials(open)
  const create = useCreateDeployment()

  const usesGit = source === 'git_dockerfile' || source === 'git_compose'
  const usesCompose = source === 'git_compose' || source === 'raw_compose'

  const selectedServer = servers.data?.find((server) => server.id === serverID)
  const caps = capabilitiesOf(selectedServer)

  function reset() {
    setServerID('')
    setName('')
    setSource('git_dockerfile')
    setRepoURL('')
    setGitRef('main')
    setCredentialID('')
    setDockerfilePath('Dockerfile')
    setBuildContext('.')
    setComposePath('docker-compose.yml')
    setComposeContent(SAMPLE_COMPOSE)
    setImageRef('')
    setContainerPort('80')
    setEnvText('')
    create.reset()
  }

  /**
   * Parses the KEY=value textarea.
   *
   * Anything whose name looks secret is marked as such, so a password does not
   * end up stored in the clear because someone forgot to tick a box. The
   * server encrypts on that flag alone.
   */
  function parseEnv(): EnvVar[] {
    const secretish = /(PASSWORD|SECRET|TOKEN|KEY|CREDENTIAL|PRIVATE|DSN|_URL$)/i
    return envText
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line !== '' && !line.startsWith('#'))
      .map((line) => {
        const index = line.indexOf('=')
        if (index <= 0) return null
        const key = line.slice(0, index).trim()
        const value = line.slice(index + 1).trim()
        return { key, value, is_secret: secretish.test(key) }
      })
      .filter((entry): entry is EnvVar => entry !== null)
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    create.mutate(
      {
        server_id: serverID,
        name,
        source_type: source,
        build_strategy: 'remote',
        repo_url: usesGit ? repoURL : undefined,
        git_ref: usesGit ? gitRef : undefined,
        git_credential_id: usesGit && credentialID ? credentialID : null,
        dockerfile_path: source === 'git_dockerfile' ? dockerfilePath : undefined,
        build_context: source === 'git_dockerfile' ? buildContext : undefined,
        compose_path: source === 'git_compose' ? composePath : undefined,
        compose_content: source === 'raw_compose' ? composeContent : undefined,
        image_ref: source === 'image' ? imageRef : undefined,
        container_port: usesCompose ? undefined : Number(containerPort) || 80,
        env: parseEnv(),
      },
      {
        onSuccess: (deployment) => {
          setOpen(false)
          reset()
          void navigate(`/deployments/${deployment.id}`)
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
        {trigger ?? (
          <Button>
            <Plus aria-hidden />
            New deployment
          </Button>
        )}
      </DialogTrigger>

      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <form onSubmit={onSubmit} noValidate className="grid gap-5">
          <DialogHeader>
            <DialogTitle>New deployment</DialogTitle>
            <DialogDescription>
              dockdeploy clones and builds on the server itself, then publishes the container on a
              loopback port. Point a domain at it afterwards.
            </DialogDescription>
          </DialogHeader>

          {formError ? <Alert variant="danger">{formError}</Alert> : null}

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="deploy-name">Name</Label>
              <Input
                id="deploy-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Marketing site"
                required
                autoFocus
                aria-invalid={Boolean(errors.name)}
              />
              <FieldError message={errors.name} />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="deploy-server">Server</Label>
              <Select value={serverID} onValueChange={setServerID}>
                <SelectTrigger id="deploy-server">
                  <SelectValue placeholder="Choose a server" />
                </SelectTrigger>
                <SelectContent>
                  {servers.data?.map((server) => (
                    <SelectItem key={server.id} value={server.id}>
                      {server.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldError message={errors.server_id} />
            </div>
          </div>

          <div className="space-y-2">
            <Label>Source</Label>
            <div className="grid gap-2 sm:grid-cols-2">
              {SOURCES.map(({ value, label, hint, icon: Icon }) => (
                <button
                  key={value}
                  type="button"
                  onClick={() => setSource(value)}
                  className={cn(
                    'flex items-start gap-2.5 rounded-md border p-3 text-left transition-colors',
                    source === value
                      ? 'border-primary bg-primary/10'
                      : 'hover:bg-accent/50 border-border',
                  )}
                >
                  <Icon className="text-muted-foreground mt-0.5 size-4 shrink-0" aria-hidden />
                  <span className="min-w-0">
                    <span className="block text-sm font-medium">{label}</span>
                    <span className="text-muted-foreground block text-xs">{hint}</span>
                  </span>
                </button>
              ))}
            </div>
            <FieldError message={errors.source_type} />
          </div>

          {/* Warn before the deploy fails, using what the capability probe
              already found on the chosen server. */}
          {usesCompose && selectedServer && caps && !caps.compose_command ? (
            <Alert variant="warning" className="text-xs">
              {selectedServer.name} has no Docker Compose installed, so this deployment will not
              run there.
            </Alert>
          ) : null}
          {usesGit && selectedServer && caps && !caps.git_version ? (
            <Alert variant="warning" className="text-xs">
              {selectedServer.name} has no git installed, so it cannot clone a repository.
            </Alert>
          ) : null}

          {usesGit ? (
            <div className="grid gap-4">
              <div className="grid gap-4 sm:grid-cols-[1fr_10rem]">
                <div className="space-y-1.5">
                  <Label htmlFor="deploy-repo">Repository</Label>
                  <Input
                    id="deploy-repo"
                    value={repoURL}
                    onChange={(e) => setRepoURL(e.target.value)}
                    placeholder="https://github.com/you/app.git"
                    className="font-mono text-xs"
                    required
                    aria-invalid={Boolean(errors.repo_url)}
                  />
                  <FieldError message={errors.repo_url} />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="deploy-ref">Branch or tag</Label>
                  <Input
                    id="deploy-ref"
                    value={gitRef}
                    onChange={(e) => setGitRef(e.target.value)}
                    className="font-mono text-xs"
                    aria-invalid={Boolean(errors.git_ref)}
                  />
                  <FieldError message={errors.git_ref} />
                </div>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="deploy-cred">Credential</Label>
                <Select value={credentialID || 'none'} onValueChange={(v) => setCredentialID(v === 'none' ? '' : v)}>
                  <SelectTrigger id="deploy-cred">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">None (public repository)</SelectItem>
                    {credentials.data?.map((credential) => (
                      <SelectItem key={credential.id} value={credential.id}>
                        {credential.name} ({credential.kind === 'token' ? 'token' : 'SSH key'})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {credentials.data?.length === 0 ? (
                  <p className="text-muted-foreground text-xs">
                    Add credentials under Settings to deploy a private repository.
                  </p>
                ) : null}
              </div>
            </div>
          ) : null}

          {source === 'git_dockerfile' ? (
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="deploy-dockerfile">Dockerfile path</Label>
                <Input
                  id="deploy-dockerfile"
                  value={dockerfilePath}
                  onChange={(e) => setDockerfilePath(e.target.value)}
                  className="font-mono text-xs"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="deploy-context">Build context</Label>
                <Input
                  id="deploy-context"
                  value={buildContext}
                  onChange={(e) => setBuildContext(e.target.value)}
                  className="font-mono text-xs"
                />
              </div>
            </div>
          ) : null}

          {source === 'git_compose' ? (
            <div className="space-y-1.5">
              <Label htmlFor="deploy-compose-path">Compose file path</Label>
              <Input
                id="deploy-compose-path"
                value={composePath}
                onChange={(e) => setComposePath(e.target.value)}
                className="font-mono text-xs"
              />
            </div>
          ) : null}

          {source === 'raw_compose' ? (
            <div className="space-y-1.5">
              <Label htmlFor="deploy-compose">Compose file</Label>
              <Textarea
                id="deploy-compose"
                value={composeContent}
                onChange={(e) => setComposeContent(e.target.value)}
                className="min-h-40 font-mono text-xs"
                spellCheck={false}
                aria-invalid={Boolean(errors.compose_content)}
              />
              <FieldError message={errors.compose_content} />
              <p className="text-muted-foreground text-xs">
                Variables below are written to a <code className="font-mono">.env</code> beside it,
                so <code className="font-mono">{'${NAME}'}</code> works here.
              </p>
            </div>
          ) : null}

          {source === 'image' ? (
            <div className="space-y-1.5">
              <Label htmlFor="deploy-image">Image</Label>
              <Input
                id="deploy-image"
                value={imageRef}
                onChange={(e) => setImageRef(e.target.value)}
                placeholder="nginx:alpine"
                className="font-mono text-xs"
                required
                aria-invalid={Boolean(errors.image_ref)}
              />
              <FieldError message={errors.image_ref} />
            </div>
          ) : null}

          {!usesCompose ? (
            <div className="space-y-1.5 sm:max-w-40">
              <Label htmlFor="deploy-port">Container port</Label>
              <Input
                id="deploy-port"
                value={containerPort}
                onChange={(e) => setContainerPort(e.target.value)}
                inputMode="numeric"
                aria-invalid={Boolean(errors.container_port)}
              />
              <FieldError message={errors.container_port} />
              <p className="text-muted-foreground text-xs">
                The port your app listens on inside the container.
              </p>
            </div>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="deploy-env">Environment variables</Label>
            <Textarea
              id="deploy-env"
              value={envText}
              onChange={(e) => setEnvText(e.target.value)}
              placeholder={'DATABASE_URL=postgres://...\nLOG_LEVEL=info'}
              className="min-h-24 font-mono text-xs"
              spellCheck={false}
              aria-invalid={Boolean(errors.env)}
            />
            <FieldError message={errors.env} />
            <p className="text-muted-foreground text-xs">
              One <code className="font-mono">KEY=value</code> per line. Names containing PASSWORD,
              SECRET, TOKEN or KEY are encrypted automatically and never shown again.
            </p>
          </div>

          <DialogFooter>
            <Button type="submit" disabled={create.isPending || !serverID || !name}>
              {create.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
              Create deployment
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
