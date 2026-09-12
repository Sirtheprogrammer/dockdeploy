import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { AlertCircle, Maximize2, Minimize2, RefreshCw, Terminal as TermIcon, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { serverTerminalWebSocketURL, type Server } from '@/lib/servers'

interface ServerTerminalProps {
  server: Server
  onClose?: () => void
}

export function ServerTerminal({ server, onClose }: ServerTerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>('connecting')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [isFullscreen, setIsFullscreen] = useState(false)

  const connect = useCallback(() => {
    if (!containerRef.current) return

    // Clean up existing WebSocket if any
    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }

    setStatus('connecting')
    setErrorMessage(null)

    // Initialize xterm if not already created
    if (!termRef.current) {
      const term = new Terminal({
        cursorBlink: true,
        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
        fontSize: 13,
        lineHeight: 1.25,
        theme: {
          background: '#09090b',
          foreground: '#f4f4f5',
          cursor: '#38bdf8',
          selectionBackground: '#27272a',
          black: '#18181b',
          red: '#f87171',
          green: '#4ade80',
          yellow: '#facc15',
          blue: '#60a5fa',
          magenta: '#c084fc',
          cyan: '#38bdf8',
          white: '#f4f4f5',
          brightBlack: '#71717a',
          brightRed: '#ef4444',
          brightGreen: '#22c55e',
          brightYellow: '#eab308',
          brightBlue: '#3b82f6',
          brightMagenta: '#a855f7',
          brightCyan: '#06b6d4',
          brightWhite: '#ffffff',
        },
      })

      const fitAddon = new FitAddon()
      term.loadAddon(fitAddon)
      term.open(containerRef.current)

      termRef.current = term
      fitAddonRef.current = fitAddon
    }

    const term = termRef.current
    const fitAddon = fitAddonRef.current

    try {
      fitAddon?.fit()
    } catch {
      // Ignore if container is not yet visible
    }

    term.reset()
    term.writeln(`\x1b[38;5;244mConnecting to \x1b[1;38;5;255m${server.username}@${server.host}\x1b[0;38;5;244m...\x1b[0m\r\n`)

    const url = serverTerminalWebSocketURL(server.id, term.cols, term.rows)
    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onopen = () => {
      setStatus('connected')
      term.focus()
      // Send initial terminal dimensions
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
      }
    }

    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') {
        term.write(ev.data)
      } else if (ev.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(ev.data))
      }
    }

    ws.onerror = () => {
      setStatus('error')
      setErrorMessage('Failed to connect to the interactive server terminal.')
      term.writeln('\r\n\x1b[31;1mConnection error.\x1b[0m\r\n')
    }

    ws.onclose = (ev) => {
      setStatus('disconnected')
      if (ev.reason) {
        term.writeln(`\r\n\x1b[33mDisconnected: ${ev.reason}\x1b[0m\r\n`)
      } else {
        term.writeln('\r\n\x1b[33mTerminal connection closed.\x1b[0m\r\n')
      }
    }

    // Capture keystrokes from terminal and pipe to WebSocket
    const onDataDisposable = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data)
      }
    })

    return () => {
      onDataDisposable.dispose()
    }
  }, [server.id, server.username, server.host])

  useEffect(() => {
    const cleanup = connect()

    // Handle container resize
    const container = containerRef.current
    if (!container) return

    const resizeObserver = new ResizeObserver(() => {
      if (fitAddonRef.current && termRef.current) {
        try {
          fitAddonRef.current.fit()
          const { cols, rows } = termRef.current
          if (wsRef.current?.readyState === WebSocket.OPEN) {
            wsRef.current.send(JSON.stringify({ type: 'resize', cols, rows }))
          }
        } catch {
          // ignore layout exceptions
        }
      }
    })

    resizeObserver.observe(container)

    return () => {
      resizeObserver.disconnect()
      cleanup?.()
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
      if (termRef.current) {
        termRef.current.dispose()
        termRef.current = null
      }
    }
  }, [connect])

  const clearTerminal = () => {
    termRef.current?.clear()
  }

  return (
    <div
      className={`flex flex-col overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950 shadow-2xl transition-all ${
        isFullscreen ? 'fixed inset-4 z-50 rounded-xl' : 'h-[32rem] w-full'
      }`}
    >
      {/* Terminal Title Bar */}
      <div className="flex items-center justify-between border-b border-zinc-800 bg-zinc-900/90 px-4 py-2 select-none">
        <div className="flex items-center gap-2.5">
          <TermIcon className="size-4 text-sky-400" aria-hidden />
          <span className="font-mono text-xs font-semibold text-zinc-200">
            {server.username}@{server.host}
            {server.port !== 22 ? `:${server.port}` : ''}
          </span>
          <span
            className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10px] font-medium tracking-wide uppercase ${
              status === 'connected'
                ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                : status === 'connecting'
                  ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                  : 'bg-zinc-500/10 text-zinc-400 border border-zinc-500/20'
            }`}
          >
            <span
              className={`size-1.5 rounded-full ${
                status === 'connected'
                  ? 'bg-emerald-400 animate-pulse'
                  : status === 'connecting'
                    ? 'bg-amber-400 animate-ping'
                    : 'bg-zinc-500'
              }`}
            />
            {status}
          </span>
        </div>

        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800"
            onClick={clearTerminal}
            title="Clear output"
          >
            <Trash2 className="size-3.5" />
            <span className="hidden sm:inline">Clear</span>
          </Button>

          {status === 'disconnected' || status === 'error' ? (
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs border-zinc-700 bg-zinc-800/80 text-zinc-200 hover:bg-zinc-700"
              onClick={connect}
            >
              <RefreshCw className="size-3.5 mr-1" />
              Reconnect
            </Button>
          ) : null}

          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800"
            onClick={() => setIsFullscreen(!isFullscreen)}
            title={isFullscreen ? 'Exit full screen' : 'Full screen'}
          >
            {isFullscreen ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
          </Button>

          {onClose ? (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-zinc-400 hover:text-destructive hover:bg-zinc-800"
              onClick={onClose}
              title="Close terminal"
            >
              &times;
            </Button>
          ) : null}
        </div>
      </div>

      {errorMessage ? (
        <div className="flex items-center gap-2 border-b border-red-500/20 bg-red-950/40 px-4 py-2 text-xs text-red-300">
          <AlertCircle className="size-4 shrink-0 text-red-400" />
          <span>{errorMessage}</span>
        </div>
      ) : null}

      {/* XTerm viewport mount point */}
      <div
        ref={containerRef}
        className="relative flex-1 p-2 bg-[#09090b] focus:outline-none overflow-hidden"
        onClick={() => termRef.current?.focus()}
      />
    </div>
  )
}
