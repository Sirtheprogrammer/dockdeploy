import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'

export type AIProvider = 'openai' | 'anthropic' | 'deepseek' | 'openrouter' | 'gemini' | 'custom'

export interface AISettings {
  user_id: string
  provider: AIProvider
  model: string
  base_url: string
  temperature: number
  system_prompt_custom: string
  has_api_key: boolean
  created_at: string
  updated_at: string
}

export interface AIConversation {
  id: string
  user_id: string
  title: string
  created_at: string
  updated_at: string
}

export interface SafeguardAction {
  id: string
  type: string
  title: string
  description: string
  danger_level: 'low' | 'medium' | 'high'
  permission: string
  server_id?: string
  resource_id?: string
  payload?: Record<string, unknown>
}

export interface AIMessage {
  id: string
  conversation_id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  metadata?: {
    server_id?: string
    deployment_id?: string
    container_id?: string
    safeguard_action?: SafeguardAction
  }
  created_at: string
}

export interface ServerDiagnosticReport {
  server_id: string
  server_name: string
  collected_at: string
  health_issues: string[]
  metrics?: {
    health: string
    cpu: { usage_percent: number; cores: number; load1: number; load5: number; load15: number }
    memory: { used_percent: number; used_bytes: number; total_bytes: number; swap_used_percent: number }
    disk: { used_percent: number; used_bytes: number; total_bytes: number }
    docker: { status: string; containers_running: number; containers_stopped: number; containers_total: number }
  }
  containers?: Array<{
    id: string
    name: string
    image: string
    state: string
    status: string
    ports: Array<{ host_port: number; container_port: number }>
  }>
  container_logs?: Record<string, string>
  summary_markdown: string
}

export const AI_PROVIDER_PRESETS: Record<
  AIProvider,
  { name: string; defaultBaseUrl: string; defaultModel: string; models: string[] }
> = {
  openai: {
    name: 'OpenAI',
    defaultBaseUrl: 'https://api.openai.com/v1',
    defaultModel: 'gpt-4o',
    models: ['gpt-4o', 'gpt-4o-mini', 'o3-mini', 'o1', 'gpt-4-turbo'],
  },
  anthropic: {
    name: 'Anthropic Claude',
    defaultBaseUrl: 'https://api.anthropic.com/v1',
    defaultModel: 'claude-3-7-sonnet-20250219',
    models: [
      'claude-3-7-sonnet-20250219',
      'claude-3-5-sonnet-20241022',
      'claude-3-5-haiku-20241022',
      'claude-3-opus-20240229',
    ],
  },
  deepseek: {
    name: 'DeepSeek',
    defaultBaseUrl: 'https://api.deepseek.com/v1',
    defaultModel: 'deepseek-chat',
    models: ['deepseek-chat', 'deepseek-reasoner'],
  },
  openrouter: {
    name: 'OpenRouter',
    defaultBaseUrl: 'https://openrouter.ai/api/v1',
    defaultModel: 'anthropic/claude-3.5-sonnet',
    models: [
      'anthropic/claude-3.5-sonnet',
      'deepseek/deepseek-chat',
      'deepseek/deepseek-r1',
      'meta-llama/llama-3.3-70b-instruct',
      'google/gemini-2.0-flash-exp:free',
    ],
  },
  gemini: {
    name: 'Google Gemini',
    defaultBaseUrl: 'https://generativelanguage.googleapis.com/v1beta/openai',
    defaultModel: 'gemini-2.0-flash',
    models: ['gemini-2.0-flash', 'gemini-1.5-pro', 'gemini-1.5-flash'],
  },
  custom: {
    name: 'Custom (Ollama, LocalAI, vLLM, OpenAI Compatible)',
    defaultBaseUrl: 'http://localhost:11434/v1',
    defaultModel: 'llama3.2',
    models: ['llama3.2', 'mistral', 'qwen2.5-coder', 'custom'],
  },
}

export function useAISettings() {
  return useQuery({
    queryKey: ['ai', 'settings'],
    queryFn: () => api.get<AISettings>('/ai/settings'),
  })
}

export function useUpdateAISettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Partial<AISettings> & { api_key?: string }) =>
      api.put<AISettings>('/ai/settings', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['ai', 'settings'] })
    },
  })
}

export function useTestAIConnection() {
  return useMutation({
    mutationFn: (body: { provider: string; model: string; base_url: string; api_key?: string }) =>
      api.post<{ status: string; message: string; provider: string; model: string }>('/ai/test', body),
  })
}

export function useAIConversations() {
  return useQuery({
    queryKey: ['ai', 'conversations'],
    queryFn: async () => {
      const res = await api.get<{ conversations: AIConversation[] }>('/ai/conversations')
      return res.conversations ?? []
    },
  })
}

export function useAIConversation(conversationId: string | null) {
  return useQuery({
    queryKey: ['ai', 'conversations', conversationId],
    queryFn: () =>
      conversationId
        ? api.get<{ conversation: AIConversation; messages: AIMessage[] }>(
            `/ai/conversations/${conversationId}`,
          )
        : null,
    enabled: Boolean(conversationId),
  })
}

export function useCreateAIConversation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { title: string }) => api.post<AIConversation>('/ai/conversations', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['ai', 'conversations'] })
    },
  })
}

export function useDeleteAIConversation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (conversationId: string) => api.delete(`/ai/conversations/${conversationId}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['ai', 'conversations'] })
    },
  })
}

export function useAIDiagnoseServer(serverId: string | null, containerId?: string) {
  return useQuery({
    queryKey: ['ai', 'diagnose', serverId, containerId],
    queryFn: () =>
      serverId
        ? api.get<ServerDiagnosticReport>(`/ai/diagnose/servers/${serverId}`, {
            query: containerId ? { container_id: containerId } : undefined,
          })
        : null,
    enabled: Boolean(serverId),
  })
}
