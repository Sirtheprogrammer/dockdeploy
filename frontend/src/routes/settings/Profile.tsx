import { Loader2, Monitor } from 'lucide-react'
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
  useRevokeSession,
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
      <SessionsCard />
    </div>
  )
}
