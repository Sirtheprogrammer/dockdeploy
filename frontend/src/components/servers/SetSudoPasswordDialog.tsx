import { Eye, EyeOff, KeyRound, Loader2, ShieldCheck, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'

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
import { useSetServerSudoPassword, type Server } from '@/lib/servers'

interface SetSudoPasswordDialogProps {
  server: Server
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SetSudoPasswordDialog({ server, open, onOpenChange }: SetSudoPasswordDialogProps) {
  const [sudoPassword, setSudoPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)

  const setSudo = useSetServerSudoPassword(server.id)

  const handleSave = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await setSudo.mutateAsync({ sudo_password: sudoPassword })
      toast.success(
        sudoPassword
          ? 'Server sudo password encrypted and stored successfully!'
          : 'Stored sudo password cleared from server credentials.',
      )
      setSudoPassword('')
      onOpenChange(false)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to update sudo password'
      toast.error(msg)
    }
  }

  const handleClear = async () => {
    try {
      await setSudo.mutateAsync({ sudo_password: '' })
      toast.success('Stored sudo password removed from server credentials.')
      setSudoPassword('')
      onOpenChange(false)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to clear sudo password'
      toast.error(msg)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md p-6 bg-zinc-950 border-zinc-800 text-zinc-100">
        <form onSubmit={handleSave} className="space-y-4">
          <DialogHeader>
            <div className="flex items-center gap-2.5">
              <div className="flex size-9 items-center justify-center rounded-lg border border-sky-500/20 bg-sky-500/10 text-sky-400">
                <KeyRound className="size-5" />
              </div>
              <div>
                <DialogTitle className="text-base font-semibold text-zinc-100">
                  Configure Sudo Password
                </DialogTitle>
                <DialogDescription className="text-xs text-zinc-400">
                  {server.name} ({server.username}@{server.host})
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-3 text-xs space-y-1">
            <div className="flex items-center gap-2 font-medium text-zinc-200">
              <ShieldCheck className="size-4 text-emerald-400" />
              <span>Encrypted Storage</span>
            </div>
            <p className="text-[11px] text-zinc-400 leading-relaxed">
              Your sudo password is encrypted with AES-256-GCM. It is used exclusively to install
              virtual hosts, reload Nginx, and renew TLS certificates.
            </p>
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <Label htmlFor="sudo-pass-input" className="text-xs font-medium text-zinc-300">
                {server.has_sudo_password ? 'Update Sudo Password' : 'New Sudo Password'}
              </Label>
              {server.has_sudo_password ? (
                <span className="text-[10px] text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 rounded px-1.5 py-0.5">
                  Currently Stored
                </span>
              ) : null}
            </div>
            <div className="relative">
              <Input
                id="sudo-pass-input"
                type={showPassword ? 'text' : 'password'}
                value={sudoPassword}
                onChange={(e) => setSudoPassword(e.target.value)}
                placeholder="Enter password..."
                autoFocus
                className="pr-9 font-mono text-xs bg-zinc-900 border-zinc-800 text-zinc-100 placeholder:text-zinc-500 focus-visible:ring-sky-500"
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

          <DialogFooter className="pt-2 flex items-center justify-between sm:justify-between w-full">
            {server.has_sudo_password ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="text-red-400 hover:text-red-300 hover:bg-red-950/30 text-xs"
                onClick={handleClear}
                disabled={setSudo.isPending}
              >
                <Trash2 className="size-3 mr-1" />
                Clear Password
              </Button>
            ) : (
              <div />
            )}

            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="border-zinc-800 text-zinc-300 hover:bg-zinc-900 hover:text-zinc-100"
                onClick={() => onOpenChange(false)}
                disabled={setSudo.isPending}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                size="sm"
                disabled={setSudo.isPending || !sudoPassword.trim()}
                className="bg-sky-600 hover:bg-sky-500 text-white font-medium"
              >
                {setSudo.isPending ? (
                  <>
                    <Loader2 className="size-3.5 mr-2 animate-spin" />
                    Saving...
                  </>
                ) : (
                  'Save Sudo Password'
                )}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
