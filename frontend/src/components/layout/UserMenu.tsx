import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { ChevronsUpDown, LogOut, Settings, User } from 'lucide-react'
import { useNavigate } from 'react-router'

import { ROLE_LABELS, useLogout, useSession } from '@/lib/session'
import { cn } from '@/lib/utils'

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).slice(0, 2)
  return parts.map((part) => part[0]?.toUpperCase() ?? '').join('') || '?'
}

export function UserMenu({ compact = false }: { compact?: boolean }) {
  const navigate = useNavigate()
  const { data: user } = useSession()
  const logout = useLogout()

  if (!user) return null

  const itemClass =
    'flex w-full cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground'

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger
        className={cn(
          'hover:bg-accent/50 focus-visible:outline-ring flex items-center rounded-md transition-colors focus-visible:outline-2',
          compact ? 'p-1' : 'w-full gap-2 px-1.5 py-1.5 text-left',
        )}
        aria-label="User menu"
      >
        <span className="bg-primary/15 text-primary flex size-7 shrink-0 items-center justify-center rounded-full text-xs font-medium">
          {initials(user.name)}
        </span>
        {!compact && (
          <>
            <span className="min-w-0 flex-1 leading-tight">
              <span className="block truncate text-xs font-medium">{user.name}</span>
              <span className="text-muted-foreground block truncate text-[11px]">
                {ROLE_LABELS[user.role]}
              </span>
            </span>
            <ChevronsUpDown className="text-muted-foreground size-3.5 shrink-0" aria-hidden />
          </>
        )}
      </DropdownMenu.Trigger>

      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align={compact ? 'end' : 'start'}
          sideOffset={6}
          className="bg-popover text-popover-foreground z-50 min-w-56 rounded-md border p-1 shadow-md"
        >
          <div className="px-2 py-1.5">
            <p className="truncate text-sm font-medium">{user.name}</p>
            <p className="text-muted-foreground truncate font-mono text-xs">{user.email}</p>
          </div>
          <DropdownMenu.Separator className="bg-border my-1 h-px" />

          <DropdownMenu.Item className={itemClass} onSelect={() => void navigate('/settings/profile')}>
            <User className="size-4" aria-hidden />
            Your profile
          </DropdownMenu.Item>
          <DropdownMenu.Item className={itemClass} onSelect={() => void navigate('/settings')}>
            <Settings className="size-4" aria-hidden />
            Settings
          </DropdownMenu.Item>

          <DropdownMenu.Separator className="bg-border my-1 h-px" />
          <DropdownMenu.Item
            className={cn(itemClass, 'text-destructive')}
            disabled={logout.isPending}
            onSelect={() =>
              logout.mutate(undefined, { onSettled: () => void navigate('/login', { replace: true }) })
            }
          >
            <LogOut className="size-4" aria-hidden />
            Sign out
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}
