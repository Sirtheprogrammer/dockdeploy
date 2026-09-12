import { CopyButton } from '@/components/CopyButton'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { Domain } from '@/lib/domains'

interface ViewConfigDialogProps {
  domain: Domain | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ViewConfigDialog({ domain, open, onOpenChange }: ViewConfigDialogProps) {
  if (!domain) return null

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center justify-between pr-6">
            <span>Nginx Configuration: {domain.hostname}</span>
          </DialogTitle>
          <DialogDescription>
            This virtual host configuration is deployed to the server and managed by dockdeploy.
          </DialogDescription>
        </DialogHeader>

        <div className="relative flex-1 overflow-hidden rounded-md border bg-muted/40 font-mono text-xs">
          <div className="absolute right-2 top-2 z-10">
            <CopyButton value={domain.config_rendered || '# No configuration rendered yet'} />
          </div>
          <pre className="max-h-[50vh] overflow-auto p-4 text-xs leading-relaxed">
            <code>{domain.config_rendered || '# No configuration rendered yet'}</code>
          </pre>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
