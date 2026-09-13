import {
  AlertCircle,
  Check,
  CheckCircle2,
  Copy,
  Eye,
  EyeOff,
  KeyRound,
  Loader2,
  Play,
  ShieldAlert,
  Terminal,
} from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'

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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useExecRoot, type ExecRootResult, type Server } from '@/lib/servers'

interface ServerRootExecDialogProps {
  server: Server
  open: boolean
  onOpenChange: (open: boolean) => void
}

const PRESET_COMMANDS = [
  { label: 'Verify Sudo', cmd: 'id && whoami' },
  { label: 'Check Nginx', cmd: 'nginx -t && systemctl status nginx --no-pager' },
  { label: 'Reload Nginx', cmd: 'systemctl reload nginx || service nginx reload' },
  {
    label: 'Install Nginx & Certbot (Debian/Ubuntu)',
    cmd: 'apt-get update && apt-get install -y nginx certbot python3-certbot-nginx',
  },
  { label: 'Check Disk Usage', cmd: 'df -h && du -sh /var/lib/docker 2>/dev/null || true' },
]

export function ServerRootExecDialog({ server, open, onOpenChange }: ServerRootExecDialogProps) {
  const [command, setCommand] = useState('nginx -t')
  const [sudoPassword, setSudoPassword] = useState('')
  const [saveSudo, setSaveSudo] = useState(true)
  const [showPassword, setShowPassword] = useState(false)
  const [result, setResult] = useState<ExecRootResult | null>(null)
  const [copied, setCopied] = useState(false)

  const execRoot = useExecRoot(server.id)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!command.trim()) return

    try {
      const res = await execRoot.mutateAsync({
        command: command.trim(),
        sudo_password: sudoPassword.trim() || undefined,
        save_sudo: saveSudo,
      })
      setResult(res)
      if (res.success) {
        toast.success(`Root command executed successfully (exit code ${res.exit_code})`)
      } else {
        toast.error(`Root command failed with exit code ${res.exit_code}`)
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Execution failed'
      toast.error(msg)
    }
  }

  const handleCopyOutput = async () => {
    if (!result) return
    const out = [result.stdout, result.stderr].filter(Boolean).join('\n')
    try {
      await navigator.clipboard.writeText(out)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // ignore
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto p-6 bg-zinc-950 border-zinc-800 text-zinc-100">
        <form onSubmit={handleSubmit} className="space-y-4">
          <DialogHeader>
            <div className="flex items-center gap-2.5">
              <div className="flex size-9 items-center justify-center rounded-lg border border-amber-500/20 bg-amber-500/10 text-amber-400">
                <ShieldAlert className="size-5" />
              </div>
              <div>
                <DialogTitle className="text-base font-semibold text-zinc-100">
                  Execute Elevated Root Command
                </DialogTitle>
                <DialogDescription className="text-xs text-zinc-400">
                  Run maintenance or configuration commands with sudo privileges on{' '}
                  <span className="text-zinc-200 font-medium">{server.name}</span> ({server.host})
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          {/* Quick presets */}
          <div className="space-y-1.5">
            <Label className="text-xs text-zinc-400">Quick Commands:</Label>
            <div className="flex flex-wrap gap-1.5">
              {PRESET_COMMANDS.map((preset) => (
                <button
                  key={preset.label}
                  type="button"
                  onClick={() => setCommand(preset.cmd)}
                  className="rounded border border-zinc-800 bg-zinc-900 px-2 py-1 text-[11px] font-mono text-zinc-300 hover:border-zinc-700 hover:bg-zinc-800 hover:text-white transition-colors"
                >
                  {preset.label}
                </button>
              ))}
            </div>
          </div>

          {/* Command input */}
          <div className="space-y-1.5">
            <Label htmlFor="root-command-input" className="text-xs font-medium text-zinc-300">
              Command to execute as root
            </Label>
            <div className="relative">
              <Terminal className="absolute left-3 top-2.5 size-4 text-sky-400" />
              <Input
                id="root-command-input"
                value={command}
                onChange={(e) => setCommand(e.target.value)}
                placeholder="e.g. nginx -t or systemctl restart nginx"
                required
                className="pl-9 font-mono text-xs bg-zinc-900 border-zinc-800 text-zinc-100 placeholder:text-zinc-500 focus-visible:ring-amber-500"
              />
            </div>
          </div>

          {/* Command being run preview */}
          <div className="rounded-lg border border-zinc-800 bg-zinc-900/80 p-3 font-mono text-xs space-y-1">
            <span className="text-[10px] uppercase font-semibold text-zinc-400">
              Command will be run as:
            </span>
            <div className="text-amber-400 font-mono text-[11px] break-all">
              sudo sh -c {JSON.stringify(command || '')}
            </div>
          </div>

          {/* Sudo password input */}
          <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
            <div className="space-y-1.5">
              <Label htmlFor="server-sudo-pass" className="text-xs font-medium text-zinc-300">
                Sudo Password {server.has_sudo_password ? '(optional override)' : '(required)'}
              </Label>
              <div className="relative">
                <KeyRound className="absolute left-3 top-2.5 size-4 text-zinc-500" />
                <Input
                  id="server-sudo-pass"
                  type={showPassword ? 'text' : 'password'}
                  value={sudoPassword}
                  onChange={(e) => setSudoPassword(e.target.value)}
                  placeholder={
                    server.has_sudo_password
                      ? '•••••••• (using saved server password)'
                      : 'Enter sudo password...'
                  }
                  className="pl-9 pr-9 font-mono text-xs bg-zinc-900 border-zinc-800 text-zinc-100 placeholder:text-zinc-500 focus-visible:ring-amber-500"
                />
                <button
                  type="button"
                  className="absolute right-2.5 top-2.5 text-zinc-400 hover:text-zinc-200"
                  onClick={() => setShowPassword((prev) => !prev)}
                  tabIndex={-1}
                >
                  {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              </div>
            </div>

            <div className="flex items-end pb-1">
              <div className="flex items-center gap-2">
                <input
                  id="save-sudo-opt"
                  type="checkbox"
                  checked={saveSudo}
                  onChange={(e) => setSaveSudo(e.target.checked)}
                  className="size-4 rounded border-zinc-700 bg-zinc-900 text-amber-600 focus:ring-amber-500"
                />
                <Label
                  htmlFor="save-sudo-opt"
                  className="text-xs text-zinc-400 cursor-pointer select-none"
                >
                  Save on server
                </Label>
              </div>
            </div>
          </div>

          {/* Results Console */}
          {result ? (
            <div className="space-y-1.5 pt-2">
              <div className="flex items-center justify-between text-xs">
                <div className="flex items-center gap-2">
                  <span className="font-semibold text-zinc-300">Output:</span>
                  <Badge variant={result.success ? 'success' : 'danger'} className="text-[10px]">
                    {result.success ? (
                      <CheckCircle2 className="size-3 mr-1" />
                    ) : (
                      <AlertCircle className="size-3 mr-1" />
                    )}
                    Exit code: {result.exit_code}
                  </Badge>
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-[11px] text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800"
                  onClick={handleCopyOutput}
                >
                  {copied ? (
                    <Check className="size-3 mr-1 text-emerald-400" />
                  ) : (
                    <Copy className="size-3 mr-1" />
                  )}
                  {copied ? 'Copied' : 'Copy'}
                </Button>
              </div>

              <div className="max-h-52 overflow-auto rounded-lg border border-zinc-800 bg-black p-3 font-mono text-xs shadow-inner">
                {result.stdout ? (
                  <pre className="text-zinc-200 whitespace-pre-wrap break-all text-[11px]">
                    {result.stdout}
                  </pre>
                ) : null}
                {result.stderr ? (
                  <pre className="text-red-400 whitespace-pre-wrap break-all text-[11px] mt-1">
                    {result.stderr}
                  </pre>
                ) : null}
                {!result.stdout && !result.stderr ? (
                  <span className="text-zinc-500 italic text-xs">(no output returned)</span>
                ) : null}
              </div>
            </div>
          ) : null}

          <DialogFooter className="pt-3 gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="border-zinc-800 text-zinc-300 hover:bg-zinc-900 hover:text-zinc-100"
              onClick={() => onOpenChange(false)}
            >
              Close
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={execRoot.isPending || !command.trim()}
              className="bg-amber-600 hover:bg-amber-500 text-white font-medium"
            >
              {execRoot.isPending ? (
                <>
                  <Loader2 className="size-3.5 mr-2 animate-spin" />
                  Running as Root...
                </>
              ) : (
                <>
                  <Play className="size-3.5 mr-1.5 fill-current" />
                  Execute as Root
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
