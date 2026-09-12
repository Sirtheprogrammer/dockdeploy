import { Globe, Plus } from 'lucide-react'

import { EmptyState } from '@/components/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'

export function Domains() {
  return (
    <>
      <PageHeader
        title="Domains"
        description="nginx virtual hosts and TLS certificates on your servers."
        actions={
          <Button disabled>
            <Plus aria-hidden />
            Add domain
          </Button>
        }
      />
      <EmptyState
        icon={Globe}
        title="No domains configured"
        description="Once a deployment is running, point a hostname at it. dockdeploy writes an nginx vhost on the server, validates it before enabling it, then issues a certificate."
      />
    </>
  )
}
