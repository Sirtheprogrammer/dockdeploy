import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router'

import { AuthLayout, FieldError } from '@/components/AuthLayout'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, type ApiError } from '@/lib/api'
import { ROLE_DESCRIPTIONS, ROLE_LABELS, useAcceptInvitation, type Role } from '@/lib/session'

interface InvitationPreview {
  email: string
  name: string
  role: Role
}

/**
 * Split out so it mounts only once the invitation has loaded. That lets the
 * name field seed itself from the invite in a useState initializer instead of
 * syncing it back in an effect.
 */
function AcceptForm({ token, invitation }: { token: string; invitation: InvitationPreview }) {
  const navigate = useNavigate()
  const accept = useAcceptInvitation(token)

  const [name, setName] = useState(invitation.name)
  const [password, setPassword] = useState('')

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    accept.mutate({ name, password }, { onSuccess: () => void navigate('/', { replace: true }) })
  }

  const fieldErrors = accept.error?.fields ?? {}
  const formError =
    accept.error && Object.keys(fieldErrors).length === 0 ? accept.error.message : null

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate>
      {formError ? <Alert variant="danger">{formError}</Alert> : null}

      <div className="bg-muted/40 flex items-start gap-3 rounded-md border p-3">
        <Badge variant="info">{ROLE_LABELS[invitation.role]}</Badge>
        <p className="text-muted-foreground text-xs leading-relaxed">
          {ROLE_DESCRIPTIONS[invitation.role]}
        </p>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="name">Your name</Label>
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
        <Label htmlFor="password">Choose a password</Label>
        <Input
          id="password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
          required
          aria-invalid={Boolean(fieldErrors.password)}
        />
        {fieldErrors.password ? (
          <FieldError message={fieldErrors.password} />
        ) : (
          <p className="text-muted-foreground text-xs">At least 12 characters.</p>
        )}
      </div>

      <Button type="submit" className="w-full" disabled={accept.isPending}>
        {accept.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
        Create account
      </Button>
    </form>
  )
}

export function AcceptInvite() {
  const { token = '' } = useParams()

  const invitation = useQuery<InvitationPreview, ApiError>({
    queryKey: ['invitation', token],
    queryFn: ({ signal }) => api.get<InvitationPreview>(`/auth/invitations/${token}`, { signal }),
    retry: false,
    enabled: token !== '',
  })

  if (token === '' || invitation.isError) {
    return (
      <AuthLayout
        title="This link is not usable"
        description={invitation.error?.message}
        footer={
          <Link to="/login" className="hover:text-foreground underline underline-offset-4">
            Go to sign in
          </Link>
        }
      >
        <p className="text-muted-foreground text-sm">
          Invitation links expire after seven days and can only be used once. Ask an administrator
          for a new one.
        </p>
      </AuthLayout>
    )
  }

  if (invitation.isPending) {
    return (
      <AuthLayout title="Checking your invitation">
        <div className="text-muted-foreground flex items-center gap-2 text-sm">
          <Loader2 className="size-4 animate-spin" aria-hidden />
          One moment.
        </div>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout
      title="Accept your invitation"
      description={`You were invited as ${invitation.data.email}.`}
    >
      <AcceptForm token={token} invitation={invitation.data} />
    </AuthLayout>
  )
}
