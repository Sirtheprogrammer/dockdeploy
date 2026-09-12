import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

/**
 * Shown wherever a list has nothing in it yet. Empty states are the first
 * thing a new self-hoster sees on every page, so each one names the next
 * concrete action rather than just reporting emptiness.
 */
export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
}: {
  icon: LucideIcon
  title: string
  description: string
  action?: ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
      <div className="bg-muted text-muted-foreground mb-4 rounded-lg p-3">
        <Icon className="size-5" aria-hidden />
      </div>
      <h2 className="text-sm font-medium">{title}</h2>
      <p className="text-muted-foreground mt-1.5 max-w-sm text-sm text-balance">{description}</p>
      {action ? <div className="mt-5">{action}</div> : null}
    </div>
  )
}
