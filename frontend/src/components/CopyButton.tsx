import { Check, Copy } from 'lucide-react'
import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'

/**
 * Copies a one-time secret. Invitation links and API tokens are shown exactly
 * once, so this is usually the only chance to capture them.
 */
export function CopyButton({
  value,
  label = 'Copy',
  className,
}: {
  value: string
  label?: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 2000)
    return () => clearTimeout(timer)
  }, [copied])

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
    } catch {
      // Clipboard access is blocked outside a secure context; the value is
      // already selectable on screen, so there is nothing to recover from.
    }
  }

  return (
    <Button type="button" variant="outline" size="sm" className={className} onClick={() => void copy()}>
      {copied ? <Check className="text-success" aria-hidden /> : <Copy aria-hidden />}
      {copied ? 'Copied' : label}
    </Button>
  )
}
