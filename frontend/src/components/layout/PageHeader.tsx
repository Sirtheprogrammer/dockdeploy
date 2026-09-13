import type { ReactNode } from 'react'

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string
  description?: string
  actions?: ReactNode
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-4 sm:px-6 sm:py-6 lg:px-8">
      <div className="min-w-0 space-y-1">
        <h1 className="truncate text-xl sm:text-2xl font-semibold tracking-tight">{title}</h1>
        {description ? <p className="text-muted-foreground max-w-2xl text-xs sm:text-sm">{description}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2 w-full sm:w-auto mt-1 sm:mt-0">{actions}</div> : null}
    </div>
  )
}
