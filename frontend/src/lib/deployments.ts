import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { ApiError, api } from '@/lib/api'

export type SourceType = 'git_dockerfile' | 'git_compose' | 'raw_compose' | 'image'
export type BuildStrategy = 'remote' | 'registry'
export type DeploymentStatus = 'never_deployed' | 'deploying' | 'running' | 'stopped' | 'failed'
export type RunStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'
export type RunTrigger = 'manual' | 'webhook' | 'api' | 'rollback'

export interface Deployment {
  id: string
  server_id: string
  server_name?: string
  name: string
  slug: string
  source_type: SourceType
  repo_url: string
  git_ref: string
  git_credential_id: string | null
  dockerfile_path: string
  build_context: string
  compose_path: string
  compose_content: string
  image_ref: string
  build_strategy: BuildStrategy
  registry_id: string | null
  image_name: string
  workdir: string
  host_port: number | null
  container_port: number
  status: DeploymentStatus
  current_run_id: string | null
  created_at: string
  updated_at: string
}

export interface Run {
  id: string
  deployment_id: string
  number: number
  trigger: RunTrigger
  status: RunStatus
  commit_sha: string
  image_ref: string
  error: string
  queued_at: string
  started_at: string | null
  finished_at: string | null
}

export interface EnvVar {
  key: string
  value: string
  is_secret: boolean
}

/** How each source is described in the UI, so the wording stays consistent. */
export const SOURCE_LABELS: Record<SourceType, string> = {
  git_dockerfile: 'Git repository with a Dockerfile',
  git_compose: 'Git repository with a compose file',
  raw_compose: 'Compose file',
  image: 'Existing image',
}

export const SOURCE_SHORT: Record<SourceType, string> = {
  git_dockerfile: 'Dockerfile',
  git_compose: 'Compose (git)',
  raw_compose: 'Compose',
  image: 'Image',
}

export const deploymentsKey = ['deployments'] as const
export const deploymentKey = (id: string) => ['deployments', id] as const
export const runsKey = (id: string) => ['deployments', id, 'runs'] as const

export function useDeployments() {
  return useQuery<Deployment[], ApiError>({
    queryKey: deploymentsKey,
    queryFn: async ({ signal }) =>
      (await api.get<{ deployments: Deployment[] }>('/deployments', { signal })).deployments,
  })
}

export function useDeployment(id: string) {
  return useQuery<Deployment, ApiError>({
    queryKey: deploymentKey(id),
    queryFn: ({ signal }) => api.get<Deployment>(`/deployments/${id}`, { signal }),
    enabled: id !== '',
  })
}

export interface CreateDeploymentInput {
  server_id: string
  name: string
  source_type: SourceType
  repo_url?: string
  git_ref?: string
  git_credential_id?: string | null
  dockerfile_path?: string
  build_context?: string
  compose_path?: string
  compose_content?: string
  image_ref?: string
  build_strategy: BuildStrategy
  registry_id?: string | null
  image_name?: string
  container_port?: number
  env?: EnvVar[]
}

export function useCreateDeployment() {
  const queryClient = useQueryClient()
  return useMutation<Deployment, ApiError, CreateDeploymentInput>({
    mutationFn: (body) => api.post<Deployment>('/deployments', body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: deploymentsKey })
    },
  })
}

