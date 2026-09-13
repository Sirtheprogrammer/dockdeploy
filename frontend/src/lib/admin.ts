import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, type ApiError } from '@/lib/api'
import { sessionKey, type CurrentUser, type Role, type User, type UserStatus } from '@/lib/session'

// --- users --------------------------------------------------------------

export const usersKey = ['users'] as const

export function useUsers(enabled = true) {
  return useQuery<User[], ApiError>({
    queryKey: usersKey,
    queryFn: async ({ signal }) => (await api.get<{ users: User[] }>('/users', { signal })).users,
    enabled,
  })
}

export interface UpdateUserInput {
  id: string
  name: string
  role: Role
  status: UserStatus
}

export function useUpdateUser() {
  const queryClient = useQueryClient()
  return useMutation<User, ApiError, UpdateUserInput>({
    mutationFn: ({ id, ...body }) => api.patch<User>(`/users/${id}`, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: usersKey })
    },
  })
}

export function useDeleteUser() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.delete<void>(`/users/${id}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: usersKey })
    },
  })
}

// --- invitations --------------------------------------------------------

export const invitationsKey = ['invitations'] as const

export interface Invitation {
  id: string
  email: string
  name: string
  role: Role
  invited_by: string | null
  expires_at: string
  created_at: string
}

export function useInvitations(enabled = true) {
  return useQuery<Invitation[], ApiError>({
    queryKey: invitationsKey,
    queryFn: async ({ signal }) =>
      (await api.get<{ invitations: Invitation[] }>('/invitations', { signal })).invitations,
    enabled,
  })
}

/** The invite URL comes back only from the create call and is never re-fetchable. */
export interface CreatedInvitation extends Invitation {
  url: string
}

export function useCreateInvitation() {
  const queryClient = useQueryClient()
  return useMutation<CreatedInvitation, ApiError, { email: string; name: string; role: Role }>({
    mutationFn: (body) => api.post<CreatedInvitation>('/invitations', body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: invitationsKey })
    },
  })
}

export function useDeleteInvitation() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.delete<void>(`/invitations/${id}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: invitationsKey })
    },
  })
}

// --- API tokens ---------------------------------------------------------

export const tokensKey = ['tokens'] as const

export interface ApiTokenSummary {
  id: string
  name: string
  prefix: string
  last_used_at: string | null
  expires_at: string | null
  created_at: string
}

export function useTokens() {
  return useQuery<ApiTokenSummary[], ApiError>({
    queryKey: tokensKey,
    queryFn: async ({ signal }) =>
      (await api.get<{ tokens: ApiTokenSummary[] }>('/tokens', { signal })).tokens,
  })
}

/** The secret is present exactly once, in the create response. */
export interface CreatedToken extends ApiTokenSummary {
  token: string
}

export function useCreateToken() {
  const queryClient = useQueryClient()
  return useMutation<CreatedToken, ApiError, { name: string; expires_in_days: number }>({
    mutationFn: (body) => api.post<CreatedToken>('/tokens', body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: tokensKey })
    },
  })
}

export function useDeleteToken() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.delete<void>(`/tokens/${id}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: tokensKey })
    },
  })
}

// --- audit log ----------------------------------------------------------

export interface AuditEntry {
  id: number
  user_id: string | null
  actor_email: string
  action: string
  resource_type: string
  resource_id: string
  status: number
  ip: string
  meta: Record<string, unknown>
  created_at: string
}

export function useAudit(limit = 50) {
  return useQuery<{ entries: AuditEntry[]; before: string | null }, ApiError>({
    queryKey: ['audit', limit],
    queryFn: ({ signal }) =>
      api.get<{ entries: AuditEntry[]; before: string | null }>('/audit', {
        signal,
        query: { limit },
      }),
  })
}

// --- profile ------------------------------------------------------------

export function useUpdateProfile() {
  const queryClient = useQueryClient()
  return useMutation<CurrentUser, ApiError, { name: string }>({
    mutationFn: (body) => api.patch<CurrentUser>('/auth/me', body),
    onSuccess: (user) => {
      queryClient.setQueryData(sessionKey, user)
      void queryClient.invalidateQueries({ queryKey: usersKey })
    },
  })
}

export function useChangePassword() {
  return useMutation<void, ApiError, { current_password: string; new_password: string }>({
    mutationFn: (body) => api.post<void>('/auth/password', body),
  })
}

export interface SessionSummary {
  id: string
  current: boolean
  ip: string | null
  user_agent: string | null
  last_used_at: string
  created_at: string
}

export const activeSessionsKey = ['auth', 'sessions'] as const

export function useActiveSessions() {
  return useQuery<SessionSummary[], ApiError>({
    queryKey: activeSessionsKey,
    queryFn: async ({ signal }) =>
      (await api.get<{ sessions: SessionSummary[] }>('/auth/sessions', { signal })).sessions,
  })
}

export function useRevokeSession() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.delete<void>(`/auth/sessions/${id}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: activeSessionsKey })
    },
  })
}

// --- 2FA ----------------------------------------------------------------

export interface TwoFactorSetupResponse {
  secret: string
  otpauth_url: string
}

export interface TwoFactorEnableResponse {
  success: boolean
  recovery_codes: string[]
}

export const twoFactorStatusKey = ['auth', '2fa', 'status'] as const

export function use2FAStatus() {
  return useQuery<{ enabled: boolean }, ApiError>({
    queryKey: twoFactorStatusKey,
    queryFn: ({ signal }) => api.get<{ enabled: boolean }>('/auth/2fa/status', { signal }),
  })
}

export function useSetup2FA() {
  return useMutation<TwoFactorSetupResponse, ApiError, void>({
    mutationFn: () => api.post<TwoFactorSetupResponse>('/auth/2fa/setup'),
  })
}

export function useEnable2FA() {
  const queryClient = useQueryClient()
  return useMutation<TwoFactorEnableResponse, ApiError, { code: string }>({
    mutationFn: (body) => api.post<TwoFactorEnableResponse>('/auth/2fa/enable', body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: twoFactorStatusKey })
      void queryClient.invalidateQueries({ queryKey: sessionKey })
    },
  })
}

export function useDisable2FA() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, { password?: string; code?: string }>({
    mutationFn: (body) => api.post<void>('/auth/2fa/disable', body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: twoFactorStatusKey })
      void queryClient.invalidateQueries({ queryKey: sessionKey })
    },
  })
}

export function useRegenerateRecoveryCodes() {
  return useMutation<{ recovery_codes: string[] }, ApiError, { password: string }>({
    mutationFn: (body) => api.post<{ recovery_codes: string[] }>('/auth/2fa/recovery-codes', body),
  })
}

