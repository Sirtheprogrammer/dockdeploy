import { Bot, CheckCircle2, Eye, EyeOff, Loader2, PlayCircle, ShieldCheck, Sparkles } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import {
  AI_PROVIDER_PRESETS,
  useAISettings,
  useTestAIConnection,
  useUpdateAISettings,
  type AIProvider,
  type AISettings,
} from '@/lib/ai'

function AIAssistantSettingsForm({ initialSettings }: { initialSettings: AISettings }) {
  const update = useUpdateAISettings()
  const testConn = useTestAIConnection()

  const [provider, setProvider] = useState<AIProvider>(initialSettings.provider || 'openai')

  const initialPreset = AI_PROVIDER_PRESETS[initialSettings.provider || 'openai']
  const isModelInPreset = initialPreset && initialPreset.models.includes(initialSettings.model)

  const [model, setModel] = useState(isModelInPreset ? initialSettings.model : 'custom')
  const [isCustomModel, setIsCustomModel] = useState(!isModelInPreset)
  const [customModelName, setCustomModelName] = useState(!isModelInPreset ? initialSettings.model : '')
  const [baseUrl, setBaseUrl] = useState(initialSettings.base_url || '')
  const [apiKey, setApiKey] = useState('')
  const [showApiKey, setShowApiKey] = useState(false)
  const [temperature, setTemperature] = useState(initialSettings.temperature ?? 0.7)
  const [systemPrompt, setSystemPrompt] = useState(initialSettings.system_prompt_custom || '')
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null)

  const preset = AI_PROVIDER_PRESETS[provider] || AI_PROVIDER_PRESETS.openai

  function handleProviderChange(nextProvider: AIProvider) {
    setProvider(nextProvider)
    const nextPreset = AI_PROVIDER_PRESETS[nextProvider]
    if (nextPreset) {
      setModel(nextPreset.defaultModel)
      setIsCustomModel(false)
      setBaseUrl(nextPreset.defaultBaseUrl)
    }
    setTestResult(null)
  }

  function handleModelChange(selected: string) {
    if (selected === 'custom') {
      setIsCustomModel(true)
      setModel('custom')
    } else {
      setIsCustomModel(false)
      setModel(selected)
    }
    setTestResult(null)
  }

  async function handleTest(event: React.MouseEvent) {
    event.preventDefault()
    setTestResult(null)
    const activeModel = isCustomModel ? customModelName.trim() : model

    try {
      const res = await testConn.mutateAsync({
        provider,
        model: activeModel,
        base_url: baseUrl.trim(),
        api_key: apiKey.trim() || undefined,
      })
      setTestResult({ success: true, message: res.message || 'Connection successful!' })
      toast.success('Connection verified successfully!')
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to connect to provider.'
      setTestResult({ success: false, message: msg })
      toast.error(msg)
    }
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    const activeModel = isCustomModel ? customModelName.trim() : model
    if (!activeModel) {
      toast.error('Please specify a model name.')
      return
    }

    try {
      await update.mutateAsync({
        provider,
        model: activeModel,
        base_url: baseUrl.trim(),
        api_key: apiKey.trim() || undefined,
        temperature: Number(temperature),
        system_prompt_custom: systemPrompt.trim(),
      })
      setApiKey('')
      toast.success('AI Assistant settings saved successfully.')
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to update settings.'
      toast.error(msg)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Sparkles className="text-primary size-5" />
              <CardTitle>LLM Provider & Model</CardTitle>
            </div>
            <Badge variant="outline" className="gap-1.5 font-normal">
              <ShieldCheck className="text-emerald-500 size-3.5" />
              AES-256-GCM Encrypted
            </Badge>
          </div>
          <CardDescription>
            Choose OpenAI, Anthropic Claude, DeepSeek, OpenRouter, Google Gemini, or an OpenAI-compatible endpoint.
          </CardDescription>
        </CardHeader>

        <CardContent className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="provider">Provider</Label>
              <Select value={provider} onValueChange={(val) => handleProviderChange(val as AIProvider)}>
                <SelectTrigger id="provider" className="w-full">
                  <SelectValue placeholder="Select provider" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="openai">OpenAI (Official)</SelectItem>
                  <SelectItem value="anthropic">Anthropic Claude</SelectItem>
                  <SelectItem value="deepseek">DeepSeek</SelectItem>
                  <SelectItem value="openrouter">OpenRouter API</SelectItem>
                  <SelectItem value="gemini">Google Gemini</SelectItem>
                  <SelectItem value="custom">Custom (Ollama / LocalAI / vLLM)</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="model">Model</Label>
              <Select value={model} onValueChange={handleModelChange}>
                <SelectTrigger id="model" className="w-full">
                  <SelectValue placeholder="Select model" />
                </SelectTrigger>
                <SelectContent>
                  {preset.models.map((m) => (
                    <SelectItem key={m} value={m}>
                      {m}
                    </SelectItem>
                  ))}
                  <SelectItem value="custom">Other / Custom Model...</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          {isCustomModel && (
            <div className="space-y-2 animate-in fade-in">
              <Label htmlFor="customModel">Custom Model Name</Label>
              <Input
                id="customModel"
                placeholder="e.g. meta-llama/llama-3-70b or claude-3-5-sonnet"
                value={customModelName}
                onChange={(e) => setCustomModelName(e.target.value)}
                required
              />
            </div>
          )}

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label htmlFor="baseUrl">API Base URL</Label>
              <span className="text-muted-foreground text-xs">
                Default: {preset.defaultBaseUrl}
              </span>
            </div>
            <Input
              id="baseUrl"
              placeholder={preset.defaultBaseUrl}
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
            />
            <p className="text-muted-foreground text-xs">
              Leave empty to use the official endpoint, or customize for enterprise proxies and local models.
            </p>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label htmlFor="apiKey">API Key / Token</Label>
              {initialSettings.has_api_key && (
                <Badge variant="success" className="gap-1 text-xs">
                  <CheckCircle2 className="size-3" />
                  Key is securely stored
                </Badge>
              )}
            </div>
            <div className="relative">
              <Input
                id="apiKey"
                type={showApiKey ? 'text' : 'password'}
                placeholder={
                  initialSettings.has_api_key
                    ? '•••••••••••••••• (Leave blank to keep existing key)'
                    : 'sk-...'
                }
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                className="pr-10"
              />
              <button
                type="button"
                onClick={() => setShowApiKey(!showApiKey)}
                className="text-muted-foreground hover:text-foreground absolute top-1/2 right-3 -translate-y-1/2 p-0.5"
                tabIndex={-1}
              >
                {showApiKey ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
              </button>
            </div>
            <p className="text-muted-foreground text-xs">
              Your key is sealed with AES-256-GCM before storage and is never exposed to the client.
            </p>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label htmlFor="temp">Temperature: {temperature}</Label>
              <span className="text-muted-foreground text-xs">
                {temperature <= 0.3 ? 'Deterministic' : temperature >= 0.9 ? 'Creative' : 'Balanced'}
              </span>
            </div>
            <input
              id="temp"
              type="range"
              min="0"
              max="1.5"
              step="0.05"
              value={temperature}
              onChange={(e) => setTemperature(parseFloat(e.target.value))}
              className="accent-primary w-full cursor-pointer"
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Bot className="text-primary size-5" />
            <CardTitle>Custom System Guidelines</CardTitle>
          </div>
          <CardDescription>
            Provide specific instructions, default domain naming rules, or server management conventions.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Textarea
            rows={4}
            placeholder="e.g. Always generate Nginx configurations with strict security headers and WebSocket support. Prefer upstream port 8080."
            value={systemPrompt}
            onChange={(e) => setSystemPrompt(e.target.value)}
          />
        </CardContent>
      </Card>

      {testResult && (
        <Alert variant={testResult.success ? 'info' : 'danger'} className="animate-in fade-in flex items-start gap-2.5">
          {testResult.success ? (
            <CheckCircle2 className="text-emerald-500 size-4.5 shrink-0 mt-0.5" />
          ) : (
            <ShieldCheck className="text-destructive size-4.5 shrink-0 mt-0.5" />
          )}
          <div className="min-w-0">
            <div className="font-semibold text-xs tracking-tight">
              {testResult.success ? 'Test Successful' : 'Connection Test Failed'}
            </div>
            <div className="text-xs text-muted-foreground mt-0.5">{testResult.message}</div>
          </div>
        </Alert>
      )}

      <div className="flex items-center justify-between gap-3 border-t pt-4">
        <Button
          type="button"
          variant="outline"
          onClick={handleTest}
          disabled={testConn.isPending}
          className="gap-2"
        >
          {testConn.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <PlayCircle className="size-4" />
          )}
          Test Connection
        </Button>

        <Button type="submit" disabled={update.isPending} className="gap-2">
          {update.isPending && <Loader2 className="size-4 animate-spin" />}
          Save Settings
        </Button>
      </div>
    </form>
  )
}

export function AIAssistantSettings() {
  const { data: settings, isLoading } = useAISettings()

  if (isLoading || !settings) {
    return (
      <div className="flex h-48 items-center justify-center">
        <Loader2 className="text-primary size-6 animate-spin" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-lg font-medium tracking-tight">AI Assistant Configuration</h3>
        <p className="text-muted-foreground text-sm">
          Connect your choice of LLM provider to power automated server diagnostics, log troubleshooting,
          and Nginx configuration generation with strict safeguard permissions.
        </p>
      </div>

      <AIAssistantSettingsForm key={settings.updated_at || 'ready'} initialSettings={settings} />
    </div>
  )
}
