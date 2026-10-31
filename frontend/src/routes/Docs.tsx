import {
  Bot,
  Check,
  ChevronDown,
  ChevronRight,
  Code2,
  Copy,
  Globe,
  Key,
  Network,
  Search,
  Terminal,
} from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'

import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

function CodeSnippet({ code, language = 'bash' }: { code: string; language?: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // fallback
    }
  }

  return (
    <div className="relative my-3 rounded-lg border border-border/80 bg-zinc-950/80 font-mono text-xs shadow-inner">
      <div className="flex items-center justify-between border-b border-border/40 px-3 py-1.5 text-[11px] text-zinc-400">
        <span className="uppercase tracking-wider">{language}</span>
        <button
          type="button"
          onClick={handleCopy}
          className="flex items-center gap-1.5 rounded px-2 py-0.5 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
        >
          {copied ? (
            <>
              <Check className="size-3 text-emerald-400" />
              <span className="text-emerald-400">Copied</span>
            </>
          ) : (
            <>
              <Copy className="size-3" />
              <span>Copy</span>
            </>
          )}
        </button>
      </div>
      <div className="overflow-x-auto p-3 text-zinc-200 leading-relaxed">
        <pre>{code}</pre>
      </div>
    </div>
  )
}

interface MCPToolDoc {
  name: string
  category: 'Execution' | 'Containers' | 'Files' | 'Servers' | 'Deployments'
  description: string
  params: { name: string; type: string; required: boolean; description: string }[]
  example: string
}

