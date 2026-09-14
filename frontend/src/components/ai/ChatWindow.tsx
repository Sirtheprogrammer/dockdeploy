import {
  AlertCircle,
  Bot,
  FileCode2,
  Loader2,
  Plus,
  Rocket,
  RotateCcw,
  Send,
  Server as ServerIcon,
  Sparkles,
  Stethoscope,
  Trash2,
  User,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'
import { toast } from 'sonner'

import { CodeBlock } from '@/components/ai/CodeBlock'
import { SafeguardCard } from '@/components/ai/SafeguardCard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import {
  useAIConversation,
  useAIConversations,
  useAISettings,
  useCreateAIConversation,
  useDeleteAIConversation,
  type AIMessage,
  type SafeguardAction,
} from '@/lib/ai'
import { useDeployments } from '@/lib/deployments'
import { useServers } from '@/lib/servers'
import { cn } from '@/lib/utils'

interface ChatWindowProps {
  initialServerId?: string
  initialDeploymentId?: string
  onClose?: () => void
}

type SnippetPart =
  | { type: 'text'; content: string }
  | { type: 'code'; language: string; code: string }

export function ChatWindow({ initialServerId, initialDeploymentId }: ChatWindowProps) {
  const { data: settings } = useAISettings()
  const { data: conversations = [], refetch: refetchConvs } = useAIConversations()
  const { data: servers = [] } = useServers()
  const { data: deployments = [] } = useDeployments()

  const createConv = useCreateAIConversation()
  const deleteConv = useDeleteAIConversation()

  const [activeConvId, setActiveConvId] = useState<string | null>(null)
  const [prevInitialServer, setPrevInitialServer] = useState(initialServerId)
  const [selectedServerId, setSelectedServerId] = useState<string>(initialServerId || 'all')
  if (initialServerId !== prevInitialServer) {
    setPrevInitialServer(initialServerId)
    setSelectedServerId(initialServerId || 'all')
  }

  const [prevInitialDeploy, setPrevInitialDeploy] = useState(initialDeploymentId)
  const [selectedDeploymentId, setSelectedDeploymentId] = useState<string>(
    initialDeploymentId || 'all',
  )
  if (initialDeploymentId !== prevInitialDeploy) {
    setPrevInitialDeploy(initialDeploymentId)
    setSelectedDeploymentId(initialDeploymentId || 'all')
  }

  const { data: convData } = useAIConversation(activeConvId)

  const [optimisticMessages, setOptimisticMessages] = useState<AIMessage[]>([])
  const messages = useMemo(() => {
    const base = convData?.messages ?? []
    return activeConvId ? [...base, ...optimisticMessages] : optimisticMessages
  }, [activeConvId, convData?.messages, optimisticMessages])

  const [inputPrompt, setInputPrompt] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)
  const [streamingContent, setStreamingContent] = useState('')
  const [pendingAction, setPendingAction] = useState<SafeguardAction | null>(null)

  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [messages, streamingContent, pendingAction])

  async function handleNewChat() {
    try {
      const conv = await createConv.mutateAsync({ title: 'New Assistant Session' })
      setActiveConvId(conv.id)
      setOptimisticMessages([])
      setStreamingContent('')
      setPendingAction(null)
      refetchConvs()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to start chat'
      toast.error(msg)
    }
  }

  async function handleDeleteChat() {
    if (!activeConvId) return
    try {
      await deleteConv.mutateAsync(activeConvId)
      setActiveConvId(null)
      setOptimisticMessages([])
      refetchConvs()
      toast.success('Conversation deleted')
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to delete conversation'
      toast.error(msg)
    }
  }

  async function handleSendMessage(overridePrompt?: string) {
    const textToSend = overridePrompt || inputPrompt
    if (!textToSend.trim() || isStreaming) return

    if (!settings?.has_api_key && settings?.provider !== 'custom') {
      toast.error(
        'Please configure an API key in Settings -> AI Assistant before using the assistant.',
      )
      return
    }

    const optimisticUserMsg: AIMessage = {
      id: `temp-${Date.now()}`,
      conversation_id: activeConvId || '',
      role: 'user',
      content: textToSend.trim(),
      created_at: new Date().toISOString(),
    }

    setOptimisticMessages((prev) => [...prev, optimisticUserMsg])
    setInputPrompt('')
    setIsStreaming(true)
    setStreamingContent('')
    setPendingAction(null)

    try {
      const resp = await fetch('/api/ai/chat', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        credentials: 'same-origin',
        body: JSON.stringify({
          conversation_id: activeConvId || undefined,
          message: textToSend.trim(),
          server_id: selectedServerId !== 'all' ? selectedServerId : undefined,
          deployment_id: selectedDeploymentId !== 'all' ? selectedDeploymentId : undefined,
        }),
      })

      if (!resp.ok) {
        let errMessage = 'Failed to generate response'
        try {
          const errData = await resp.json()
          errMessage = errData.error?.message || errData.message || errMessage
        } catch {
          // ignore non-json error responses
        }
        throw new Error(errMessage)
      }

      const reader = resp.body?.getReader()
      if (!reader) throw new Error('Readable stream not available')

      const decoder = new TextDecoder()
      let accumulatedText = ''
      let detectedAction: SafeguardAction | null = null
      let assignedConvId = activeConvId

      let buffer = ''
      while (true) {
        const { value, done } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() || ''

        let currentEvent = ''
        for (const line of lines) {
          const trimmed = line.trim()
          if (!trimmed) continue

          if (trimmed.startsWith('event:')) {
            currentEvent = trimmed.replace('event:', '').trim()
            continue
          }

          if (trimmed.startsWith('data:')) {
            const dataStr = trimmed.replace('data:', '').trim()
            try {
              const dataObj = JSON.parse(dataStr)

              if (currentEvent === 'conversation' && dataObj.id) {
                assignedConvId = dataObj.id
                if (!activeConvId) {
                  setActiveConvId(dataObj.id)
                  refetchConvs()
                }
              } else if (currentEvent === 'chunk' && dataObj.text) {
                accumulatedText += dataObj.text
                setStreamingContent(accumulatedText)
              } else if (currentEvent === 'action') {
                detectedAction = dataObj as SafeguardAction
                setPendingAction(detectedAction)
              } else if (currentEvent === 'error') {
                toast.error(dataObj.message || 'Stream error')
              }
            } catch {
              // ignore invalid chunk frames
            }
          }
        }
      }

      const finalAssistantMsg: AIMessage = {
        id: `asst-${Date.now()}`,
        conversation_id: assignedConvId || '',
        role: 'assistant',
        content: accumulatedText,
        metadata: detectedAction ? { safeguard_action: detectedAction } : undefined,
        created_at: new Date().toISOString(),
      }

      setOptimisticMessages((prev) => [...prev, finalAssistantMsg])
      setStreamingContent('')
      setPendingAction(null)
      refetchConvs()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Error occurred while communicating with AI Assistant'
      toast.error(msg)
    } finally {
      setIsStreaming(false)
    }
  }

  function renderContentWithSnippets(content: string, actionFromMeta?: SafeguardAction) {
    const safeguardRegex = /```safeguard_action\s*(\{[\s\S]*?\})\s*```/g
    let actionObj = actionFromMeta

    const sanitizedContent = content.replace(safeguardRegex, (_, jsonStr) => {
      try {
        if (!actionObj) {
          actionObj = JSON.parse(jsonStr)
        }
      } catch {
        // ignore malformed safeguard action
      }
      return ''
    })

    const codeBlockRegex = /```(\w+)?\n([\s\S]*?)```/g
    const parts: SnippetPart[] = []
    let lastIdx = 0
    let match: RegExpExecArray | null

    while ((match = codeBlockRegex.exec(sanitizedContent)) !== null) {
      if (match.index > lastIdx) {
        parts.push({
          type: 'text',
          content: sanitizedContent.substring(lastIdx, match.index),
        })
      }
      const code = match[2] ? match[2].trim() : ''
      parts.push({
        type: 'code',
        language: match[1] || 'text',
        code,
      })
      lastIdx = match.index + match[0].length
    }

    if (lastIdx < sanitizedContent.length) {
      parts.push({
        type: 'text',
        content: sanitizedContent.substring(lastIdx),
      })
    }

    return (
      <div className="space-y-2 text-xs leading-relaxed text-zinc-100">
        {parts.map((p, idx) =>
          p.type === 'code' ? (
            <CodeBlock key={idx} language={p.language} code={p.code} />
          ) : (
            <p key={idx} className="whitespace-pre-wrap">
              {p.content}
            </p>
          ),
        )}

        {actionObj && <SafeguardCard action={actionObj} />}
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col bg-background">
      {/* Context & Conversation Bar */}
      <div className="flex flex-col gap-2 border-b bg-muted/30 p-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <Select
              value={activeConvId || 'none'}
              onValueChange={(val) => {
                setOptimisticMessages([])
                if (val === 'none') {
                  setActiveConvId(null)
                } else {
                  setActiveConvId(val)
                }
              }}
            >
              <SelectTrigger className="h-8 max-w-[220px] text-xs">
                <SelectValue placeholder="New Conversation" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">+ New Chat</SelectItem>
                {conversations.map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Button
              variant="outline"
              size="icon"
              className="size-8 shrink-0"
              onClick={handleNewChat}
              title="New Chat Session"
            >
              <Plus className="size-3.5" />
            </Button>
          </div>

          <div className="flex items-center gap-1.5 shrink-0">
            <Link to="/settings/ai">
              <Badge variant="outline" className="gap-1 text-[10px] font-normal cursor-pointer hover:bg-accent transition-colors">
                <Sparkles className="size-3 text-primary" />
                {settings?.provider ? `${settings.provider} (${settings.model})` : 'Configure Provider'}
              </Badge>
            </Link>

            {activeConvId && (
              <Button
                variant="ghost"
                size="icon"
                className="size-8 text-zinc-400 hover:text-rose-400"
                onClick={handleDeleteChat}
                title="Delete this chat"
              >
                <Trash2 className="size-3.5" />
              </Button>
            )}
          </div>
        </div>

        {/* Server & Deployment Context Selectors */}
        <div className="flex items-center gap-2 pt-1">
          <div className="flex flex-1 items-center gap-1 min-w-0">
            <ServerIcon className="text-muted-foreground size-3.5 shrink-0" />
            <Select value={selectedServerId} onValueChange={setSelectedServerId}>
              <SelectTrigger className="h-7 text-[11px]">
                <SelectValue placeholder="All Servers" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Server: None (Global)</SelectItem>
                {servers.map((s) => (
                  <SelectItem key={s.id} value={s.id}>
                    {s.name} ({s.host})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-1 items-center gap-1 min-w-0">
            <Rocket className="text-muted-foreground size-3.5 shrink-0" />
            <Select value={selectedDeploymentId} onValueChange={setSelectedDeploymentId}>
              <SelectTrigger className="h-7 text-[11px]">
                <SelectValue placeholder="All Deployments" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Deploy: None</SelectItem>
                {deployments.map((d) => (
                  <SelectItem key={d.id} value={d.id}>
                    {d.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>

      {/* Messages Scroll Area */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.length === 0 && !streamingContent && (
          <div className="flex flex-col items-center justify-center h-full text-center py-8 px-4">
            <div className="bg-primary/10 text-primary p-3 rounded-2xl mb-3">
              <Sparkles className="size-6" />
            </div>
            <h4 className="font-semibold text-sm tracking-tight">Dockdeploy Multipurpose AI</h4>
            <p className="text-muted-foreground text-xs max-w-sm mt-1 mb-6">
              Diagnose servers, troubleshoot container crash loops, inspect logs, and generate safe Nginx configs with built-in safeguard permissions.
            </p>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 w-full max-w-md text-left">
              <button
                type="button"
                onClick={() =>
                  handleSendMessage(
                    'Run a comprehensive diagnostic check on the active server. Check CPU, memory, disk, and container crash loops.',
                  )
                }
                className="flex items-start gap-2.5 p-2.5 rounded-lg border border-border/70 bg-card hover:bg-accent/70 hover:border-border transition-all text-xs text-muted-foreground hover:text-foreground group"
              >
                <Stethoscope className="size-4 text-emerald-500 shrink-0 mt-0.5" />
                <div>
                  <span className="font-medium text-foreground block">Diagnose Server</span>
                  <span className="text-[11px]">Inspect health & container states</span>
                </div>
              </button>

              <button
                type="button"
                onClick={() =>
                  handleSendMessage(
                    'Inspect the recent logs of failing or restarting containers and identify the root cause.',
                  )
                }
                className="flex items-start gap-2.5 p-2.5 rounded-lg border border-border/70 bg-card hover:bg-accent/70 hover:border-border transition-all text-xs text-muted-foreground hover:text-foreground group"
              >
                <AlertCircle className="size-4 text-amber-500 shrink-0 mt-0.5" />
                <div>
                  <span className="font-medium text-foreground block">Analyze Crash Logs</span>
                  <span className="text-[11px]">Find OOM, exit codes & stacktraces</span>
                </div>
              </button>

              <button
                type="button"
                onClick={() =>
                  handleSendMessage(
                    'Generate an Nginx reverse proxy configuration for domain app.example.com forwarding to port 3000 with WebSocket and SSL Let\'s Encrypt support.',
                  )
                }
                className="flex items-start gap-2.5 p-2.5 rounded-lg border border-border/70 bg-card hover:bg-accent/70 hover:border-border transition-all text-xs text-muted-foreground hover:text-foreground group"
              >
                <FileCode2 className="size-4 text-blue-500 shrink-0 mt-0.5" />
                <div>
                  <span className="font-medium text-foreground block">Generate Nginx Config</span>
                  <span className="text-[11px]">Reverse proxy, WebSocket & SSL</span>
                </div>
              </button>

              <button
                type="button"
                onClick={() =>
                  handleSendMessage(
                    'How do I troubleshoot a 502 Bad Gateway error between Nginx and a Docker container on this server?',
                  )
                }
                className="flex items-start gap-2.5 p-2.5 rounded-lg border border-border/70 bg-card hover:bg-accent/70 hover:border-border transition-all text-xs text-muted-foreground hover:text-foreground group"
              >
                <RotateCcw className="size-4 text-purple-500 shrink-0 mt-0.5" />
                <div>
                  <span className="font-medium text-foreground block">Fix 502 Bad Gateway</span>
                  <span className="text-[11px]">Network ports, sockets & upstreams</span>
                </div>
              </button>
            </div>
          </div>
        )}

        {messages.map((m) => (
          <div
            key={m.id}
            className={cn(
              'flex gap-2.5 max-w-[92%]',
              m.role === 'user' ? 'ml-auto flex-row-reverse' : 'mr-auto',
            )}
          >
            <div
              className={cn(
                'size-7 rounded-lg flex items-center justify-center shrink-0 mt-0.5',
                m.role === 'user'
                  ? 'bg-primary text-primary-foreground'
                  : 'bg-zinc-800 text-primary',
              )}
            >
              {m.role === 'user' ? <User className="size-4" /> : <Bot className="size-4" />}
            </div>

            <div
              className={cn(
                'rounded-xl px-3.5 py-2.5 shadow-sm text-xs',
                m.role === 'user'
                  ? 'bg-primary text-primary-foreground font-normal'
                  : 'bg-card border border-border/60 text-card-foreground',
              )}
            >
              {m.role === 'user' ? (
                <p className="whitespace-pre-wrap">{m.content}</p>
              ) : (
                renderContentWithSnippets(m.content, m.metadata?.safeguard_action)
              )}
            </div>
          </div>
        ))}

        {/* Live Streaming Content */}
        {isStreaming && (
          <div className="flex gap-2.5 max-w-[92%] mr-auto">
            <div className="size-7 rounded-lg bg-zinc-800 text-primary flex items-center justify-center shrink-0 mt-0.5">
              <Loader2 className="size-4 animate-spin" />
            </div>

            <div className="rounded-xl px-3.5 py-2.5 shadow-sm text-xs bg-card border border-border/60 text-card-foreground">
              {streamingContent ? (
                renderContentWithSnippets(streamingContent)
              ) : (
                <div className="flex items-center gap-1.5 text-muted-foreground py-0.5">
                  <span className="size-1.5 rounded-full bg-primary animate-pulse" />
                  <span className="size-1.5 rounded-full bg-primary animate-pulse delay-150" />
                  <span className="size-1.5 rounded-full bg-primary animate-pulse delay-300" />
                  <span className="text-[11px] ml-1">Analyzing system state...</span>
                </div>
              )}

              {pendingAction && <SafeguardCard action={pendingAction} />}
            </div>
          </div>
        )}
      </div>

      {/* Input Area */}
      <div className="border-t bg-card/60 p-3">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            handleSendMessage()
          }}
          className="relative flex items-end gap-2"
        >
          <Textarea
            value={inputPrompt}
            onChange={(e) => setInputPrompt(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                handleSendMessage()
              }
            }}
            placeholder="Ask AI Assistant (e.g. diagnose server, check container logs, generate nginx config)..."
            rows={2}
            className="resize-none pr-12 text-xs min-h-[52px]"
          />
          <Button
            type="submit"
            size="icon"
            disabled={!inputPrompt.trim() || isStreaming}
            className="absolute right-2 bottom-2 size-8 shrink-0"
          >
            {isStreaming ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Send className="size-3.5" />
            )}
          </Button>
        </form>
        <div className="flex items-center justify-between mt-2 text-[10px] text-muted-foreground px-1">
          <span>Press Enter to send, Shift+Enter for newline</span>
          <span className="flex items-center gap-1">
            <span className="size-1.5 rounded-full bg-emerald-500" />
            Safeguards Active
          </span>
        </div>
      </div>
    </div>
  )
}
