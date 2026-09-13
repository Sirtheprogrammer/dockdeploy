import { Eye, EyeOff, Globe, KeyRound, Loader2, Plus, ShieldAlert } from 'lucide-react'
import { useState, type FormEvent, type ReactNode } from 'react'

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
import { useDeployments } from '@/lib/deployments'
import { useCreateDomain } from '@/lib/domains'
import { capabilitiesOf, useServers } from '@/lib/servers'

interface AddDomainDialogProps {
  trigger?: ReactNode
  defaultDeploymentID?: string
  defaultServerID?: string
  onSuccess?: () => void
}

export function AddDomainDialog({
  trigger,
  defaultDeploymentID,
  defaultServerID,
  onSuccess,
}: AddDomainDialogProps) {
  const [open, setOpen] = useState(false)
  const [serverID, setServerID] = useState(defaultServerID ?? '')
  const [deploymentID, setDeploymentID] = useState(defaultDeploymentID ?? '')
  const [hostname, setHostname] = useState('')
  const [upstreamPort, setUpstreamPort] = useState('')
  const [websocket, setWebsocket] = useState(false)
  const [enableSSL, setEnableSSL] = useState(true)
  const [sudoPassword, setSudoPassword] = useState('')
  const [saveSudo, setSaveSudo] = useState(true)
  const [showPassword, setShowPassword] = useState(false)

  const servers = useServers()
  const deployments = useDeployments()
  const createDomain = useCreateDomain()

  const selectedServer = servers.data?.find((s) => s.id === serverID)
  const caps = capabilitiesOf(selectedServer)

  // Filter deployments by chosen server if any
  const availableDeployments = deployments.data?.filter(
    (d) => !serverID || d.server_id === serverID,
  )

  function handleDeploymentChange(val: string) {
    const depID = val === 'none' ? '' : val
    setDeploymentID(depID)
    if (depID) {
      const dep = deployments.data?.find((d) => d.id === depID)
      if (dep) {
        if (!serverID) setServerID(dep.server_id)
        if (dep.host_port) setUpstreamPort(String(dep.host_port))
      }
    }
  }

  function handleServerChange(val: string) {
    setServerID(val)
    if (deploymentID) {
      const dep = deployments.data?.find((d) => d.id === deploymentID)
      if (dep && dep.server_id !== val) {
        setDeploymentID('')
      }
    }
  }

  function reset() {
    setServerID(defaultServerID ?? '')
    setDeploymentID(defaultDeploymentID ?? '')
    setHostname('')
    setUpstreamPort('')
    setWebsocket(false)
    setEnableSSL(true)
    setSudoPassword('')
    setSaveSudo(true)
    setShowPassword(false)
    createDomain.reset()
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    createDomain.mutate(
      {
        server_id: serverID,
        deployment_id: deploymentID || undefined,
        hostname: hostname.trim().toLowerCase(),
        upstream_port: Number(upstreamPort) || 80,
        websocket,
        ssl_mode: enableSSL ? 'letsencrypt' : 'none',
        sudo_password: sudoPassword.trim() || undefined,
        save_sudo: saveSudo,
      },
      {
        onSuccess: () => {
          setOpen(false)
          reset()
          onSuccess?.()
        },
      },
    )
  }

  const errors = createDomain.error?.fields ?? {}
  const formError =
    createDomain.error && Object.keys(errors).length === 0
      ? createDomain.error.message
      : null

  const nginxMissing = selectedServer && caps && !caps.nginx_version
  const certbotMissing = enableSSL && selectedServer && caps && !caps.certbot_version
  const needsSudoPrompt =
    selectedServer &&
    !selectedServer.has_sudo_password &&
    caps &&
    caps.sudo_mode !== 'root' &&
    caps.sudo_mode !== 'nopasswd'

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
            Add domain
          </Button>
        )}
      </DialogTrigger>
      <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
        <form onSubmit={onSubmit} className="space-y-4">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Globe className="h-5 w-5 text-muted-foreground" />
              Add Domain
            </DialogTitle>
            <DialogDescription>
              Create an Nginx virtual host on your server with automated Let's Encrypt TLS.
            </DialogDescription>
          </DialogHeader>

          {formError ? (
            <Alert variant="danger" className="text-xs">
              {formError}
            </Alert>
          ) : null}

          {nginxMissing ? (
            <Alert variant="warning" className="text-xs">
              {selectedServer.name} does not have Nginx installed. Install nginx before adding domains.
            </Alert>
          ) : null}

          {certbotMissing ? (
            <Alert variant="warning" className="text-xs">
              certbot is not installed on {selectedServer.name}. Certificates cannot be issued automatically without it.
            </Alert>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="domain-server">Server</Label>
            <Select value={serverID} onValueChange={handleServerChange} required>
              <SelectTrigger id="domain-server">
                <SelectValue placeholder="Choose a server..." />
              </SelectTrigger>
              <SelectContent>
                {servers.data?.map((s) => (
                  <SelectItem key={s.id} value={s.id}>
                    {s.name} ({s.host})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldError message={errors.server_id} />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="domain-deployment">Linked deployment (optional)</Label>
            <Select value={deploymentID || 'none'} onValueChange={handleDeploymentChange}>
              <SelectTrigger id="domain-deployment">
                <SelectValue placeholder="None (standalone upstream)" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">None (standalone upstream)</SelectItem>
                {availableDeployments?.map((d) => (
                  <SelectItem key={d.id} value={d.id}>
                    {d.name} {d.host_port ? `(port ${d.host_port})` : ''}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldError message={errors.deployment_id} />
          </div>

          <div className="grid gap-3 sm:grid-cols-[1fr_7rem]">
            <div className="space-y-1.5">
              <Label htmlFor="domain-hostname">Hostname</Label>
              <Input
                id="domain-hostname"
                value={hostname}
                onChange={(e) => setHostname(e.target.value)}
                placeholder="app.example.com"
                required
                className="font-mono text-sm"
                aria-invalid={Boolean(errors.hostname)}
              />
              <FieldError message={errors.hostname} />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="domain-port">Upstream Port</Label>
              <Input
                id="domain-port"
                type="number"
                value={upstreamPort}
                onChange={(e) => setUpstreamPort(e.target.value)}
                placeholder="8080"
                required
                min={1}
                max={65535}
                className="font-mono text-sm"
                aria-invalid={Boolean(errors.upstream_port)}
              />
              <FieldError message={errors.upstream_port} />
            </div>
          </div>

          <div className="rounded-lg border p-3 space-y-3 bg-muted/20">
            <div className="flex items-center justify-between">
              <div>
                <Label htmlFor="domain-ssl" className="cursor-pointer font-medium">
                  Let's Encrypt TLS (HTTPS)
                </Label>
                <p className="text-xs text-muted-foreground">
                  Issue automatic SSL certificate and redirect HTTP to HTTPS.
                </p>
              </div>
              <input
                id="domain-ssl"
                type="checkbox"
                checked={enableSSL}
                onChange={(e) => setEnableSSL(e.target.checked)}
                className="h-4 w-4 rounded border-gray-300 text-primary focus:ring-primary"
              />
            </div>

            <div className="flex items-center justify-between border-t pt-3">
              <div>
                <Label htmlFor="domain-ws" className="cursor-pointer font-medium">
                  WebSocket Support
                </Label>
                <p className="text-xs text-muted-foreground">
                  Pass Upgrade and Connection headers for realtime protocols.
                </p>
              </div>
              <input
                id="domain-ws"
                type="checkbox"
                checked={websocket}
                onChange={(e) => setWebsocket(e.target.checked)}
                className="h-4 w-4 rounded border-gray-300 text-primary focus:ring-primary"
              />
            </div>
          </div>

          {/* Sudo / Elevated Permissions Section */}
          <div className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-3 space-y-2.5">
            <div className="flex items-center gap-2">
              <ShieldAlert className="size-4 text-amber-400 shrink-0" />
              <div className="flex-1">
                <span className="text-xs font-semibold text-zinc-100">
                  Elevated Root Privileges (Sudo)
                </span>
                <p className="text-[11px] text-muted-foreground leading-tight">
                  Writing virtual hosts to /etc/nginx and reloading Nginx requires sudo.
                </p>
              </div>
              {selectedServer?.has_sudo_password ? (
                <span className="text-[10px] bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded px-1.5 py-0.5">
                  Password Saved
                </span>
              ) : needsSudoPrompt ? (
                <span className="text-[10px] bg-amber-500/10 text-amber-400 border border-amber-500/20 rounded px-1.5 py-0.5">
                  Password Required
                </span>
              ) : null}
            </div>

            <div className="space-y-1.5 pt-1">
              <Label htmlFor="domain-sudo" className="text-xs font-medium flex items-center gap-1.5">
                <KeyRound className="size-3 text-muted-foreground" />
                Server Sudo Password {selectedServer?.has_sudo_password ? '(optional override)' : ''}
              </Label>
              <div className="relative">
                <Input
                  id="domain-sudo"
                  type={showPassword ? 'text' : 'password'}
                  value={sudoPassword}
                  onChange={(e) => setSudoPassword(e.target.value)}
                  placeholder={
                    selectedServer?.has_sudo_password
                      ? '•••••••• (using saved server sudo password)'
                      : 'Enter server sudo password...'
                  }
                  className="pr-9 font-mono text-xs"
                />
                <button
                  type="button"
                  className="absolute right-2.5 top-2.5 text-muted-foreground hover:text-foreground"
                  onClick={() => setShowPassword((prev) => !prev)}
                  tabIndex={-1}
                >
                  {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              </div>
            </div>

            <div className="flex items-center gap-2 pt-0.5">
              <input
                id="domain-save-sudo"
                type="checkbox"
                checked={saveSudo}
                onChange={(e) => setSaveSudo(e.target.checked)}
                className="size-3.5 rounded border-gray-300 text-primary focus:ring-primary"
              />
              <Label
                htmlFor="domain-save-sudo"
                className="text-[11px] text-muted-foreground cursor-pointer select-none font-normal"
              >
                Remember sudo password on server credentials for future deployments
              </Label>
            </div>
          </div>

          <DialogFooter className="pt-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={createDomain.isPending}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={createDomain.isPending}>
              {createDomain.isPending ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Deploying...
                </>
              ) : (
                'Add Domain'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
