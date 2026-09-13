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
export function CapabilityList({
  capabilities,
  onInstallDocker,
}: {
  capabilities: Capabilities | null
  onInstallDocker?: () => void
}) {
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
      {!capabilities.docker_socket_ok && onInstallDocker ? (
        <div className="flex items-center justify-between gap-3 rounded-lg border border-blue-500/30 bg-blue-500/10 p-3 text-xs">
          <div className="text-blue-200">
            <span className="font-semibold">Docker is not running on this server.</span>
            <p className="text-[11px] text-blue-300/80 mt-0.5">
              Install Docker Engine and Compose using the official installer.
            </p>
          </div>
          <button
            type="button"
            onClick={onInstallDocker}
            className="shrink-0 rounded-md bg-blue-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-blue-500 transition-colors"
          >
            Install Docker
          </button>
        </div>
      ) : null}

      <div className="divide-y rounded-md border px-4 py-1">
        <div className="flex items-center justify-between gap-3 py-1.5 text-sm">
          <span className="text-muted-foreground">Docker</span>
          <div className="flex items-center gap-2">
            <span className="flex items-center gap-1.5 font-mono text-xs">
              {capabilities.docker_socket_ok ? (
                <Check className="text-success size-3.5" aria-hidden />
              ) : (
                <Minus className="text-muted-foreground size-3.5" aria-hidden />
              )}
              {capabilities.docker_version || 'not found'}
            </span>
            {!capabilities.docker_socket_ok && onInstallDocker ? (
              <button
                type="button"
                onClick={onInstallDocker}
                className="text-[11px] font-medium text-blue-400 hover:text-blue-300 underline underline-offset-2"
              >
                Install
              </button>
            ) : null}
          </div>
        </div>
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
