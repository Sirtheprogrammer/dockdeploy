/**
 * Typed client for the Go API.
 *
 * Every failure — network, HTTP, or a structured server error — arrives as an
 * `ApiError`, so callers have exactly one thing to catch and TanStack Query
 * error boundaries can render `error.message` without narrowing first.
 */

/** Mirrors api.ErrorCode in the Go server. */
export type ApiErrorCode =
  | 'bad_request'
  | 'validation_failed'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'method_not_allowed'
  | 'conflict'
  | 'rate_limited'
  | 'internal_error'
  | 'unavailable'
  | 'network_error'

export class ApiError extends Error {
  readonly code: ApiErrorCode
  readonly status: number
  /** Per-field messages from a validation_failed response. */
  readonly fields: Record<string, string>

  constructor(
    code: ApiErrorCode,
    message: string,
    status: number,
    fields: Record<string, string> = {},
  ) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
    this.fields = fields
  }

  /** True when the user needs to sign in again. */
  get isAuthFailure() {
    return this.code === 'unauthorized'
  }
}

interface ErrorEnvelope {
  error?: { code?: ApiErrorCode; message?: string; fields?: Record<string, string> }
}

export interface RequestOptions extends Omit<RequestInit, 'body' | 'method'> {
  /** Serialised as JSON unless it is FormData. */
  body?: unknown
  /** Appended to the URL, skipping null/undefined values. */
  query?: Record<string, string | number | boolean | null | undefined>
}

const BASE = '/api'

function buildUrl(path: string, query: RequestOptions['query']): string {
  const url = `${BASE}${path.startsWith('/') ? path : `/${path}`}`
  if (!query) return url
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== null) params.set(key, String(value))
  }
  const qs = params.toString()
  return qs ? `${url}?${qs}` : url
}

async function toApiError(response: Response): Promise<ApiError> {
  let envelope: ErrorEnvelope = {}
  try {
    envelope = (await response.json()) as ErrorEnvelope
  } catch {
    // A proxy or crash can return HTML; fall through to the status text.
  }
  const detail = envelope.error
  return new ApiError(
    detail?.code ?? 'internal_error',
    detail?.message ?? `Request failed with status ${response.status}.`,
    response.status,
    detail?.fields ?? {},
  )
}

async function request<T>(
  method: string,
  path: string,
  { body, query, headers, ...init }: RequestOptions = {},
): Promise<T> {
  const isFormData = body instanceof FormData
  const requestHeaders = new Headers(headers)
  if (body !== undefined && !isFormData) {
    requestHeaders.set('Content-Type', 'application/json')
  }

  let response: Response
  try {
    response = await fetch(buildUrl(path, query), {
      ...init,
      method,
      headers: requestHeaders,
      // Sessions are cookie-based; the Vite dev proxy keeps this same-origin.
      credentials: 'same-origin',
      body: isFormData ? body : body === undefined ? undefined : JSON.stringify(body),
    })
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    throw new ApiError('network_error', 'Could not reach the server.', 0)
  }

  if (!response.ok) throw await toApiError(response)

  if (response.status === 204 || response.headers.get('Content-Length') === '0') {
    return undefined as T
  }
  return (await response.json()) as T
}

export const api = {
  get: <T>(path: string, options?: RequestOptions) => request<T>('GET', path, options),
  post: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>('POST', path, { ...options, body }),
  put: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>('PUT', path, { ...options, body }),
  patch: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>('PATCH', path, { ...options, body }),
  delete: <T>(path: string, options?: RequestOptions) => request<T>('DELETE', path, options),
}

/** Response shape of GET /api/health. */
export interface Health {
  status: 'ok' | 'degraded'
  version: string
  uptime_seconds: number
  database: 'ok' | 'unreachable'
}
