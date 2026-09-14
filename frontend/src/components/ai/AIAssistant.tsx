import {
  GripHorizontal,
  Maximize2,
  Minimize2,
  Move,
  PanelRightClose,
  PanelRightOpen,
  Sparkles,
  X,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { ChatWindow } from '@/components/ai/ChatWindow'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export type AssistantMode = 'docked' | 'floating'

interface AIAssistantProps {
  serverId?: string
  deploymentId?: string
}

export function AIAssistant({ serverId, deploymentId }: AIAssistantProps) {
  const [isOpen, setIsOpen] = useState<boolean>(() => {
    return localStorage.getItem('dockdeploy:ai:open') === 'true'
  })

  const [mode, setMode] = useState<AssistantMode>(() => {
    return (localStorage.getItem('dockdeploy:ai:mode') as AssistantMode) || 'docked'
  })

  const [isMinimized, setIsMinimized] = useState<boolean>(false)

  // Position for floating mode (percentages or pixels)
  const [position, setPosition] = useState<{ x: number; y: number }>(() => {
    const saved = localStorage.getItem('dockdeploy:ai:pos')
    if (saved) {
      try {
        return JSON.parse(saved)
      } catch {
        /* ignore invalid JSON in storage */
      }
    }
    // Default floating position: bottom right area
    return {
      x: Math.max(20, window.innerWidth - 480),
      y: Math.max(40, window.innerHeight - 680),
    }
  })

  const isDraggingRef = useRef(false)
  const dragOffsetRef = useRef({ x: 0, y: 0 })

  useEffect(() => {
    localStorage.setItem('dockdeploy:ai:open', String(isOpen))
  }, [isOpen])

  useEffect(() => {
    localStorage.setItem('dockdeploy:ai:mode', mode)
  }, [mode])

  useEffect(() => {
    localStorage.setItem('dockdeploy:ai:pos', JSON.stringify(position))
  }, [position])

  // Global keyboard shortcut: Cmd+J or Ctrl+J
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'j') {
        e.preventDefault()
        setIsOpen((prev) => !prev)
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [])

  // Drag listeners
  function handleMouseDown(e: React.MouseEvent) {
    if (mode !== 'floating') return
    isDraggingRef.current = true
    dragOffsetRef.current = {
      x: e.clientX - position.x,
      y: e.clientY - position.y,
    }

    function handleMouseMove(moveEvent: MouseEvent) {
      if (!isDraggingRef.current) return
      const nextX = Math.max(10, Math.min(window.innerWidth - 360, moveEvent.clientX - dragOffsetRef.current.x))
      const nextY = Math.max(10, Math.min(window.innerHeight - 100, moveEvent.clientY - dragOffsetRef.current.y))
      setPosition({ x: nextX, y: nextY })
    }

    function handleMouseUp() {
      isDraggingRef.current = false
      window.removeEventListener('mousemove', handleMouseMove)
      window.removeEventListener('mouseup', handleMouseUp)
    }

    window.addEventListener('mousemove', handleMouseMove)
    window.addEventListener('mouseup', handleMouseUp)
  }

  if (!isOpen) {
    return (
      <button
        type="button"
        onClick={() => setIsOpen(true)}
        className="fixed bottom-5 right-5 z-40 flex items-center gap-2 rounded-full border border-primary/30 bg-primary px-3.5 py-2.5 text-xs font-semibold text-primary-foreground shadow-xl transition-transform hover:scale-105 active:scale-95 focus-visible:outline-2"
        title="Open AI Assistant (Ctrl+J / Cmd+J)"
      >
        <Sparkles className="size-4 animate-pulse" />
        <span>AI Assistant</span>
        <kbd className="bg-primary-foreground/20 rounded px-1.5 py-0.5 text-[10px] font-mono">
          ⌘J
        </kbd>
      </button>
    )
  }

  return (
    <>
      {/* Docked Sidebar Mode */}
      {mode === 'docked' && (
        <aside
          className={cn(
            'fixed inset-y-0 right-0 z-40 flex flex-col border-l bg-background shadow-2xl transition-all duration-300',
            isMinimized ? 'w-14' : 'w-full sm:w-[440px] md:w-[460px] lg:w-[480px]',
          )}
        >
          {/* Header */}
          <div className="flex h-14 items-center justify-between border-b px-4 bg-muted/40 shrink-0">
            <div className="flex items-center gap-2 min-w-0">
              <span className="bg-primary/15 text-primary flex size-7 items-center justify-center rounded-lg">
                <Sparkles className="size-4" />
              </span>
              {!isMinimized && (
                <div className="min-w-0 leading-none">
                  <span className="text-sm font-semibold tracking-tight">AI Assistant</span>
                  <span className="text-muted-foreground block text-[10px] truncate">
                    Multipurpose Diagnostic & Ops
                  </span>
                </div>
              )}
            </div>

            <div className="flex items-center gap-1">
              {!isMinimized && (
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7 text-muted-foreground hover:text-foreground"
                  onClick={() => setMode('floating')}
                  title="Switch to Floating Window (Movable)"
                >
                  <Move className="size-3.5" />
                </Button>
              )}

              <Button
                variant="ghost"
                size="icon"
                className="size-7 text-muted-foreground hover:text-foreground"
                onClick={() => setIsMinimized(!isMinimized)}
                title={isMinimized ? 'Expand sidebar' : 'Minimize sidebar'}
              >
                {isMinimized ? <PanelRightOpen className="size-3.5" /> : <PanelRightClose className="size-3.5" />}
              </Button>

              <Button
                variant="ghost"
                size="icon"
                className="size-7 text-muted-foreground hover:text-foreground"
                onClick={() => setIsOpen(false)}
                title="Close assistant"
              >
                <X className="size-4" />
              </Button>
            </div>
          </div>

          {!isMinimized && (
            <div className="flex-1 min-h-0">
              <ChatWindow initialServerId={serverId} initialDeploymentId={deploymentId} />
            </div>
          )}
        </aside>
      )}

      {/* Movable Floating Window Mode */}
      {mode === 'floating' && (
        <div
          style={{
            left: `${position.x}px`,
            top: `${position.y}px`,
          }}
          className={cn(
            'fixed z-50 flex flex-col rounded-2xl border border-border/80 bg-background shadow-2xl transition-[width,height] backdrop-blur-md',
            isMinimized
              ? 'w-72 h-12 overflow-hidden'
              : 'w-[95vw] sm:w-[460px] md:w-[500px] h-[620px] max-h-[90vh]',
          )}
        >
          {/* Draggable Header */}
          <div
            onMouseDown={handleMouseDown}
            className="flex h-12 items-center justify-between border-b px-3.5 bg-muted/60 rounded-t-2xl cursor-grab active:cursor-grabbing select-none shrink-0"
          >
            <div className="flex items-center gap-2 min-w-0 pointer-events-none">
              <GripHorizontal className="size-4 text-muted-foreground/70" />
              <span className="bg-primary/15 text-primary flex size-6 items-center justify-center rounded-md">
                <Sparkles className="size-3.5" />
              </span>
              <span className="text-xs font-semibold tracking-tight truncate">
                AI Assistant
              </span>
              <Badge variant="outline" className="text-[9px] px-1 py-0 h-4 uppercase tracking-wider text-muted-foreground font-normal">
                Movable
              </Badge>
            </div>

            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                className="size-6 text-muted-foreground hover:text-foreground"
                onClick={() => setMode('docked')}
                title="Dock to Right Sidebar"
              >
                <PanelRightClose className="size-3.5" />
              </Button>

              <Button
                variant="ghost"
                size="icon"
                className="size-6 text-muted-foreground hover:text-foreground"
                onClick={() => setIsMinimized(!isMinimized)}
                title={isMinimized ? 'Expand' : 'Minimize'}
              >
                {isMinimized ? <Maximize2 className="size-3.5" /> : <Minimize2 className="size-3.5" />}
              </Button>

              <Button
                variant="ghost"
                size="icon"
                className="size-6 text-muted-foreground hover:text-foreground"
                onClick={() => setIsOpen(false)}
                title="Close"
              >
                <X className="size-3.5" />
              </Button>
            </div>
          </div>

          {!isMinimized && (
            <div className="flex-1 min-h-0 overflow-hidden rounded-b-2xl">
              <ChatWindow initialServerId={serverId} initialDeploymentId={deploymentId} />
            </div>
          )}
        </div>
      )}
    </>
  )
}
