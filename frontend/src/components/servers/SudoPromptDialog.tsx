import { AlertCircle, Check, Copy, Eye, EyeOff, KeyRound, Loader2, ShieldAlert, Terminal } from 'lucide-react'
import { useState, type FormEvent } from 'react'

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

interface SudoPromptDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title?: string
  description?: string
  serverName?: string
  serverHost?: string
  commands?: string[] | string
  isPending?: boolean
  error?: string | null
  actionLabel?: string
  onConfirm: (sudoPassword: string, saveSudo: boolean) => Promise<void> | void
}

export function SudoPromptDialog({
  open,
  onOpenChange,
  title = 'Elevated Root Privileges Required',
  description = 'This operation requires root / sudo privileges on the remote server.',
  serverName,
  serverHost,
  commands,
  isPending = false,
  error = null,
  actionLabel = 'Run with Root Privileges',
  onConfirm,
}: SudoPromptDialogProps) {
  const [sudoPassword, setSudoPassword] = useState('')
  const [saveSudo, setSaveSudo] = useState(true)
  const [showPassword, setShowPassword] = useState(false)
  const [copied, setCopied] = useState(false)
  const [localError, setLocalError] = useState<string | null>(null)

  const commandsText = Array.isArray(commands)
    ? commands.join('\n')
    : typeof commands === 'string'
      ? commands
      : ''

  const handleCopyCommands = async () => {
    if (!commandsText) return
    try {
      await navigator.clipboard.writeText(commandsText)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // ignore
    }
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!sudoPassword.trim()) {
      setLocalError('Please enter the server sudo password.')
      return
    }
    setLocalError(null)
    try {
      await onConfirm(sudoPassword.trim(), saveSudo)
      setSudoPassword('')
    } catch (err: unknown) {
      if (err instanceof Error) {
        setLocalError(err.message)
      } else {
        setLocalError('Failed to execute command with sudo.')
      }
    }
  }

  const displayError = localError || error

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!isPending) {
          onOpenChange(next)
          if (!next) {
            setLocalError(null)
            setSudoPassword('')
          }
        }
      }}
    >
      <DialogContent className="max-w-xl p-6 bg-zinc-950 border-zinc-800 text-zinc-100">
        <form onSubmit={handleSubmit} className="space-y-4">
          <DialogHeader>
            <div className="flex items-center gap-2.5">
              <div className="flex size-9 items-center justify-center rounded-lg border border-amber-500/20 bg-amber-500/10 text-amber-400">
                <ShieldAlert className="size-5" />
              </div>
              <div>
                <DialogTitle className="text-base font-semibold text-zinc-100">{title}</DialogTitle>
                <DialogDescription className="text-xs text-zinc-400">
                  {description}
                  {serverName ? (
                    <span className="ml-1 text-zinc-300 font-medium">
                      on {serverName} {serverHost ? `(${serverHost})` : ''}
                    </span>
                  ) : null}
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          {displayError ? (
            <div className="flex items-start gap-2.5 rounded-lg border border-red-500/30 bg-red-950/40 p-3 text-xs text-red-300">
              <AlertCircle className="size-4 shrink-0 mt-0.5 text-red-400" />
              <div className="space-y-1">
                <span className="font-semibold text-red-200">Execution Error:</span>
                <p className="font-mono text-[11px] leading-tight text-red-300/90 break-all">
                  {displayError}
                </p>
              </div>
            </div>
          ) : null}

          {/* Commands Being Executed View */}
          {commandsText ? (
            <div className="space-y-1.5">
              <div className="flex items-center justify-between text-xs text-zinc-400">
                <span className="flex items-center gap-1.5 font-medium text-zinc-300">
                  <Terminal className="size-3.5 text-sky-400" />
                  Commands Executed as Root:
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-[11px] text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800"
                  onClick={handleCopyCommands}
                >
                  {copied ? (
                    <Check className="size-3 mr-1 text-emerald-400" />
                  ) : (
                    <Copy className="size-3 mr-1" />
                  )}
                  {copied ? 'Copied' : 'Copy'}
                </Button>
              </div>
              <div className="relative max-h-40 overflow-auto rounded-lg border border-zinc-800 bg-zinc-900/80 p-3 font-mono text-xs text-emerald-300 leading-relaxed shadow-inner">
                <pre className="whitespace-pre-wrap break-all text-[11px] font-mono">
                  {commandsText}
                </pre>
              </div>
            </div>
          ) : null}

          {/* Password Input */}
          <div className="space-y-2 pt-1">
            <Label htmlFor="sudo-password-input" className="text-xs font-medium text-zinc-200">
              Server Sudo Password
            </Label>
            <div className="relative">
              <KeyRound className="absolute left-3 top-2.5 size-4 text-zinc-500" />
              <Input
                id="sudo-password-input"
                type={showPassword ? 'text' : 'password'}
                value={sudoPassword}
                onChange={(e) => setSudoPassword(e.target.value)}
                placeholder="Enter password for sudo / root access..."
                autoFocus
                className="pl-9 pr-10 font-mono text-xs bg-zinc-900 border-zinc-800 text-zinc-100 placeholder:text-zinc-500 focus-visible:ring-sky-500"
                disabled={isPending}
              />
              <button
                type="button"
                className="absolute right-3 top-2.5 text-zinc-400 hover:text-zinc-200"
                onClick={() => setShowPassword((prev) => !prev)}
                tabIndex={-1}
              >
                {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
              </button>
            </div>
            <p className="text-[11px] text-zinc-400 leading-tight">
              Password will be securely transmitted over SSH and will not be displayed in logs.
            </p>
          </div>

          {/* Remember Password Checkbox */}
          <div className="flex items-center gap-2 pt-1">
            <input
              id="save-sudo-checkbox"
              type="checkbox"
              checked={saveSudo}
              onChange={(e) => setSaveSudo(e.target.checked)}
              className="size-4 rounded border-zinc-700 bg-zinc-900 text-sky-600 focus:ring-sky-500"
            />
            <Label
              htmlFor="save-sudo-checkbox"
              className="text-xs text-zinc-300 cursor-pointer select-none font-normal"
            >
              Remember this sudo password on server credentials (for automated deployments & cert renewals)
            </Label>
          </div>

          <DialogFooter className="pt-3 gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="border-zinc-800 text-zinc-300 hover:bg-zinc-900 hover:text-zinc-100"
              onClick={() => onOpenChange(false)}
              disabled={isPending}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={isPending || !sudoPassword.trim()}
              className="bg-amber-600 hover:bg-amber-500 text-white font-medium"
            >
              {isPending ? (
                <>
                  <Loader2 className="size-3.5 mr-2 animate-spin" />
                  Executing as Root...
                </>
              ) : (
                actionLabel
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
