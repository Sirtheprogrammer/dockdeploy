import { Boxes } from 'lucide-react'
import type { ReactNode } from 'react'

/**
 * Frame for the three unauthenticated screens: sign in, first-run setup, and
 * accepting an invitation. Deliberately plain — the sidebar and its navigation
 * are meaningless before there is a session.
 */
export function AuthLayout({
  title,
  description,
  children,
  footer,
}: {
  title: string
  description?: string
  children: ReactNode
  footer?: ReactNode
}) {
  return (
    <div className="bg-surface flex min-h-screen flex-col items-center justify-center px-4 py-12">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex items-center justify-center gap-3">
          <span className="bg-primary/12 text-primary flex size-10 items-center justify-center rounded-xl">
            <Boxes className="size-5" aria-hidden />
          </span>
          <div>
            <span className="block text-base font-semibold tracking-tight">dockdeploy</span>
            <span className="text-muted-foreground block text-xs">Infrastructure, clearly managed.</span>
          </div>
        </div>

        <div className="bg-card rounded-xl border p-6 shadow-lg shadow-black/5">
          <div className="mb-6 space-y-1.5">
            <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
            {description ? <p className="text-muted-foreground text-sm">{description}</p> : null}
          </div>
          {children}
        </div>

        {footer ? <div className="text-muted-foreground mt-5 text-center text-sm">{footer}</div> : null}
      </div>
    </div>
  )
}

/** Renders the message for a field the server rejected. */
export function FieldError({ message }: { message?: string }) {
  if (!message) return null
  return <p className="text-destructive text-xs">{message}</p>
}
