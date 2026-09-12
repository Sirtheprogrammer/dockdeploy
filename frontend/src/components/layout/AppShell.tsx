import { Boxes, Globe, LayoutDashboard, Rocket, Server, Settings } from 'lucide-react'
import { NavLink, Outlet } from 'react-router'

import { HealthIndicator } from '@/components/HealthIndicator'
import { UserMenu } from '@/components/layout/UserMenu'
import { cn } from '@/lib/utils'

const NAV = [
  { to: '/', label: 'Overview', icon: LayoutDashboard, end: true },
  { to: '/servers', label: 'Servers', icon: Server },
  { to: '/deployments', label: 'Deployments', icon: Rocket },
  { to: '/domains', label: 'Domains', icon: Globe },
  { to: '/settings', label: 'Settings', icon: Settings },
] as const

export function AppShell() {
  return (
    <div className="bg-surface flex min-h-screen">
      <a
        href="#main"
        className="bg-primary text-primary-foreground sr-only rounded-md px-3 py-2 text-sm focus:not-sr-only focus:absolute focus:top-3 focus:left-3 focus:z-50"
      >
        Skip to content
      </a>

      <aside className="bg-background sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r md:flex">
        <div className="flex h-16 items-center gap-3 border-b px-5">
          <span className="bg-primary/12 text-primary flex size-8 items-center justify-center rounded-lg">
            <Boxes className="size-4.5" aria-hidden />
          </span>
          <div className="min-w-0">
            <span className="block text-sm font-semibold tracking-tight">dockdeploy</span>
            <span className="text-muted-foreground block text-[11px]">Control plane</span>
          </div>
        </div>

        <nav className="flex-1 space-y-1 px-3 py-5" aria-label="Main">
          {NAV.map(({ to, label, icon: Icon, ...rest }) => (
            <NavLink
              key={to}
              to={to}
              end={'end' in rest ? rest.end : undefined}
              className={({ isActive }) =>
                cn(
                  'group flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors',
                  isActive
                    ? 'bg-accent text-accent-foreground font-medium shadow-sm'
                    : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground',
                )
              }
            >
              <Icon className="size-4 shrink-0" aria-hidden />
              {label}
            </NavLink>
          ))}
        </nav>

        <div className="space-y-3 border-t p-3">
          <UserMenu />
          <div className="border-t pt-3">
            <HealthIndicator />
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Sidebar is hidden below md; this keeps navigation reachable there. */}
        <header className="bg-background flex h-16 items-center gap-2 border-b px-3 md:hidden">
          <span className="bg-primary/12 text-primary flex size-8 shrink-0 items-center justify-center rounded-lg">
            <Boxes className="size-4" aria-hidden />
          </span>
          <nav className="scrollbar-thin flex flex-1 items-center gap-1 overflow-x-auto" aria-label="Main">
            {NAV.map(({ to, label, ...rest }) => (
              <NavLink
                key={to}
                to={to}
                end={'end' in rest ? rest.end : undefined}
                className={({ isActive }) =>
                  cn(
                    'rounded-lg px-3 py-2 text-sm whitespace-nowrap',
                    isActive ? 'bg-accent text-accent-foreground font-medium' : 'text-muted-foreground',
                  )
                }
              >
                {label}
              </NavLink>
            ))}
          </nav>
          <div className="w-40 shrink-0">
            <UserMenu />
          </div>
        </header>

        <main id="main" className="min-w-0 flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
