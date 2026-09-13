import {
  ArrowRight,
  ChevronRight,
  Download,
  File,
  FileArchive,
  FileEdit,
  FilePlus,
  Folder,
  FolderArchive,
  FolderUp,
  HardDrive,
  Home,
  Loader2,
  RefreshCw,
  Send,
  Upload,
} from 'lucide-react'
import { useRef, useState, type FormEvent } from 'react'

import { ServerFileEditor } from './ServerFileEditor'
import { EmptyState } from '@/components/EmptyState'
import { Alert } from '@/components/ui/alert'
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
import {
  serverArchiveDownloadURL,
  serverFileDownloadURL,
  useDirectoryListing,
  useServers,
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

  // Transfer dialog state
  const [transferItem, setTransferItem] = useState<FileEntry | null>(null)
  const [targetServerID, setTargetServerID] = useState('')
  const [targetPath, setTargetPath] = useState('')
  const [transferResult, setTransferResult] = useState<FileTransferResult | null>(null)

  // Queries & Mutations
  const { data: listing, isPending, isError, error, refetch, isFetching } = useDirectoryListing(
    server.id,
    currentPath,
  )
  const { data: allServers = [] } = useServers()
  const upload = useUploadFile(server.id)
  const transfer = useTransferFile()

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
          void refetch()
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
        </div>

        <div className="flex items-center gap-2">
          {canWrite && (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setEditingFilePath(null)
                  setIsNewFile(true)
                  setEditorOpen(true)
                }}
              >
                <FilePlus className="size-3.5" aria-hidden />
                New File
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setUploadDialogOpen(true)
                  upload.reset()
                }}
              >
                <Upload className="size-3.5" aria-hidden />
                Upload File
              </Button>
            </>
          )}

          <Button
            asChild
            variant="outline"
            size="sm"
            title="Download current directory as .tar.gz archive"
          >
            <a
              href={serverArchiveDownloadURL(server.id, listing?.path || currentPath)}
              download
            >
              <FolderArchive className="size-3.5" aria-hidden />
              Archive Dir
            </a>
          </Button>

          <Button
            variant="ghost"
            size="sm"
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
            <Button type="submit" size="sm" className="h-8">
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
                  className="rounded px-1.5 py-0.5 hover:bg-accent hover:text-accent-foreground font-medium text-foreground"
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
        <Alert variant="danger">
          <div className="flex flex-col gap-2">
            <span>Failed to read path: {error.message}</span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                className="w-fit"
                onClick={() => handleNavigate('~')}
              >
                Return to Home (~)
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="w-fit"
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
                    {entry.permissions}
                  </TableCell>

                  <TableCell className="text-xs text-muted-foreground">
                    {formatRelative(entry.mod_time)}
                  </TableCell>

                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1.5 opacity-80 group-hover:opacity-100 transition-opacity">
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

                      {entry.is_dir ? (
                        <Button
                          asChild
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          title="Download folder as .tar.gz archive"
                        >
                          <a href={serverArchiveDownloadURL(server.id, entry.path)} download>
                            <Download className="size-3.5 mr-1" aria-hidden />
                            Archive
                          </a>
                        </Button>
                      ) : (
                        <Button
                          asChild
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          title="Download single file"
                        >
                          <a href={serverFileDownloadURL(server.id, entry.path)} download>
                            <Download className="size-3.5 mr-1" aria-hidden />
                            Download
                          </a>
                        </Button>
                      )}

                      {canWrite && destinationServers.length > 0 && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          onClick={() => openTransferModal(entry)}
                          title="Transfer directly to another server"
                        >
                          <Send className="size-3.5 mr-1" aria-hidden />
                          Transfer
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
        onSaved={() => void refetch()}
      />
    </div>
  )
}
