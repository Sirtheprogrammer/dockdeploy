import { Loader2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router'

import { AuthLayout, FieldError } from '@/components/AuthLayout'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useSetup, useSetupStatus } from '@/lib/session'

const MIN_PASSWORD_LENGTH = 12

export function Setup() {
  const navigate = useNavigate()
  const setup = useSetup()
  const { data: status, isPending } = useSetupStatus()

  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')

  if (isPending) return null
  // Setup runs exactly once; afterwards this route is just a redirect.
  if (status && !status.needed) return <Navigate to="/login" replace />

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    setup.mutate(
      { email, name, password },
      { onSuccess: () => void navigate('/', { replace: true }) },
    )
  }

  const fieldErrors = setup.error?.fields ?? {}
  const formError = setup.error && Object.keys(fieldErrors).length === 0 ? setup.error.message : null

  return (
    <AuthLayout
      title="Create the first account"
      description="This account is an administrator. Nobody else can sign up unless you invite them."
    >
      <form onSubmit={onSubmit} className="space-y-4" noValidate>
        {formError ? <Alert variant="danger">{formError}</Alert> : null}

        <div className="space-y-1.5">
          <Label htmlFor="name">Name</Label>
          <Input
            id="name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoComplete="name"
            autoFocus
            required
            aria-invalid={Boolean(fieldErrors.name)}
          />
          <FieldError message={fieldErrors.name} />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="username"
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
            autoComplete="new-password"
            required
            aria-invalid={Boolean(fieldErrors.password)}
            aria-describedby="password-hint"
          />
          {fieldErrors.password ? (
            <FieldError message={fieldErrors.password} />
          ) : (
            <p id="password-hint" className="text-muted-foreground text-xs">
              At least {MIN_PASSWORD_LENGTH} characters. A passphrase beats a short complex
              password.
            </p>
          )}
        </div>

        <Button type="submit" className="w-full" disabled={setup.isPending}>
          {setup.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
          Create account
        </Button>
      </form>
    </AuthLayout>
  )
}
