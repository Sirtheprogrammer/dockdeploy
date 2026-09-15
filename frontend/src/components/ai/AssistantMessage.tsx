import {
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
} from 'lucide-react'
import { useMemo, useState } from 'react'

import { MarkdownRenderer } from '@/components/ai/MarkdownRenderer'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { SafeguardAction } from '@/lib/ai'
import { cn } from '@/lib/utils'

interface AssistantMessageProps {
  content: string
  actionFromMeta?: SafeguardAction
  isStreaming?: boolean
  className?: string
}

export function AssistantMessage({
  content,
  actionFromMeta,
  isStreaming,
  className,
}: AssistantMessageProps) {
  const [copied, setCopied] = useState(false)
  const [isExpanded, setIsExpanded] = useState(false)

  // Message statistics
  const { wordCount, isVeryLong } = useMemo(() => {
    const trimmed = content.trim()
    if (!trimmed) return { wordCount: 0, isVeryLong: false }
    const words = trimmed.split(/\s+/).length
    const lines = trimmed.split('\n').length
    // Consider long if > 35 lines or > 1200 characters or > 300 words
    const long = lines > 35 || trimmed.length > 1400 || words > 300
    return { wordCount: words, isVeryLong: long }
  }, [content])

  function handleCopy() {
    // Strip any raw safeguard action blocks for clean clipboard copy
    const cleanContent = content.replace(/```safeguard_action[\s\S]*?```/g, '').trim()
    void navigator.clipboard.writeText(cleanContent)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className={cn('group/msg relative flex flex-col', className)}>
      {/* Header bar with word count and copy button */}
      {!isStreaming && content.trim() && (
        <div className="flex items-center justify-between pb-1.5 mb-1.5 border-b border-border/40 text-[10px] text-muted-foreground">
          <div className="flex items-center gap-2">
            <span className="font-semibold text-foreground/80">AI Assistant</span>
            {wordCount > 100 && (
              <Badge variant="outline" className="text-[9px] px-1 py-0 h-4 text-muted-foreground font-normal">
                ~{wordCount} words
              </Badge>
            )}
          </div>

          <Button
            variant="ghost"
            size="icon"
            onClick={handleCopy}
            className="size-6 text-muted-foreground hover:text-foreground opacity-80 group-hover/msg:opacity-100 transition-opacity"
            title="Copy full response"
          >
            {copied ? <Check className="size-3 text-emerald-400" /> : <Copy className="size-3" />}
          </Button>
        </div>
      )}

      {/* Message Body with Markdown Rendering */}
      <div
        className={cn(
          'transition-[max-height] duration-200 overflow-hidden relative',
          isVeryLong && !isExpanded && !isStreaming ? 'max-h-[380px]' : '',
        )}
      >
        <MarkdownRenderer content={content} actionFromMeta={actionFromMeta} />

        {/* Fade overlay if collapsed */}
        {isVeryLong && !isExpanded && !isStreaming && (
          <div className="absolute inset-x-0 bottom-0 h-24 bg-gradient-to-t from-card via-card/80 to-transparent pointer-events-none" />
        )}
      </div>

      {/* Expand / Collapse Toggle for very long content */}
      {isVeryLong && !isStreaming && (
        <div className="pt-2 flex justify-center">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setIsExpanded(!isExpanded)}
            className="h-6 px-2 text-[11px] gap-1 text-muted-foreground hover:text-foreground bg-card shadow-xs"
          >
            {isExpanded ? (
              <>
                <ChevronUp className="size-3" />
                Show less
              </>
            ) : (
              <>
                <ChevronDown className="size-3" />
                Show full response ({wordCount} words)
              </>
            )}
          </Button>
        </div>
      )}
    </div>
  )
}
