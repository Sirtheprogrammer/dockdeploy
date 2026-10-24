import { AlertCircle, Loader2 } from 'lucide-react'
import { useSearchParams } from 'react-router'

import { ServerTerminal } from '@/components/servers/ServerTerminal'
import { Button } from '@/components/ui/button'
import { useServer } from '@/lib/servers'

export function TerminalPopup() {
  const [searchParams] = useSearchParams()
  const serverId = searchParams.get('serverId') || ''

  const { data: server, isLoading, isError, error } = useServer(serverId)

  function handleClose() {
    window.close()
  }

  if (isLoading) {
    return (
      <div className="flex h-screen w-screen flex-col items-center justify-center bg-zinc-950 text-zinc-100">
        <Loader2 className="size-8 animate-spin text-sky-400" />
        <p className="mt-3 text-sm text-zinc-400">Connecting to server terminal...</p>
      </div>
    )
  }

  if (isError || !server) {
    return (
      <div className="flex h-screen w-screen flex-col items-center justify-center bg-zinc-950 p-6 text-center text-zinc-100">
        <AlertCircle className="size-10 text-rose-500" />
        <h2 className="mt-3 text-base font-semibold">Server Not Found</h2>
        <p className="mt-1 text-sm text-zinc-400">
          {error instanceof Error ? error.message : 'Could not locate server for interactive terminal session.'}
        </p>
        <Button variant="outline" size="sm" onClick={handleClose} className="mt-4 border-zinc-800 text-zinc-200">
          Close Window
        </Button>
      </div>
    )
  }

  return (
    <div className="flex h-screen w-screen flex-col bg-zinc-950 text-zinc-100 overflow-hidden select-none">
      <ServerTerminal
        server={server}
        standalone={true}
        onClose={handleClose}
      />
    </div>
  )
}
