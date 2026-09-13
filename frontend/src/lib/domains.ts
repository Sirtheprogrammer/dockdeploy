import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { ApiError, api } from '@/lib/api'

export type DomainSSLMode = 'none' | 'letsencrypt'
export type DomainStatus = 'pending' | 'active' | 'error'

export interface Domain {
  id: string
  deployment_id?: string | null
  server_id: string
  hostname: string
  upstream_port: number
  ssl_mode: DomainSSLMode
  websocket: boolean
  config_rendered: string
  cert_expires_at?: string | null
  status: DomainStatus
  status_message: string
  created_by?: string | null
  created_at: string
  updated_at: string
  server_name?: string
  deployment_name?: string
}

export interface CreateDomainInput {
  server_id: string
  deployment_id?: string
  hostname: string
  upstream_port: number
  ssl_mode?: DomainSSLMode
  websocket?: boolean
  sudo_password?: string
  save_sudo?: boolean
}

export interface DomainSudoActionInput {
  id: string
  sudo_password?: string
  save_sudo?: boolean
}

export const domainsKey = ['domains'] as const
export const domainKey = (id: string) => ['domains', id] as const

export function useDomains(params?: { server_id?: string; deployment_id?: string }) {
  return useQuery<Domain[], ApiError>({
    queryKey: [...domainsKey, params?.server_id ?? '', params?.deployment_id ?? ''],
    queryFn: async ({ signal }) => {
      const q = new URLSearchParams()
      if (params?.server_id) q.set('server_id', params.server_id)
      if (params?.deployment_id) q.set('deployment_id', params.deployment_id)
      const qs = q.toString() ? `?${q.toString()}` : ''
      const res = await api.get<{ domains: Domain[] }>(`/domains${qs}`, { signal })
      return res.domains
    },
  })
}

export function useDomain(id: string) {
  return useQuery<Domain, ApiError>({
    queryKey: domainKey(id),
    queryFn: ({ signal }) => api.get<Domain>(`/domains/${id}`, { signal }),
    enabled: id !== '',
  })
}

export function useCreateDomain() {
  const queryClient = useQueryClient()
  return useMutation<Domain, ApiError, CreateDomainInput>({
    mutationFn: (body) => api.post<Domain>('/domains', body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: domainsKey })
    },
  })
}

export function useDeleteDomain() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.delete<void>(`/domains/${id}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: domainsKey })
    },
  })
}

export function useIssueSSL() {
  const queryClient = useQueryClient()
  return useMutation<Domain, ApiError, DomainSudoActionInput | string>({
    mutationFn: (arg) => {
      const id = typeof arg === 'string' ? arg : arg.id
      const body =
        typeof arg === 'string'
          ? {}
          : { sudo_password: arg.sudo_password, save_sudo: arg.save_sudo }
      return api.post<Domain>(`/domains/${id}/ssl`, body)
    },
    onSuccess: (_, arg) => {
      const id = typeof arg === 'string' ? arg : arg.id
      void queryClient.invalidateQueries({ queryKey: domainsKey })
      void queryClient.invalidateQueries({ queryKey: domainKey(id) })
    },
  })
}

export function useSyncDomain() {
  const queryClient = useQueryClient()
  return useMutation<Domain, ApiError, DomainSudoActionInput | string>({
    mutationFn: (arg) => {
      const id = typeof arg === 'string' ? arg : arg.id
      const body =
        typeof arg === 'string'
          ? {}
          : { sudo_password: arg.sudo_password, save_sudo: arg.save_sudo }
      return api.post<Domain>(`/domains/${id}/sync`, body)
    },
    onSuccess: (_, arg) => {
      const id = typeof arg === 'string' ? arg : arg.id
      void queryClient.invalidateQueries({ queryKey: domainsKey })
      void queryClient.invalidateQueries({ queryKey: domainKey(id) })
    },
  })
}
