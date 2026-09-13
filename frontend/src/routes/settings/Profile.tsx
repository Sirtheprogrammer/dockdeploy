import {
  Check,
  Copy,
  Download,
  Loader2,
  Monitor,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
} from 'lucide-react'
import QRCode from 'qrcode'
import { useState, type FormEvent } from 'react'

import { FieldError } from '@/components/AuthLayout'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  useActiveSessions,
  useChangePassword,
  useDisable2FA,
  useEnable2FA,
  useRegenerateRecoveryCodes,
  useRevokeSession,
  useSetup2FA,
  useUpdateProfile,
} from '@/lib/admin'
import { ROLE_DESCRIPTIONS, ROLE_LABELS, useSession } from '@/lib/session'
import { formatRelative } from '@/lib/utils'

function ProfileCard() {
  const { data: user } = useSession()
  const update = useUpdateProfile()
  const [name, setName] = useState(user?.name ?? '')

  if (!user) return null

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    update.mutate({ name })
  }

  const fieldErrors = update.error?.fields ?? {}

  return (
    <Card>
      <CardHeader>
        <CardTitle>Profile</CardTitle>
        <CardDescription>
          Your email is fixed after the account is created. Ask an admin if it needs to change.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="max-w-sm space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="profile-name">Name</Label>
            <Input
              id="profile-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              aria-invalid={Boolean(fieldErrors.name)}
            />
            <FieldError message={fieldErrors.name} />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="profile-email">Email</Label>
            <Input id="profile-email" value={user.email} disabled className="font-mono" />
          </div>

          <div className="space-y-1.5">
            <Label>Role</Label>
            <div className="flex items-center gap-2">
              <Badge variant="info">{ROLE_LABELS[user.role]}</Badge>
              <span className="text-muted-foreground text-xs">{ROLE_DESCRIPTIONS[user.role]}</span>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <Button type="submit" disabled={update.isPending || name === user.name}>
              {update.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
              Save
            </Button>
            {update.isSuccess ? <span className="text-success text-sm">Saved</span> : null}
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

function PasswordCard() {
  const change = useChangePassword()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    change.mutate(
      { current_password: current, new_password: next },
      {
        onSuccess: () => {
          setCurrent('')
          setNext('')
        },
      },
    )
  }

  const fieldErrors = change.error?.fields ?? {}
  const formError = change.error && Object.keys(fieldErrors).length === 0 ? change.error.message : null

  return (
    <Card>
      <CardHeader>
        <CardTitle>Password</CardTitle>
        <CardDescription>
          Changing your password signs out every other device immediately.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="max-w-sm space-y-4" noValidate>
          {formError ? <Alert variant="danger">{formError}</Alert> : null}
          {change.isSuccess ? (
            <Alert variant="info">Password changed. Other sessions have been signed out.</Alert>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="current-password">Current password</Label>
            <Input
              id="current-password"
              type="password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              autoComplete="current-password"
              aria-invalid={Boolean(fieldErrors.current_password)}
            />
            <FieldError message={fieldErrors.current_password} />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="new-password">New password</Label>
            <Input
              id="new-password"
              type="password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              autoComplete="new-password"
              aria-invalid={Boolean(fieldErrors.new_password)}
            />
            {fieldErrors.new_password ? (
              <FieldError message={fieldErrors.new_password} />
            ) : (
              <p className="text-muted-foreground text-xs">At least 12 characters.</p>
            )}
          </div>

          <Button type="submit" disabled={change.isPending || !current || !next}>
            {change.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
            Change password
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

function TwoFactorAuthCard() {
  const { data: user } = useSession()
  const setup2FA = useSetup2FA()
  const enable2FA = useEnable2FA()
  const disable2FA = useDisable2FA()
  const regenCodes = useRegenerateRecoveryCodes()

  const [mode, setMode] = useState<'idle' | 'setup' | 'recovery-codes' | 'disable' | 'regenerate'>('idle')
  const [qrCodeUrl, setQrCodeUrl] = useState<string | null>(null)
  const [confirmationCode, setConfirmationCode] = useState('')
  const [disablePassword, setDisablePassword] = useState('')
  const [disableCode, setDisableCode] = useState('')
  const [regenPassword, setRegenPassword] = useState('')
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [copiedCodes, setCopiedCodes] = useState(false)
  const [copiedSecret, setCopiedSecret] = useState(false)

  if (!user) return null

  const isEnabled = user.totp_enabled

  function startSetup() {
    setMode('setup')
    setConfirmationCode('')
    setQrCodeUrl(null)
    setup2FA.mutate(undefined, {
      onSuccess: (data) => {
        void QRCode.toDataURL(data.otpauth_url, { width: 192, margin: 2 }).then(setQrCodeUrl)
      },
    })
  }

  function handleEnable(e: FormEvent) {
    e.preventDefault()
    if (!confirmationCode.trim()) return
    enable2FA.mutate(
      { code: confirmationCode.trim() },
      {
        onSuccess: (data) => {
          setRecoveryCodes(data.recovery_codes)
          setMode('recovery-codes')
          setConfirmationCode('')
        },
      },
    )
  }

  function handleDisable(e: FormEvent) {
    e.preventDefault()
    disable2FA.mutate(
      { password: disablePassword || undefined, code: disableCode || undefined },
      {
        onSuccess: () => {
          setMode('idle')
          setDisablePassword('')
          setDisableCode('')
        },
      },
    )
  }

  function handleRegenerate(e: FormEvent) {
    e.preventDefault()
    if (!regenPassword) return
    regenCodes.mutate(
      { password: regenPassword },
      {
        onSuccess: (data) => {
          setRecoveryCodes(data.recovery_codes)
          setMode('recovery-codes')
          setRegenPassword('')
        },
      },
    )
  }

  function copyCodes() {
    void navigator.clipboard.writeText(recoveryCodes.join('\n'))
    setCopiedCodes(true)
    setTimeout(() => setCopiedCodes(false), 2000)
  }

  function downloadCodes() {
    const text = `dockdeploy Two-Factor Authentication Backup Recovery Codes\nAccount: ${user?.email}\nGenerated: ${new Date().toISOString()}\n\nEach code can be used exactly once:\n\n${recoveryCodes.map((c, i) => `${i + 1}. ${c}`).join('\n')}\n`
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `dockdeploy-recovery-codes-${user?.email ?? 'account'}.txt`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle>Two-Factor Authentication (2FA)</CardTitle>
          {isEnabled ? (
            <Badge variant="success">Active</Badge>
          ) : (
            <Badge variant="outline">Disabled</Badge>
          )}
        </div>
        <CardDescription>
          Protect your account with a time-based one-time password (TOTP) from authenticator apps like Google Authenticator, Authy, or 1Password.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {mode === 'idle' && (
          <>
            {isEnabled ? (
              <div className="space-y-4">
                <div className="flex items-center gap-3 rounded-md border border-emerald-500/20 bg-emerald-500/10 p-3 text-emerald-700 dark:text-emerald-300">
                  <ShieldCheck className="size-5 shrink-0" />
                  <div className="text-xs">
                    <p className="font-semibold text-sm">Two-factor authentication is active</p>
                    <p className="text-muted-foreground mt-0.5">
                      Your account requires an authenticator code or backup recovery code on sign in.
                    </p>
                  </div>
                </div>

                <div className="flex flex-wrap items-center gap-3 pt-1">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setMode('regenerate')}
                  >
                    <RefreshCw className="mr-1 size-3.5" />
                    Regenerate recovery codes
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    className="text-destructive hover:bg-destructive/10"
                    onClick={() => setMode('disable')}
                  >
                    Disable 2FA
                  </Button>
                </div>
              </div>
            ) : (
              <div className="space-y-3">
                <p className="text-xs text-muted-foreground">
                  Two-factor authentication adds an extra layer of security to prevent unauthorized access, even if your password is compromised.
                </p>
                <Button onClick={startSetup} disabled={setup2FA.isPending}>
                  {setup2FA.isPending ? <Loader2 className="animate-spin" aria-hidden /> : <ShieldCheck className="mr-1.5 size-4" />}
                  Set up Two-Factor Authentication
                </Button>
              </div>
            )}
          </>
        )}

        {mode === 'setup' && (
          <div className="space-y-4 rounded-lg border p-4 bg-muted/20">
            <h4 className="font-semibold text-sm">Scan QR Code or Enter Key</h4>
            <p className="text-xs text-muted-foreground">
              Scan this QR code using Google Authenticator, Authy, Bitwarden, 1Password, or any standard TOTP app.
            </p>

            {setup2FA.isPending && (
              <div className="flex items-center justify-center p-8">
                <Loader2 className="animate-spin size-6 text-muted-foreground" />
              </div>
            )}

            {setup2FA.error && (
              <Alert variant="danger">{setup2FA.error.message}</Alert>
            )}

            {setup2FA.data && (
              <div className="space-y-4">
                <div className="flex flex-col sm:flex-row items-center gap-6">
                  {qrCodeUrl ? (
                    <div className="p-2 bg-white rounded-lg shadow-sm border shrink-0">
                      <img src={qrCodeUrl} alt="2FA QR Code" className="size-44" />
                    </div>
                  ) : (
                    <div className="size-44 flex items-center justify-center bg-muted rounded shrink-0">
                      <Loader2 className="animate-spin size-6" />
                    </div>
                  )}

                  <div className="space-y-2 flex-1 w-full">
                    <Label className="text-xs">Manual Entry Key</Label>
                    <div className="flex items-center gap-2">
                      <Input
                        readOnly
                        value={setup2FA.data.secret}
                        className="font-mono text-xs bg-muted/60"
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          void navigator.clipboard.writeText(setup2FA.data.secret)
                          setCopiedSecret(true)
                          setTimeout(() => setCopiedSecret(false), 2000)
                        }}
                      >
                        {copiedSecret ? <Check className="size-4 text-emerald-500" /> : <Copy className="size-4" />}
                        {copiedSecret ? 'Copied' : 'Copy'}
                      </Button>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      If you cannot scan the QR code, copy and paste this key into your authenticator app.
                    </p>
                  </div>
                </div>

                <form onSubmit={handleEnable} className="space-y-3 pt-2 border-t" noValidate>
                  {enable2FA.error && (
                    <Alert variant="danger">{enable2FA.error.message}</Alert>
                  )}
                  <div className="space-y-1.5 max-w-xs">
                    <Label htmlFor="2fa-confirm">Confirmation Code</Label>
                    <Input
                      id="2fa-confirm"
                      type="text"
                      inputMode="numeric"
                      autoComplete="one-time-code"
                      maxLength={6}
                      value={confirmationCode}
                      onChange={(e) => setConfirmationCode(e.target.value)}
                      placeholder="123456"
                      className="font-mono tracking-widest text-center"
                      autoFocus
                    />
                    <p className="text-xs text-muted-foreground">Enter the 6-digit code shown in your authenticator app.</p>
                  </div>

                  <div className="flex items-center gap-2">
                    <Button type="submit" disabled={enable2FA.isPending || confirmationCode.trim().length !== 6}>
                      {enable2FA.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
                      Confirm & Enable 2FA
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      onClick={() => {
                        setMode('idle')
                        setConfirmationCode('')
                      }}
                    >
                      Cancel
                    </Button>
                  </div>
                </form>
              </div>
            )}
          </div>
        )}

        {mode === 'recovery-codes' && (
          <div className="space-y-4 rounded-lg border p-4 bg-muted/20">
            <div className="flex items-center gap-2 text-emerald-600 dark:text-emerald-400">
              <ShieldCheck className="size-5" />
              <h4 className="font-semibold text-sm">Save your Backup Recovery Codes</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              If you lose your authenticator device, you can use these recovery codes to sign in. Each code can be used only once. Keep them in a safe place, like your password manager.
            </p>

            <div className="grid grid-cols-2 sm:grid-cols-5 gap-2 bg-background p-3 rounded-md border font-mono text-xs text-center select-all">
              {recoveryCodes.map((code) => (
                <div key={code} className="p-1.5 bg-muted/40 rounded border border-border/40">
                  {code}
                </div>
              ))}
            </div>

            <div className="flex flex-wrap items-center gap-2 pt-2">
              <Button variant="outline" size="sm" onClick={copyCodes}>
                {copiedCodes ? <Check className="mr-1.5 size-4 text-emerald-500" /> : <Copy className="mr-1.5 size-4" />}
                {copiedCodes ? 'Copied to clipboard' : 'Copy all codes'}
              </Button>
              <Button variant="outline" size="sm" onClick={downloadCodes}>
                <Download className="mr-1.5 size-4" />
                Download as text file
              </Button>
              <Button size="sm" onClick={() => setMode('idle')}>
                I have saved my codes
              </Button>
            </div>
          </div>
        )}

        {mode === 'regenerate' && (
          <div className="space-y-4 rounded-lg border p-4 bg-muted/20">
            <h4 className="font-semibold text-sm">Regenerate Recovery Codes</h4>
            <p className="text-xs text-muted-foreground">
              Generating new recovery codes invalidates all previous recovery codes. Please confirm your current password to continue.
            </p>

            {regenCodes.error && (
              <Alert variant="danger">{regenCodes.error.message}</Alert>
            )}

            <form onSubmit={handleRegenerate} className="max-w-sm space-y-3" noValidate>
              <div className="space-y-1.5">
                <Label htmlFor="regen-password">Current Password</Label>
                <Input
                  id="regen-password"
                  type="password"
                  value={regenPassword}
                  onChange={(e) => setRegenPassword(e.target.value)}
                  autoComplete="current-password"
                  autoFocus
                  required
                />
              </div>

              <div className="flex items-center gap-2">
                <Button type="submit" disabled={regenCodes.isPending || !regenPassword}>
                  {regenCodes.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
                  Regenerate Codes
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => {
                    setMode('idle')
                    setRegenPassword('')
                  }}
                >
                  Cancel
                </Button>
              </div>
            </form>
          </div>
        )}

        {mode === 'disable' && (
          <div className="space-y-4 rounded-lg border border-destructive/30 p-4 bg-destructive/5">
            <div className="flex items-center gap-2 text-destructive">
              <ShieldAlert className="size-5" />
              <h4 className="font-semibold text-sm">Disable Two-Factor Authentication</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Disabling 2FA makes your account less secure. To confirm, enter your account password or current 6-digit code.
            </p>

            {disable2FA.error && (
              <Alert variant="danger">{disable2FA.error.message}</Alert>
            )}

            <form onSubmit={handleDisable} className="max-w-sm space-y-3" noValidate>
              <div className="space-y-1.5">
                <Label htmlFor="disable-password">Account Password</Label>
                <Input
                  id="disable-password"
                  type="password"
                  value={disablePassword}
                  onChange={(e) => setDisablePassword(e.target.value)}
                  placeholder="Enter password"
                  autoComplete="current-password"
                />
              </div>

              <div className="text-center text-xs text-muted-foreground my-1">— OR —</div>

              <div className="space-y-1.5">
                <Label htmlFor="disable-code">6-Digit Authenticator Code</Label>
                <Input
                  id="disable-code"
                  type="text"
                  inputMode="numeric"
                  maxLength={6}
                  value={disableCode}
                  onChange={(e) => setDisableCode(e.target.value)}
                  placeholder="123456"
                  className="font-mono text-center tracking-widest"
                />
              </div>

              <div className="flex items-center gap-2 pt-2">
                <Button
                  type="submit"
                  variant="destructive"
                  disabled={disable2FA.isPending || (!disablePassword && !disableCode)}
                >
                  {disable2FA.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
                  Confirm & Disable 2FA
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => {
                    setMode('idle')
                    setDisablePassword('')
                    setDisableCode('')
                  }}
                >
                  Cancel
                </Button>
              </div>
            </form>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function SessionsCard() {
  const { data: sessions, isPending } = useActiveSessions()
  const revoke = useRevokeSession()

  return (
    <Card>
      <CardHeader>
        <CardTitle>Active sessions</CardTitle>
        <CardDescription>Browsers currently signed in as you.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {isPending ? <p className="text-muted-foreground text-sm">Loading.</p> : null}
        {sessions?.map((session) => (
          <div
            key={session.id}
            className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3"
          >
            <div className="flex min-w-0 items-start gap-3">
              <Monitor className="text-muted-foreground mt-0.5 size-4 shrink-0" aria-hidden />
              <div className="min-w-0">
                <p className="truncate text-sm">
                  {session.user_agent ?? 'Unknown device'}
                  {session.current ? (
                    <Badge variant="success" className="ml-2">
                      This device
                    </Badge>
                  ) : null}
                </p>
                <p className="text-muted-foreground font-mono text-xs">
                  {session.ip ?? 'unknown ip'} &middot; active {formatRelative(session.last_used_at)}
                </p>
              </div>
            </div>
            {!session.current ? (
              <Button
                variant="outline"
                size="sm"
                disabled={revoke.isPending}
                onClick={() => revoke.mutate(session.id)}
              >
                Sign out
              </Button>
            ) : null}
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

export function Profile() {
  return (
    <div className="grid max-w-3xl gap-4">
      <ProfileCard />
      <PasswordCard />
      <TwoFactorAuthCard />
      <SessionsCard />
    </div>
  )
}
