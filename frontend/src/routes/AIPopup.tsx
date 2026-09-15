import { Sparkles, X } from 'lucide-react'
import { useSearchParams } from 'react-router'

import { ChatWindow } from '@/components/ai/ChatWindow'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useServers } from '@/lib/servers'

export function AIPopup() {
  const [searchParams] = useSearchParams()
  const serverId = searchParams.get('serverId') || undefined
  const deploymentId = searchParams.get('deploymentId') || undefined

  const { data: servers = [] } = useServers()
  const activeServer = servers.find((s) => s.id === serverId)

  function handleClose() {
    window.close()
  }

  return (
    <div className="flex h-screen w-screen flex-col bg-background text-foreground overflow-hidden select-none">
      {/* Standalone Window Title Bar */}
      <header className="flex h-11 items-center justify-between border-b px-3.5 bg-muted/60 shrink-0 select-none">
        <div className="flex items-center gap-2 min-w-0">
          <span className="bg-primary/15 text-primary flex size-6 items-center justify-center rounded-md">
            <Sparkles className="size-3.5" />
          </span>
          <span className="text-xs font-semibold tracking-tight">AI Assistant</span>
          <Badge variant="outline" className="text-[9px] px-1 py-0 h-4 font-mono text-muted-foreground">
            External Window
          </Badge>

          {activeServer && (
            <Badge variant="outline" className="text-[10px] hidden sm:inline-flex">
              {activeServer.name}
            </Badge>
          )}
        </div>

        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            className="size-7 text-muted-foreground hover:text-foreground"
            onClick={handleClose}
            title="Close this window"
          >
            <X className="size-3.5" />
          </Button>
        </div>
      </header>

      {/* Full Window Chat Area */}
      <main className="flex-1 min-h-0 overflow-hidden">
        <ChatWindow initialServerId={serverId} initialDeploymentId={deploymentId} />
      </main>
    </div>
  )
}
