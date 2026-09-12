import { Loader2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'

import { AuthLayout, FieldError } from '@/components/AuthLayout'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useLogin, useSession, useSetupStatus } from '@/lib/session'

export function Login() {
  const navigate = useNavigate()
  const location = useLocation()
  const login = useLogin()
  const { data: session } = useSession()
  const { data: setup } = useSetupStatus()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  // An instance with no accounts has nothing to sign in to.
  if (setup?.needed) return <Navigate to="/setup" replace />
  if (session) return <Navigate to="/" replace />

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    login.mutate(
      { email, password },
      {
        onSuccess: () => {
          // Return to whatever the user was trying to reach.
          const from = (location.state as { from?: string } | null)?.from
          void navigate(from ?? '/', { replace: true })
        },
      },
    )
  }

  // A 422 carries per-field messages; 401 is a single message above the form.
  const fieldErrors = login.error?.fields ?? {}
  const formError =
    login.error && Object.keys(fieldErrors).length === 0 ? login.error.message : null

  return (
    <AuthLayout title="Sign in" description="Access your deployment control plane.">
      <form onSubmit={onSubmit} className="space-y-4" noValidate>
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
