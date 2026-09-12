import { Link } from 'react-router'

import { Button } from '@/components/ui/button'

export function NotFound() {
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-4 px-6 text-center">
      <p className="text-muted-foreground font-mono text-sm">404</p>
      <h1 className="text-lg font-semibold">This page does not exist</h1>
      <Button asChild variant="outline">
        <Link to="/">Back to overview</Link>
      </Button>
    </div>
  )
}
