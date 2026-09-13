import CodeMirror from '@uiw/react-codemirror'
import { oneDark } from '@codemirror/theme-one-dark'
import {
  AlertCircle,
  Check,
  Copy,
  Download,
  ExternalLink,
  FileCode,
  Globe,
  KeyRound,
  Lock,
  Radio,
  RefreshCw,
  Unlock,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { SudoPromptDialog } from '@/components/servers/SudoPromptDialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useSyncDomain, type Domain } from '@/lib/domains'

interface ViewConfigDialogProps {
  domain: Domain | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

function generateFallbackConfig(domain: Domain): string {
  const wsBlock = domain.websocket
    ? `    # WebSocket proxy headers\n    proxy_set_header Upgrade $http_upgrade;\n    proxy_set_header Connection "upgrade";\n`
    : ''

  if (domain.ssl_mode === 'letsencrypt') {
    return `# Auto-generated Nginx Virtual Host for ${domain.hostname}
# Upstream Target: 127.0.0.1:${domain.upstream_port} (SSL: Let's Encrypt)

server {
    listen 80;
    listen [::]:80;
    server_name ${domain.hostname};

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name ${domain.hostname};

    ssl_certificate /etc/letsencrypt/live/${domain.hostname}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${domain.hostname}/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:${domain.upstream_port};
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
${wsBlock}    }
}
`
  }

  return `# Auto-generated Nginx Virtual Host for ${domain.hostname}
# Upstream Target: 127.0.0.1:${domain.upstream_port}

server {
    listen 80;
    listen [::]:80;
    server_name ${domain.hostname};

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        proxy_pass http://127.0.0.1:${domain.upstream_port};
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
${wsBlock}    }
}
`
}

export function ViewConfigDialog({ domain, open, onOpenChange }: ViewConfigDialogProps) {
  const [copied, setCopied] = useState(false)
  const [showSudoDialog, setShowSudoDialog] = useState(false)
  const syncDomain = useSyncDomain()

  const configText = useMemo(() => {
    if (!domain) return ''
    if (domain.config_rendered && domain.config_rendered.trim().length > 0) {
      return domain.config_rendered.trim()
    }
    return generateFallbackConfig(domain)
  }, [domain])

  const nginxCommands = useMemo(() => {
    if (!domain) return []
    return [
      `# 1. Install virtual host configuration in Nginx sites directory`,
      `cp /tmp/dockdeploy-${domain.hostname}.conf /etc/nginx/sites-available/${domain.hostname}.conf`,
      `ln -sf /etc/nginx/sites-available/${domain.hostname}.conf /etc/nginx/sites-enabled/${domain.hostname}.conf`,
      `chmod 644 /etc/nginx/sites-available/${domain.hostname}.conf`,
      ``,
      `# 2. Test system Nginx configuration syntax`,
      `nginx -t`,
      ``,
      `# 3. Reload Nginx service without dropping connections`,
      `systemctl reload nginx || service nginx reload || nginx -s reload`,
      ...(domain.ssl_mode === 'letsencrypt'
        ? [
            ``,
            `# 4. Request Let's Encrypt TLS Certificate via ACME webroot challenge`,
            `certbot certonly --webroot -w /var/www/certbot -d ${domain.hostname} --non-interactive --agree-tos`,
            ``,
            `# 5. Reload Nginx with updated TLS certificate paths`,
            `systemctl reload nginx || service nginx reload || nginx -s reload`,
          ]
        : []),
    ]
  }, [domain])

  if (!domain) return null

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(configText)
      setCopied(true)
      toast.success('Configuration copied to clipboard')
      setTimeout(() => setCopied(false), 2000)
    } catch {
      toast.error('Failed to copy configuration')
    }
  }

  const handleDownload = () => {
    try {
      const blob = new Blob([configText], { type: 'text/plain;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `${domain.hostname}.conf`
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(url)
      toast.success(`Downloaded ${domain.hostname}.conf`)
    } catch {
      toast.error('Failed to trigger download')
    }
  }

  const handleDeployWithSudo = async (sudoPassword: string, saveSudo: boolean) => {
    try {
      await syncDomain.mutateAsync({
        id: domain.id,
        sudo_password: sudoPassword,
        save_sudo: saveSudo,
      })
      setShowSudoDialog(false)
      toast.success('Virtual host deployed and Nginx reloaded successfully!')
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Deployment failed'
      toast.error(msg)
      throw err
    }
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-w-3xl max-h-[88vh] flex flex-col gap-3">
          <DialogHeader>
            <div className="flex items-center justify-between gap-2 pr-6">
              <DialogTitle className="flex items-center gap-2 font-mono text-base font-semibold">
                <FileCode className="size-5 text-sky-400" />
                <span>Nginx Configuration: {domain.hostname}</span>
              </DialogTitle>
              <a
                href={`${domain.ssl_mode === 'letsencrypt' ? 'https' : 'http'}://${domain.hostname}`}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                <Globe className="size-3.5" />
                <span>Visit</span>
                <ExternalLink className="size-3" />
              </a>
            </div>
            <DialogDescription>
              Virtual host configuration deployed to the server and managed by dockdeploy.
            </DialogDescription>
          </DialogHeader>

          <div className="flex flex-wrap items-center gap-2">
            <Badge
              variant={
                domain.status === 'active'
                  ? 'success'
                  : domain.status === 'error'
                    ? 'danger'
                    : 'warning'
              }
            >
              Status: {domain.status}
            </Badge>
            <Badge variant={domain.ssl_mode === 'letsencrypt' ? 'success' : 'outline'}>
              {domain.ssl_mode === 'letsencrypt' ? (
                <Lock className="size-3" />
              ) : (
                <Unlock className="size-3" />
              )}
              {domain.ssl_mode === 'letsencrypt' ? "Let's Encrypt SSL" : 'HTTP Only'}
            </Badge>
            <Badge variant="outline" className="font-mono text-xs">
              Upstream: 127.0.0.1:{domain.upstream_port}
            </Badge>
            {domain.websocket && (
              <Badge variant="info">
                <Radio className="size-3" />
                WebSocket
              </Badge>
            )}
          </div>

          {domain.status === 'error' && domain.status_message && (
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">
              <div className="flex items-start gap-2.5">
                <AlertCircle className="size-4 shrink-0 mt-0.5" />
                <div className="space-y-0.5">
                  <span className="font-semibold">Nginx Deployment Notice:</span>
                  <p className="font-mono text-[11px] leading-tight text-destructive/90 break-all">
                    {domain.status_message}
                  </p>
                </div>
              </div>
              <Button
                type="button"
                size="sm"
                className="shrink-0 bg-amber-600 hover:bg-amber-500 text-white font-medium text-xs h-7 px-3 shadow"
                onClick={() => setShowSudoDialog(true)}
              >
                <KeyRound className="size-3.5 mr-1.5" />
                Deploy with Sudo Password
              </Button>
            </div>
          )}

          <div className="relative flex-1 overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950 font-mono text-xs shadow-inner">
            <div className="flex items-center justify-between border-b border-zinc-800 bg-zinc-900/90 px-3 py-1.5 text-[11px] text-zinc-400">
              <span className="font-mono">{domain.hostname}.conf</span>
              <div className="flex items-center gap-1.5">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-[11px] text-zinc-300 hover:text-white hover:bg-zinc-800"
                  onClick={handleCopy}
                >
                  {copied ? (
                    <Check className="size-3 mr-1 text-emerald-400" />
                  ) : (
                    <Copy className="size-3 mr-1" />
                  )}
                  {copied ? 'Copied' : 'Copy'}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-[11px] text-zinc-300 hover:text-white hover:bg-zinc-800"
                  onClick={handleDownload}
                >
                  <Download className="size-3 mr-1" />
                  Download
                </Button>
              </div>
            </div>
            <div className="max-h-[48vh] overflow-auto">
              <CodeMirror
                value={configText}
                height="auto"
                theme={oneDark}
                readOnly={true}
                editable={false}
                basicSetup={{
                  lineNumbers: true,
                  foldGutter: true,
                  highlightActiveLineGutter: false,
                  highlightSpecialChars: true,
                  drawSelection: true,
                  syntaxHighlighting: true,
                }}
                className="text-xs font-mono"
              />
            </div>
          </div>

          <DialogFooter className="flex items-center justify-between sm:justify-between w-full">
            <div className="flex items-center gap-2">
              <span className="text-[11px] text-muted-foreground font-mono">
                {configText.split('\n').length} lines
              </span>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 text-xs text-amber-400 hover:text-amber-300 hover:bg-zinc-800"
                onClick={() => setShowSudoDialog(true)}
              >
                <RefreshCw className="size-3 mr-1.5" />
                Deploy / Re-apply with Sudo
              </Button>
            </div>
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Sudo Execution Modal with Command Visibility */}
      <SudoPromptDialog
        open={showSudoDialog}
        onOpenChange={setShowSudoDialog}
        title={`Deploy Nginx Virtual Host for ${domain.hostname}`}
        description="Installing Nginx virtual hosts in /etc/nginx and reloading the web server requires root privileges."
        serverName={domain.server_name}
        commands={nginxCommands}
        actionLabel="Deploy as Root"
        isPending={syncDomain.isPending}
        error={syncDomain.error?.message}
        onConfirm={handleDeployWithSudo}
      />
    </>
  )
}