const MCP_TOOLS: MCPToolDoc[] = [
  {
    name: 'dockdeploy_exec_command',
    category: 'Execution',
    description: 'Execute arbitrary shell commands directly on a managed server host (or local server). Supports working directory, timeouts, and elevated sudo execution.',
    params: [
      { name: 'command', type: 'string', required: true, description: 'Shell command line to execute (e.g. "df -h", "uptime", "ps aux")' },
      { name: 'server_id', type: 'string', required: false, description: 'Server ID (defaults to local or first server)' },
      { name: 'sudo', type: 'boolean', required: false, description: 'Run elevated with root / sudo permissions' },
      { name: 'workdir', type: 'string', required: false, description: 'Working directory path on the server' },
      { name: 'timeout_seconds', type: 'integer', required: false, description: 'Execution timeout in seconds (default 60, max 600)' },
    ],
    example: `{\n  "name": "dockdeploy_exec_command",\n  "arguments": {\n    "command": "uname -a && df -h /",\n    "sudo": false\n  }\n}`,
  },
  {
    name: 'dockdeploy_docker_exec',
    category: 'Execution',
    description: 'Execute a shell command inside a running Docker container on the server.',
    params: [
      { name: 'container_id', type: 'string', required: true, description: 'Container ID or container name' },
      { name: 'command', type: 'string', required: true, description: 'Command to run inside the container' },
      { name: 'user', type: 'string', required: false, description: 'User or UID to execute as inside container (e.g. "root")' },
      { name: 'workdir', type: 'string', required: false, description: 'Working directory inside the container' },
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
      { name: 'sudo', type: 'boolean', required: false, description: 'Run docker exec with host sudo elevation' },
    ],
    example: `{\n  "name": "dockdeploy_docker_exec",\n  "arguments": {\n    "container_id": "api-service",\n    "command": "env && cat /app/config.json"\n  }\n}`,
  },
  {
    name: 'dockdeploy_read_file',
    category: 'Files',
    description: 'Read the contents and metadata of a file on the server filesystem.',
    params: [
      { name: 'path', type: 'string', required: true, description: 'Absolute file path to read on the server' },
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
      { name: 'max_bytes', type: 'integer', required: false, description: 'Maximum bytes to read (default 65536)' },
    ],
    example: `{\n  "name": "dockdeploy_read_file",\n  "arguments": {\n    "path": "/etc/nginx/sites-available/default"\n  }\n}`,
  },
  {
    name: 'dockdeploy_write_file',
    category: 'Files',
    description: 'Create or update file contents on the server filesystem.',
    params: [
      { name: 'path', type: 'string', required: true, description: 'Absolute file path to write on the server' },
      { name: 'content', type: 'string', required: true, description: 'Full text content to write into the file' },
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
      { name: 'sudo_password', type: 'string', required: false, description: 'Sudo password if writing to root-protected locations' },
    ],
    example: `{\n  "name": "dockdeploy_write_file",\n  "arguments": {\n    "path": "/etc/environment",\n    "content": "NODE_ENV=production\\n"\n  }\n}`,
  },
  {
    name: 'dockdeploy_list_files',
    category: 'Files',
    description: 'List directory entries on the server with file sizes, permissions, and modification times.',
    params: [
      { name: 'path', type: 'string', required: true, description: 'Directory path to list (e.g. "/var/log" or "/var/lib/docker")' },
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
    ],
    example: `{\n  "name": "dockdeploy_list_files",\n  "arguments": {\n    "path": "/var/log"\n  }\n}`,
  },
  {
    name: 'dockdeploy_list_servers',
    category: 'Servers',
    description: 'List all servers registered in dockdeploy with their IP addresses, connection mode, and online/offline status.',
    params: [],
    example: `{\n  "name": "dockdeploy_list_servers",\n  "arguments": {}\n}`,
  },
  {
    name: 'dockdeploy_get_server',
    category: 'Servers',
    description: 'Retrieve detailed specs for a server: OS, kernel, Docker version, Compose version, Sudo mode, and warnings.',
    params: [
      { name: 'server_id', type: 'string', required: false, description: 'Server ID (defaults to local/first)' },
    ],
    example: `{\n  "name": "dockdeploy_get_server",\n  "arguments": {}\n}`,
  },
  {
    name: 'dockdeploy_server_diagnostics',
    category: 'Servers',
    description: 'Collect complete system metrics and health diagnostics (CPU, RAM, disk usage, Docker daemon status, container health).',
    params: [
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
    ],
    example: `{\n  "name": "dockdeploy_server_diagnostics",\n  "arguments": {}\n}`,
  },
  {
    name: 'dockdeploy_list_containers',
    category: 'Containers',
    description: 'List all running and stopped Docker containers with their states, images, and port mappings.',
    params: [
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
      { name: 'all', type: 'boolean', required: false, description: 'Include stopped containers (default: true)' },
    ],
    example: `{\n  "name": "dockdeploy_list_containers",\n  "arguments": {\n    "all": true\n  }\n}`,
  },
  {
    name: 'dockdeploy_inspect_container',
    category: 'Containers',
    description: 'Inspect full container metadata: state, mounts, network bindings, IP, environment variables, and restart policy.',
    params: [
      { name: 'container_id', type: 'string', required: true, description: 'Container ID or container name' },
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
    ],
    example: `{\n  "name": "dockdeploy_inspect_container",\n  "arguments": {\n    "container_id": "redis"\n  }\n}`,
  },
  {
    name: 'dockdeploy_container_action',
    category: 'Containers',
    description: 'Perform lifecycle actions on a Docker container: start, stop, restart, pause, or unpause.',
    params: [
      { name: 'container_id', type: 'string', required: true, description: 'Container ID or container name' },
      { name: 'action', type: 'string', required: true, description: 'One of: start, stop, restart, pause, unpause' },
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
    ],
    example: `{\n  "name": "dockdeploy_container_action",\n  "arguments": {\n    "container_id": "web-service",\n    "action": "restart"\n  }\n}`,
  },
  {
    name: 'dockdeploy_get_container_logs',
    category: 'Containers',
    description: 'Retrieve recent logs from a container stdout and stderr.',
    params: [
      { name: 'container_id', type: 'string', required: true, description: 'Container ID or container name' },
      { name: 'tail', type: 'integer', required: false, description: 'Number of lines to fetch (default: 100)' },
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
    ],
    example: `{\n  "name": "dockdeploy_get_container_logs",\n  "arguments": {\n    "container_id": "web-service",\n    "tail": 50\n  }\n}`,
  },
  {
    name: 'dockdeploy_restart_container',
    category: 'Containers',
    description: 'Restart a specific Docker container on a managed server.',
    params: [
      { name: 'container_id', type: 'string', required: true, description: 'Container ID or container name' },
      { name: 'server_id', type: 'string', required: false, description: 'Server ID' },
    ],
    example: `{\n  "name": "dockdeploy_restart_container",\n  "arguments": {\n    "container_id": "web-service"\n  }\n}`,
  },
  {
    name: 'dockdeploy_list_deployments',
    category: 'Deployments',
    description: 'List all configured deployments, git repositories, and their latest build status in dockdeploy.',
    params: [],
    example: `{\n  "name": "dockdeploy_list_deployments",\n  "arguments": {}\n}`,
  },
  {
    name: 'dockdeploy_trigger_deployment',
    category: 'Deployments',
    description: 'Trigger a new deployment and build run for an application.',
    params: [
      { name: 'deployment_id', type: 'string', required: true, description: 'Deployment ID to build and deploy' },
    ],
    example: `{\n  "name": "dockdeploy_trigger_deployment",\n  "arguments": {\n    "deployment_id": "dep_12345"\n  }\n}`,
  },
  {
    name: 'dockdeploy_get_deployment_logs',
    category: 'Deployments',
    description: 'Fetch build and deployment run logs to diagnose failures and inspect stdout/stderr steps.',
    params: [
      { name: 'deployment_id', type: 'string', required: true, description: 'Deployment ID' },
      { name: 'run_id', type: 'string', required: false, description: 'Run ID (defaults to latest run)' },
    ],
    example: `{\n  "name": "dockdeploy_get_deployment_logs",\n  "arguments": {\n    "deployment_id": "dep_12345"\n  }\n}`,
  },
]