export function useDeleteDeployment() {
  const queryClient = useQueryClient()
  return useMutation<{ deleted: boolean; note: string }, ApiError, string>({
    mutationFn: (id) => api.delete<{ deleted: boolean; note: string }>(`/deployments/${id}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: deploymentsKey })
    },
  })
}

// --- runs ----------------------------------------------------------------

export function useRuns(deploymentID: string, live: boolean) {
  return useQuery<Run[], ApiError>({
    queryKey: runsKey(deploymentID),
    queryFn: async ({ signal }) =>
      (await api.get<{ runs: Run[] }>(`/deployments/${deploymentID}/runs`, { signal })).runs,
    enabled: deploymentID !== '',
    // While something is building, poll so the history and status settle
    // without the user reloading.
    refetchInterval: live ? 3000 : false,
  })
}

export function useDeploy(deploymentID: string) {
  const queryClient = useQueryClient()
  return useMutation<Run, ApiError, { rollback_run_id?: string } | void>({
    mutationFn: (variables) => api.post<Run>(`/deployments/${deploymentID}/runs`, variables ?? {}),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: runsKey(deploymentID) })
      void queryClient.invalidateQueries({ queryKey: deploymentKey(deploymentID) })
    },
  })
}

export interface WebhookInfo {
  webhook_url: string
  webhook_secret: string
}

export function useDeploymentWebhook(deploymentID: string) {
  return useQuery<WebhookInfo, ApiError>({
    queryKey: ['deployments', deploymentID, 'webhook'],
    queryFn: ({ signal }) => api.get<WebhookInfo>(`/deployments/${deploymentID}/webhook`, { signal }),
    enabled: deploymentID !== '',
  })
}

export function useCancelRun(deploymentID: string) {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, string>({
    mutationFn: (runID) =>
      api.post<void>(`/deployments/${deploymentID}/runs/${runID}/cancel`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: runsKey(deploymentID) })
    },
  })
}

// --- environment ---------------------------------------------------------

export function useDeploymentEnv(deploymentID: string) {
  return useQuery<EnvVar[], ApiError>({
    queryKey: ['deployments', deploymentID, 'env'],
    queryFn: async ({ signal }) =>
      (await api.get<{ env: EnvVar[] | null }>(`/deployments/${deploymentID}/env`, { signal }))
        .env ?? [],
    enabled: deploymentID !== '',
  })
}

export function useSetDeploymentEnv(deploymentID: string) {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, EnvVar[]>({
    mutationFn: (env) => api.put<void>(`/deployments/${deploymentID}/env`, { env }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['deployments', deploymentID, 'env'] })
    },
  })
}

// --- credentials ---------------------------------------------------------

export interface GitCredential {
  id: string
  name: string
  provider: string
  kind: 'token' | 'ssh_key'
  username: string
  created_at: string
}

export interface Registry {
  id: string
  name: string
  url: string
  username: string
  created_at: string
}

export const gitCredentialsKey = ['git-credentials'] as const
export const registriesKey = ['registries'] as const

/**
 * Both credential lists need `credential:read`, which only admins have. The
 * deployment wizard is usable by members too, so a 403 here is expected and
 * resolves to an empty list rather than an error.
 */
export function useGitCredentials(enabled = true) {
  return useQuery<GitCredential[], ApiError>({
    queryKey: gitCredentialsKey,
    queryFn: async ({ signal }) => {
      try {
        return (
          (await api.get<{ credentials: GitCredential[] | null }>('/git-credentials', { signal }))
            .credentials ?? []
        )
      } catch (error) {
        if (error instanceof ApiError && error.status === 403) return []
        throw error
      }
    },
    enabled,
  })
}

export function useCreateGitCredential() {
  const queryClient = useQueryClient()
  return useMutation<
    GitCredential,
    ApiError,
    { name: string; kind: 'token' | 'ssh_key'; username: string; secret: string }
  >({
    mutationFn: (body) => api.post<GitCredential>('/git-credentials', body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: gitCredentialsKey })
    },
  })
}

export function useDeleteGitCredential() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.delete<void>(`/git-credentials/${id}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: gitCredentialsKey })
    },
  })
}

export function useRegistries(enabled = true) {
  return useQuery<Registry[], ApiError>({
    queryKey: registriesKey,
    queryFn: async ({ signal }) => {
      try {
        return (
          (await api.get<{ registries: Registry[] | null }>('/registries', { signal }))
            .registries ?? []
        )
      } catch (error) {
        if (error instanceof ApiError && error.status === 403) return []
        throw error
      }
    },
    enabled,
  })
}

export function useCreateRegistry() {
  const queryClient = useQueryClient()
  return useMutation<
    Registry,
    ApiError,
    { name: string; url: string; username: string; password: string }
  >({
    mutationFn: (body) => api.post<Registry>('/registries', body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: registriesKey })
    },
  })
}

export function useDeleteRegistry() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.delete<void>(`/registries/${id}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: registriesKey })
    },
  })
}

/** A run that is still going, if any. Drives polling and the log pane. */
export function activeRun(runs: Run[] | undefined): Run | undefined {
  return runs?.find((run) => run.status === 'queued' || run.status === 'running')
}
