import { AlertTriangle, Check, Minus } from 'lucide-react'

import { Alert } from '@/components/ui/alert'
import type { Capabilities, SudoMode } from '@/lib/servers'

const SUDO_LABELS: Record<SudoMode, string> = {
  root: 'root',
  nopasswd: 'passwordless',
  password: 'needs password',
  none: 'unavailable',
}

function Row({ label, value, ok }: { label: string; value: string; ok: boolean }) {
  return (
    <div className="flex items-center justify-between gap-3 py-1.5 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="flex items-center gap-1.5 font-mono text-xs">
        {ok ? (
          <Check className="text-success size-3.5" aria-hidden />
        ) : (
          <Minus className="text-muted-foreground size-3.5" aria-hidden />
        )}
        {value}
      </span>
    </div>
  )
}

/**
 * What the platform found on a server.
 *
 * Missing tooling is shown as absent rather than as an error: a machine with
 * Docker but no nginx is perfectly usable for deployments, and only the
 * warnings explain what will not work.
 */
export function CapabilityList({ capabilities }: { capabilities: Capabilities | null }) {
  if (!capabilities) {
    return (
      <Alert variant="danger">
        The server could not be inspected. Check the status message on the server page.
      </Alert>
    )
  }

  const warnings = capabilities.warnings ?? []

  return (
    <div className="space-y-3">
      <div className="divide-y rounded-md border px-4 py-1">
        <Row
          label="Docker"
          value={capabilities.docker_version || 'not found'}
          ok={capabilities.docker_socket_ok}
        />
        <Row
          label="Compose"
          value={
            capabilities.compose_command
              ? `${capabilities.compose_command} ${capabilities.compose_version}`
              : 'not found'
          }
          ok={Boolean(capabilities.compose_command)}
        />
        <Row label="git" value={capabilities.git_version || 'not found'} ok={Boolean(capabilities.git_version)} />
        <Row
          label="nginx"
          value={capabilities.nginx_version || 'not found'}
          ok={Boolean(capabilities.nginx_version)}
        />
        <Row
          label="certbot"
          value={capabilities.certbot_version || 'not found'}
          ok={Boolean(capabilities.certbot_version)}
        />
        <Row
          label="sudo"
          value={SUDO_LABELS[capabilities.sudo_mode] ?? capabilities.sudo_mode}
          ok={capabilities.sudo_mode !== 'none'}
        />
        {capabilities.os ? <Row label="OS" value={capabilities.os} ok /> : null}
      </div>

      {warnings.map((warning) => (
        <Alert key={warning} variant="warning" className="flex gap-2 text-xs leading-relaxed">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
          <span>{warning}</span>
        </Alert>
      ))}
    </div>
  )
}
