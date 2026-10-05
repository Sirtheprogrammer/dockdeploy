import {
  Bot,
  CheckCircle2,
  Cpu,
  Eye,
  EyeOff,
  Laptop,
  Loader2,
  PlayCircle,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Terminal,
} from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { CopyButton } from '@/components/CopyButton'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  AI_PROVIDER_PRESETS,
  isLocalAIProvider,
  useAISettings,
  useDetectLocalAgents,
  useTestAIConnection,
  useUpdateAISettings,
  type AIProvider,
  type AISettings,
  type DetectedAgent,
} from '@/lib/ai'

function AIAssistantSettingsForm({ initialSettings }: { initialSettings: AISettings }) {
  const update = useUpdateAISettings()
  const testConn = useTestAIConnection()
  const { data: detectResult, isFetching: isDetecting, refetch: refetchDetect } = useDetectLocalAgents()

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
  const isLocal = isLocalAIProvider(provider)

  // Determine dynamic models (e.g. for Ollama)
  const detectedOllama = detectResult?.agents.find((a) => a.id === 'ollama')
  const availableModels =
    provider === 'ollama' && detectedOllama?.models && detectedOllama.models.length > 0
      ? detectedOllama.models
      : preset.models

  function handleProviderChange(nextProvider: AIProvider) {
    setProvider(nextProvider)
    const nextPreset = AI_PROVIDER_PRESETS[nextProvider]
    if (nextPreset) {
      if (nextProvider === 'ollama' && detectedOllama?.models && detectedOllama.models[0]) {
        setModel(detectedOllama.models[0])
      } else {
        setModel(nextPreset.defaultModel)
      }
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

  function applyDetectedAgent(agent: DetectedAgent) {
    const provId = agent.id as AIProvider
    setProvider(provId)
    const agentPreset = AI_PROVIDER_PRESETS[provId]
    const chosenModel =
      agent.default_model ||
      (agent.models && agent.models[0]) ||
      (agentPreset ? agentPreset.defaultModel : 'default')

    setModel(chosenModel)
    setIsCustomModel(false)
    setBaseUrl(agent.endpoint || (agentPreset ? agentPreset.defaultBaseUrl : ''))
    setApiKey('')
    setTestResult(null)
    toast.success(`Connected to ${agent.name} with model ${chosenModel}! Zero API key needed.`)
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
    <div className="space-y-8 max-w-4xl min-w-0">
      {/* 1. Auto-Detected Local AI Agents Card */}
      <Card className="border-primary/20 bg-primary/[0.02]">
        <CardHeader className="pb-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Laptop className="text-primary size-5" />
              <CardTitle className="text-base sm:text-lg">Local AI Agents & Models (Auto-Detected)</CardTitle>
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => refetchDetect()}
              disabled={isDetecting}
              className="h-8 gap-1.5 text-xs"
            >
              <RefreshCw className={`size-3.5 ${isDetecting ? 'animate-spin' : ''}`} />
              Rescan System
            </Button>
          </div>
          <CardDescription className="text-xs sm:text-sm">
            dockdeploy automatically scans your local environment for running LLMs and installed agent CLIs so you can run the agent flow without needing external API keys.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {detectResult?.agents.map((agent) => {
              const isSelected = provider === agent.id
              const isAvailable = agent.available

              return (
                <div
                  key={agent.id}
                  className={`flex flex-col justify-between rounded-lg border p-3.5 transition-all ${
                    isSelected
                      ? 'border-primary bg-primary/[0.05] ring-1 ring-primary'
                      : isAvailable
                        ? 'border-border bg-card hover:border-border/80'
                        : 'border-border/50 bg-muted/20 opacity-60'
                  }`}
                >
                  <div className="space-y-2">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2 font-medium text-sm">
                        {agent.type === 'cli_agent' ? (
                          <Terminal className="size-4 text-primary shrink-0" />
                        ) : (
                          <Cpu className="size-4 text-emerald-500 shrink-0" />
                        )}
                        <span className="truncate">{agent.name}</span>
                      </div>
                      {isAvailable ? (
                        <Badge
                          variant="outline"
                          className="bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20 text-[10px] px-1.5 py-0 shrink-0"
                        >
                          {agent.status === 'online' ? 'Online' : 'Ready'}
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="text-muted-foreground text-[10px] px-1.5 py-0 shrink-0">
                          Not found
                        </Badge>
                      )}
                    </div>

                    <p className="text-muted-foreground text-xs line-clamp-2 leading-relaxed">
                      {agent.description}
                    </p>

                    {agent.models && agent.models.length > 0 && (
                      <div className="flex flex-wrap gap-1 pt-1">
                        {agent.models.slice(0, 3).map((m) => (
                          <span
                            key={m}
                            className="bg-muted px-1.5 py-0.5 rounded text-[10px] font-mono text-muted-foreground"
                          >
                            {m}
                          </span>
                        ))}
                        {agent.models.length > 3 && (
                          <span className="text-[10px] text-muted-foreground self-center">
                            +{agent.models.length - 3} more
                          </span>
                        )}
                      </div>
                    )}
                  </div>

                  <div className="pt-3">
                    <Button
                      type="button"
                      size="sm"
                      variant={isSelected ? 'default' : 'outline'}
                      className="w-full h-8 text-xs font-medium gap-1.5"
                      disabled={!isAvailable}
                      onClick={() => applyDetectedAgent(agent)}
                    >
                      {isSelected ? (
                        <>
                          <CheckCircle2 className="size-3.5" />
                          Active Agent
                        </>
                      ) : isAvailable ? (
                        'Use This Agent'
                      ) : (
                        'Unavailable'
                      )}
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        </CardContent>
      </Card>

      {/* 2. Provider Settings Form */}
      <form onSubmit={handleSubmit} className="space-y-6">
        <Card>
          <CardHeader>
            <div className="flex flex-wrap sm:flex-nowrap items-start sm:items-center justify-between gap-2">
              <div className="flex items-center gap-2 min-w-0">
                <Sparkles className="text-primary size-5 shrink-0" />
                <CardTitle className="text-base sm:text-lg">LLM Provider & Model Configuration</CardTitle>
              </div>
              <Badge variant="outline" className="gap-1.5 font-normal text-xs shrink-0">
                <ShieldCheck className="text-emerald-500 size-3.5" />
                AES-256-GCM Encrypted
              </Badge>
            </div>
            <CardDescription className="text-xs sm:text-sm">
              Configure your local or cloud LLM. Local agents run with zero external API key dependencies.
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
                    <div className="px-2 py-1.5 text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
                      Local Agents & Models (Zero API Key)
                    </div>
                    <SelectItem value="ollama">Ollama (Local LLM Daemon)</SelectItem>
                    <SelectItem value="antigravity">Antigravity CLI (agy)</SelectItem>
                    <SelectItem value="claude-code">Claude Code CLI (Local)</SelectItem>
                    <SelectItem value="hermes">Hermes Agent CLI (Local)</SelectItem>
                    <SelectItem value="copilot">GitHub Copilot (Local)</SelectItem>
                    <SelectItem value="lmstudio">LM Studio (Local Server)</SelectItem>
                    <SelectItem value="custom">Custom (LocalAI / vLLM / OpenAI Compatible)</SelectItem>

                    <div className="px-2 py-1.5 text-[11px] font-semibold text-muted-foreground uppercase tracking-wider mt-2 border-t pt-2">
                      Cloud Providers (API Key Required)
                    </div>
                    <SelectItem value="openai">OpenAI (Cloud)</SelectItem>
                    <SelectItem value="anthropic">Anthropic Claude (Cloud API)</SelectItem>
                    <SelectItem value="deepseek">DeepSeek (Cloud)</SelectItem>
                    <SelectItem value="openrouter">OpenRouter API (Cloud)</SelectItem>
                    <SelectItem value="gemini">Google Gemini (Cloud)</SelectItem>
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
                    {availableModels.map((m) => (
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
                  placeholder="e.g. meta-llama/llama-3-70b or gemma3:1b"
                  value={customModelName}
                  onChange={(e) => setCustomModelName(e.target.value)}
                  required
                />
              </div>
            )}

            <div className="space-y-2">
              <div className="flex flex-wrap items-center justify-between gap-1">
                <Label htmlFor="baseUrl">API Base URL / Command Mode</Label>
                <span className="text-muted-foreground text-[11px] sm:text-xs font-mono truncate max-w-full">
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
                {isLocal
                  ? 'Local runtime endpoint or direct CLI execution target.'
                  : 'Leave empty to use official provider cloud API, or customize for enterprise proxies.'}
              </p>
            </div>

            <div className="space-y-2">
              <div className="flex flex-wrap items-center justify-between gap-1.5">
                <Label htmlFor="apiKey">
                  API Key / Token {isLocal && <span className="text-muted-foreground font-normal">(Optional for Local Agents)</span>}
                </Label>
                {initialSettings.has_api_key && (
                  <Badge variant="success" className="gap-1 text-xs shrink-0">
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
                    isLocal
                      ? 'Not required for local agents & models (leave blank)'
                      : initialSettings.has_api_key
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
                {isLocal
                  ? 'Zero API keys required when running with detected local agents or Ollama.'
                  : 'Your cloud API key is sealed with AES-256-GCM before storage and never returned to the client.'}
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

        <div className="flex flex-col-reverse sm:flex-row sm:items-center sm:justify-between gap-3 border-t pt-4">
          <Button
            type="button"
            variant="outline"
            onClick={handleTest}
            disabled={testConn.isPending}
            className="w-full sm:w-auto gap-2"
          >
            {testConn.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <PlayCircle className="size-4" />
            )}
            Test Connection
          </Button>

          <Button type="submit" disabled={update.isPending} className="w-full sm:w-auto gap-2">
            {update.isPending && <Loader2 className="size-4 animate-spin" />}
            Save Settings
          </Button>
        </div>
      </form>

      {/* 3. Connect External Agents to Dockdeploy (MCP) */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Terminal className="text-primary size-5" />
            <CardTitle className="text-base sm:text-lg">Connect External Agents to Dockdeploy (MCP)</CardTitle>
          </div>
          <CardDescription className="text-xs sm:text-sm">
            Your local agents (Claude Code, Antigravity, Hermes, Cursor, Copilot) can connect directly to this dockdeploy instance via the open Model Context Protocol (MCP) to inspect servers, check container logs, and trigger deployment flows.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="claude" className="space-y-4">
            <TabsList>
              <TabsTrigger value="claude">Claude Code</TabsTrigger>
              <TabsTrigger value="hermes">Hermes Agent</TabsTrigger>
              <TabsTrigger value="antigravity">Antigravity (agy)</TabsTrigger>
              <TabsTrigger value="cursor">Cursor / VS Code</TabsTrigger>
              <TabsTrigger value="http">HTTP Agent API</TabsTrigger>
            </TabsList>

            <TabsContent value="claude" className="space-y-2">
              <p className="text-xs text-muted-foreground">
                Add dockdeploy as an MCP tool server in Claude Code CLI:
              </p>
              <div className="flex items-center gap-2 bg-muted p-2.5 rounded-md font-mono text-xs overflow-x-auto">
                <span className="flex-1 select-all">claude mcp add dockdeploy -- ./dockdeploy mcp</span>
                <CopyButton value="claude mcp add dockdeploy -- ./dockdeploy mcp" />
              </div>
            </TabsContent>

            <TabsContent value="hermes" className="space-y-2">
              <p className="text-xs text-muted-foreground">
                Connect Hermes Agent to dockdeploy using stdio MCP:
              </p>
              <div className="flex items-center gap-2 bg-muted p-2.5 rounded-md font-mono text-xs overflow-x-auto">
                <span className="flex-1 select-all">hermes mcp add dockdeploy -- ./dockdeploy mcp</span>
                <CopyButton value="hermes mcp add dockdeploy -- ./dockdeploy mcp" />
              </div>
            </TabsContent>

            <TabsContent value="antigravity" className="space-y-2">
              <p className="text-xs text-muted-foreground">
                Add to your Antigravity configuration in <code className="bg-muted px-1 py-0.5 rounded">~/.gemini/antigravity-cli/settings.json</code>:
              </p>
              <div className="relative bg-muted p-2.5 rounded-md font-mono text-xs overflow-x-auto">
                <pre>{`{
  "mcpServers": {
    "dockdeploy": {
      "command": "./dockdeploy",
      "args": ["mcp"]
    }
  }
}`}</pre>
                <div className="absolute top-2 right-2">
                  <CopyButton
                    value={`{
  "mcpServers": {
    "dockdeploy": {
      "command": "./dockdeploy",
      "args": ["mcp"]
    }
  }
}`}
                  />
                </div>
              </div>
            </TabsContent>

            <TabsContent value="cursor" className="space-y-2">
              <p className="text-xs text-muted-foreground">
                Add to <code className="bg-muted px-1 py-0.5 rounded">.cursor/mcp.json</code> or VS Code MCP settings:
              </p>
              <div className="relative bg-muted p-2.5 rounded-md font-mono text-xs overflow-x-auto">
                <pre>{`{
  "mcpServers": {
    "dockdeploy": {
      "command": "./dockdeploy",
      "args": ["mcp"]
    }
  }
}`}</pre>
                <div className="absolute top-2 right-2">
                  <CopyButton
                    value={`{
  "mcpServers": {
    "dockdeploy": {
      "command": "./dockdeploy",
      "args": ["mcp"]
    }
  }
}`}
                  />
                </div>
              </div>
            </TabsContent>

            <TabsContent value="http" className="space-y-2">
              <p className="text-xs text-muted-foreground">
                Connect external agents over HTTP to dockdeploy's MCP endpoint:
              </p>
              <div className="flex items-center gap-2 bg-muted p-2.5 rounded-md font-mono text-xs overflow-x-auto">
                <span className="flex-1 select-all">curl -X POST http://localhost:8081/api/mcp -H &quot;Authorization: Bearer &lt;API_TOKEN&gt;&quot;</span>
                <CopyButton value="curl -X POST http://localhost:8081/api/mcp -H 'Authorization: Bearer <API_TOKEN>'" />
              </div>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </div>
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
        <h3 className="text-lg font-medium tracking-tight">AI Assistant & Local Agent Configuration</h3>
        <p className="text-muted-foreground text-sm">
          Run your DevOps agent flow with detected local agents (Ollama, Antigravity, Claude Code, Hermes, Copilot) with zero API keys, or connect external models directly to dockdeploy.
        </p>
      </div>

      <AIAssistantSettingsForm key={settings.updated_at || 'ready'} initialSettings={settings} />
    </div>
  )
}
