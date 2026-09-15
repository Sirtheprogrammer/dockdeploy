import { NavLink, Outlet } from 'react-router'

import { PageHeader } from '@/components/layout/PageHeader'
import { can, useSession } from '@/lib/session'
import { cn } from '@/lib/utils'

export function SettingsLayout() {
  const { data: user } = useSession()

  const tabs = [
    { to: '/settings', label: 'Profile', end: true, show: true },
    { to: '/settings/ai', label: 'AI Assistant', show: true },
    { to: '/settings/tokens', label: 'API tokens', show: true },
    { to: '/settings/credentials', label: 'Credentials', show: can(user, 'credential:read') },
    { to: '/settings/users', label: 'Users', show: can(user, 'user:read') },
    { to: '/settings/audit', label: 'Audit log', show: can(user, 'audit:read') },
  ].filter((tab) => tab.show)

  return (
    <>
      <PageHeader title="Settings" description="Your account and this instance." />

      <div className="w-full max-w-full border-b">
        <div className="flex items-center gap-1 overflow-x-auto px-4 sm:px-6 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden scroll-smooth">
          {tabs.map(({ to, label, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  '-mb-px border-b-2 px-3 sm:px-3.5 py-2.5 text-xs sm:text-sm font-medium whitespace-nowrap transition-colors shrink-0',
                  isActive
                    ? 'border-primary text-foreground font-semibold'
                    : 'text-muted-foreground hover:text-foreground border-transparent',
                )
              }
            >
              {label}
            </NavLink>
          ))}
        </div>
      </div>

      <div className="p-4 sm:p-6 lg:p-8 min-w-0 max-w-full">
        <Outlet />
      </div>
    </>
  )
}
