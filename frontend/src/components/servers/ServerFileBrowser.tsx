import {
  ArrowRight,
  ChevronRight,
  Copy,
  Download,
  File,
  FileArchive,
  FileEdit,
  FilePlus,
  Folder,
  FolderArchive,
  FolderPlus,
  FolderUp,
  HardDrive,
  Home,
  KeyRound,
  Loader2,
  Lock,
  Move,
  PieChart,
  RefreshCw,
  Send,
  Shield,
  ShieldCheck,
  Trash2,
  Upload,
} from 'lucide-react'
import { useRef, useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { ServerFileEditor } from './ServerFileEditor'
import { EmptyState } from '@/components/EmptyState'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  serverArchiveDownloadURL,
  serverFileDownloadURL,
  useChmodFile,
  useCopyFile,
  useCreateDirectory,
  useDeleteFile,
  useDirectoryListing,
  useMoveFile,
  useScanStorage,
  useServers,
  useSetServerSudoPassword,
  useTransferFile,
  useUploadFile,
  type FileEntry,
  type FileTransferResult,
  type Server,
} from '@/lib/servers'
import { formatBytes, formatRelative } from '@/lib/utils'

interface ServerFileBrowserProps {
  server: Server
  initialPath?: string
  canWrite?: boolean
}

