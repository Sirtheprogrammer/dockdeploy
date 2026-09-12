import { ArrowDownToLine, Loader2, Pause, Play } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface Line {
  id: number
  stream: 'stdout' | 'stderr'
  text: string
}

// Browsers slow to a crawl long before a busy container stops logging, so the
// buffer is capped and the oldest lines are dropped.
const MAX_LINES = 5000

export function LogViewer({ serverID, containerID }: { serverID: string; containerID: string }) {
  const [lines, setLines] = useState<Line[]>([])
  const [connected, setConnected] = useState(false)
  const [following, setFollowing] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const scrollRef = useRef<HTMLDivElement>(null)
  const nextID = useRef(0)
  // Held in a ref as well as state so the scroll effect reads the current
  // value without re-subscribing the stream on every toggle.
  const followingRef = useRef(true)

  useEffect(() => {
    followingRef.current = following
  }, [following])

  // No state reset here: the parent remounts this component with a key per
  // container, so each container gets a fresh buffer without syncing state
  // inside an effect.
  useEffect(() => {
    const source = new EventSource(
      `/api/servers/${serverID}/containers/${containerID}/logs?tail=500&follow=true`,
      { withCredentials: true },
    )

    source.addEventListener('open', () => setConnected(true))

    source.addEventListener('log', (event) => {
      try {
        const parsed = JSON.parse((event as MessageEvent<string>).data) as {
          stream: 'stdout' | 'stderr'
          text: string
        }
        setLines((current) => {
          const next = [...current, { id: nextID.current++, ...parsed }]
          return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
        })
      } catch {
        // A malformed frame is not worth tearing the stream down for.
      }
    })

    source.addEventListener('error', () => {
      // EventSource fires this both for a dropped connection and for the
      // normal end of a stream, and reconnects on its own. Only surface it as
      // a hard failure once the browser has actually given up.
      setConnected(false)
      if (source.readyState === EventSource.CLOSED) {
        setError('The log stream closed. Reopen the container to reconnect.')
      }
    })

    source.addEventListener('end', () => {
      setConnected(false)
      source.close()
    })

    return () => {
      source.close()
      setConnected(false)
    }
  }, [serverID, containerID])

  // Pin to the bottom while following.
  useEffect(() => {
    if (!followingRef.current) return
    const node = scrollRef.current
    if (node) node.scrollTop = node.scrollHeight
  }, [lines])

  // Scrolling up is how a user says "stop moving"; scrolling back to the
  // bottom resumes. Fighting the scroll position is the fastest way to make a
  // log pane unusable.
  const onScroll = useCallback(() => {
    const node = scrollRef.current
    if (!node) return
    const atBottom = node.scrollHeight - node.scrollTop - node.clientHeight < 40
    setFollowing(atBottom)
  }, [])

  function jumpToBottom() {
    const node = scrollRef.current
    if (node) node.scrollTop = node.scrollHeight
    setFollowing(true)
  }

  return (
    <div className="flex h-[28rem] flex-col overflow-hidden rounded-md border">
      <div className="bg-muted/40 flex items-center justify-between gap-2 border-b px-3 py-1.5">
        <div className="text-muted-foreground flex items-center gap-2 text-xs">
          {connected ? (
            <>
              <span className="bg-success size-1.5 rounded-full" />
              Streaming
            </>
          ) : error ? (
            <span className="text-destructive">{error}</span>
          ) : (
            <>
              <Loader2 className="size-3 animate-spin" aria-hidden />
              Connecting
            </>
          )}
          <span className="tabular">
            {lines.length >= MAX_LINES ? `${MAX_LINES}+ lines` : `${lines.length} lines`}
          </span>
        </div>

        <Button
          variant="ghost"
          size="sm"
          onClick={following ? () => setFollowing(false) : jumpToBottom}
        >
          {following ? (
            <>
              <Pause aria-hidden />
              Pause scroll
            </>
          ) : (
            <>
              <Play aria-hidden />
              Follow
            </>
          )}
        </Button>
      </div>

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="scrollbar-thin bg-surface flex-1 overflow-auto p-3 font-mono text-xs leading-relaxed"
      >
        {lines.length === 0 ? (
          <p className="text-muted-foreground">Waiting for output.</p>
        ) : (
          lines.map((line) => (
            <div
              key={line.id}
              className={cn(
                'break-all whitespace-pre-wrap',
                line.stream === 'stderr' ? 'text-destructive' : 'text-foreground/85',
              )}
            >
              {line.text}
            </div>
          ))
        )}
      </div>

      {!following && lines.length > 0 ? (
        <button
          onClick={jumpToBottom}
          className="bg-primary text-primary-foreground flex items-center justify-center gap-1.5 py-1.5 text-xs font-medium"
        >
          <ArrowDownToLine className="size-3.5" aria-hidden />
          Jump to latest
        </button>
      ) : null}
    </div>
  )
}
