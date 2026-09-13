import { ArrowLeft, KeyRound, Loader2, ShieldCheck } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'

import { AuthLayout, FieldError } from '@/components/AuthLayout'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useLogin, useLogin2FA, useSession, useSetupStatus } from '@/lib/session'

export function Login() {
  const navigate = useNavigate()
  const location = useLocation()
  const login = useLogin()
  const login2FA = useLogin2FA()
  const { data: session } = useSession()
  const { data: setup } = useSetupStatus()

  const [step, setStep] = useState<'credentials' | '2fa'>('credentials')
  const [tempToken, setTempToken] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [useRecoveryCode, setUseRecoveryCode] = useState(false)

  // An instance with no accounts has nothing to sign in to.
  if (setup?.needed) return <Navigate to="/setup" replace />
  if (session) return <Navigate to="/" replace />

  const targetUrl = (location.state as { from?: string } | null)?.from ?? '/'

  function onSubmitCredentials(event: FormEvent) {
    event.preventDefault()
    login.mutate(
      { email, password },
      {
        onSuccess: (res) => {
          if ('requires_2fa' in res && res.requires_2fa) {
            setTempToken(res.temp_token)
            setStep('2fa')
            setCode('')
            return
          }
          void navigate(targetUrl, { replace: true })
        },
      },
    )
  }

  function onSubmit2FA(event: FormEvent) {
    event.preventDefault()
    if (!code.trim() || !tempToken) return

    login2FA.mutate(
      { temp_token: tempToken, code: code.trim() },
      {
        onSuccess: () => {
          void navigate(targetUrl, { replace: true })
        },
      },
    )
  }

  if (step === '2fa') {
    const errorMsg = login2FA.error?.message

    return (
      <AuthLayout
        title="Two-Factor Verification"
        description="Enter the verification code from your authenticator app or a recovery code."
      >
        <form onSubmit={onSubmit2FA} className="space-y-4" noValidate>
          {errorMsg ? <Alert variant="danger">{errorMsg}</Alert> : null}

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <Label htmlFor="2fa-code">
                {useRecoveryCode ? 'Backup Recovery Code' : '6-Digit Authenticator Code'}
              </Label>
              <button
                type="button"
                onClick={() => {
                  setUseRecoveryCode(!useRecoveryCode)
                  setCode('')
                }}
                className="text-xs text-primary hover:underline"
              >
                {useRecoveryCode ? 'Use authenticator code' : 'Use recovery code'}
              </button>
            </div>
            <div className="relative">
              <Input
                id="2fa-code"
                type="text"
                inputMode={useRecoveryCode ? 'text' : 'numeric'}
                autoComplete="one-time-code"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder={useRecoveryCode ? 'xxxx-xxxx' : '123456'}
                className="font-mono text-center tracking-widest text-base"
                autoFocus
                required
              />
              <div className="absolute right-3 top-2.5 text-muted-foreground pointer-events-none">
                {useRecoveryCode ? <KeyRound className="size-4" /> : <ShieldCheck className="size-4" />}
              </div>
            </div>
            <p className="text-muted-foreground text-xs">
              {useRecoveryCode
                ? 'Enter one of the 8-character recovery codes saved during 2FA setup.'
                : 'Open Google Authenticator, Authy, or your password manager to retrieve the code.'}
            </p>
          </div>

          <Button type="submit" className="w-full" disabled={login2FA.isPending || !code.trim()}>
            {login2FA.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
            Verify & Sign in
          </Button>

          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="w-full text-xs text-muted-foreground"
            onClick={() => {
              setStep('credentials')
              setTempToken('')
              setCode('')
            }}
          >
            <ArrowLeft className="mr-1 size-3.5" />
            Back to email & password
          </Button>
        </form>
      </AuthLayout>
    )
  }

  // A 422 carries per-field messages; 401 is a single message above the form.
  const fieldErrors = login.error?.fields ?? {}
  const formError =
    login.error && Object.keys(fieldErrors).length === 0 ? login.error.message : null

  return (
    <AuthLayout title="Sign in" description="Access your deployment control plane.">
      <form onSubmit={onSubmitCredentials} className="space-y-4" noValidate>
        {formError ? <Alert variant="danger">{formError}</Alert> : null}

        <div className="space-y-1.5">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="username"
            autoFocus
            required
            aria-invalid={Boolean(fieldErrors.email)}
          />
          <FieldError message={fieldErrors.email} />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
            aria-invalid={Boolean(fieldErrors.password)}
          />
          <FieldError message={fieldErrors.password} />
        </div>

        <Button type="submit" className="w-full" disabled={login.isPending}>
          {login.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
          Sign in
        </Button>
      </form>
    </AuthLayout>
  )
}
