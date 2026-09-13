import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { markdown } from '@codemirror/lang-markdown'
import { python } from '@codemirror/lang-python'
import { yaml } from '@codemirror/lang-yaml'
import { oneDark } from '@codemirror/theme-one-dark'
import CodeMirror from '@uiw/react-codemirror'
import {
  AlertTriangle,
  Check,
  Clock,
  Copy,
  Download,
  FileCode,
  HardDrive,
  Loader2,
  Maximize2,
  Minimize2,
  Save,
  Shield,
  Undo2,
} from 'lucide-react'
import { useMemo, useState } from 'react'

import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  serverFileDownloadURL,
  useFileContent,
  useSaveFileContent,
  type Server,
} from '@/lib/servers'
import { cn, formatBytes, formatRelative } from '@/lib/utils'

type SupportedLanguage =
  | 'yaml'
  | 'json'
  | 'javascript'
  | 'python'
  | 'markdown'
  | 'dockerfile'
  | 'shell'
  | 'plaintext'

function detectLanguage(filename: string): SupportedLanguage {
  const lower = filename.toLowerCase()
  const base = lower.split('/').pop() || ''

  if (
    base === 'dockerfile' ||
    base.startsWith('dockerfile.') ||
    base === 'containerfile'
  ) {
    return 'dockerfile'
  }

  if (
    base === 'docker-compose.yml' ||
    base === 'docker-compose.yaml' ||
    base === 'compose.yml' ||
    base === 'compose.yaml' ||
    base.endsWith('.yml') ||
    base.endsWith('.yaml')
  ) {
    return 'yaml'
  }

  if (
    base.endsWith('.json') ||
    base.endsWith('.jsonc') ||
    base === '.eslintrc' ||
    base === '.babelrc'
  ) {
    return 'json'
  }

  if (
    base.endsWith('.js') ||
    base.endsWith('.jsx') ||
    base.endsWith('.ts') ||
    base.endsWith('.tsx') ||
    base.endsWith('.mjs') ||
    base.endsWith('.cjs')
  ) {
    return 'javascript'
  }

  if (base.endsWith('.py') || base.endsWith('.pyw')) {
    return 'python'
  }

  if (base.endsWith('.md') || base.endsWith('.markdown')) {
    return 'markdown'
  }

  if (
    base.endsWith('.sh') ||
    base.endsWith('.bash') ||
    base.endsWith('.zsh') ||
    base === '.env' ||
    base.startsWith('.env.') ||
    base === 'makefile'
  ) {
    return 'shell'
  }

  return 'plaintext'
}

interface ServerFileEditorProps {
  server: Server
  filePath: string | null
  isOpen: boolean
  onClose: () => void
  canWrite?: boolean
  isNewFile?: boolean
  initialDirectory?: string
  onSaved?: () => void
}

