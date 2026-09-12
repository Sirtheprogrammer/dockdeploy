import { Route, Routes } from 'react-router'

import { RequireAuth } from '@/components/RequireAuth'
import { AppShell } from '@/components/layout/AppShell'
import { AcceptInvite } from '@/routes/AcceptInvite'
import { DeploymentDetail } from '@/routes/DeploymentDetail'
import { Deployments } from '@/routes/Deployments'
import { Domains } from '@/routes/Domains'
import { Login } from '@/routes/Login'
import { NotFound } from '@/routes/NotFound'
import { Overview } from '@/routes/Overview'
import { ServerDetail } from '@/routes/ServerDetail'
import { Servers } from '@/routes/Servers'
import { Setup } from '@/routes/Setup'
import { Audit } from '@/routes/settings/Audit'
import { Credentials } from '@/routes/settings/Credentials'
import { Profile } from '@/routes/settings/Profile'
import { SettingsLayout } from '@/routes/settings/SettingsLayout'
import { Tokens } from '@/routes/settings/Tokens'
import { Users } from '@/routes/settings/Users'

export default function App() {
  return (
    <Routes>
      {/* Reachable without a session. Each one redirects away once it no
          longer applies, so there is no way to sit on a stale form. */}
      <Route path="/login" element={<Login />} />
      <Route path="/setup" element={<Setup />} />
      <Route path="/invite/:token" element={<AcceptInvite />} />

      <Route element={<RequireAuth />}>
        <Route element={<AppShell />}>
          <Route index element={<Overview />} />
          <Route path="servers" element={<Servers />} />
          <Route path="servers/:serverID" element={<ServerDetail />} />
          <Route path="deployments" element={<Deployments />} />
          <Route path="deployments/:deploymentID" element={<DeploymentDetail />} />
          <Route path="domains" element={<Domains />} />

          {/* Users and Audit are admin-only. The tabs are hidden for other
              roles and the API refuses them regardless, so a direct visit
              shows the error rather than leaking anything. */}
          <Route path="settings" element={<SettingsLayout />}>
            <Route index element={<Profile />} />
            <Route path="profile" element={<Profile />} />
            <Route path="tokens" element={<Tokens />} />
            <Route path="credentials" element={<Credentials />} />
            <Route path="users" element={<Users />} />
            <Route path="audit" element={<Audit />} />
          </Route>

          <Route path="*" element={<NotFound />} />
        </Route>
      </Route>
    </Routes>
  )
}
