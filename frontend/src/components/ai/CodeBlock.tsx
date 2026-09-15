import { Check, Copy } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'

import { cn } from '@/lib/utils'

interface CodeBlockProps {
  language?: string
  code: string
  className?: string
}

export function CodeBlock({ language, code, className }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)

  function handleCopy() {
    navigator.clipboard.writeText(code)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className={cn('group bg-zinc-950 my-2 overflow-hidden rounded-lg border border-zinc-800 text-xs font-mono', className)}>
      <div className="flex items-center justify-between border-b border-zinc-800/80 bg-zinc-900/60 px-3 py-1.5 text-zinc-400">
        <span className="text-[11px] font-medium tracking-wide uppercase">{language || 'code'}</span>
        <Button
          variant="ghost"
          size="icon"
          onClick={handleCopy}
          className="h-6 w-6 text-zinc-400 hover:text-zinc-100"
          title="Copy code"
        >
          {copied ? <Check className="size-3 text-emerald-400" /> : <Copy className="size-3" />}
        </Button>
      </div>
      <pre className="scrollbar-thin overflow-x-auto p-3 text-zinc-200 leading-relaxed max-h-96">
        <code>{code}</code>
      </pre>
    </div>
  )
}
