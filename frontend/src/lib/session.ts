import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from '@tanstack/react-query'

import { ApiError, api } from '@/lib/api'

export type Role = 'admin' | 'member' | 'viewer'
export type UserStatus = 'active' | 'suspended'

/** Mirrors auth.Permission in the Go server. */
export type Permission =
  | 'self'
  | 'server:read'
  | 'server:write'
  | 'server:delete'
  | 'container:operate'
  | 'deployment:read'
  | 'deployment:write'
  | 'deployment:deploy'
  | 'deployment:delete'
  | 'domain:read'
  | 'domain:write'
  | 'credential:read'
  | 'credential:write'
  | 'user:read'
  | 'user:write'
  | 'audit:read'

export interface User {
  id: string
  email: string
  name: string
  role: Role
  status: UserStatus
  last_login_at: string | null
  totp_enabled: boolean
  created_at: string
  updated_at: string
}

export type LoginResponse = CurrentUser | {
  requires_2fa: true
  temp_token: string
}

export interface CurrentUser extends User {
  permissions: Permission[]
}

export const sessionKey = ['session'] as const
export const setupKey = ['setup'] as const

/**
 * The signed-in user, or null when nobody is signed in.
 *
 * A 401 is a legitimate answer here rather than an error, so it resolves to
 * null and lets the router redirect. Anything else stays an error so a broken
 * backend is not mistaken for a signed-out user.
 */
export function useSession(): UseQueryResult<CurrentUser | null, ApiError> {
  return useQuery<CurrentUser | null, ApiError>({
    queryKey: sessionKey,
    queryFn: async ({ signal }) => {
      try {
        return await api.get<CurrentUser>('/auth/me', { signal })
      } catch (error) {
        if (error instanceof ApiError && error.isAuthFailure) return null
        throw error
      }
    },
    retry: false,
    staleTime: 60_000,
  })
}

/** Whether this instance still needs its first administrator. */
export function useSetupStatus() {
  return useQuery({
    queryKey: setupKey,
    queryFn: ({ signal }) => api.get<{ needed: boolean }>('/auth/setup', { signal }),
    retry: false,
    staleTime: Infinity,
  })
}

/** Checks a permission the server would enforce anyway; this only hides UI. */
export function can(user: CurrentUser | null | undefined, permission: Permission): boolean {
  return user?.permissions.includes(permission) ?? false
}

export const ROLE_LABELS: Record<Role, string> = {
  admin: 'Admin',
  member: 'Member',
  viewer: 'Viewer',
}

export const ROLE_DESCRIPTIONS: Record<Role, string> = {
  admin: 'Full access, including servers, credentials, users and the audit log.',
  member: 'Can operate servers, deployments and domains. No user or credential management.',
  viewer: 'Read-only access to servers, deployments and domains.',
}

export function useLogin() {
  const queryClient = useQueryClient()
  return useMutation<LoginResponse, ApiError, { email: string; password: string }>({
    mutationFn: (body) => api.post<LoginResponse>('/auth/login', body),
    onSuccess: (res) => {
      if ('requires_2fa' in res && res.requires_2fa) {
        return
      }
      queryClient.setQueryData(sessionKey, res)
    },
  })
}

export function useLogin2FA() {
  const queryClient = useQueryClient()
  return useMutation<CurrentUser, ApiError, { temp_token: string; code: string }>({
    mutationFn: (body) => api.post<CurrentUser>('/auth/login/2fa', body),
    onSuccess: (user) => {
      queryClient.setQueryData(sessionKey, user)
    },
  })
}

export function useSetup() {
  const queryClient = useQueryClient()
  return useMutation<CurrentUser, ApiError, { email: string; name: string; password: string }>({
    mutationFn: (body) => api.post<CurrentUser>('/auth/setup', body),
    onSuccess: (user) => {
      queryClient.setQueryData(sessionKey, user)
      queryClient.setQueryData(setupKey, { needed: false })
    },
  })
}

export function useAcceptInvitation(token: string) {
  const queryClient = useQueryClient()
  return useMutation<CurrentUser, ApiError, { name: string; password: string }>({
    mutationFn: (body) => api.post<CurrentUser>(`/auth/invitations/${token}/accept`, body),
    onSuccess: (user) => {
      queryClient.setQueryData(sessionKey, user)
    },
  })
}

export function useLogout() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, void>({
    mutationFn: () => api.post<void>('/auth/logout'),
    onSettled: () => {
      // Clear everything, not just the session: cached servers and deployments
      // belong to the account that just signed out.
      queryClient.clear()
      queryClient.setQueryData(sessionKey, null)
    },
  })
}