interface APIEndpointDoc {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH'
  path: string
  title: string
  category: string
  permission: string
  description: string
  requestBody?: string
  responseBody?: string
}

const API_ENDPOINTS: APIEndpointDoc[] = [
  // MCP Protocol
  {
    method: 'POST',
    path: '/api/v1/mcp',
    title: 'Model Context Protocol JSON-RPC',
    category: 'Model Context Protocol (MCP)',
    permission: 'server:read',
    description: 'Executes MCP 2024-11-05 protocol requests (initialize, ping, tools/list, tools/call, resources/list, resources/read, prompts/list, prompts/get). Supports single requests and batch JSON arrays.',
    requestBody: `{\n  "jsonrpc": "2.0",\n  "id": 1,\n  "method": "tools/call",\n  "params": {\n    "name": "dockdeploy_exec_command",\n    "arguments": {\n      "command": "uptime"\n    }\n  }\n}`,
    responseBody: `{\n  "jsonrpc": "2.0",\n  "id": 1,\n  "result": {\n    "content": [\n      {\n        "type": "text",\n        "text": "Command executed on **Local Server** (exit: 0)..."\n      }\n    ]\n  }\n}`,
  },
  {
    method: 'GET',
    path: '/api/v1/mcp',
    title: 'MCP Server-Sent Events (SSE) Endpoint',
    category: 'Model Context Protocol (MCP)',
    permission: 'server:read',
    description: 'Connects AI clients via Server-Sent Events. Pass header "Accept: text/event-stream" or query param "?token=<API_TOKEN>". Emits endpoint discovery event.',
    responseBody: `event: endpoint\ndata: /api/v1/mcp?token=YOUR_API_TOKEN\n\n`,
  },
  // Servers
  {
    method: 'GET',
    path: '/api/v1/servers',
    title: 'List Servers',
    category: 'Servers',
    permission: 'server:read',
    description: 'List all managed servers and their current status (online, offline, unknown).',
    responseBody: `[\n  {\n    "id": "srv_123",\n    "name": "Production VPS",\n    "host": "192.168.1.10",\n    "port": 22,\n    "username": "deploy",\n    "auth_method": "key",\n    "status": "online"\n  }\n]`,
  },
  {
    method: 'POST',
    path: '/api/v1/servers',
    title: 'Create Server',
    category: 'Servers',
    permission: 'server:write',
    description: 'Register a new server using SSH private key or password authentication.',
    requestBody: `{\n  "name": "Production-02",\n  "host": "203.0.113.5",\n  "port": 22,\n  "username": "deploy",\n  "auth_method": "key",\n  "private_key": "-----BEGIN OPENSSH PRIVATE KEY...-----",\n  "docker_socket": "/var/run/docker.sock"\n}`,
  },
  {
    method: 'GET',
    path: '/api/v1/servers/{serverID}',
    title: 'Get Server Details',
    category: 'Servers',
    permission: 'server:read',
    description: 'Retrieve full server details including system capabilities and probe output.',
  },
  {
    method: 'POST',
    path: '/api/v1/servers/{serverID}/probe',
    title: 'Probe Server',
    category: 'Servers',
    permission: 'server:read',
    description: 'Connects to the server over SSH to test Docker, Compose, Nginx, and Sudo capabilities.',
  },
  {
    method: 'GET',
    path: '/api/v1/servers/{serverID}/metrics',
    title: 'Server Metrics',
    category: 'Servers',
    permission: 'server:read',
    description: 'Collects real-time CPU, RAM, disk, and load metrics from the target machine.',
    responseBody: `{\n  "cpu_percent": 18.5,\n  "memory_used_bytes": 4294967296,\n  "memory_total_bytes": 17179869184,\n  "disk_used_bytes": 42949672960,\n  "disk_total_bytes": 107374182400,\n  "docker_running": true,\n  "container_count": 8\n}`,
  },
  {
    method: 'POST',
    path: '/api/v1/servers/{serverID}/exec-root',
    title: 'Execute Root Command',
    category: 'Servers',
    permission: 'server:write',
    description: 'Runs an administrative command elevated with sudo on the remote server.',
    requestBody: `{\n  "command": "apt-get update && apt-get -y upgrade",\n  "save_sudo": true\n}`,
  },
  // Containers
  {
    method: 'GET',
    path: '/api/v1/servers/{serverID}/containers',
    title: 'List Containers',
    category: 'Containers',
    permission: 'server:read',
    description: 'List all running and stopped Docker containers on the server.',
  },
  {
    method: 'GET',
    path: '/api/v1/servers/{serverID}/containers/{containerID}/logs',
    title: 'Container Logs',
    category: 'Containers',
    permission: 'server:read',
    description: 'Retrieve recent stdout/stderr output lines from a container.',
  },
  {
    method: 'POST',
    path: '/api/v1/servers/{serverID}/containers/{containerID}/actions',
    title: 'Container Action',
    category: 'Containers',
    permission: 'container:operate',
    description: 'Perform start, stop, restart, pause, unpause, or remove actions.',
    requestBody: `{\n  "action": "restart"\n}`,
  },
  {
    method: 'POST',
    path: '/api/v1/servers/{serverID}/docker/prune',
    title: 'Prune Docker Objects',
    category: 'Containers',
    permission: 'container:operate',
    description: 'Reclaims disk space by pruning stopped containers, dangling images, and unused networks.',
    requestBody: `{\n  "target": "all",\n  "all_images": false\n}`,
  },
  // Files
  {
    method: 'GET',
    path: '/api/v1/servers/{serverID}/files',
    title: 'List Files & Directories',
    category: 'Files',
    permission: 'server:read',
    description: 'Browse directories on the server with file sizes and permissions.',
  },
  {
    method: 'GET',
    path: '/api/v1/servers/{serverID}/files/content',
    title: 'Read File Content',
    category: 'Files',
    permission: 'server:read',
    description: 'Reads text file content from the server filesystem.',
  },
  {
    method: 'PUT',
    path: '/api/v1/servers/{serverID}/files/content',
    title: 'Save File Content',
    category: 'Files',
    permission: 'server:write',
    description: 'Overwrites or creates a file on the server filesystem.',
    requestBody: `{\n  "path": "/etc/docker/daemon.json",\n  "content": "{\\n  \\"log-level\\": \\"info\\"\\n}"\n}`,
  },
  // Deployments
  {
    method: 'GET',
    path: '/api/v1/deployments',
    title: 'List Deployments',
    category: 'Deployments',
    permission: 'deployment:read',
    description: 'List all application deployments, target servers, and status.',
  },
  {
    method: 'POST',
    path: '/api/v1/deployments',
    title: 'Create Deployment',
    category: 'Deployments',
    permission: 'deployment:write',
    description: 'Configure a new application deployment from Git repo or Docker image.',
    requestBody: `{\n  "name": "marketing-site",\n  "server_id": "srv_123",\n  "source_type": "git",\n  "git_repo": "https://github.com/org/repo.git",\n  "git_branch": "main",\n  "dockerfile_path": "Dockerfile",\n  "container_port": 3000\n}`,
  },
  {
    method: 'POST',
    path: '/api/v1/deployments/{deploymentID}/runs',
    title: 'Trigger Deployment Run',
    category: 'Deployments',
    permission: 'deployment:deploy',
    description: 'Queues a new build and deploy run. Returns immediately with run ID.',
    requestBody: `{\n  "rollback_run_id": ""\n}`,
    responseBody: `{\n  "id": "run_987",\n  "number": 42,\n  "status": "queued",\n  "trigger": "api"\n}`,
  },
  {
    method: 'GET',
    path: '/api/v1/deployments/{deploymentID}/runs/{runID}/logs',
    title: 'Deployment Run Logs',
    category: 'Deployments',
    permission: 'deployment:read',
    description: 'Streams or replays the full build and deploy log stream.',
  },
  // Domains
  {
    method: 'GET',
    path: '/api/v1/domains',
    title: 'List Domains',
    category: 'Domains',
    permission: 'domain:read',
    description: 'List virtual hosts, SSL status, and target upstream applications.',
  },
  {
    method: 'POST',
    path: '/api/v1/domains/{domainID}/ssl',
    title: 'Issue SSL Certificate',
    category: 'Domains',
    permission: 'domain:write',
    description: 'Requests a Let\'s Encrypt SSL certificate via Certbot HTTP-01 webroot challenge.',
  },
  {
    method: 'POST',
    path: '/api/v1/domains/{domainID}/sync',
    title: 'Sync Nginx VHost',
    category: 'Domains',
    permission: 'domain:write',
    description: 'Regenerates Nginx configuration and reloads the proxy daemon.',
  },
  // Tokens
  {
    method: 'GET',
    path: '/api/v1/tokens',
    title: 'List API Tokens',
    category: 'API Tokens',
    permission: 'self',
    description: 'List personal API tokens created for automation and MCP.',
  },
  {
    method: 'POST',
    path: '/api/v1/tokens',
    title: 'Create API Token',
    category: 'API Tokens',
    permission: 'self',
    description: 'Creates a new API token with an expiration timeframe.',
    requestBody: `{\n  "name": "Cursor / Claude Code MCP",\n  "expires_in_days": 90\n}`,
  },
]

