import { Globe, Loader2, Plus } from 'lucide-react'
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
  const sudoMissing = selectedServer && caps && caps.sudo_mode === 'none'
  const certbotMissing = enableSSL && selectedServer && caps && !caps.certbot_version

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
      <DialogContent className="max-w-lg">
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

          {formError ? <Alert variant="danger">{formError}</Alert> : null}

          {nginxMissing ? (
            <Alert variant="warning" className="text-xs">
              {selectedServer.name} does not have Nginx installed. Install nginx before adding domains.
            </Alert>
          ) : null}

          {sudoMissing ? (
            <Alert variant="warning" className="text-xs">
              SSH user on {selectedServer.name} does not have sudo privileges required for Nginx.
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
