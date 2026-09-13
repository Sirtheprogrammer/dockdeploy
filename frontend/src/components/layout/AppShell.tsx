import { Boxes, Globe, LayoutDashboard, Menu, Rocket, Server, Settings, X } from 'lucide-react'
import { useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router'

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
  const [drawerOpen, setDrawerOpen] = useState(false)
  const location = useLocation()

  return (
    <div className="bg-surface flex min-h-screen">
      <a
        href="#main"
        className="bg-primary text-primary-foreground sr-only rounded-md px-3 py-2 text-sm focus:not-sr-only focus:absolute focus:top-3 focus:left-3 focus:z-50"
      >
        Skip to content
      </a>

      {/* Desktop Sidebar */}
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
        {/* Mobile Top Header */}
        <header className="bg-background/95 backdrop-blur sticky top-0 z-30 flex h-14 items-center justify-between border-b px-4 md:hidden">
          <div className="flex items-center gap-2.5">
            <span className="bg-primary/12 text-primary flex size-8 shrink-0 items-center justify-center rounded-lg">
              <Boxes className="size-4.5" aria-hidden />
            </span>
            <div className="leading-none">
              <span className="text-sm font-semibold tracking-tight">dockdeploy</span>
              <span className="text-muted-foreground block text-[10px]">Control plane</span>
            </div>
          </div>

          <div className="flex items-center gap-1.5">
            <UserMenu compact />
            <button
              type="button"
              onClick={() => setDrawerOpen(!drawerOpen)}
              className="hover:bg-accent focus-visible:outline-ring flex size-9 items-center justify-center rounded-md text-zinc-400 transition-colors hover:text-zinc-100 focus-visible:outline-2"
              aria-label={drawerOpen ? 'Close navigation' : 'Open navigation'}
            >
              {drawerOpen ? <X className="size-5" /> : <Menu className="size-5" />}
            </button>
          </div>
        </header>

        {/* Mobile Slide-Over Navigation Drawer */}
        {drawerOpen && (
          <div className="fixed inset-0 z-50 md:hidden">
            <div
              className="fixed inset-0 bg-black/60 backdrop-blur-xs animate-in fade-in"
              onClick={() => setDrawerOpen(false)}
            />
            <div className="bg-background fixed inset-y-0 right-0 z-50 flex w-72 flex-col border-l shadow-2xl animate-in slide-in-from-right duration-200">
              <div className="flex h-14 items-center justify-between border-b px-4">
                <div className="flex items-center gap-2">
                  <span className="bg-primary/12 text-primary flex size-7 items-center justify-center rounded-lg">
                    <Boxes className="size-4" aria-hidden />
                  </span>
                  <span className="text-sm font-semibold">Navigation</span>
                </div>
                <button
                  type="button"
                  onClick={() => setDrawerOpen(false)}
                  className="hover:bg-accent rounded-md p-1.5 text-zinc-400 hover:text-zinc-100"
                >
                  <X className="size-4.5" />
                </button>
              </div>

              <nav className="flex-1 space-y-1 p-3" aria-label="Mobile Main">
                {NAV.map(({ to, label, icon: Icon, ...rest }) => (
                  <NavLink
                    key={to}
                    to={to}
                    end={'end' in rest ? rest.end : undefined}
                    onClick={() => setDrawerOpen(false)}
                    className={({ isActive }) =>
                      cn(
                        'flex items-center gap-3 rounded-lg px-3 py-3 text-sm transition-colors min-h-[44px]',
                        isActive
                          ? 'bg-accent text-accent-foreground font-medium'
                          : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground',
                      )
                    }
                  >
                    <Icon className="size-4.5 shrink-0" aria-hidden />
                    {label}
                  </NavLink>
                ))}
              </nav>

              <div className="space-y-3 border-t p-4 bg-zinc-950/40">
                <UserMenu />
                <div className="border-t pt-3">
                  <HealthIndicator />
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Main Content Area */}
        <main id="main" className="min-w-0 flex-1 pb-20 md:pb-6">
          <Outlet />
        </main>

        {/* Mobile Bottom Navigation Bar (Sleek quick-switch) */}
        <nav
          className="bg-background/95 backdrop-blur-md fixed bottom-0 inset-x-0 z-40 flex h-14 items-center justify-around border-t px-2 md:hidden"
          aria-label="Mobile Quick Nav"
        >
          {NAV.map(({ to, label, icon: Icon, ...rest }) => {
            const isExact = 'end' in rest ? rest.end : false
            const isActive = isExact
              ? location.pathname === to
              : location.pathname === to || location.pathname.startsWith(`${to}/`)
            return (
              <NavLink
                key={to}
                to={to}
                end={'end' in rest ? rest.end : undefined}
                className={cn(
                  'flex flex-col items-center justify-center flex-1 h-full py-1 text-[10px] font-medium transition-colors min-w-0',
                  isActive
                    ? 'text-primary'
                    : 'text-muted-foreground hover:text-foreground',
                )}
              >
                <Icon className={cn('size-4.5 transition-transform', isActive && 'scale-110')} aria-hidden />
                <span className="truncate mt-0.5 max-w-[64px]">{label}</span>
              </NavLink>
            )
          })}
        </nav>
      </div>
    </div>
  )
}
