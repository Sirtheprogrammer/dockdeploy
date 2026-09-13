import {
  AlertCircle,
  Check,
  CheckCircle2,
  Copy,
  Download,
  Eye,
  EyeOff,
  Loader2,
  PackageCheck,
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
import {
  useInstallDocker,
  useProbeServer,
  type InstallDockerResult,
  type Server,
} from '@/lib/servers'

interface InstallDockerDialogProps {
  server: Server
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess?: () => void
}

export function InstallDockerDialog({
  server,
  open,
  onOpenChange,
  onSuccess,
}: InstallDockerDialogProps) {
  const [method, setMethod] = useState<'script' | 'repo'>('script')
  const [sudoPassword, setSudoPassword] = useState('')
  const [saveSudo, setSaveSudo] = useState(true)
  const [showPassword, setShowPassword] = useState(false)
  const [result, setResult] = useState<InstallDockerResult | null>(null)
  const [copied, setCopied] = useState(false)

  const installDocker = useInstallDocker(server.id)
  const probe = useProbeServer(server.id)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()

    try {
      const res = await installDocker.mutateAsync({
        method,
        sudo_password: sudoPassword.trim() || undefined,
        save_sudo: saveSudo,
      })
      setResult(res)
      if (res.success) {
        toast.success('Docker installed successfully and daemon is running!')
        void probe.mutate()
        onSuccess?.()
      } else {
        toast.error(`Docker installation exited with code ${res.exit_code}`)
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Installation failed'
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
              <div className="flex size-9 items-center justify-center rounded-lg border border-blue-500/20 bg-blue-500/10 text-blue-400">
                <PackageCheck className="size-5" />
              </div>
              <div>
                <DialogTitle className="text-base text-zinc-100">
                  Install Docker & Compose
                </DialogTitle>
                <DialogDescription className="text-xs text-zinc-400">
                  Official installation guide for fresh server{' '}
                  <span className="font-mono text-zinc-200">{server.name}</span> ({server.host})
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          <div className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-3 text-xs text-zinc-300 space-y-2">
            <p className="font-medium text-zinc-200">
              This automated installer performs the official Docker setup:
            </p>
            <ul className="list-disc pl-4 space-y-1 text-zinc-400">
              <li>Installs Docker Engine, containerd, and Compose plugin via official Docker channels.</li>
              <li>Enables and starts the <code className="text-blue-300">docker.service</code> daemon.</li>
              <li>Adds user <code className="text-blue-300">{server.username}</code> to the <code className="text-blue-300">docker</code> group.</li>
              <li>Configures the Docker socket permissions and verifies connectivity.</li>
            </ul>
          </div>

          <div className="space-y-2">
            <Label className="text-xs font-medium text-zinc-300">Installation Method</Label>
            <div className="grid grid-cols-2 gap-3">
              <button
                type="button"
                onClick={() => setMethod('script')}
                className={`p-3 rounded-md border text-left transition-colors ${
                  method === 'script'
                    ? 'border-blue-500 bg-blue-500/10 text-blue-200'
                    : 'border-zinc-800 bg-zinc-900/40 text-zinc-400 hover:border-zinc-700'
                }`}
              >
                <div className="font-medium text-xs text-zinc-200">Official Convenience Script</div>
                <div className="text-[11px] text-zinc-400 mt-1">
                  get.docker.com (Recommended for all Linux distros: Ubuntu, Debian, CentOS, RHEL, Rocky, Alma)
                </div>
              </button>

              <button
                type="button"
                onClick={() => setMethod('repo')}
                className={`p-3 rounded-md border text-left transition-colors ${
                  method === 'repo'
                    ? 'border-blue-500 bg-blue-500/10 text-blue-200'
                    : 'border-zinc-800 bg-zinc-900/40 text-zinc-400 hover:border-zinc-700'
                }`}
              >
                <div className="font-medium text-xs text-zinc-200">Official APT Repository</div>
                <div className="text-[11px] text-zinc-400 mt-1">
                  Step-by-step setup of Docker GPG keys and official apt sources (Debian / Ubuntu only)
                </div>
              </button>
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="docker-sudo" className="text-xs font-medium text-zinc-300">
              Sudo Password (if required for non-root user)
            </Label>
            <div className="relative">
              <Input
                id="docker-sudo"
                type={showPassword ? 'text' : 'password'}
                value={sudoPassword}
                onChange={(e) => setSudoPassword(e.target.value)}
                placeholder={server.has_sudo_password ? '•••••••• (Stored password available)' : 'Enter sudo password if user needs elevation'}
                className="pr-10 bg-zinc-900 border-zinc-800 text-zinc-100 placeholder:text-zinc-500 font-mono text-xs"
              />
              <button
                type="button"
                onClick={() => setShowPassword(!showPassword)}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-zinc-400 hover:text-zinc-200"
              >
                {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
              </button>
            </div>
            {sudoPassword.trim() ? (
              <label className="flex items-center gap-2 text-xs text-zinc-400 cursor-pointer pt-1">
                <input
                  type="checkbox"
                  checked={saveSudo}
                  onChange={(e) => setSaveSudo(e.target.checked)}
                  className="rounded border-zinc-700 bg-zinc-900 text-blue-500 focus:ring-0"
                />
                Save sudo password securely for future operations on this server
              </label>
            ) : null}
          </div>

          {result ? (
            <div className="space-y-2 border-t border-zinc-800 pt-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  {result.success ? (
                    <Badge variant="outline" className="border-emerald-500/30 bg-emerald-500/10 text-emerald-400 gap-1 text-xs">
                      <CheckCircle2 className="size-3" />
                      Installation Succeeded
                    </Badge>
                  ) : (
                    <Badge variant="outline" className="border-rose-500/30 bg-rose-500/10 text-rose-400 gap-1 text-xs">
                      <AlertCircle className="size-3" />
                      Exit code: {result.exit_code}
                    </Badge>
                  )}
                  {result.capabilities?.docker_version ? (
                    <span className="text-xs text-zinc-400 font-mono">
                      Docker {result.capabilities.docker_version}
                    </span>
                  ) : null}
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={handleCopyOutput}
                  className="h-7 text-xs text-zinc-400 hover:text-zinc-100"
                >
                  {copied ? <Check className="size-3 text-emerald-400" /> : <Copy className="size-3" />}
                  {copied ? 'Copied' : 'Copy log'}
                </Button>
              </div>

              <div className="rounded-md border border-zinc-800 bg-black/80 p-3 font-mono text-[11px] max-h-48 overflow-y-auto whitespace-pre-wrap leading-relaxed">
                {result.stdout && (
                  <div className="text-zinc-300">{result.stdout}</div>
                )}
                {result.stderr && (
                  <div className="text-rose-400 mt-2">{result.stderr}</div>
                )}
                {!result.stdout && !result.stderr && (
                  <div className="text-zinc-500 italic">No output produced</div>
                )}
              </div>
            </div>
          ) : null}

          <DialogFooter className="gap-2 sm:gap-0 pt-2 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              className="border-zinc-800 text-zinc-300 hover:bg-zinc-900"
            >
              Close
            </Button>
            <Button
              type="submit"
              disabled={installDocker.isPending}
              className="bg-blue-600 hover:bg-blue-500 text-white font-medium"
            >
              {installDocker.isPending ? (
                <>
                  <Loader2 className="mr-2 size-4 animate-spin" />
                  Installing Docker...
                </>
              ) : (
                <>
                  <Download className="mr-2 size-4" />
                  Install Docker Now
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
