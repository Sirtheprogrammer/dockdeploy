import { NavLink, Outlet } from 'react-router'

import { PageHeader } from '@/components/layout/PageHeader'
import { can, useSession } from '@/lib/session'
import { cn } from '@/lib/utils'

export function SettingsLayout() {
  const { data: user } = useSession()

  const tabs = [
    { to: '/settings', label: 'Profile', end: true, show: true },
    { to: '/settings/tokens', label: 'API tokens', show: true },
    { to: '/settings/credentials', label: 'Credentials', show: can(user, 'credential:read') },
    { to: '/settings/users', label: 'Users', show: can(user, 'user:read') },
    { to: '/settings/audit', label: 'Audit log', show: can(user, 'audit:read') },
  ].filter((tab) => tab.show)

  return (
    <>
      <PageHeader title="Settings" description="Your account and this instance." />

      <div className="scrollbar-thin flex items-center gap-1 overflow-x-auto border-b px-4 sm:px-6">
        {tabs.map(({ to, label, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cn(
                '-mb-px border-b-2 px-3 py-2.5 text-sm font-medium whitespace-nowrap transition-colors',
                isActive
                  ? 'border-primary text-foreground'
                  : 'text-muted-foreground hover:text-foreground border-transparent',
              )
            }
          >
            {label}
          </NavLink>
        ))}
      </div>

      <div className="p-4 sm:p-6 lg:p-8">
        <Outlet />
      </div>
    </>
  )
}
