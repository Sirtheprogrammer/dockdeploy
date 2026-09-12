import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, type ApiError } from '@/lib/api'

export type ServerStatus = 'unknown' | 'online' | 'offline' | 'unauthorized'
export type AuthMethod = 'password' | 'key'
export type SudoMode = 'none' | 'nopasswd' | 'password' | 'root'
export type NginxLayout = '' | 'debian' | 'confd'

/** Mirrors sshx.Capabilities. */
export interface Capabilities {
  os: string
  kernel: string
  docker_version: string
  docker_socket_ok: boolean
  compose_command: string
  compose_version: string
  sudo_mode: SudoMode
  nginx_version: string
  nginx_layout: NginxLayout
  certbot_version: string
  git_version: string
  warnings: string[] | null
}

export interface Server {
  id: string
  name: string
  host: string
  port: number
  username: string
  auth_method: AuthMethod
  host_key_fingerprint: string
  docker_socket: string
  status: ServerStatus
  status_message: string
  capabilities: Capabilities | Record<string, never>
  last_seen_at: string | null
  has_sudo_password: boolean
  created_at: string
  updated_at: string
}

export interface Container {
  id: string
  name: string
  image: string
  image_id: string
  state: string
  status: string
  health?: string
  created_at: string
  ports: {
    host?: string
    host_port?: number
    container_port: number
    protocol: string
  }[]
  networks: string[]
  managed_by?: string
  deployment_id?: string
  compose_project?: string
}

export interface DockerInfo {
  server_version: string
  operating_system: string
  architecture: string
  cpus: number
  memory_bytes: number
  containers: number
  containers_running: number
  containers_stopped: number
  images: number
}

export const serversKey = ['servers'] as const
export const serverKey = (id: string) => ['servers', id] as const

export function useServers() {
  return useQuery<Server[], ApiError>({
    queryKey: serversKey,
    queryFn: async ({ signal }) =>
      (await api.get<{ servers: Server[] }>('/servers', { signal })).servers,
  })
}

export function useServer(id: string) {
  return useQuery<Server, ApiError>({
    queryKey: serverKey(id),
    queryFn: ({ signal }) => api.get<Server>(`/servers/${id}`, { signal }),
    enabled: id !== '',
  })
}

/**
 * Reads a server's SSH host key so the user can confirm it.
 *
 * This runs before any credential leaves the browser: the key exchange
 * completes before authentication, so the machine can be vouched for first.
 */
export function useFingerprint() {
  return useMutation<{ fingerprint: string; key_type: string }, ApiError, { host: string; port: number }>({
    mutationFn: (body) => api.post<{ fingerprint: string; key_type: string }>('/servers/fingerprint', body),
  })
}

export interface CreateServerInput {
  name: string
  host: string
  port: number
  username: string
  auth_method: AuthMethod
  password?: string
  private_key?: string
  passphrase?: string
  sudo_password?: string
  docker_socket?: string
  host_key_fingerprint: string
}

export interface CreateServerResult {
  server: Server
  capabilities: Capabilities | null
}

export function useCreateServer() {
  const queryClient = useQueryClient()
  return useMutation<CreateServerResult, ApiError, CreateServerInput>({
    mutationFn: (body) => api.post<CreateServerResult>('/servers', body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: serversKey })
    },
  })
}

export function useUpdateServer(id: string) {
  const queryClient = useQueryClient()
  return useMutation<Server, ApiError, { name: string; docker_socket: string }>({
    mutationFn: (body) => api.patch<Server>(`/servers/${id}`, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: serversKey })
      void queryClient.invalidateQueries({ queryKey: serverKey(id) })
    },
  })
}

export function useDeleteServer() {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.delete<void>(`/servers/${id}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: serversKey })
    },
  })
}

export function useProbeServer(id: string) {
  const queryClient = useQueryClient()
  return useMutation<
    { server: Server; capabilities: Capabilities | null; reachable: boolean },
    ApiError,
    void
  >({
    mutationFn: () => api.post(`/servers/${id}/probe`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: serversKey })
      void queryClient.invalidateQueries({ queryKey: serverKey(id) })
    },
  })
}

// --- what is running on a server ----------------------------------------

export const containersKey = (serverID: string) => ['servers', serverID, 'containers'] as const

export function useContainers(serverID: string) {
  return useQuery<Container[], ApiError>({
    queryKey: containersKey(serverID),
    queryFn: async ({ signal }) =>
      (await api.get<{ containers: Container[] }>(`/servers/${serverID}/containers`, { signal }))
        .containers,
    enabled: serverID !== '',
    // Container state changes outside this tool constantly, so a short poll
    // keeps the list honest without a websocket.
    refetchInterval: 10_000,
  })
}

export function useDockerInfo(serverID: string) {
  return useQuery<DockerInfo, ApiError>({
    queryKey: ['servers', serverID, 'docker'],
    queryFn: ({ signal }) => api.get<DockerInfo>(`/servers/${serverID}/docker`, { signal }),
    enabled: serverID !== '',
  })
}

export type ContainerAction =
  | 'start'
  | 'stop'
  | 'restart'
  | 'kill'
  | 'pause'
  | 'unpause'
  | 'remove'

export function useContainerAction(serverID: string) {
  const queryClient = useQueryClient()
  return useMutation<void, ApiError, { containerID: string; action: ContainerAction }>({
    mutationFn: ({ containerID, action }) =>
      api.post<void>(`/servers/${serverID}/containers/${containerID}/actions`, { action }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: containersKey(serverID) })
    },
  })
}

export interface ImageSummary {
  Id: string
  RepoTags: string[] | null
  Size: number
  Created: number
}

export interface VolumeSummary {
  Name: string
  Driver: string
  Mountpoint: string
  CreatedAt?: string
}

export interface NetworkSummary {
  Id: string
  Name: string
  Driver: string
  Scope: string
  Internal: boolean
}

export function useImages(serverID: string, enabled: boolean) {
  return useQuery<ImageSummary[], ApiError>({
    queryKey: ['servers', serverID, 'images'],
    queryFn: async ({ signal }) =>
      (await api.get<{ images: ImageSummary[] }>(`/servers/${serverID}/images`, { signal })).images ?? [],
    enabled: enabled && serverID !== '',
  })
}

export function useVolumes(serverID: string, enabled: boolean) {
  return useQuery<VolumeSummary[], ApiError>({
    queryKey: ['servers', serverID, 'volumes'],
    queryFn: async ({ signal }) =>
      (await api.get<{ volumes: VolumeSummary[] }>(`/servers/${serverID}/volumes`, { signal })).volumes ??
      [],
    enabled: enabled && serverID !== '',
  })
}

export function useNetworks(serverID: string, enabled: boolean) {
  return useQuery<NetworkSummary[], ApiError>({
    queryKey: ['servers', serverID, 'networks'],
    queryFn: async ({ signal }) =>
      (await api.get<{ networks: NetworkSummary[] }>(`/servers/${serverID}/networks`, { signal }))
        .networks ?? [],
    enabled: enabled && serverID !== '',
  })
}

/** Capabilities come back as an empty object before the first probe. */
export function capabilitiesOf(server: Server | undefined): Capabilities | null {
  if (!server) return null
  const caps = server.capabilities as Capabilities
  return caps && typeof caps.docker_version === 'string' ? caps : null
}