export function ServerFileEditor({
  server,
  filePath,
  isOpen,
  onClose,
  canWrite = true,
  isNewFile = false,
  initialDirectory = '~',
  onSaved,
}: ServerFileEditorProps) {
  const [userContent, setUserContent] = useState<string | null>(null)
  const [newFileName, setNewFileName] = useState('')
  const [manualLang, setManualLang] = useState<SupportedLanguage | null>(null)
  const [isMaximized, setIsMaximized] = useState(false)
  const [copied, setCopied] = useState(false)
  const [lastSavedTime, setLastSavedTime] = useState<Date | null>(null)

  const activePath = isNewFile
    ? initialDirectory === '/'
      ? `/${newFileName}`
      : `${initialDirectory}/${newFileName}`
    : filePath || ''

  const fileName = isNewFile ? newFileName : filePath?.split('/').pop() || ''

  const { data: fileData, isPending, isError, error } = useFileContent(
    server.id,
    isNewFile ? null : filePath,
  )
  const saveMutation = useSaveFileContent(server.id)

  const currentContent = userContent !== null ? userContent : (fileData?.content ?? '')
  const isDirty = userContent !== null && userContent !== (fileData?.content ?? '')

  const detectedLang = useMemo(() => {
    if (manualLang) return manualLang
    return detectLanguage(fileName)
  }, [fileName, manualLang])

  const languageExtension = useMemo(() => {
    switch (detectedLang) {
      case 'json':
        return [json()]
      case 'yaml':
      case 'dockerfile':
        return [yaml()]
      case 'javascript':
        return [javascript({ jsx: true, typescript: true })]
      case 'python':
        return [python()]
      case 'markdown':
        return [markdown()]
      default:
        return []
    }
  }, [detectedLang])

  // Calculate lines & size
  const lineCount = useMemo(() => {
    if (!currentContent) return 1
    return currentContent.split('\n').length
  }, [currentContent])

  const approximateBytes = useMemo(() => {
    return new Blob([currentContent]).size
  }, [currentContent])

  function handleSave() {
    if (!canWrite || saveMutation.isPending) return
    if (isNewFile && !newFileName.trim()) return

    saveMutation.mutate(
      {
        path: activePath,
        content: currentContent,
      },
      {
        onSuccess: () => {
          setUserContent(null)
          setLastSavedTime(new Date())
          onSaved?.()
        },
      },
    )
  }

  function handleCopy() {
    void navigator.clipboard.writeText(currentContent)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  function handleClose() {
    if (isDirty) {
      if (!confirm('You have unsaved changes. Are you sure you want to discard them and close?')) {
        return
      }
    }
    setUserContent(null)
    setManualLang(null)
    setNewFileName('')
    onClose()
  }

  // Keyboard shortcut Ctrl+S / Cmd+S
  function handleKeyDown(e: React.KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
      e.preventDefault()
      handleSave()
    }
  }

  if (!isOpen) return null

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && handleClose()}>
      <DialogContent
        className={cn(
          'flex flex-col gap-0 p-0 overflow-hidden bg-card text-card-foreground transition-all duration-200 border-border/80 shadow-2xl',
          isMaximized
            ? 'fixed inset-2 max-w-[calc(100vw-1rem)] w-[calc(100vw-1rem)] h-[calc(100vh-1rem)] max-h-[calc(100vh-1rem)] rounded-lg translate-x-0 translate-y-0 top-2 left-2'
            : 'max-w-5xl w-[94vw] h-[88vh] max-h-[88vh] rounded-xl',
        )}
        onKeyDown={handleKeyDown}
      >
        {/* Top Header & Metadata Bar */}
        <div className="flex flex-col border-b border-border bg-muted/30 px-4 py-3 gap-2 shrink-0">
          <div className="flex flex-wrap items-center justify-between gap-3 pr-8">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="flex size-8 items-center justify-center rounded-md bg-primary/10 text-primary shrink-0">
                <FileCode className="size-4" aria-hidden />
              </div>

              {isNewFile ? (
                <div className="flex items-center gap-2">
                  <Input
                    placeholder="e.g. docker-compose.yml or script.sh"
                    value={newFileName}
                    onChange={(e) => setNewFileName(e.target.value)}
                    className="h-8 w-64 text-xs font-mono"
                    autoFocus
                  />
                  <span className="text-xs text-muted-foreground font-mono">
                    in {initialDirectory}
                  </span>
                </div>
              ) : (
                <div className="min-w-0">
                  <DialogTitle className="flex items-center gap-2 text-sm font-semibold truncate font-mono">
                    {fileName || 'File Editor'}
                    {isDirty ? (
                      <Badge variant="warning" className="h-5 text-[10px] font-sans">
                        Unsaved *
                      </Badge>
                    ) : lastSavedTime ? (
                      <Badge variant="success" className="h-5 text-[10px] font-sans">
                        Saved
                      </Badge>
                    ) : null}
                  </DialogTitle>
                  <DialogDescription className="text-xs font-mono text-muted-foreground truncate">
                    {activePath}
                  </DialogDescription>
                </div>
              )}
            </div>

            {/* Quick Actions & Format Selector */}
            <div className="flex items-center gap-2">
              <Select
                value={detectedLang}
                onValueChange={(val) => setManualLang(val as SupportedLanguage)}
              >
                <SelectTrigger className="h-8 w-32 text-xs">
                  <SelectValue placeholder="Syntax" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="yaml">YAML</SelectItem>
                  <SelectItem value="json">JSON</SelectItem>
                  <SelectItem value="dockerfile">Dockerfile</SelectItem>
                  <SelectItem value="javascript">JS / TS</SelectItem>
                  <SelectItem value="python">Python</SelectItem>
                  <SelectItem value="markdown">Markdown</SelectItem>
                  <SelectItem value="shell">Shell / Env</SelectItem>
                  <SelectItem value="plaintext">Plain Text</SelectItem>
                </SelectContent>
              </Select>

              <Button
                variant="ghost"
                size="sm"
                className="h-8 px-2.5 text-xs"
                onClick={handleCopy}
                title="Copy file contents"
              >
                {copied ? (
                  <Check className="size-3.5 text-success mr-1.5" />
                ) : (
                  <Copy className="size-3.5 mr-1.5" />
                )}
                {copied ? 'Copied' : 'Copy'}
              </Button>

              {!isNewFile && filePath && (
                <Button
                  asChild
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2.5 text-xs"
                  title="Download file"
                >
                  <a href={serverFileDownloadURL(server.id, filePath)} download>
                    <Download className="size-3.5 mr-1.5" />
                    Download
                  </a>
                </Button>
              )}

              {isDirty && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2.5 text-xs"
                  onClick={() => setUserContent(null)}
                  title="Discard edits and reload from disk"
                >
                  <Undo2 className="size-3.5 mr-1.5" />
                  Discard
                </Button>
              )}

              {canWrite && (
                <Button
                  size="sm"
                  className="h-8 px-3 text-xs"
                  onClick={handleSave}
                  disabled={saveMutation.isPending || (!isDirty && !isNewFile)}
                  title="Save changes (Ctrl+S or Cmd+S)"
                >
                  {saveMutation.isPending ? (
                    <Loader2 className="animate-spin size-3.5 mr-1.5" />
                  ) : (
                    <Save className="size-3.5 mr-1.5" />
                  )}
                  {saveMutation.isPending ? 'Saving...' : 'Save'}
                </Button>
              )}

              <Button
                variant="ghost"
                size="sm"
                className="h-8 w-8 p-0"
                onClick={() => setIsMaximized(!isMaximized)}
                title={isMaximized ? 'Restore dialog' : 'Maximize editor'}
              >
                {isMaximized ? (
                  <Minimize2 className="size-4" />
                ) : (
                  <Maximize2 className="size-4" />
                )}
              </Button>
            </div>
          </div>

          {/* Metadata Badges strip */}
          <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground font-mono">
            <span className="flex items-center gap-1">
              <HardDrive className="size-3 text-muted-foreground/70" />
              {formatBytes(fileData?.size ?? approximateBytes)}
            </span>
            <span>&middot;</span>
            <span>{lineCount} lines</span>
            <span>&middot;</span>
            <span className="flex items-center gap-1">
              <Shield className="size-3 text-muted-foreground/70" />
              {fileData?.permissions || '0644'}
            </span>
            {fileData?.mod_time && (
              <>
                <span>&middot;</span>
                <span className="flex items-center gap-1">
                  <Clock className="size-3 text-muted-foreground/70" />
                  Modified {formatRelative(fileData.mod_time)}
                </span>
              </>
            )}
            <span className="ml-auto font-sans text-[11px] text-muted-foreground/80 hidden sm:inline">
              Press <kbd className="rounded border bg-muted px-1 py-0.5 text-[10px]">Ctrl+S</kbd> to save
            </span>
          </div>
        </div>

        {/* Error notification */}
        {saveMutation.isError && (
          <div className="px-4 py-2 bg-destructive/10 border-b border-destructive/20 text-destructive text-xs">
            Failed to save: {saveMutation.error.message}
          </div>
        )}

        {/* Editor Body */}
        <div className="relative flex-1 min-h-0 overflow-hidden bg-[#282c34]">
          {isPending && !isNewFile ? (
            <div className="flex h-full flex-col justify-center items-center gap-3 p-8 text-muted-foreground bg-card">
              <Loader2 className="animate-spin size-8 text-primary" />
              <p className="text-sm">Reading file content from {server.name}...</p>
            </div>
          ) : isError && !isNewFile ? (
            <div className="p-6">
              <Alert variant="danger">
                Failed to open file: {error.message}
              </Alert>
            </div>
          ) : fileData?.is_binary ? (
            <div className="flex h-full flex-col items-center justify-center p-8 text-center bg-card">
              <div className="flex size-12 items-center justify-center rounded-full bg-amber-500/10 text-amber-500 mb-4">
                <AlertTriangle className="size-6" />
              </div>
              <h3 className="text-base font-semibold mb-1">Binary File Detected</h3>
              <p className="text-sm text-muted-foreground max-w-md mb-4">
                This file contains binary data (e.g. image, archive, or executable) and cannot be displayed or edited in the code editor.
              </p>
              {filePath && (
                <Button asChild variant="outline">
                  <a href={serverFileDownloadURL(server.id, filePath)} download>
                    <Download className="size-4 mr-2" />
                    Download File Instead
                  </a>
                </Button>
              )}
            </div>
          ) : (
            <div className="h-full w-full overflow-auto">
              <CodeMirror
                value={currentContent}
                height="100%"
                theme={oneDark}
                extensions={languageExtension}
                onChange={(value) => setUserContent(value)}
                readOnly={!canWrite}
                basicSetup={{
                  lineNumbers: true,
                  highlightActiveLineGutter: true,
                  highlightSpecialChars: true,
                  history: true,
                  foldGutter: true,
                  drawSelection: true,
                  dropCursor: true,
                  allowMultipleSelections: true,
                  indentOnInput: true,
                  syntaxHighlighting: true,
                  bracketMatching: true,
                  closeBrackets: true,
                  autocompletion: true,
                  rectangularSelection: true,
                  crosshairCursor: true,
                  highlightActiveLine: true,
                  highlightSelectionMatches: true,
                  closeBracketsKeymap: true,
                  defaultKeymap: true,
                  searchKeymap: true,
                  historyKeymap: true,
                  foldKeymap: true,
                  completionKeymap: true,
                  lintKeymap: true,
                }}
                className="h-full text-xs sm:text-sm font-mono"
              />
            </div>
          )}
        </div>

        {/* Bottom Status Bar */}
        <div className="flex items-center justify-between border-t border-border bg-muted/40 px-4 py-1.5 text-xs text-muted-foreground shrink-0 font-mono">
          <div className="flex items-center gap-3">
            <span className="uppercase">{detectedLang}</span>
            <span>&middot;</span>
            <span>UTF-8</span>
            <span>&middot;</span>
            <span>Spaces: 2</span>
          </div>

          <div className="flex items-center gap-2">
            {!canWrite && (
              <span className="text-amber-500 font-sans text-xs">Read-only mode</span>
            )}
            {isDirty && (
              <span className="text-amber-400 text-xs font-sans font-medium">
                Unsaved edits
              </span>
            )}
            {lastSavedTime && !isDirty && (
              <span className="text-success text-xs font-sans">
                Saved at {lastSavedTime.toLocaleTimeString()}
              </span>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
