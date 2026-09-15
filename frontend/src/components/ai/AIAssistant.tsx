import {
  ExternalLink,
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
import { toast } from 'sonner'

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

  // Resizable width for docked sidebar (default 460px, min 320px, max 850px)
  const [sidebarWidth, setSidebarWidth] = useState<number>(() => {
    const saved = localStorage.getItem('dockdeploy:ai:sidebar_width')
    return saved ? Math.max(320, Math.min(850, parseInt(saved, 10))) : 460
  })

  // Position & size for floating mode
  const [position, setPosition] = useState<{ x: number; y: number }>(() => {
    const saved = localStorage.getItem('dockdeploy:ai:pos')
    if (saved) {
      try {
        return JSON.parse(saved)
      } catch {
        /* ignore invalid JSON */
      }
    }
    return {
      x: Math.max(20, (typeof window !== 'undefined' ? window.innerWidth : 1000) - 520),
      y: Math.max(40, (typeof window !== 'undefined' ? window.innerHeight : 800) - 700),
    }
  })

  const [floatingSize, setFloatingSize] = useState<{ width: number; height: number }>(() => {
    const saved = localStorage.getItem('dockdeploy:ai:floating_size')
    if (saved) {
      try {
        const parsed = JSON.parse(saved)
        return {
          width: Math.max(340, Math.min(window.innerWidth - 20, parsed.width || 480)),
          height: Math.max(360, Math.min(window.innerHeight - 40, parsed.height || 640)),
        }
      } catch {
        /* ignore invalid JSON */
      }
    }
    return {
      width: Math.min(500, (typeof window !== 'undefined' ? window.innerWidth - 30 : 500)),
      height: Math.min(640, (typeof window !== 'undefined' ? window.innerHeight - 80 : 640)),
    }
  })

  const isDraggingRef = useRef(false)
  const dragOffsetRef = useRef({ x: 0, y: 0 })

  const isResizingSidebarRef = useRef(false)
  const resizeStartXRef = useRef(0)
  const startWidthRef = useRef(460)

  const isResizingFloatingRef = useRef(false)
  const resizeFloatingStartRef = useRef({ x: 0, y: 0, w: 480, h: 640 })

  useEffect(() => {
    localStorage.setItem('dockdeploy:ai:open', String(isOpen))
  }, [isOpen])

  useEffect(() => {
    localStorage.setItem('dockdeploy:ai:mode', mode)
  }, [mode])

  useEffect(() => {
    localStorage.setItem('dockdeploy:ai:pos', JSON.stringify(position))
  }, [position])

  useEffect(() => {
    localStorage.setItem('dockdeploy:ai:sidebar_width', String(sidebarWidth))
  }, [sidebarWidth])

  useEffect(() => {
    localStorage.setItem('dockdeploy:ai:floating_size', JSON.stringify(floatingSize))
  }, [floatingSize])

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

  // Floating window dragging listener
  function handleMouseDown(e: React.MouseEvent) {
    if (mode !== 'floating') return
    isDraggingRef.current = true
    dragOffsetRef.current = {
      x: e.clientX - position.x,
      y: e.clientY - position.y,
    }

    function handleMouseMove(moveEvent: MouseEvent) {
      if (!isDraggingRef.current) return
      const nextX = Math.max(10, Math.min(window.innerWidth - 320, moveEvent.clientX - dragOffsetRef.current.x))
      const nextY = Math.max(10, Math.min(window.innerHeight - 80, moveEvent.clientY - dragOffsetRef.current.y))
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

  // Docked sidebar resize listener
  function handleSidebarResizeStart(e: React.MouseEvent) {
    e.preventDefault()
    isResizingSidebarRef.current = true
    resizeStartXRef.current = e.clientX
    startWidthRef.current = sidebarWidth

    function onMouseMove(moveEvent: MouseEvent) {
      if (!isResizingSidebarRef.current) return
      const delta = resizeStartXRef.current - moveEvent.clientX
      const maxW = Math.max(400, window.innerWidth - 320)
      const nextW = Math.max(320, Math.min(maxW, startWidthRef.current + delta))
      setSidebarWidth(nextW)
    }

    function onMouseUp() {
      isResizingSidebarRef.current = false
      window.removeEventListener('mousemove', onMouseMove)
      window.removeEventListener('mouseup', onMouseUp)
    }

    window.addEventListener('mousemove', onMouseMove)
    window.addEventListener('mouseup', onMouseUp)
  }

  // Floating window corner resize listener
  function handleFloatingResizeStart(e: React.MouseEvent) {
    e.preventDefault()
    e.stopPropagation()
    isResizingFloatingRef.current = true
    resizeFloatingStartRef.current = {
      x: e.clientX,
      y: e.clientY,
      w: floatingSize.width,
      h: floatingSize.height,
    }

    function onMouseMove(moveEvent: MouseEvent) {
      if (!isResizingFloatingRef.current) return
      const deltaX = moveEvent.clientX - resizeFloatingStartRef.current.x
      const deltaY = moveEvent.clientY - resizeFloatingStartRef.current.y
      const nextW = Math.max(340, Math.min(window.innerWidth - 24, resizeFloatingStartRef.current.w + deltaX))
      const nextH = Math.max(350, Math.min(window.innerHeight - 40, resizeFloatingStartRef.current.h + deltaY))
      setFloatingSize({ width: nextW, height: nextH })
    }

    function onMouseUp() {
      isResizingFloatingRef.current = false
      window.removeEventListener('mousemove', onMouseMove)
      window.removeEventListener('mouseup', onMouseUp)
    }

    window.addEventListener('mousemove', onMouseMove)
    window.addEventListener('mouseup', onMouseUp)
  }

  // Open in an independent native external popup window
  function handleOpenExternal() {
    const width = 540
    const height = 780
    const left = Math.max(0, window.screen.width - width - 60)
    const top = 80
    const params = new URLSearchParams()
    if (serverId) params.set('serverId', serverId)
    if (deploymentId) params.set('deploymentId', deploymentId)
    const query = params.toString() ? `?${params.toString()}` : ''

    const popup = window.open(
      `/ai-popup${query}`,
      'dockdeploy_ai_assistant',
      `width=${width},height=${height},left=${left},top=${top},resizable=yes,scrollbars=yes,status=no,toolbar=no,menubar=no`,
    )

    if (popup) {
      popup.focus()
      setIsOpen(false)
      toast.info('AI Assistant opened in separate external window')
    } else {
      toast.error('Popup blocked by browser. Please allow popups for this site.')
    }
  }

  // Floating launch toggle button (when closed)
  if (!isOpen) {
    return (
      <button
        type="button"
        onClick={() => setIsOpen(true)}
        className="fixed bottom-20 right-4 sm:bottom-5 sm:right-5 z-40 flex items-center gap-2 rounded-full border border-primary/30 bg-primary px-3.5 py-2.5 text-xs font-semibold text-primary-foreground shadow-xl transition-transform hover:scale-105 active:scale-95 focus-visible:outline-2 cursor-pointer"
        title="Open AI Assistant (Ctrl+J / Cmd+J)"
      >
        <Sparkles className="size-4 animate-pulse" />
        <span>AI Assistant</span>
        <kbd className="bg-primary-foreground/20 rounded px-1.5 py-0.5 text-[10px] font-mono hidden sm:inline-block">
          ⌘J
        </kbd>
      </button>
    )
  }

  return (
    <>
      {/* 
        DOCKED MODE:
        - Desktop: Sits in flex container alongside center content so center content compacts.
        - Mobile: Renders as full-width slide-over drawer overlay.
      */}
      {mode === 'docked' && (
        <>
          {/* Desktop in-layout compacting sidebar */}
          <aside
            style={{ width: isMinimized ? '3.5rem' : `${sidebarWidth}px` }}
            className="sticky top-0 h-screen shrink-0 border-l bg-background hidden md:flex flex-col select-auto relative transition-[width] duration-150 z-30"
          >
            {/* Left border resize handle */}
            {!isMinimized && (
              <div
                onMouseDown={handleSidebarResizeStart}
                className="group/resize absolute inset-y-0 -left-1.5 w-3 cursor-ew-resize z-40 flex items-center justify-center select-none"
                title="Drag to resize sidebar width"
              >
                <div className="w-1 h-12 rounded-full bg-border/80 group-hover/resize:bg-primary transition-colors" />
              </div>
            )}

            {/* Header */}
            <div className="flex h-14 items-center justify-between border-b px-3.5 bg-muted/40 shrink-0">
              <div className="flex items-center gap-2 min-w-0">
                <span className="bg-primary/15 text-primary flex size-7 items-center justify-center rounded-lg">
                  <Sparkles className="size-4" />
                </span>
                {!isMinimized && (
                  <div className="min-w-0 leading-none">
                    <span className="text-sm font-semibold tracking-tight">AI Assistant</span>
                    <span className="text-muted-foreground block text-[10px] truncate">
                      Diagnostic & Ops
                    </span>
                  </div>
                )}
              </div>

              <div className="flex items-center gap-1">
                {!isMinimized && (
                  <>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-7 text-muted-foreground hover:text-foreground"
                      onClick={handleOpenExternal}
                      title="Open in independent window outside browser"
                    >
                      <ExternalLink className="size-3.5" />
                    </Button>

                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-7 text-muted-foreground hover:text-foreground"
                      onClick={() => setMode('floating')}
                      title="Switch to Floating Window (Movable)"
                    >
                      <Move className="size-3.5" />
                    </Button>
                  </>
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

                {!isMinimized && (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-7 text-muted-foreground hover:text-foreground"
                    onClick={() => setIsOpen(false)}
                    title="Close assistant"
                  >
                    <X className="size-4" />
                  </Button>
                )}
              </div>
            </div>

            {/* Body */}
            {!isMinimized && (
              <div className="flex-1 min-h-0 overflow-hidden">
                <ChatWindow initialServerId={serverId} initialDeploymentId={deploymentId} />
              </div>
            )}
          </aside>

          {/* Mobile Overlay Drawer */}
          <aside className="fixed inset-y-0 right-0 z-50 flex flex-col border-l bg-background shadow-2xl w-full md:hidden">
            <div className="flex h-14 items-center justify-between border-b px-4 bg-muted/40 shrink-0">
              <div className="flex items-center gap-2 min-w-0">
                <span className="bg-primary/15 text-primary flex size-7 items-center justify-center rounded-lg">
                  <Sparkles className="size-4" />
                </span>
                <span className="text-sm font-semibold tracking-tight">AI Assistant</span>
              </div>

              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7 text-muted-foreground hover:text-foreground"
                  onClick={handleOpenExternal}
                  title="Open in external window"
                >
                  <ExternalLink className="size-3.5" />
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

            <div className="flex-1 min-h-0">
              <ChatWindow initialServerId={serverId} initialDeploymentId={deploymentId} />
            </div>
          </aside>
        </>
      )}

      {/* 
        MOVABLE & RESIZABLE FLOATING WINDOW MODE:
        - Freely draggable via top title bar.
        - Resizable from bottom-right corner.
      */}
      {mode === 'floating' && (
        <div
          style={{
            left: `${Math.min(Math.max(8, position.x), Math.max(8, (typeof window !== 'undefined' ? window.innerWidth : 1000) - 340))}px`,
            top: `${Math.min(Math.max(8, position.y), Math.max(8, (typeof window !== 'undefined' ? window.innerHeight : 800) - 80))}px`,
            width: isMinimized ? '18rem' : `${floatingSize.width}px`,
            height: isMinimized ? '3rem' : `${floatingSize.height}px`,
          }}
          className={cn(
            'fixed z-50 flex flex-col rounded-2xl border border-border/90 bg-background shadow-2xl backdrop-blur-md transition-[width,height] max-w-[calc(100vw-1rem)] max-h-[calc(100vh-2rem)]',
            isMinimized && 'overflow-hidden',
          )}
        >
          {/* Draggable Header */}
          <div
            onMouseDown={handleMouseDown}
            className="flex h-11 items-center justify-between border-b px-3.5 bg-muted/60 rounded-t-2xl cursor-grab active:cursor-grabbing select-none shrink-0"
          >
            <div className="flex items-center gap-2 min-w-0 pointer-events-none">
              <GripHorizontal className="size-4 text-muted-foreground/70" />
              <span className="bg-primary/15 text-primary flex size-5.5 items-center justify-center rounded-md">
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
                onClick={handleOpenExternal}
                title="Open in independent window outside browser"
              >
                <ExternalLink className="size-3" />
              </Button>

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
            <div className="flex-1 min-h-0 overflow-hidden rounded-b-2xl relative">
              <ChatWindow initialServerId={serverId} initialDeploymentId={deploymentId} />

              {/* Bottom-right corner resize handle */}
              <div
                onMouseDown={handleFloatingResizeStart}
                className="absolute bottom-1 right-1 size-4 cursor-se-resize flex items-center justify-center text-muted-foreground/40 hover:text-primary transition-colors select-none z-30"
                title="Drag to resize window size"
              >
                <svg className="size-3" viewBox="0 0 16 16" fill="currentColor">
                  <path d="M14 14H12V12H14V14ZM14 10H12V8H14V10ZM10 14H8V12H10V14ZM14 6H12V4H14V6ZM6 14H4V12H6V14ZM10 10H8V8H10V10Z" />
                </svg>
              </div>
            </div>
          )}
        </div>
      )}
    </>
  )
}
