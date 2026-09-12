import { useQuery } from '@tanstack/react-query'

import { api, type Health } from '@/lib/api'

/** Polls the control-plane health endpoint that backs the sidebar indicator. */
export function useHealth() {
  return useQuery({
    queryKey: ['health'],
    queryFn: ({ signal }) => api.get<Health>('/health', { signal }),
    refetchInterval: 15_000,
    // A degraded backend answers 503; that is a valid answer, not a reason to
    // keep retrying and leave the indicator stuck on "checking".
    retry: false,
    staleTime: 0,
  })
}
