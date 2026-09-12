import { ArrowDownToLine, CircleCheck, CircleX, Loader2 } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import type { RunStatus } from '@/lib/deployments'
import { cn } from '@/lib/utils'

interface Line {
  seq: number
  stream: string
  line: string
}

// A docker build can emit tens of thousands of lines. The browser gives up
// long before the build does, so the buffer is capped.
const MAX_LINES = 8000

/**
 * Build output for one run.
 *
 * Unlike container logs, this stream is finite: the server replays everything
 * already stored, then attaches to the live feed, then closes when the run
 * reaches a terminal state. That ordering is what makes a mid-build page
 * reload show the whole log rather than only what happened after reconnecting.
 */
export function RunLogViewer({
  deploymentID,
  runID,
  initialStatus,
}: {
  deploymentID: string
  runID: string
  initialStatus: RunStatus
}) {
  const [lines, setLines] = useState<Line[]>([])
  const [status, setStatus] = useState<RunStatus>(initialStatus)
  const [failure, setFailure] = useState('')
  const [connected, setConnected] = useState(false)
  const [following, setFollowing] = useState(true)

  const scrollRef = useRef<HTMLDivElement>(null)
  // Read by the scroll effect without making it re-subscribe on every toggle.
  const followingRef = useRef(true)

  useEffect(() => {
    followingRef.current = following
  }, [following])

  useEffect(() => {
    const source = new EventSource(
      `/api/deployments/${deploymentID}/runs/${runID}/logs`,
      { withCredentials: true },
    )

    source.addEventListener('open', () => setConnected(true))

    source.addEventListener('log', (event) => {
      try {
        const entry = JSON.parse((event as MessageEvent<string>).data) as Line
        setLines((current) => {
          // The server may resend during replay; keep the log idempotent.
          if (current.length > 0 && entry.seq <= current[current.length - 1]!.seq) {
            return current
          }
          const next = [...current, entry]
          return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
        })
      } catch {
        // A malformed frame is not worth tearing the stream down for.
      }
    })

    source.addEventListener('status', (event) => {
      try {
        const payload = JSON.parse((event as MessageEvent<string>).data) as {
          status: RunStatus
          error: string
        }
        setStatus(payload.status)
        setFailure(payload.error ?? '')
      } catch {
        /* ignore */
      }
    })

    source.addEventListener('end', () => {
      setConnected(false)
      source.close()
    })

    source.addEventListener('error', () => {
      setConnected(false)
      // EventSource reconnects on its own. Only a CLOSED state means it has
      // actually given up, and the server closes the stream deliberately when
      // a run finishes, so that is not an error worth showing.
      if (source.readyState === EventSource.CLOSED) setConnected(false)
    })

    return () => {
      source.close()
      setConnected(false)
    }
  }, [deploymentID, runID])

  useEffect(() => {
    if (!followingRef.current) return
    const node = scrollRef.current
    if (node) node.scrollTop = node.scrollHeight
  }, [lines])

  const onScroll = useCallback(() => {
    const node = scrollRef.current
    if (!node) return
    setFollowing(node.scrollHeight - node.scrollTop - node.clientHeight < 40)
  }, [])

  function jumpToBottom() {
    const node = scrollRef.current
    if (node) node.scrollTop = node.scrollHeight
    setFollowing(true)
  }

  const running = status === 'queued' || status === 'running'

  return (
    <div className="flex h-[30rem] flex-col overflow-hidden rounded-md border">
      <div className="bg-muted/40 flex items-center justify-between gap-2 border-b px-3 py-1.5">
        <div className="flex items-center gap-2 text-xs">
          {running ? (
            <>
              <Loader2 className="text-muted-foreground size-3 animate-spin" aria-hidden />
              <span className="text-muted-foreground">
                {connected ? 'Building' : 'Reconnecting'}
              </span>
            </>
          ) : status === 'succeeded' ? (
            <>
              <CircleCheck className="text-success size-3.5" aria-hidden />
              <span className="text-success">Succeeded</span>
            </>
          ) : (
            <>
              <CircleX className="text-destructive size-3.5" aria-hidden />
              <span className="text-destructive">
                {status === 'cancelled' ? 'Cancelled' : 'Failed'}
              </span>
            </>
          )}
          <span className="text-muted-foreground tabular">
            {lines.length >= MAX_LINES ? `${MAX_LINES}+ lines` : `${lines.length} lines`}
          </span>
        </div>

        {running ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={following ? () => setFollowing(false) : jumpToBottom}
          >
            {following ? 'Pause scroll' : 'Follow'}
          </Button>
        ) : null}
      </div>

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="scrollbar-thin bg-surface flex-1 overflow-auto p-3 font-mono text-xs leading-relaxed"
      >
        {lines.length === 0 ? (
          <p className="text-muted-foreground">
            {running ? 'Waiting for the build to start.' : 'This run produced no output.'}
          </p>
        ) : (
          lines.map((entry) => (
            <div
              key={entry.seq}
              className={cn(
                'break-all whitespace-pre-wrap',
                // "system" lines are the platform narrating what it is doing,
                // so they are tinted to stand out from raw command output.
                entry.stream === 'system' && 'text-primary',
                entry.stream === 'error' && 'text-destructive font-medium',
                entry.stream === 'stderr' && 'text-warning',
                entry.stream === 'stdout' && 'text-foreground/85',
              )}
            >
              {entry.line}
            </div>
          ))
        )}
      </div>

      {failure ? (
        <div className="border-destructive/30 bg-destructive/10 text-destructive border-t px-3 py-2 text-xs">
          {failure}
        </div>
      ) : null}

      {!following && running ? (
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
