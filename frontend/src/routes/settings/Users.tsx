import { Loader2, Trash2, UserPlus } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { FieldError } from '@/components/AuthLayout'
import { CopyButton } from '@/components/CopyButton'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import {
  useCreateInvitation,
  useDeleteInvitation,
  useDeleteUser,
  useInvitations,
  useUpdateUser,
  useUsers,
  type CreatedInvitation,
} from '@/lib/admin'
import { ROLE_DESCRIPTIONS, ROLE_LABELS, useSession, type Role } from '@/lib/session'
import { formatRelative } from '@/lib/utils'

const ROLES: Role[] = ['admin', 'member', 'viewer']

function InviteDialog() {
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [role, setRole] = useState<Role>('member')
  const [created, setCreated] = useState<CreatedInvitation | null>(null)

  const invite = useCreateInvitation()

  function reset() {
    setEmail('')
    setName('')
    setRole('member')
    setCreated(null)
    invite.reset()
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    invite.mutate({ email, name, role }, { onSuccess: setCreated })
  }

  const fieldErrors = invite.error?.fields ?? {}
  const formError = invite.error && Object.keys(fieldErrors).length === 0 ? invite.error.message : null

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) reset()
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">
          <UserPlus aria-hidden />
          Invite
        </Button>
      </DialogTrigger>

      <DialogContent>
        {created ? (
          <>
            <DialogHeader>
              <DialogTitle>Invitation ready</DialogTitle>
              <DialogDescription>
                There is no mail server in a self-hosted install, so send this link yourself. It
                works once and expires in seven days.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-2">
              <Label htmlFor="invite-url">Invitation link</Label>
              <div className="flex gap-2">
                <Input
                  id="invite-url"
                  readOnly
                  value={created.url}
                  className="font-mono text-xs"
                  onFocus={(e) => e.currentTarget.select()}
                />
                <CopyButton value={created.url} />
              </div>
              <p className="text-muted-foreground text-xs">
                For {created.email} as {ROLE_LABELS[created.role]}.
              </p>
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={reset}>
                Invite someone else
              </Button>
              <Button onClick={() => setOpen(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={onSubmit} noValidate className="grid gap-4">
            <DialogHeader>
              <DialogTitle>Invite someone</DialogTitle>
              <DialogDescription>
                You will get a link to send them. No email is sent from this instance.
              </DialogDescription>
            </DialogHeader>

            {formError ? <Alert variant="danger">{formError}</Alert> : null}

            <div className="space-y-1.5">
              <Label htmlFor="invite-email">Email</Label>
              <Input
                id="invite-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                autoFocus
                aria-invalid={Boolean(fieldErrors.email)}
              />
              <FieldError message={fieldErrors.email} />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="invite-name">Name (optional)</Label>
              <Input id="invite-name" value={name} onChange={(e) => setName(e.target.value)} />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="invite-role">Role</Label>
              <Select value={role} onValueChange={(value) => setRole(value as Role)}>
                <SelectTrigger id="invite-role">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ROLES.map((option) => (
                    <SelectItem key={option} value={option}>
                      {ROLE_LABELS[option]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-muted-foreground text-xs">{ROLE_DESCRIPTIONS[role]}</p>
            </div>

            <DialogFooter>
              <Button type="submit" disabled={invite.isPending}>
                {invite.isPending ? <Loader2 className="animate-spin" aria-hidden /> : null}
                Create link
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

function UsersTable() {
  const { data: currentUser } = useSession()
  const { data: users, isPending } = useUsers()
  const update = useUpdateUser()
  const remove = useDeleteUser()
  const [pendingId, setPendingId] = useState<string | null>(null)

  // The server enforces all of this; disabling the controls just avoids
  // offering an action that will be refused.
  const isSelf = (id: string) => id === currentUser?.id

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2">
        <div className="space-y-1">
          <CardTitle>Users</CardTitle>
          <CardDescription>Everyone with access to this instance.</CardDescription>
        </div>
        <InviteDialog />
      </CardHeader>
      <CardContent className="px-0">
        {update.isError ? (
          <Alert variant="danger" className="mx-5 mb-3">
            {update.error.message}
          </Alert>
        ) : null}
        {remove.isError ? (
          <Alert variant="danger" className="mx-5 mb-3">
            {remove.error.message}
          </Alert>
        ) : null}

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>User</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Last sign-in</TableHead>
              <TableHead className="w-10" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isPending ? (
              <TableRow>
                <TableCell colSpan={5} className="text-muted-foreground py-6 text-center">
                  Loading.
                </TableCell>
              </TableRow>
            ) : null}

            {users?.map((user) => (
              <TableRow key={user.id}>
                <TableCell>
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">
                      {user.name}
                      {isSelf(user.id) ? (
                        <span className="text-muted-foreground ml-1.5 text-xs">(you)</span>
                      ) : null}
                    </p>
                    <p className="text-muted-foreground truncate font-mono text-xs">{user.email}</p>
                  </div>
                </TableCell>

                <TableCell>
                  <Select
                    value={user.role}
                    disabled={isSelf(user.id) || update.isPending}
                    onValueChange={(role) => {
                      setPendingId(user.id)
                      update.mutate(
                        { id: user.id, name: user.name, role: role as Role, status: user.status },
                        { onSettled: () => setPendingId(null) },
                      )
                    }}
                  >
                    <SelectTrigger className="h-8 w-32">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {ROLES.map((option) => (
                        <SelectItem key={option} value={option}>
                          {ROLE_LABELS[option]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </TableCell>

                <TableCell>
                  {user.status === 'active' ? (
                    <Badge variant="success">Active</Badge>
                  ) : (
                    <Badge variant="warning">Suspended</Badge>
                  )}
                </TableCell>

                <TableCell className="text-muted-foreground text-sm">
                  {user.last_login_at ? formatRelative(user.last_login_at) : 'Never'}
                </TableCell>

                <TableCell className="text-right">
                  <div className="flex items-center justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={isSelf(user.id) || pendingId === user.id}
                      onClick={() =>
                        update.mutate({
                          id: user.id,
                          name: user.name,
                          role: user.role,
                          status: user.status === 'active' ? 'suspended' : 'active',
                        })
                      }
                    >
                      {user.status === 'active' ? 'Suspend' : 'Restore'}
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className="text-muted-foreground hover:text-destructive"
                      disabled={isSelf(user.id) || remove.isPending}
                      aria-label={`Delete ${user.name}`}
                      onClick={() => {
                        if (confirm(`Delete ${user.email}? This cannot be undone.`)) {
                          remove.mutate(user.id)
                        }
                      }}
                    >
                      <Trash2 aria-hidden />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function PendingInvitations() {
  const { data: invitations, isPending } = useInvitations()
  const revoke = useDeleteInvitation()

  if (isPending || !invitations?.length) return null

  return (
    <Card>
      <CardHeader>
        <CardTitle>Pending invitations</CardTitle>
        <CardDescription>Links that have been created but not yet used.</CardDescription>
      </CardHeader>
      <CardContent className="px-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Email</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Expires</TableHead>
              <TableHead className="w-10" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {invitations.map((invitation) => (
              <TableRow key={invitation.id}>
                <TableCell className="font-mono text-xs">{invitation.email}</TableCell>
                <TableCell>{ROLE_LABELS[invitation.role]}</TableCell>
                <TableCell className="text-muted-foreground text-sm">
                  {formatRelative(invitation.expires_at)}
                </TableCell>
                <TableCell className="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={revoke.isPending}
                    onClick={() => revoke.mutate(invitation.id)}
                  >
                    Revoke
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

export function Users() {
  return (
    <div className="grid max-w-5xl gap-4">
      <UsersTable />
      <PendingInvitations />
    </div>
  )
}