export function Docs() {
  const [activeTab, setActiveTab] = useState<'mcp' | 'api' | 'connect' | 'examples'>('mcp')
  const [toolSearch, setToolSearch] = useState('')
  const [toolCategory, setToolCategory] = useState<string>('All')
  const [apiSearch, setApiSearch] = useState('')
  const [apiCategory, setApiCategory] = useState<string>('All')
  const [expandedTool, setExpandedTool] = useState<string | null>(null)
  const [expandedEndpoint, setExpandedEndpoint] = useState<string | null>(null)

  const originUrl = typeof window !== 'undefined' ? window.location.origin : 'https://dockdeploy.example.com'

  const filteredTools = MCP_TOOLS.filter((tool) => {
    const matchesSearch =
      tool.name.toLowerCase().includes(toolSearch.toLowerCase()) ||
      tool.description.toLowerCase().includes(toolSearch.toLowerCase())
    const matchesCategory = toolCategory === 'All' || tool.category === toolCategory
    return matchesSearch && matchesCategory
  })

  const apiCategories = ['All', ...Array.from(new Set(API_ENDPOINTS.map((e) => e.category)))]

  const filteredEndpoints = API_ENDPOINTS.filter((endpoint) => {
    const matchesSearch =
      endpoint.title.toLowerCase().includes(apiSearch.toLowerCase()) ||
      endpoint.path.toLowerCase().includes(apiSearch.toLowerCase()) ||
      endpoint.description.toLowerCase().includes(apiSearch.toLowerCase())
    const matchesCategory = apiCategory === 'All' || endpoint.category === apiCategory
    return matchesSearch && matchesCategory
  })

  return (
    <div className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8 space-y-6">
      <PageHeader
        title="API & Model Context Protocol (MCP) Docs"
        description="Comprehensive reference for connecting external AI agents, automating infrastructure via MCP, and consuming the dockdeploy REST API."
        actions={
          <div className="flex items-center gap-2">
            <Link to="/settings/tokens">
              <Button size="sm" variant="default" className="gap-2">
                <Key className="size-4" />
                <span>Create API Token</span>
              </Button>
            </Link>
          </div>
        }
      />

      {/* Top Navigation Tabs */}
      <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as any)} className="space-y-6">
        <TabsList className="grid grid-cols-2 md:grid-cols-4 w-full h-auto p-1 bg-surface border border-border/80">
          <TabsTrigger value="mcp" className="gap-2 py-2.5 text-xs sm:text-sm">
            <Bot className="size-4" />
            <span>MCP Tools ({MCP_TOOLS.length})</span>
          </TabsTrigger>
          <TabsTrigger value="connect" className="gap-2 py-2.5 text-xs sm:text-sm">
            <Network className="size-4" />
            <span>Connecting Remote MCP</span>
          </TabsTrigger>
          <TabsTrigger value="api" className="gap-2 py-2.5 text-xs sm:text-sm">
            <Code2 className="size-4" />
            <span>REST API Reference</span>
          </TabsTrigger>
          <TabsTrigger value="examples" className="gap-2 py-2.5 text-xs sm:text-sm">
            <Terminal className="size-4" />
            <span>Code & Curl Examples</span>
          </TabsTrigger>
        </TabsList>

        {/* 1. MCP TOOLS REFERENCE */}
        <TabsContent value="mcp" className="space-y-6">
          <Card className="border-border/80 bg-surface">
            <CardHeader className="pb-3">
              <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
                <div>
                  <CardTitle className="text-lg flex items-center gap-2">
                    <Bot className="size-5 text-primary" />
                    Model Context Protocol (MCP 2024-11-05) Tools
                  </CardTitle>
                  <CardDescription>
                    dockdeploy implements native MCP tooling, enabling Claude Desktop, Cursor, Antigravity, and other agents to operate your fleet.
                  </CardDescription>
                </div>
                <div className="flex items-center gap-2">
                  <Badge variant="outline" className="border-primary/40 bg-primary/10 text-primary">
                    16 Tools Available
                  </Badge>
                  <Badge variant="outline" className="border-emerald-500/40 bg-emerald-500/10 text-emerald-400">
                    Stdio & HTTP/SSE
                  </Badge>
                </div>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex flex-col sm:flex-row gap-3">
                <div className="relative flex-1">
                  <Search className="absolute left-3 top-2.5 size-4 text-muted-foreground" />
                  <Input
                    placeholder="Search tools by name, parameter or description..."
                    value={toolSearch}
                    onChange={(e) => setToolSearch(e.target.value)}
                    className="pl-9 text-xs sm:text-sm"
                  />
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {['All', 'Execution', 'Containers', 'Files', 'Servers', 'Deployments'].map((cat) => (
                    <Button
                      key={cat}
                      type="button"
                      variant={toolCategory === cat ? 'default' : 'outline'}
                      size="sm"
                      onClick={() => setToolCategory(cat)}
                      className="text-xs h-9 px-3"
                    >
                      {cat}
                    </Button>
                  ))}
                </div>
              </div>

              <div className="space-y-3 pt-2">
                {filteredTools.map((tool) => {
                  const isExpanded = expandedTool === tool.name
                  return (
                    <div
                      key={tool.name}
                      className="rounded-lg border border-border/70 bg-background/60 p-4 transition-all hover:border-border"
                    >
                      <div
                        className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 cursor-pointer select-none"
                        onClick={() => setExpandedTool(isExpanded ? null : tool.name)}
                      >
                        <div className="flex items-center gap-2.5">
                          <code className="text-sm font-semibold text-primary">{tool.name}</code>
                          <Badge variant="outline" className="text-[11px]">
                            {tool.category}
                          </Badge>
                        </div>
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                          <span>{tool.params.length} params</span>
                          {isExpanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                        </div>
                      </div>

                      <p className="mt-1.5 text-xs sm:text-sm text-muted-foreground">{tool.description}</p>

                      {isExpanded && (
                        <div className="mt-4 pt-3 border-t border-border/50 space-y-4">
                          <div>
                            <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                              Input Schema / Arguments
                            </h4>
                            {tool.params.length === 0 ? (
                              <p className="text-xs text-muted-foreground italic">No arguments required.</p>
                            ) : (
                              <div className="overflow-x-auto">
                                <table className="w-full text-xs text-left">
                                  <thead>
                                    <tr className="border-b border-border/40 text-muted-foreground">
                                      <th className="py-1.5 font-medium">Argument</th>
                                      <th className="py-1.5 font-medium">Type</th>
                                      <th className="py-1.5 font-medium">Required</th>
                                      <th className="py-1.5 font-medium">Description</th>
                                    </tr>
                                  </thead>
                                  <tbody className="divide-y divide-border/20">
                                    {tool.params.map((param) => (
                                      <tr key={param.name}>
                                        <td className="py-2 font-mono text-zinc-200">{param.name}</td>
                                        <td className="py-2 text-zinc-400">{param.type}</td>
                                        <td className="py-2">
                                          {param.required ? (
                                            <span className="text-amber-400 font-medium">Yes</span>
                                          ) : (
                                            <span className="text-muted-foreground">No</span>
                                          )}
                                        </td>
                                        <td className="py-2 text-zinc-300">{param.description}</td>
                                      </tr>
                                    ))}
                                  </tbody>
                                </table>
                              </div>
                            )}
                          </div>

                          <div>
                            <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-1">
                              Example JSON-RPC Call
                            </h4>
                            <CodeSnippet code={tool.example} language="json" />
                          </div>
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* 2. CONNECTING REMOTE MCP */}
        <TabsContent value="connect" className="space-y-6">
          <Card className="border-border/80 bg-surface">
            <CardHeader>
              <CardTitle className="text-lg flex items-center gap-2">
                <Network className="size-5 text-primary" />
                Connecting Remote MCP to Your AI Coding Agents
              </CardTitle>
              <CardDescription>
                When dockdeploy is deployed on a remote server, external AI assistants can connect via Direct HTTP/SSE or Stdio through an SSH tunnel.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-8">
              {/* Step 1: Token */}
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <span className="flex size-6 items-center justify-center rounded-full bg-primary/20 text-xs font-bold text-primary">
                    1
                  </span>
                  <h3 className="text-sm font-semibold">Generate a Dedicated API Token</h3>
                </div>
                <p className="text-xs sm:text-sm text-muted-foreground pl-8">
                  For HTTP/SSE connections, create an API token with server read/write permissions from the{' '}
                  <Link to="/settings/tokens" className="text-primary hover:underline font-medium">
                    API Tokens Settings page
                  </Link>
                  . Tokens can be passed as a header (<code className="text-xs">Authorization: Bearer dd_tok_...</code>) or as a URL query parameter (<code className="text-xs">?token=dd_tok_...</code>).
                </p>
              </div>

              {/* Method A: Direct HTTP / SSE */}
              <div className="space-y-4 rounded-xl border border-border/80 bg-background/50 p-5">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Globe className="size-4.5 text-sky-400" />
                    <h3 className="text-sm font-semibold">Method A: Direct HTTP / SSE (Cursor, Zed, Antigravity)</h3>
                  </div>
                  <Badge variant="outline" className="border-sky-500/40 text-sky-400">
                    Recommended for IDEs
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground">
                  Dockdeploy serves the MCP endpoint directly at <code className="text-xs">{originUrl}/api/v1/mcp</code>.
                </p>

                <div className="space-y-3 pt-2">
                  <h4 className="text-xs font-semibold text-zinc-300">Cursor Configuration (<code className="text-xs">.cursor/mcp.json</code>):</h4>
                  <CodeSnippet
                    code={`{
  "mcpServers": {
    "dockdeploy": {
      "url": "${originUrl}/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer YOUR_DOCKDEPLOY_API_TOKEN"
      }
    }
  }
}`}
                    language="json"
                  />

                  <h4 className="text-xs font-semibold text-zinc-300">URL with Query Token (For SSE Clients):</h4>
                  <CodeSnippet
                    code={`${originUrl}/api/v1/mcp?token=YOUR_DOCKDEPLOY_API_TOKEN`}
                    language="text"
                  />
                </div>
              </div>

              {/* Method B: Claude Desktop via MCP Proxy */}
              <div className="space-y-4 rounded-xl border border-border/80 bg-background/50 p-5">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Bot className="size-4.5 text-violet-400" />
                    <h3 className="text-sm font-semibold">Method B: Claude Desktop (<code className="text-xs">claude_desktop_config.json</code>)</h3>
                  </div>
                  <Badge variant="outline" className="border-violet-500/40 text-violet-400">
                    Via MCP Proxy
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground">
                  Claude Desktop supports stdio clients natively. Use <code className="text-xs">@modelcontextprotocol/server-proxy</code> to bridge Claude Desktop to your remote dockdeploy HTTP endpoint:
                </p>

                <CodeSnippet
                  code={`{
  "mcpServers": {
    "dockdeploy-remote": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-proxy",
        "${originUrl}/api/v1/mcp",
        "--header",
        "Authorization: Bearer YOUR_DOCKDEPLOY_API_TOKEN"
      ]
    }
  }
}`}
                  language="json"
                />
              </div>

              {/* Method C: Stdio over SSH Tunnel */}
              <div className="space-y-4 rounded-xl border border-border/80 bg-background/50 p-5">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Terminal className="size-4.5 text-emerald-400" />
                    <h3 className="text-sm font-semibold">Method C: Zero-Exposure Stdio over SSH Tunnel</h3>
                  </div>
                  <Badge variant="outline" className="border-emerald-500/40 text-emerald-400">
                    Highest Security
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground">
                  Connect without opening any HTTP ports by tunneling MCP Stdio directly through your existing SSH connection to the server.
                </p>

                <div className="space-y-3 pt-2">
                  <h4 className="text-xs font-semibold text-zinc-300">If dockdeploy runs as host binary/service:</h4>
                  <CodeSnippet
                    code={`{
  "mcpServers": {
    "dockdeploy-prod": {
      "command": "ssh",
      "args": [
        "-T",
        "deploy@your-server-ip",
        "/opt/dockdeploy/dockdeploy mcp -db /opt/dockdeploy/dockdeploy.db"
      ]
    }
  }
}`}
                    language="json"
                  />

                  <h4 className="text-xs font-semibold text-zinc-300">If dockdeploy runs inside a Docker container on the server:</h4>
                  <CodeSnippet
                    code={`{
  "mcpServers": {
    "dockdeploy-prod": {
      "command": "ssh",
      "args": [
        "-T",
        "deploy@your-server-ip",
        "docker exec -i dockdeploy /app/dockdeploy mcp"
      ]
    }
  }
}`}
                    language="json"
                  />
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* 3. REST API REFERENCE */}
        <TabsContent value="api" className="space-y-6">
          <Card className="border-border/80 bg-surface">
            <CardHeader className="pb-3">
              <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
                <div>
                  <CardTitle className="text-lg flex items-center gap-2">
                    <Code2 className="size-5 text-primary" />
                    REST API Reference
                  </CardTitle>
                  <CardDescription>
                    All API endpoints are mounted at <code className="text-xs">/api/v1</code> and authenticated with Bearer tokens or browser sessions.
                  </CardDescription>
                </div>
                <div className="flex items-center gap-2">
                  <Badge variant="outline" className="text-xs">
                    Base: /api/v1
                  </Badge>
                </div>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex flex-col sm:flex-row gap-3">
                <div className="relative flex-1">
                  <Search className="absolute left-3 top-2.5 size-4 text-muted-foreground" />
                  <Input
                    placeholder="Search endpoints by path, title, or description..."
                    value={apiSearch}
                    onChange={(e) => setApiSearch(e.target.value)}
                    className="pl-9 text-xs sm:text-sm"
                  />
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {apiCategories.map((cat) => (
                    <Button
                      key={cat}
                      type="button"
                      variant={apiCategory === cat ? 'default' : 'outline'}
                      size="sm"
                      onClick={() => setApiCategory(cat)}
                      className="text-xs h-9 px-3"
                    >
                      {cat}
                    </Button>
                  ))}
                </div>
              </div>

              <div className="space-y-3 pt-2">
                {filteredEndpoints.map((ep, idx) => {
                  const key = `${ep.method}-${ep.path}-${idx}`
                  const isExpanded = expandedEndpoint === key

                  const methodColor =
                    ep.method === 'GET'
                      ? 'border-sky-500/40 bg-sky-500/10 text-sky-400'
                      : ep.method === 'POST'
                        ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
                        : ep.method === 'PUT'
                          ? 'border-amber-500/40 bg-amber-500/10 text-amber-400'
                          : ep.method === 'DELETE'
                            ? 'border-rose-500/40 bg-rose-500/10 text-rose-400'
                            : 'border-violet-500/40 bg-violet-500/10 text-violet-400'

                  return (
                    <div
                      key={key}
                      className="rounded-lg border border-border/70 bg-background/60 p-4 transition-all hover:border-border"
                    >
                      <div
                        className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 cursor-pointer select-none"
                        onClick={() => setExpandedEndpoint(isExpanded ? null : key)}
                      >
                        <div className="flex flex-wrap items-center gap-2.5">
                          <Badge variant="outline" className={`font-mono text-xs font-bold ${methodColor}`}>
                            {ep.method}
                          </Badge>
                          <code className="text-xs sm:text-sm font-semibold text-zinc-100">{ep.path}</code>
                          <span className="text-xs text-muted-foreground font-normal">({ep.title})</span>
                        </div>
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                          <span className="text-[11px] font-mono text-zinc-400">{ep.permission}</span>
                          {isExpanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                        </div>
                      </div>

                      <p className="mt-2 text-xs sm:text-sm text-muted-foreground">{ep.description}</p>

                      {isExpanded && (
                        <div className="mt-4 pt-3 border-t border-border/50 space-y-4">
                          {ep.requestBody && (
                            <div>
                              <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-1">
                                Request Payload (JSON)
                              </h4>
                              <CodeSnippet code={ep.requestBody} language="json" />
                            </div>
                          )}

                          {ep.responseBody && (
                            <div>
                              <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-1">
                                Example Response (JSON)
                              </h4>
                              <CodeSnippet code={ep.responseBody} language="json" />
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* 4. CODE & CURL EXAMPLES */}
        <TabsContent value="examples" className="space-y-6">
          <Card className="border-border/80 bg-surface">
            <CardHeader>
              <CardTitle className="text-lg flex items-center gap-2">
                <Terminal className="size-5 text-primary" />
                Working Code & Curl Examples
              </CardTitle>
              <CardDescription>
                Copy and run real-world commands to interact directly with your dockdeploy server from terminals, scripts, or backend services.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div>
                <h3 className="text-sm font-semibold text-zinc-200">1. Execute Shell Command via MCP HTTP</h3>
                <p className="text-xs text-muted-foreground mb-2">
                  Call the MCP endpoint directly to execute shell diagnostics:
                </p>
                <CodeSnippet
                  code={`curl -X POST "${originUrl}/api/v1/mcp" \\
  -H "Authorization: Bearer YOUR_API_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "dockdeploy_exec_command",
      "arguments": {
        "command": "uname -a && docker ps",
        "sudo": false
      }
    }
  }'`}
                  language="bash"
                />
              </div>

              <div>
                <h3 className="text-sm font-semibold text-zinc-200">2. Trigger an Application Deployment</h3>
                <p className="text-xs text-muted-foreground mb-2">
                  Enqueue a build run for deployment ID <code className="text-xs">dep_123</code>:
                </p>
                <CodeSnippet
                  code={`curl -X POST "${originUrl}/api/v1/deployments/dep_123/runs" \\
  -H "Authorization: Bearer YOUR_API_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{}'`}
                  language="bash"
                />
              </div>

              <div>
                <h3 className="text-sm font-semibold text-zinc-200">3. Restart a Docker Container</h3>
                <p className="text-xs text-muted-foreground mb-2">
                  Execute container lifecycle operation:
                </p>
                <CodeSnippet
                  code={`curl -X POST "${originUrl}/api/v1/servers/srv_123/containers/web-app/actions" \\
  -H "Authorization: Bearer YOUR_API_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{"action": "restart"}'`}
                  language="bash"
                />
              </div>

              <div>
                <h3 className="text-sm font-semibold text-zinc-200">4. Fetch Container Logs</h3>
                <p className="text-xs text-muted-foreground mb-2">
                  Tail recent log output:
                </p>
                <CodeSnippet
                  code={`curl "${originUrl}/api/v1/servers/srv_123/containers/web-app/logs?tail=100" \\
  -H "Authorization: Bearer YOUR_API_TOKEN"`}
                  language="bash"
                />
              </div>

              <div>
                <h3 className="text-sm font-semibold text-zinc-200">5. Python SDK Script for MCP Tool Execution</h3>
                <CodeSnippet
                  code={`import requests

URL = "${originUrl}/api/v1/mcp"
TOKEN = "YOUR_API_TOKEN"

payload = {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "dockdeploy_exec_command",
        "arguments": {
            "command": "df -h / && docker stats --no-stream"
        }
    }
}

response = requests.post(
    URL,
    headers={"Authorization": f"Bearer {TOKEN}"},
    json=payload
)

print(response.json()["result"]["content"][0]["text"])`}
                  language="python"
                />
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
