import { marked, type Token, type Tokens } from 'marked'
import React, { useMemo } from 'react'

import { CodeBlock } from '@/components/ai/CodeBlock'
import { SafeguardCard } from '@/components/ai/SafeguardCard'
import type { SafeguardAction } from '@/lib/ai'
import { cn } from '@/lib/utils'

interface MarkdownRendererProps {
  content: string
  actionFromMeta?: SafeguardAction
  className?: string
}

function renderInlineToken(token: Token, idx: number): React.ReactNode {
  switch (token.type) {
    case 'strong': {
      const strongToken = token as Tokens.Strong
      return (
        <strong key={idx} className="font-semibold text-foreground">
          {strongToken.tokens?.map((t, i) => renderInlineToken(t, i)) ?? strongToken.text}
        </strong>
      )
    }
    case 'em': {
      const emToken = token as Tokens.Em
      return (
        <em key={idx} className="italic text-foreground/90">
          {emToken.tokens?.map((t, i) => renderInlineToken(t, i)) ?? emToken.text}
        </em>
      )
    }
    case 'codespan': {
      const codeToken = token as Tokens.Codespan
      return (
        <code
          key={idx}
          className="rounded bg-muted/90 px-1.5 py-0.5 font-mono text-[11px] text-primary border border-border/50 mx-0.5 inline-block"
        >
          {codeToken.text}
        </code>
      )
    }
    case 'link': {
      const linkToken = token as Tokens.Link
      return (
        <a
          key={idx}
          href={linkToken.href}
          target="_blank"
          rel="noopener noreferrer"
          className="text-primary hover:underline underline-offset-2 break-all"
        >
          {linkToken.tokens?.map((t, i) => renderInlineToken(t, i)) ?? linkToken.text}
        </a>
      )
    }
    case 'text':
    default:
      return (token as Tokens.Text).text || token.raw
  }
}

function renderBlockToken(token: Token, idx: number): React.ReactNode {
  switch (token.type) {
    case 'code': {
      const codeToken = token as Tokens.Code
      const lang = codeToken.lang?.toLowerCase().trim() || 'text'

      // Check for safeguard actions embedded in markdown code blocks
      if (lang === 'safeguard_action') {
        try {
          const actionObj = JSON.parse(codeToken.text) as SafeguardAction
          return <SafeguardCard key={idx} action={actionObj} />
        } catch {
          // If malformed, fallback to regular code block
        }
      }

      return (
        <CodeBlock
          key={idx}
          language={lang}
          code={codeToken.text}
          className="my-2"
        />
      )
    }

    case 'heading': {
      const heading = token as Tokens.Heading
      const inline = heading.tokens?.map((t, i) => renderInlineToken(t, i)) ?? heading.text
      switch (heading.depth) {
        case 1:
          return (
            <h1 key={idx} className="text-sm sm:text-base font-bold text-foreground mt-3 mb-1 tracking-tight">
              {inline}
            </h1>
          )
        case 2:
          return (
            <h2 key={idx} className="text-xs sm:text-sm font-semibold text-foreground mt-2.5 mb-1 tracking-tight">
              {inline}
            </h2>
          )
        case 3:
          return (
            <h3 key={idx} className="text-xs font-semibold text-foreground mt-2 mb-0.5">
              {inline}
            </h3>
          )
        default:
          return (
            <h4 key={idx} className="text-xs font-medium text-foreground/90 mt-1.5 mb-0.5">
              {inline}
            </h4>
          )
      }
    }

    case 'paragraph': {
      const p = token as Tokens.Paragraph
      return (
        <p key={idx} className="leading-relaxed my-1.5 whitespace-pre-wrap">
          {p.tokens?.map((t, i) => renderInlineToken(t, i)) ?? p.text}
        </p>
      )
    }

    case 'list': {
      const list = token as Tokens.List
      const ListTag = list.ordered ? 'ol' : 'ul'
      return (
        <ListTag
          key={idx}
          className={cn(
            'space-y-1 my-1.5 pl-5 text-xs',
            list.ordered ? 'list-decimal' : 'list-disc',
          )}
        >
          {list.items.map((item, itemIdx) => (
            <li key={itemIdx} className="leading-relaxed">
              {item.tokens?.map((t, i) => {
                if (t.type === 'text') {
                  return renderInlineToken(t, i)
                }
                return renderBlockToken(t, i)
              }) ?? item.text}
            </li>
          ))}
        </ListTag>
      )
    }

    case 'blockquote': {
      const bq = token as Tokens.Blockquote
      return (
        <blockquote
          key={idx}
          className="border-l-2 border-primary/70 bg-muted/40 pl-3 py-1.5 my-2 text-zinc-300 italic rounded-r text-xs"
        >
          {bq.tokens?.map((t, i) => renderBlockToken(t, i)) ?? bq.text}
        </blockquote>
      )
    }

    case 'table': {
      const table = token as Tokens.Table
      return (
        <div key={idx} className="w-full overflow-x-auto my-2.5 rounded-lg border border-border/80">
          <table className="w-full text-xs text-left border-collapse">
            <thead className="bg-muted/70 border-b border-border/80 text-foreground font-semibold">
              <tr>
                {table.header.map((col, colIdx) => (
                  <th key={colIdx} className="px-3 py-2 text-xs">
                    {col.tokens?.map((t, i) => renderInlineToken(t, i)) ?? col.text}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-border/40 bg-card/40">
              {table.rows.map((row, rowIdx) => (
                <tr key={rowIdx} className="hover:bg-muted/30 transition-colors">
                  {row.map((cell, cellIdx) => (
                    <td key={cellIdx} className="px-3 py-1.5 text-zinc-200">
                      {cell.tokens?.map((t, i) => renderInlineToken(t, i)) ?? cell.text}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    }

    case 'hr':
      return <hr key={idx} className="my-3 border-border/60" />

    case 'space':
      return null

    default:
      return (
        <div key={idx} className="my-1">
          {token.raw}
        </div>
      )
  }
}

export function MarkdownRenderer({
  content,
  actionFromMeta,
  className,
}: MarkdownRendererProps) {
  const tokens = useMemo(() => {
    if (!content) return []
    try {
      return marked.lexer(content)
    } catch {
      return []
    }
  }, [content])

  return (
    <div className={cn('space-y-1 text-xs leading-relaxed text-zinc-100', className)}>
      {tokens.length === 0 && content ? (
        <p className="whitespace-pre-wrap">{content}</p>
      ) : (
        tokens.map((token, idx) => renderBlockToken(token, idx))
      )}

      {/* If safeguard action was passed via metadata and not embedded in code block */}
      {actionFromMeta && (
        <div className="mt-3">
          <SafeguardCard action={actionFromMeta} />
        </div>
      )}
    </div>
  )
}
