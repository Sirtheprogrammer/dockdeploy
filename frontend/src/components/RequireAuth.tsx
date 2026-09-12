import { Loader2 } from 'lucide-react'
import { Navigate, Outlet, useLocation } from 'react-router'

import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { useSession, useSetupStatus } from '@/lib/session'

/**
 * Gate for every authenticated route.
 *
 * The three outcomes are kept distinct on purpose: an instance with no
 * accounts goes to setup, a signed-out visitor goes to sign-in, and a backend
 * that is actually broken says so rather than bouncing the user to a login
 * form that will not work either.
 */
export function RequireAuth() {
  const location = useLocation()
  const session = useSession()
  const setup = useSetupStatus()

  if (session.isPending || setup.isPending) {
    return (
      <div className="bg-surface flex min-h-screen items-center justify-center">
        <Loader2 className="text-muted-foreground size-5 animate-spin" aria-label="Loading" />
      </div>
    )
  }

  if (session.isError) {
    return (
      <div className="bg-surface flex min-h-screen items-center justify-center p-6">
        <div className="w-full max-w-md space-y-4">
          <Alert variant="danger">
            <p className="font-medium">Cannot reach the control plane</p>
            <p className="mt-1 text-sm opacity-90">{session.error.message}</p>
          </Alert>
          <Button variant="outline" className="w-full" onClick={() => session.refetch()}>
            Try again
          </Button>
        </div>
      </div>
    )
  }

  if (setup.data?.needed) return <Navigate to="/setup" replace />

  if (!session.data) {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  }

  return <Outlet />
}