export function ServerFileBrowser({
  server,
  initialPath = '~',
  canWrite = true,
}: ServerFileBrowserProps) {
  const [currentPath, setCurrentPath] = useState(initialPath)
  const [pathInput, setPathInput] = useState(initialPath)
  const [prevInitialPath, setPrevInitialPath] = useState(initialPath)
  const [isEditingPath, setIsEditingPath] = useState(false)

  // Sudo elevation state
  const [sudoPassword, setSudoPassword] = useState('')
  const [sudoInput, setSudoInput] = useState('')
  const [saveSudoPassword, setSaveSudoPassword] = useState(true)
  const [sudoDialogOpen, setSudoDialogOpen] = useState(false)

  // Adjust state during render when initialPath prop changes
  if (initialPath !== prevInitialPath) {
    setPrevInitialPath(initialPath)
    setCurrentPath(initialPath)
    setPathInput(initialPath)
  }

  // Upload dialog state
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false)
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // Editor state
  const [editorOpen, setEditorOpen] = useState(false)
  const [editingFilePath, setEditingFilePath] = useState<string | null>(null)
  const [isNewFile, setIsNewFile] = useState(false)

  // New folder state
  const [mkdirDialogOpen, setMkdirDialogOpen] = useState(false)
  const [newFolderName, setNewFolderName] = useState('')

  // Delete dialog state
  const [deleteTarget, setDeleteTarget] = useState<FileEntry | null>(null)

  // Copy dialog state
  const [copyTarget, setCopyTarget] = useState<FileEntry | null>(null)
  const [copyDest, setCopyDest] = useState('')

  // Move / Rename dialog state
  const [moveTarget, setMoveTarget] = useState<FileEntry | null>(null)
  const [moveDest, setMoveDest] = useState('')

  // Chmod dialog state
  const [chmodTarget, setChmodTarget] = useState<FileEntry | null>(null)
  const [chmodMode, setChmodMode] = useState('0755')

  // Storage scanner state
  const [scannerOpen, setScannerOpen] = useState(false)
  const [scanPath, setScanPath] = useState('/')
  const [scanMinMB, setScanMinMB] = useState(10)

  // Transfer dialog state
  const [transferItem, setTransferItem] = useState<FileEntry | null>(null)
  const [targetServerID, setTargetServerID] = useState('')
  const [targetPath, setTargetPath] = useState('')
  const [transferResult, setTransferResult] = useState<FileTransferResult | null>(null)

  // Queries & Mutations
  const { data: listing, isPending, isError, error, refetch, isFetching } = useDirectoryListing(
    server.id,
    currentPath,
    sudoPassword,
  )
  const { data: allServers = [] } = useServers()
  const upload = useUploadFile(server.id)
  const transfer = useTransferFile()
  const setSudoMutation = useSetServerSudoPassword(server.id)
  const mkdirMutation = useCreateDirectory(server.id)
  const deleteMutation = useDeleteFile(server.id)
  const copyMutation = useCopyFile(server.id)
  const moveMutation = useMoveFile(server.id)
  const chmodMutation = useChmodFile(server.id)

  const scanQuery = useScanStorage(
    server.id,
    scanPath,
    scanMinMB,
    sudoPassword,
    scannerOpen,
  )

  const isSudoRequired =
    Boolean(error?.fields?.sudo_password) ||
    error?.status === 422 ||
    (error?.message && error.message.toLowerCase().includes('sudo password is required'))

  const isElevated = server.has_sudo_password || Boolean(sudoPassword)

  const destinationServers = allServers.filter((s) => s.id !== server.id && s.status === 'online')

  function handleNavigate(path: string) {
    setCurrentPath(path)
    setPathInput(path)
    setIsEditingPath(false)
  }

  function handlePathSubmit(e: FormEvent) {
    e.preventDefault()
    if (pathInput.trim()) {
      handleNavigate(pathInput.trim())
    }
  }

  function handleSudoSubmit(e: FormEvent) {
    e.preventDefault()
    if (!sudoInput.trim()) return

    const pass = sudoInput.trim()
    setSudoPassword(pass)

    if (saveSudoPassword) {
      setSudoMutation.mutate(
        { sudo_password: pass },
        {
          onSuccess: () => {
            toast.success('Root sudo password saved for server')
          },
          onError: (err) => {
            toast.error(`Failed to save password: ${err.message}`)
          },
        },
      )
    }

    setSudoDialogOpen(false)
    setSudoInput('')
    void refetch()
  }

  function handleCreateFolder(e: FormEvent) {
    e.preventDefault()
    if (!newFolderName.trim()) return

    const folderPath =
      (listing?.path || currentPath).replace(/\/+$/, '') + '/' + newFolderName.trim()

    mkdirMutation.mutate(
      {
        path: folderPath,
        sudo_password: sudoPassword,
        save_sudo: saveSudoPassword,
      },
      {
        onSuccess: () => {
          setMkdirDialogOpen(false)
          setNewFolderName('')
          toast.success(`Created directory ${newFolderName}`)
          void refetch()
        },
        onError: (err) => {
          if (err.fields?.sudo_password) {
            setSudoDialogOpen(true)
          } else {
            toast.error(`Failed to create directory: ${err.message}`)
          }
        },
      },
    )
  }

  function handleDeleteConfirm() {
    if (!deleteTarget) return

    deleteMutation.mutate(
      {
        path: deleteTarget.path,
        sudo_password: sudoPassword,
        save_sudo: saveSudoPassword,
      },
      {
        onSuccess: () => {
          toast.success(`Deleted ${deleteTarget.name}`)
          setDeleteTarget(null)
          void refetch()
        },
        onError: (err) => {
          if (err.fields?.sudo_password) {
            setSudoDialogOpen(true)
          } else {
            toast.error(`Failed to delete: ${err.message}`)
          }
        },
      },
    )
  }

  function handleCopySubmit(e: FormEvent) {
    e.preventDefault()
    if (!copyTarget || !copyDest.trim()) return

    copyMutation.mutate(
      {
        source_path: copyTarget.path,
        target_path: copyDest.trim(),
        sudo_password: sudoPassword,
        save_sudo: saveSudoPassword,
      },
      {
        onSuccess: () => {
          toast.success(`Copied to ${copyDest}`)
          setCopyTarget(null)
          void refetch()
        },
        onError: (err) => {
          if (err.fields?.sudo_password) {
            setSudoDialogOpen(true)
          } else {
            toast.error(`Copy failed: ${err.message}`)
          }
        },
      },
    )
  }

  function handleMoveSubmit(e: FormEvent) {
    e.preventDefault()
    if (!moveTarget || !moveDest.trim()) return

    moveMutation.mutate(
      {
        source_path: moveTarget.path,
        target_path: moveDest.trim(),
        sudo_password: sudoPassword,
        save_sudo: saveSudoPassword,
      },
      {
        onSuccess: () => {
          toast.success(`Moved/renamed to ${moveDest}`)
          setMoveTarget(null)
          void refetch()
        },
        onError: (err) => {
          if (err.fields?.sudo_password) {
            setSudoDialogOpen(true)
          } else {
            toast.error(`Move failed: ${err.message}`)
          }
        },
      },
    )
  }

  function handleChmodSubmit(e: FormEvent) {
    e.preventDefault()
    if (!chmodTarget || !chmodMode.trim()) return

    chmodMutation.mutate(
      {
        path: chmodTarget.path,
        mode: chmodMode.trim(),
        sudo_password: sudoPassword,
        save_sudo: saveSudoPassword,
      },
      {
        onSuccess: () => {
          toast.success(`Updated permissions to ${chmodMode}`)
          setChmodTarget(null)
          void refetch()
        },
        onError: (err) => {
          if (err.fields?.sudo_password) {
            setSudoDialogOpen(true)
          } else {
            toast.error(`Chmod failed: ${err.message}`)
          }
        },
      },
    )
  }

  function handleStartUpload(e: FormEvent) {
    e.preventDefault()
    if (!uploadFile) return

    upload.mutate(
      {
        file: uploadFile,
        targetDir: listing?.path || currentPath,
      },
      {
        onSuccess: () => {
          setUploadDialogOpen(false)
          setUploadFile(null)
          if (fileInputRef.current) fileInputRef.current.value = ''
          toast.success('File uploaded successfully')
          void refetch()
        },
        onError: (err) => {
          toast.error(`Upload failed: ${err.message}`)
        },
      },
    )
  }

  function handleStartTransfer(e: FormEvent) {
    e.preventDefault()
    if (!transferItem || !targetServerID || !targetPath.trim()) return

    transfer.mutate(
      {
        sourceServerID: server.id,
        targetServerID,
        sourcePath: transferItem.path,
        targetPath: targetPath.trim(),
      },
      {
        onSuccess: (res) => {
          setTransferResult(res)
        },
        onError: (err) => {
          toast.error(`Transfer failed: ${err.message}`)
        },
      },
    )
  }

  function openTransferModal(item: FileEntry) {
    setTransferItem(item)
    setTargetServerID(destinationServers[0]?.id || '')
    setTargetPath(item.path)
    setTransferResult(null)
    transfer.reset()
  }

  function openCopyModal(item: FileEntry) {
    setCopyTarget(item)
    setCopyDest(`${item.path}.copy`)
    copyMutation.reset()
  }

  function openMoveModal(item: FileEntry) {
    setMoveTarget(item)
    setMoveDest(item.path)
    moveMutation.reset()
  }

  function openChmodModal(item: FileEntry) {
    setChmodTarget(item)
    setChmodMode(item.is_dir ? '0755' : '0644')
    chmodMutation.reset()
  }

  // Parse path into clickable breadcrumb parts
  const pathParts = (() => {
    const p = listing?.path || currentPath
    if (p === '/') return [{ name: '/', path: '/' }]
    const segments = p.split('/').filter(Boolean)
    const result = [{ name: '/', path: '/' }]
    let accumulated = ''
    for (const seg of segments) {
      accumulated += '/' + seg
      result.push({ name: seg, path: accumulated })
    }
    return result
  })()

  return (
    <div className="space-y-4">
      {/* Quick navigation bookmarks & Actions */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-1.5 text-xs">
          <span className="text-muted-foreground mr-1">Shortcuts:</span>
          <Button
            variant="secondary"
            size="sm"
            className="h-7 px-2.5 text-xs"
            onClick={() => handleNavigate('~')}
          >
            <Home className="size-3.5" aria-hidden />
            Home (~)
          </Button>
          <Button
            variant="secondary"
            size="sm"
            className="h-7 px-2.5 text-xs"
            onClick={() => handleNavigate('/var/lib/docker/volumes')}
          >
            <HardDrive className="size-3.5" aria-hidden />
            Docker Volumes
          </Button>
          <Button
            variant="secondary"
            size="sm"
            className="h-7 px-2.5 text-xs"
            onClick={() => handleNavigate('/etc')}
          >
            /etc
          </Button>
          <Button
            variant="secondary"
            size="sm"
            className="h-7 px-2.5 text-xs"
            onClick={() => handleNavigate('/var/log')}
          >
            /var/log
          </Button>
          <Button
            variant="secondary"
            size="sm"
            className="h-7 px-2.5 text-xs"
            onClick={() => handleNavigate('/')}
          >
            Root (/)
          </Button>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Sudo status / elevation button */}
          {isElevated ? (
            <Badge
              variant="outline"
              className="h-8 gap-1.5 px-2.5 text-xs font-medium text-emerald-500 border-emerald-500/30 bg-emerald-500/10 cursor-pointer"
              onClick={() => setSudoDialogOpen(true)}
              title="Sudo root privileges active. Click to update sudo password."
            >
              <ShieldCheck className="size-3.5" />
              Sudo Elevated
            </Badge>
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="h-8 text-xs text-amber-500 border-amber-500/30 hover:bg-amber-500/10"
              onClick={() => setSudoDialogOpen(true)}
              title="Provide sudo password to view and manage root-restricted folders"
            >
              <KeyRound className="size-3.5 mr-1" />
              Elevate (Root)
            </Button>
          )}

          {/* Storage Scanner Button */}
          <Button
            variant="outline"
            size="sm"
            className="h-8 text-xs"
            onClick={() => {
              setScanPath(listing?.path || currentPath)
              setScannerOpen(true)
            }}
            title="Scan directory for high-storage consumption files and folders"
          >
            <PieChart className="size-3.5 mr-1 text-primary" />
            Storage Scanner
          </Button>

          {canWrite && (
            <>
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => {
                  setNewFolderName('')
                  setMkdirDialogOpen(true)
                }}
              >
                <FolderPlus className="size-3.5 mr-1" aria-hidden />
                New Folder
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => {
                  setEditingFilePath(null)
                  setIsNewFile(true)
                  setEditorOpen(true)
                }}
              >
                <FilePlus className="size-3.5 mr-1" aria-hidden />
                New File
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => {
                  setUploadDialogOpen(true)
                  upload.reset()
                }}
              >
                <Upload className="size-3.5 mr-1" aria-hidden />
                Upload File
              </Button>
            </>
          )}

          <Button
            asChild
            variant="outline"
            size="sm"
            className="h-8 text-xs"
            title="Download current directory as .tar.gz archive"
          >
            <a
              href={serverArchiveDownloadURL(server.id, listing?.path || currentPath)}
              download
            >
              <FolderArchive className="size-3.5 mr-1" aria-hidden />
              Archive Dir
            </a>
          </Button>

          <Button
            variant="ghost"
            size="sm"
            className="h-8 w-8 p-0"
            onClick={() => void refetch()}
            disabled={isFetching}
            title="Refresh directory listing"
          >
            <RefreshCw
              className={`size-3.5 ${isFetching ? 'animate-spin' : ''}`}
              aria-hidden
            />
          </Button>
        </div>
      </div>

      {/* Path Bar & Breadcrumbs */}
      <div className="flex items-center gap-2 rounded-md border bg-muted/40 p-2 text-sm">
        {listing?.parent ? (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs"
            onClick={() => handleNavigate(listing.parent)}
            title="Go to parent directory"
          >
            <FolderUp className="size-4" aria-hidden />
            Up
          </Button>
        ) : null}

        {isEditingPath ? (
          <form onSubmit={handlePathSubmit} className="flex flex-1 items-center gap-2">
            <Input
              value={pathInput}
              onChange={(e) => setPathInput(e.target.value)}
              className="h-8 font-mono text-xs"
              autoFocus
              onBlur={() => setIsEditingPath(false)}
            />
            <Button type="submit" size="sm" className="h-8 text-xs">
              Go
            </Button>
          </form>
        ) : (
          <div
            className="flex flex-1 flex-wrap items-center gap-1 font-mono text-xs cursor-pointer select-none"
            onClick={() => {
              setPathInput(listing?.path || currentPath)
              setIsEditingPath(true)
            }}
            title="Click to manually edit path"
          >
            {pathParts.map((part, index) => (
              <span key={part.path} className="flex items-center gap-1">
                {index > 0 && <ChevronRight className="size-3 text-muted-foreground" />}
                <button
                  type="button"
                  className="rounded px-1.5 py-0.5 hover:bg-muted text-foreground font-semibold"
                  onClick={(e) => {
                    e.stopPropagation()
                    handleNavigate(part.path)
                  }}
                >
                  {part.name}
                </button>
              </span>
            ))}
          </div>
        )}
      </div>

      {/* Listing Content */}
      {isError ? (
        <Alert variant={isSudoRequired ? 'default' : 'danger'}>
          <div className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <Shield className="size-4 text-amber-500 shrink-0" />
              <span className="font-medium text-sm">
                {isSudoRequired
                  ? 'Root/Sudo permissions required to access this path.'
                  : `Failed to read path: ${error.message}`}
              </span>
            </div>
            {isSudoRequired && (
              <p className="text-xs text-muted-foreground">
                Path <span className="font-mono">{currentPath}</span> is protected by system root
                permissions (e.g. Docker volumes or /etc). Provide your sudo password to view and manage its files.
              </p>
            )}
            <div className="flex flex-wrap items-center gap-2 pt-1">
              <Button
                variant="default"
                size="sm"
                className="h-8 text-xs"
                onClick={() => setSudoDialogOpen(true)}
              >
                <KeyRound className="size-3.5 mr-1.5" />
                Provide Sudo Password
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => handleNavigate('~')}
              >
                Return to Home (~)
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => handleNavigate('/')}
              >
                Go to Root (/)
              </Button>
            </div>
          </div>
        </Alert>
      ) : isPending ? (
        <div className="space-y-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : listing.entries.length === 0 ? (
        <EmptyState
          icon={Folder}
          title="Directory is empty"
          description="There are no files or subdirectories inside this folder."
        />
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Size</TableHead>
                <TableHead>Permissions</TableHead>
                <TableHead>Modified</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {listing.entries.map((entry) => (
                <TableRow key={entry.path} className="group">
                  <TableCell className="font-mono text-xs">
                    <div className="flex items-center gap-2">
                      {entry.is_dir ? (
                        <Folder className="size-4 text-primary shrink-0" aria-hidden />
                      ) : entry.name.endsWith('.tar.gz') ||
                        entry.name.endsWith('.zip') ||
                        entry.name.endsWith('.tgz') ? (
                        <FileArchive
                          className="size-4 text-amber-500 shrink-0"
                          aria-hidden
                        />
                      ) : (
                        <File className="size-4 text-muted-foreground shrink-0" aria-hidden />
                      )}

                      {entry.is_dir ? (
                        <button
                          type="button"
                          className="hover:underline font-semibold text-foreground text-left"
                          onClick={() => handleNavigate(entry.path)}
                        >
                          {entry.name}
                        </button>
                      ) : (
                        <button
                          type="button"
                          className="hover:underline text-foreground text-left font-mono font-medium"
                          onClick={() => {
                            setEditingFilePath(entry.path)
                            setIsNewFile(false)
                            setEditorOpen(true)
                          }}
                          title={canWrite ? 'Click to edit file' : 'Click to view file'}
                        >
                          {entry.name}
                        </button>
                      )}

                      {entry.is_symlink && (
                        <span className="text-xs text-muted-foreground font-sans italic">
                          (symlink)
                        </span>
                      )}
                    </div>
                  </TableCell>

                  <TableCell className="tabular text-xs text-muted-foreground">
                    {entry.is_dir ? '--' : formatBytes(entry.size)}
                  </TableCell>

                  <TableCell className="font-mono text-xs text-muted-foreground">
                    <button
                      type="button"
                      className="hover:underline hover:text-foreground inline-flex items-center gap-1"
                      onClick={() => canWrite && openChmodModal(entry)}
                      title={canWrite ? 'Click to change permissions (chmod)' : entry.permissions}
                    >
                      <Lock className="size-3 text-muted-foreground/60" />
                      {entry.permissions}
                    </button>
                  </TableCell>

                  <TableCell className="text-xs text-muted-foreground">
                    {formatRelative(entry.mod_time)}
                  </TableCell>

                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1 opacity-80 group-hover:opacity-100 transition-opacity">
                      {!entry.is_dir && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          onClick={() => {
                            setEditingFilePath(entry.path)
                            setIsNewFile(false)
                            setEditorOpen(true)
                          }}
                          title={canWrite ? 'Edit file in dashboard' : 'View file'}
                        >
                          <FileEdit className="size-3.5 mr-1" aria-hidden />
                          {canWrite ? 'Edit' : 'View'}
                        </Button>
                      )}

                      {canWrite && (
                        <>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 w-7 p-0"
                            onClick={() => openCopyModal(entry)}
                            title="Copy path or file"
                          >
                            <Copy className="size-3.5 text-muted-foreground" aria-hidden />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 w-7 p-0"
                            onClick={() => openMoveModal(entry)}
                            title="Move or rename"
                          >
                            <Move className="size-3.5 text-muted-foreground" aria-hidden />
                          </Button>
                        </>
                      )}

                      {entry.is_dir ? (
                        <Button
                          asChild
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0"
                          title="Download folder as .tar.gz archive"
                        >
                          <a href={serverArchiveDownloadURL(server.id, entry.path)} download>
                            <Download className="size-3.5 text-muted-foreground" aria-hidden />
                          </a>
                        </Button>
                      ) : (
                        <Button
                          asChild
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0"
                          title="Download single file"
                        >
                          <a href={serverFileDownloadURL(server.id, entry.path)} download>
                            <Download className="size-3.5 text-muted-foreground" aria-hidden />
                          </a>
                        </Button>
                      )}

                      {canWrite && destinationServers.length > 0 && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0"
                          onClick={() => openTransferModal(entry)}
                          title="Transfer directly to another server"
                        >
                          <Send className="size-3.5 text-muted-foreground" aria-hidden />
                        </Button>
                      )}

                      {canWrite && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0 text-destructive/70 hover:text-destructive hover:bg-destructive/10"
                          onClick={() => setDeleteTarget(entry)}
                          title="Delete item"
                        >
                          <Trash2 className="size-3.5" aria-hidden />
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <div className="flex items-center justify-between border-t px-4 py-2 text-xs text-muted-foreground bg-muted/20">
            <span>
              {listing.total_dirs} directories, {listing.total_files} files
            </span>
            <span>Total size: {formatBytes(listing.total_bytes)}</span>
          </div>
        </div>
      )}

      {/* Sudo Elevation Modal */}
      <Dialog open={sudoDialogOpen} onOpenChange={setSudoDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Shield className="size-5 text-amber-500" />
              Sudo Root Elevation
            </DialogTitle>
            <DialogDescription>
              Enter the sudo/root password for user <strong>{server.username}</strong> on{' '}
              <strong>{server.name}</strong> to manage root-protected files and Docker volumes.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleSudoSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="sudo-pass">Sudo Password</Label>
              <Input
                id="sudo-pass"
                type="password"
                value={sudoInput}
                onChange={(e) => setSudoInput(e.target.value)}
                placeholder="Enter sudo password"
                autoFocus
                required
              />
            </div>

            <div className="flex items-center space-x-2 pt-1">
              <input
                id="save-sudo-check"
                type="checkbox"
                checked={saveSudoPassword}
                onChange={(e) => setSaveSudoPassword(e.target.checked)}
                className="size-4 rounded border-input text-primary focus:ring-primary"
              />
              <Label htmlFor="save-sudo-check" className="text-xs cursor-pointer font-normal">
                Save sudo password securely on this server for future sessions
              </Label>
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setSudoDialogOpen(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={!sudoInput.trim()}>
                Unlock Root Access
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Create New Folder Modal */}
      <Dialog open={mkdirDialogOpen} onOpenChange={setMkdirDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create New Directory</DialogTitle>
            <DialogDescription>
              Create a new folder inside{' '}
              <span className="font-mono">{listing?.path || currentPath}</span>.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleCreateFolder} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="folder-name">Directory Name</Label>
              <Input
                id="folder-name"
                value={newFolderName}
                onChange={(e) => setNewFolderName(e.target.value)}
                placeholder="e.g. my-app-data"
                autoFocus
                required
              />
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setMkdirDialogOpen(false)}
                disabled={mkdirMutation.isPending}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={!newFolderName.trim() || mkdirMutation.isPending}>
                {mkdirMutation.isPending && <Loader2 className="animate-spin mr-2 size-4" />}
                Create Directory
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation Modal */}
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-destructive flex items-center gap-2">
              <Trash2 className="size-5" />
              Confirm Deletion
            </DialogTitle>
            <DialogDescription>
              Are you sure you want to permanently delete this {deleteTarget?.is_dir ? 'directory and all its contents' : 'file'}?
            </DialogDescription>
          </DialogHeader>

          <div className="rounded-md border border-destructive/20 bg-destructive/5 p-3 text-xs font-mono break-all">
            {deleteTarget?.path}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setDeleteTarget(null)}
              disabled={deleteMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeleteConfirm}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending && <Loader2 className="animate-spin mr-2 size-4" />}
              Delete Permanently
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Copy Modal */}
      <Dialog open={Boolean(copyTarget)} onOpenChange={(open) => !open && setCopyTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Copy {copyTarget?.is_dir ? 'Directory' : 'File'}</DialogTitle>
            <DialogDescription>
              Specify the destination path for the copy operation.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleCopySubmit} className="space-y-4">
            <div className="space-y-2">
              <Label>Source Path</Label>
              <div className="rounded border bg-muted/30 p-2 font-mono text-xs break-all">
                {copyTarget?.path}
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="copy-dest">Destination Path</Label>
              <Input
                id="copy-dest"
                value={copyDest}
                onChange={(e) => setCopyDest(e.target.value)}
                placeholder="/path/to/destination"
                className="font-mono text-xs"
                required
              />
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setCopyTarget(null)}
                disabled={copyMutation.isPending}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={!copyDest.trim() || copyMutation.isPending}>
                {copyMutation.isPending && <Loader2 className="animate-spin mr-2 size-4" />}
                Copy Item
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Move / Rename Modal */}
      <Dialog open={Boolean(moveTarget)} onOpenChange={(open) => !open && setMoveTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Move or Rename {moveTarget?.is_dir ? 'Directory' : 'File'}</DialogTitle>
            <DialogDescription>
              Enter the new name or target destination path.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleMoveSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label>Current Path</Label>
              <div className="rounded border bg-muted/30 p-2 font-mono text-xs break-all">
                {moveTarget?.path}
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="move-dest">New Path / Name</Label>
              <Input
                id="move-dest"
                value={moveDest}
                onChange={(e) => setMoveDest(e.target.value)}
                placeholder="/path/to/new-name"
                className="font-mono text-xs"
                required
              />
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setMoveTarget(null)}
                disabled={moveMutation.isPending}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={!moveDest.trim() || moveMutation.isPending}>
                {moveMutation.isPending && <Loader2 className="animate-spin mr-2 size-4" />}
                Move / Rename
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Permissions (chmod) Modal */}
      <Dialog open={Boolean(chmodTarget)} onOpenChange={(open) => !open && setChmodTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Change Permissions (chmod)</DialogTitle>
            <DialogDescription>
              Set octal permission mode for{' '}
              <span className="font-mono font-semibold">{chmodTarget?.name}</span>.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleChmodSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label>Target</Label>
              <div className="rounded border bg-muted/30 p-2 font-mono text-xs break-all">
                {chmodTarget?.path} ({chmodTarget?.permissions})
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="chmod-mode">Octal Mode</Label>
              <Input
                id="chmod-mode"
                value={chmodMode}
                onChange={(e) => setChmodMode(e.target.value)}
                placeholder="0755"
                className="font-mono text-xs"
                required
              />
            </div>

            <div className="flex flex-wrap gap-1.5 pt-1">
              <span className="text-xs text-muted-foreground mr-1 self-center">Presets:</span>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                className="h-6 text-[11px] px-2 font-mono"
                onClick={() => setChmodMode('0755')}
              >
                755 (Dir default)
              </Button>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                className="h-6 text-[11px] px-2 font-mono"
                onClick={() => setChmodMode('0644')}
              >
                644 (File default)
              </Button>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                className="h-6 text-[11px] px-2 font-mono"
                onClick={() => setChmodMode('0700')}
              >
                700 (Private)
              </Button>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                className="h-6 text-[11px] px-2 font-mono"
                onClick={() => setChmodMode('0777')}
              >
                777 (Full access)
              </Button>
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setChmodTarget(null)}
                disabled={chmodMutation.isPending}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={!chmodMode.trim() || chmodMutation.isPending}>
                {chmodMutation.isPending && <Loader2 className="animate-spin mr-2 size-4" />}
                Apply Permissions
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Storage Scanner Modal */}
      <Dialog open={scannerOpen} onOpenChange={setScannerOpen}>
        <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <PieChart className="size-5 text-primary" />
              Storage & Large File Scanner
            </DialogTitle>
            <DialogDescription>
              Discover high-resource consumption directories and files consuming disk space on{' '}
              <strong>{server.name}</strong>.
            </DialogDescription>
          </DialogHeader>

          <div className="flex flex-wrap items-center gap-2 border-b pb-3">
            <div className="flex-1 min-w-[200px]">
              <Label className="text-xs text-muted-foreground mb-1 block">Scan Path</Label>
              <Input
                value={scanPath}
                onChange={(e) => setScanPath(e.target.value)}
                className="h-8 font-mono text-xs"
                placeholder="/"
              />
            </div>
            <div className="w-[130px]">
              <Label className="text-xs text-muted-foreground mb-1 block">Min File Size</Label>
              <Select
                value={scanMinMB.toString()}
                onValueChange={(val) => setScanMinMB(parseInt(val, 10))}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="10">&gt; 10 MB</SelectItem>
                  <SelectItem value="50">&gt; 50 MB</SelectItem>
                  <SelectItem value="100">&gt; 100 MB</SelectItem>
                  <SelectItem value="500">&gt; 500 MB</SelectItem>
                  <SelectItem value="1024">&gt; 1 GB</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="self-end">
              <Button
                size="sm"
                className="h-8 text-xs"
                onClick={() => {
                  void scanQuery.refetch()
                }}
                disabled={scanQuery.isFetching}
              >
                {scanQuery.isFetching ? (
                  <Loader2 className="animate-spin mr-1.5 size-3.5" />
                ) : (
                  <RefreshCw className="mr-1.5 size-3.5" />
                )}
                Scan Now
              </Button>
            </div>
          </div>

          <div className="flex-1 overflow-y-auto min-h-[300px] py-2">
            {scanQuery.isFetching ? (
              <div className="flex flex-col items-center justify-center py-16 text-muted-foreground gap-3">
                <Loader2 className="size-8 animate-spin text-primary" />
                <p className="text-xs">Analyzing disk usage with du and find...</p>
              </div>
            ) : scanQuery.isError ? (
              <Alert variant="danger">
                <span>Failed to scan storage: {scanQuery.error.message}</span>
                {scanQuery.error.fields?.sudo_password && (
                  <div className="mt-2">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setSudoDialogOpen(true)}
                    >
                      <KeyRound className="size-3.5 mr-1" />
                      Provide Sudo Password
                    </Button>
                  </div>
                )}
              </Alert>
            ) : scanQuery.data ? (() => {
              const topDirs = scanQuery.data.top_large_dirs ?? scanQuery.data.top_directories ?? []
              const topFiles = scanQuery.data.top_large_files ?? scanQuery.data.largest_files ?? []
              return (
                <Tabs defaultValue="dirs" className="w-full">
                  <TabsList className="grid w-full grid-cols-2 mb-3">
                    <TabsTrigger value="dirs">
                      Top Directories ({topDirs.length})
                    </TabsTrigger>
                    <TabsTrigger value="files">
                      Largest Files ({topFiles.length})
                    </TabsTrigger>
                  </TabsList>

                  <TabsContent value="dirs" className="space-y-2">
                    {topDirs.length === 0 ? (
                      <p className="text-center text-xs text-muted-foreground py-8">
                        No large directories found under this path.
                      </p>
                    ) : (
                      topDirs.map((dir) => (
                        <div
                          key={dir.path}
                          className="flex items-center justify-between p-2.5 rounded border bg-muted/20 hover:bg-muted/40 transition-colors text-xs font-mono"
                        >
                          <div className="flex items-center gap-2 min-w-0 mr-2">
                            <Folder className="size-4 text-primary shrink-0" />
                            <span className="truncate text-foreground" title={dir.path}>
                              {dir.path}
                            </span>
                          </div>
                          <div className="flex items-center gap-2 shrink-0">
                            <Badge variant="outline" className="font-mono text-[11px]">
                              {dir.size_human || dir.human_size || ''}
                            </Badge>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-7 text-xs font-sans"
                              onClick={() => {
                                handleNavigate(dir.path)
                                setScannerOpen(false)
                              }}
                            >
                              Jump Here
                            </Button>
                          </div>
                        </div>
                      ))
                    )}
                  </TabsContent>

                  <TabsContent value="files" className="space-y-2">
                    {topFiles.length === 0 ? (
                      <p className="text-center text-xs text-muted-foreground py-8">
                        No files exceeding {scanMinMB}MB found under this path.
                      </p>
                    ) : (
                      topFiles.map((file) => (
                        <div
                          key={file.path}
                          className="flex items-center justify-between p-2.5 rounded border bg-muted/20 hover:bg-muted/40 transition-colors text-xs font-mono"
                        >
                          <div className="flex items-center gap-2 min-w-0 mr-2">
                            <File className="size-4 text-muted-foreground shrink-0" />
                            <span className="truncate text-foreground" title={file.path}>
                              {file.path}
                            </span>
                          </div>
                          <div className="flex items-center gap-2 shrink-0">
                            <Badge variant="outline" className="font-mono text-[11px]">
                              {file.size_human || file.human_size || ''}
                            </Badge>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-7 text-xs font-sans"
                              onClick={() => {
                                setEditingFilePath(file.path)
                                setIsNewFile(false)
                                setEditorOpen(true)
                              }}
                            >
                              View
                            </Button>
                            {canWrite && (
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-7 w-7 p-0 text-destructive/70 hover:text-destructive hover:bg-destructive/10"
                                onClick={() =>
                                  setDeleteTarget({
                                    name: file.path.split('/').pop() || file.path,
                                    path: file.path,
                                    size: file.size_bytes ?? file.size ?? 0,
                                    mode: '',
                                    is_dir: false,
                                    is_symlink: false,
                                    mod_time: '',
                                    permissions: '',
                                  })
                                }
                                title="Delete file"
                              >
                                <Trash2 className="size-3.5" />
                              </Button>
                            )}
                          </div>
                        </div>
                      ))
                    )}
                  </TabsContent>
                </Tabs>
              )
            })() : null}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setScannerOpen(false)}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Upload Modal */}
      <Dialog open={uploadDialogOpen} onOpenChange={setUploadDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Upload File to Server</DialogTitle>
            <DialogDescription>
              Upload a file directly to <span className="font-mono">{listing?.path || currentPath}</span> on{' '}
              <strong>{server.name}</strong>.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleStartUpload} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="upload-file">Select File</Label>
              <Input
                id="upload-file"
                ref={fileInputRef}
                type="file"
                required
                onChange={(e) => setUploadFile(e.target.files?.[0] || null)}
              />
            </div>

            {upload.isError && (
              <Alert variant="danger">{upload.error.message}</Alert>
            )}

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setUploadDialogOpen(false)}
                disabled={upload.isPending}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={!uploadFile || upload.isPending}>
                {upload.isPending && <Loader2 className="animate-spin mr-2 size-4" />}
                {upload.isPending ? 'Uploading...' : 'Upload'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Server-to-Server Transfer Modal */}
      <Dialog
        open={Boolean(transferItem)}
        onOpenChange={(open) => {
          if (!open) {
            setTransferItem(null)
            setTransferResult(null)
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Transfer to Another Server</DialogTitle>
            <DialogDescription>
              Directly stream {transferItem?.is_dir ? 'directory' : 'file'}{' '}
              <span className="font-mono font-semibold">{transferItem?.name}</span> from{' '}
              <strong>{server.name}</strong> to another destination server.
            </DialogDescription>
          </DialogHeader>

          {transferResult ? (
            <div className="space-y-4 py-2">
              <Alert variant="info">
                <div className="space-y-1">
                  <div className="font-semibold text-sm">Transfer Completed Successfully!</div>
                  <div className="text-xs">
                    Transferred {formatBytes(transferResult.bytes_copied)} in{' '}
                    {(transferResult.duration_ms / 1000).toFixed(2)}s to{' '}
                    <span className="font-mono">{transferResult.target_path}</span>.
                  </div>
                </div>
              </Alert>

              <DialogFooter>
                <Button
                  onClick={() => {
                    setTransferItem(null)
                    setTransferResult(null)
                  }}
                >
                  Close
                </Button>
              </DialogFooter>
            </div>
          ) : (
            <form onSubmit={handleStartTransfer} className="space-y-4">
              <div className="space-y-2">
                <Label>Source Server & Path</Label>
                <div className="rounded border bg-muted/40 p-2.5 text-xs font-mono">
                  <div>
                    <span className="text-muted-foreground">Server:</span> {server.name} ({server.host})
                  </div>
                  <div className="mt-1 truncate">
                    <span className="text-muted-foreground">Path:</span> {transferItem?.path}
                  </div>
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="dest-server">Destination Server</Label>
                <Select value={targetServerID} onValueChange={setTargetServerID}>
                  <SelectTrigger id="dest-server">
                    <SelectValue placeholder="Select target server" />
                  </SelectTrigger>
                  <SelectContent>
                    {destinationServers.map((s) => (
                      <SelectItem key={s.id} value={s.id}>
                        {s.name} ({s.host})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label htmlFor="dest-path">Destination Path</Label>
                <Input
                  id="dest-path"
                  value={targetPath}
                  onChange={(e) => setTargetPath(e.target.value)}
                  placeholder="e.g. /home/user or /var/lib/docker/volumes"
                  className="font-mono text-xs"
                  required
                />
                <span className="text-[11px] text-muted-foreground">
                  The directory on the target server where contents will be extracted/written.
                </span>
              </div>

              {transfer.isError && (
                <Alert variant="danger">{transfer.error.message}</Alert>
              )}

              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setTransferItem(null)}
                  disabled={transfer.isPending}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={!targetServerID || !targetPath.trim() || transfer.isPending}
                >
                  {transfer.isPending ? (
                    <Loader2 className="animate-spin mr-2 size-4" />
                  ) : (
                    <ArrowRight className="mr-2 size-4" />
                  )}
                  {transfer.isPending ? 'Transferring...' : 'Start Transfer'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      {/* File Editor Modal */}
      <ServerFileEditor
        server={server}
        filePath={editingFilePath}
        isOpen={editorOpen}
        onClose={() => {
          setEditorOpen(false)
          setEditingFilePath(null)
          setIsNewFile(false)
        }}
        canWrite={canWrite}
        isNewFile={isNewFile}
        initialDirectory={listing?.path || currentPath}
        sudoPassword={sudoPassword}
        onSaved={() => void refetch()}
      />
    </div>
  )
}
