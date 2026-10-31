import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Flame,
  Globe,
  Layers,
  Loader2,
  Lock,
  Network,
  RefreshCw,
  Shield,
  ShieldAlert,
  ShieldCheck,
  Zap,
} from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  useRunSecurityScan,
  useServerSecurityAudit,
  type Server,
} from '@/lib/servers'
import { formatRelative } from '@/lib/utils'

interface ServerSecurityTabProps {
  server: Server
  canOperate?: boolean
}

type SecuritySubTab = 'overview' | 'findings' | 'attacks' | 'ports' | 'docker'

export function ServerSecurityTab({ server, canOperate = true }: ServerSecurityTabProps) {
  const [subTab, setSubTab] = useState<SecuritySubTab>('overview')
  const [severityFilter, setSeverityFilter] = useState<string>('all')
  const [searchQuery, setSearchQuery] = useState('')

  const auditQuery = useServerSecurityAudit(server.id)
  const runScan = useRunSecurityScan(server.id)

  const audit = auditQuery.data

  const handleScan = () => {
    runScan.mutate(undefined, {
      onSuccess: () => {
        toast.success('Security audit scan completed successfully.')
      },
      onError: (err) => {
        toast.error(`Security scan failed: ${err.message}`)
      },
    })
  }

  if (auditQuery.isPending) {
    return (
      <div className="space-y-4 p-4 sm:p-6">
        <Skeleton className="h-28 w-full rounded-xl" />
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
          <Skeleton className="h-24 rounded-lg" />
          <Skeleton className="h-24 rounded-lg" />
          <Skeleton className="h-24 rounded-lg" />
          <Skeleton className="h-24 rounded-lg" />
        </div>
        <Skeleton className="h-64 rounded-xl" />
      </div>
    )
  }

  if (auditQuery.isError) {
    return (
      <div className="p-4 sm:p-6 space-y-4">
        <Alert variant="danger">
          <AlertCircle className="size-4" />
          <span>Could not retrieve security audit: {auditQuery.error.message}</span>
        </Alert>
        <Button onClick={() => auditQuery.refetch()} variant="outline" size="sm" className="gap-2">
          <RefreshCw className="size-3.5" />
          Retry Security Audit
        </Button>
      </div>
    )
  }

  if (!audit) return null

  // Score styling
  const scoreColor =
    audit.score >= 85
      ? 'text-emerald-400 border-emerald-500/30 bg-emerald-500/10'
      : audit.score >= 65
        ? 'text-amber-400 border-amber-500/30 bg-amber-500/10'
        : 'text-rose-400 border-rose-500/30 bg-rose-500/10'

  const gradeColor =
    audit.grade === 'A'
      ? 'bg-emerald-500/20 text-emerald-400 border-emerald-500/40'
      : audit.grade === 'B'
        ? 'bg-blue-500/20 text-blue-400 border-blue-500/40'
        : audit.grade === 'C'
          ? 'bg-amber-500/20 text-amber-400 border-amber-500/40'
          : 'bg-rose-500/20 text-rose-400 border-rose-500/40'

  const filteredFindings = audit.findings.filter((f) => {
    const matchesSev = severityFilter === 'all' || f.severity === severityFilter
    const matchesSearch =
      f.title.toLowerCase().includes(searchQuery.toLowerCase()) ||
      f.description.toLowerCase().includes(searchQuery.toLowerCase()) ||
      f.category.toLowerCase().includes(searchQuery.toLowerCase())
    return matchesSev && matchesSearch
  })

  return (
    <div className="space-y-6 pt-2">
      {/* Top Security Banner */}
      <Card className="border-border/80 bg-surface overflow-hidden">
        <CardContent className="p-5 sm:p-6">
          <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-6">
            <div className="flex items-start sm:items-center gap-5">
              {/* Score Circular Badge */}
              <div
                className={`flex size-20 shrink-0 flex-col items-center justify-center rounded-2xl border-2 font-mono shadow-inner ${scoreColor}`}
              >
                <span className="text-2xl font-black leading-none">{audit.score}</span>
                <span className="text-[10px] uppercase tracking-wider font-semibold opacity-80 mt-1">
                  Grade {audit.grade}
                </span>
              </div>

              <div className="space-y-1.5 min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="text-lg sm:text-xl font-bold tracking-tight">Host Security Posture</h2>
                  <Badge variant="outline" className={`font-mono font-bold uppercase text-[11px] ${gradeColor}`}>
                    {audit.status}
                  </Badge>
                  <span className="text-xs text-muted-foreground">
                    Scanned {formatRelative(audit.scanned_at)}
                  </span>
                </div>
                <p className="text-xs sm:text-sm text-muted-foreground max-w-2xl leading-relaxed">
                  {audit.summary}
                </p>
              </div>
            </div>

            <div className="flex items-center gap-2.5 shrink-0 self-start lg:self-auto">
              <Button
                type="button"
                variant="default"
                size="sm"
                onClick={handleScan}
                disabled={runScan.isPending || !canOperate}
                className="gap-2 font-medium"
              >
                {runScan.isPending ? (
                  <>
                    <Loader2 className="size-4 animate-spin" />
                    <span>Auditing Server...</span>
                  </>
                ) : (
                  <>
                    <Shield className="size-4" />
                    <span>Run Security Scan</span>
                  </>
                )}
              </Button>
            </div>
          </div>

          {/* Quick Metrics Cards */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mt-6 pt-6 border-t border-border/50">
            <div className="rounded-lg border border-border/60 bg-background/50 p-3">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>Critical Risks</span>
                <ShieldAlert className="size-3.5 text-rose-400" />
              </div>
              <p className="text-xl font-bold text-rose-400 mt-1">{audit.critical_count}</p>
            </div>

            <div className="rounded-lg border border-border/60 bg-background/50 p-3">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>High Vulnerabilities</span>
                <AlertTriangle className="size-3.5 text-amber-400" />
              </div>
              <p className="text-xl font-bold text-amber-400 mt-1">{audit.high_count}</p>
            </div>

            <div className="rounded-lg border border-border/60 bg-background/50 p-3">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>SSH Brute-Force Hits</span>
                <Flame className="size-3.5 text-orange-400" />
              </div>
              <p className="text-xl font-bold text-orange-400 mt-1">
                {audit.ssh_analysis.failed_attempts_count}
              </p>
            </div>

            <div className="rounded-lg border border-border/60 bg-background/50 p-3">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>Risky Public Ports</span>
                <Network className="size-3.5 text-sky-400" />
              </div>
              <p className="text-xl font-bold text-sky-400 mt-1">
                {audit.exposed_ports.filter((p) => p.is_risky).length}
              </p>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Sub navigation inside Security Tab */}
      <Tabs value={subTab} onValueChange={(v) => setSubTab(v as SecuritySubTab)} className="space-y-5">
        <TabsList className="bg-surface border border-border/70 p-1 h-auto flex flex-wrap gap-1">
          <TabsTrigger value="overview" className="text-xs sm:text-sm gap-1.5 py-1.5">
            <ShieldCheck className="size-3.5" />
            <span>Overview & Hardening</span>
          </TabsTrigger>
          <TabsTrigger value="findings" className="text-xs sm:text-sm gap-1.5 py-1.5">
            <AlertCircle className="size-3.5" />
            <span>Findings ({audit.findings.length})</span>
          </TabsTrigger>
          <TabsTrigger value="attacks" className="text-xs sm:text-sm gap-1.5 py-1.5">
            <Flame className="size-3.5" />
            <span>Attack Patterns</span>
          </TabsTrigger>
          <TabsTrigger value="ports" className="text-xs sm:text-sm gap-1.5 py-1.5">
            <Network className="size-3.5" />
            <span>Network Ports ({audit.exposed_ports.length})</span>
          </TabsTrigger>
          <TabsTrigger value="docker" className="text-xs sm:text-sm gap-1.5 py-1.5">
            <Layers className="size-3.5" />
            <span>Docker Risks ({audit.container_risks.length})</span>
          </TabsTrigger>
        </TabsList>

        {/* 1. OVERVIEW & HARDENING */}
        <TabsContent value="overview" className="space-y-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-5">
            {/* Host Firewall Status */}
            <Card className="border-border/80 bg-surface">
              <CardHeader className="pb-3">
                <CardTitle className="text-sm font-semibold flex items-center gap-2">
                  <Lock className="size-4 text-primary" />
                  Host Firewall Status
                </CardTitle>
                <CardDescription className="text-xs">
                  A packet filter blocks port scans, brute-force sweeps, and database probes.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="flex items-center justify-between p-3 rounded-lg border border-border/60 bg-background/60">
                  <span className="text-xs text-muted-foreground">Active Firewall:</span>
                  <Badge
                    variant="outline"
                    className={`font-mono text-xs uppercase ${
                      audit.system_integrity.firewall_status.includes('active')
                        ? 'border-emerald-500/40 text-emerald-400 bg-emerald-500/10'
                        : 'border-rose-500/40 text-rose-400 bg-rose-500/10'
                    }`}
                  >
                    {audit.system_integrity.firewall_status}
                  </Badge>
                </div>

                {audit.system_integrity.firewall_status.includes('disabled') && (
                  <div className="rounded-lg bg-rose-500/10 border border-rose-500/20 p-3 text-xs text-rose-300 space-y-2">
                    <p className="font-semibold">Recommended Fix:</p>
                    <code className="block p-2 bg-black/60 rounded text-[11px] font-mono text-rose-200">
                      sudo ufw default deny incoming && sudo ufw allow ssh && sudo ufw allow 80/tcp && sudo ufw allow 443/tcp && sudo ufw enable
                    </code>
                  </div>
                )}
              </CardContent>
            </Card>

            {/* Attack Surface Summary */}
            <Card className="border-border/80 bg-surface">
              <CardHeader className="pb-3">
                <CardTitle className="text-sm font-semibold flex items-center gap-2">
                  <Zap className="size-4 text-amber-400" />
                  Attack Surface Summary
                </CardTitle>
                <CardDescription className="text-xs">
                  Summary of exposed vectors observed across this server's interfaces.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-2.5 text-xs">
                <div className="flex items-center justify-between py-1.5 border-b border-border/40">
                  <span className="text-muted-foreground">SSH Brute-Force Logged:</span>
                  <span className="font-mono font-bold text-zinc-200">
                    {audit.ssh_analysis.failed_attempts_count} attempts
                  </span>
                </div>
                <div className="flex items-center justify-between py-1.5 border-b border-border/40">
                  <span className="text-muted-foreground">Publicly Bound Sockets:</span>
                  <span className="font-mono font-bold text-zinc-200">
                    {audit.exposed_ports.filter((p) => p.is_public).length} sockets
                  </span>
                </div>
                <div className="flex items-center justify-between py-1.5 border-b border-border/40">
                  <span className="text-muted-foreground">Privileged Containers:</span>
                  <span className="font-mono font-bold text-zinc-200">
                    {audit.container_risks.filter((c) => c.is_privileged).length} container(s)
                  </span>
                </div>
                <div className="flex items-center justify-between py-1.5">
                  <span className="text-muted-foreground">Web Exploit Signatures:</span>
                  <span className="font-mono font-bold text-zinc-200">
                    {audit.web_attacks.scans_detected + audit.web_attacks.sqli_attempts + audit.web_attacks.path_traversal_attempts} detected
                  </span>
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Top Priority Findings Highlights */}
          {audit.findings.length > 0 && (
            <div className="space-y-3">
              <h3 className="text-sm font-semibold text-zinc-200">Top Priority Security Findings</h3>
              <div className="grid grid-cols-1 gap-3">
                {audit.findings.slice(0, 3).map((f) => (
                  <div
                    key={f.id}
                    className="p-4 rounded-xl border border-border/70 bg-surface/70 space-y-2"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <Badge
                          variant="outline"
                          className={`uppercase text-[10px] font-bold ${
                            f.severity === 'critical'
                              ? 'border-rose-500/40 bg-rose-500/10 text-rose-400'
                              : f.severity === 'high'
                                ? 'border-amber-500/40 bg-amber-500/10 text-amber-400'
                                : 'border-blue-500/40 bg-blue-500/10 text-blue-400'
                          }`}
                        >
                          {f.severity}
                        </Badge>
                        <span className="text-sm font-semibold text-zinc-100">{f.title}</span>
                      </div>
                      <span className="text-xs text-muted-foreground">{f.category}</span>
                    </div>
                    <p className="text-xs text-muted-foreground">{f.description}</p>
                    <div className="pt-1 text-xs text-zinc-300">
                      <span className="text-muted-foreground font-semibold">Remediation: </span>
                      {f.remediation}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </TabsContent>

        {/* 2. FINDINGS LIST */}
        <TabsContent value="findings" className="space-y-4">
          <div className="flex flex-col sm:flex-row gap-3">
            <div className="relative flex-1">
              <Input
                placeholder="Search findings by title, category, or description..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="text-xs sm:text-sm"
              />
            </div>
            <div className="flex flex-wrap gap-1.5">
              {['all', 'critical', 'high', 'medium', 'low'].map((sev) => (
                <Button
                  key={sev}
                  type="button"
                  variant={severityFilter === sev ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setSeverityFilter(sev)}
                  className="text-xs h-9 px-3 uppercase"
                >
                  {sev}
                </Button>
              ))}
            </div>
          </div>

          {filteredFindings.length === 0 ? (
            <div className="rounded-xl border border-border/60 bg-surface p-8 text-center space-y-2">
              <CheckCircle2 className="size-8 text-emerald-400 mx-auto" />
              <p className="text-sm font-semibold text-zinc-200">No findings matched your criteria.</p>
              <p className="text-xs text-muted-foreground">
                Your server passed the evaluated inspection rules for this category.
              </p>
            </div>
          ) : (
            <div className="space-y-3">
              {filteredFindings.map((finding) => {
                const sevBadge =
                  finding.severity === 'critical'
                    ? 'border-rose-500/40 bg-rose-500/10 text-rose-400'
                    : finding.severity === 'high'
                      ? 'border-amber-500/40 bg-amber-500/10 text-amber-400'
                      : finding.severity === 'medium'
                        ? 'border-yellow-500/40 bg-yellow-500/10 text-yellow-400'
                        : 'border-blue-500/40 bg-blue-500/10 text-blue-400'

                return (
                  <div
                    key={finding.id}
                    className="rounded-xl border border-border/70 bg-surface p-4 sm:p-5 space-y-3"
                  >
                    <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                      <div className="flex items-center gap-2.5">
                        <Badge variant="outline" className={`font-mono text-[11px] font-bold uppercase ${sevBadge}`}>
                          {finding.severity}
                        </Badge>
                        <h4 className="text-sm font-semibold text-zinc-100">{finding.title}</h4>
                      </div>
                      <Badge variant="outline" className="text-xs text-muted-foreground self-start sm:self-auto">
                        {finding.category}
                      </Badge>
                    </div>

                    <p className="text-xs sm:text-sm text-muted-foreground">{finding.description}</p>

                    {finding.evidence && (
                      <div className="rounded-lg bg-zinc-950/70 border border-border/40 p-2.5 text-xs font-mono text-zinc-300">
                        <span className="text-muted-foreground block text-[10px] uppercase tracking-wider mb-1">
                          Evidence / Detection:
                        </span>
                        <pre className="whitespace-pre-wrap overflow-x-auto">{finding.evidence}</pre>
                      </div>
                    )}

                    <div className="rounded-lg bg-primary/5 border border-primary/20 p-3 text-xs text-zinc-200 space-y-1">
                      <span className="font-semibold text-primary block text-[11px] uppercase tracking-wider">
                        Remediation Step:
                      </span>
                      <p>{finding.remediation}</p>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </TabsContent>

        {/* 3. ATTACK PATTERNS (SSH & WEB) */}
        <TabsContent value="attacks" className="space-y-6">
          {/* SSH Attacks */}
          <Card className="border-border/80 bg-surface">
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle className="text-base flex items-center gap-2">
                    <Flame className="size-4.5 text-orange-400" />
                    SSH Authentication & Brute-Force Activity
                  </CardTitle>
                  <CardDescription className="text-xs">
                    Analysis of authentication attempts and automated password guessing sweeps.
                  </CardDescription>
                </div>
                <Badge variant="outline" className="text-xs font-mono">
                  {audit.ssh_analysis.failed_attempts_count} Failures
                </Badge>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              {audit.ssh_analysis.top_attacker_ips.length > 0 ? (
                <div className="space-y-3">
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    Top Offending Attacker IP Addresses
                  </h4>
                  <div className="overflow-x-auto">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead className="text-xs">Attacker IP</TableHead>
                          <TableHead className="text-xs">Failed Hits</TableHead>
                          <TableHead className="text-xs">Quick Mitigation</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody className="text-xs">
                        {audit.ssh_analysis.top_attacker_ips.map((ip) => (
                          <TableRow key={ip.ip}>
                            <TableCell className="font-mono font-medium text-rose-300">{ip.ip}</TableCell>
                            <TableCell className="font-mono font-bold">{ip.count}</TableCell>
                            <TableCell>
                              <code className="text-[11px] bg-zinc-950 px-2 py-0.5 rounded border border-border/40 text-zinc-300">
                                ufw deny from {ip.ip}
                              </code>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                </div>
              ) : (
                <div className="p-4 rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-xs text-emerald-300">
                  ✅ No recent SSH brute-force or authentication flood detected in auth logs.
                </div>
              )}

              {audit.ssh_analysis.recent_attacks.length > 0 && (
                <div className="space-y-2 pt-2">
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    Recent Suspicious SSH Log Events
                  </h4>
                  <div className="rounded-lg bg-zinc-950 p-3 font-mono text-[11px] text-zinc-300 max-h-48 overflow-y-auto space-y-1">
                    {audit.ssh_analysis.recent_attacks.map((log, i) => (
                      <div key={i} className="truncate text-rose-300/90">
                        {log}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Web Exploits */}
          <Card className="border-border/80 bg-surface">
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle className="text-base flex items-center gap-2">
                    <Globe className="size-4.5 text-sky-400" />
                    Web Exploitation & Scanner Probing
                  </CardTitle>
                  <CardDescription className="text-xs">
                    Vulnerability probing patterns detected in reverse proxy or web server access logs.
                  </CardDescription>
                </div>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs">
                <div className="rounded-lg border border-border/50 bg-background/50 p-2.5">
                  <span className="text-muted-foreground block text-[11px]">SQL Injection:</span>
                  <span className="text-base font-bold text-zinc-200 mt-1 block">
                    {audit.web_attacks.sqli_attempts}
                  </span>
                </div>
                <div className="rounded-lg border border-border/50 bg-background/50 p-2.5">
                  <span className="text-muted-foreground block text-[11px]">Path Traversal:</span>
                  <span className="text-base font-bold text-zinc-200 mt-1 block">
                    {audit.web_attacks.path_traversal_attempts}
                  </span>
                </div>
                <div className="rounded-lg border border-border/50 bg-background/50 p-2.5">
                  <span className="text-muted-foreground block text-[11px]">Command Injection:</span>
                  <span className="text-base font-bold text-zinc-200 mt-1 block">
                    {audit.web_attacks.command_injection_attempts}
                  </span>
                </div>
                <div className="rounded-lg border border-border/50 bg-background/50 p-2.5">
                  <span className="text-muted-foreground block text-[11px]">Bot Probe Scans:</span>
                  <span className="text-base font-bold text-zinc-200 mt-1 block">
                    {audit.web_attacks.scans_detected}
                  </span>
                </div>
              </div>

              {audit.web_attacks.recent_web_attacks.length > 0 && (
                <div className="space-y-2 pt-2">
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    Captured Web Attack Signatures
                  </h4>
                  <div className="rounded-lg bg-zinc-950 p-3 font-mono text-[11px] text-zinc-300 max-h-48 overflow-y-auto space-y-1">
                    {audit.web_attacks.recent_web_attacks.map((log, i) => (
                      <div key={i} className="truncate text-amber-300/90">
                        {log}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        {/* 4. NETWORK PORTS */}
        <TabsContent value="ports" className="space-y-4">
          <Card className="border-border/80 bg-surface">
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Network className="size-4.5 text-sky-400" />
                Listening Sockets & Public Port Exposure
              </CardTitle>
              <CardDescription className="text-xs">
                Ports bound to 0.0.0.0 or [::] are directly exposed to the public Internet unless shielded by a firewall.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="text-xs">Port</TableHead>
                      <TableHead className="text-xs">Bind Address</TableHead>
                      <TableHead className="text-xs">Process</TableHead>
                      <TableHead className="text-xs">Exposure</TableHead>
                      <TableHead className="text-xs">Risk Assessment</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody className="text-xs">
                    {audit.exposed_ports.map((p, idx) => (
                      <TableRow key={idx}>
                        <TableCell className="font-mono font-bold">{p.port}/{p.protocol.toUpperCase()}</TableCell>
                        <TableCell className="font-mono text-zinc-300">{p.bind_addr}</TableCell>
                        <TableCell className="text-zinc-200">{p.process}</TableCell>
                        <TableCell>
                          {p.is_public ? (
                            <Badge variant="outline" className="border-amber-500/40 text-amber-400 text-[10px]">
                              Public (0.0.0.0)
                            </Badge>
                          ) : (
                            <Badge variant="outline" className="border-emerald-500/40 text-emerald-400 text-[10px]">
                              Localhost
                            </Badge>
                          )}
                        </TableCell>
                        <TableCell>
                          {p.is_risky ? (
                            <div className="flex items-center gap-1.5 text-rose-400 font-medium">
                              <AlertCircle className="size-3.5 shrink-0" />
                              <span>{p.risk_note}</span>
                            </div>
                          ) : (
                            <span className="text-muted-foreground">Normal</span>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* 5. DOCKER CONTAINER RISKS */}
        <TabsContent value="docker" className="space-y-4">
          <Card className="border-border/80 bg-surface">
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Layers className="size-4.5 text-primary" />
                Docker Container Isolation & Privilege Risks
              </CardTitle>
              <CardDescription className="text-xs">
                Inspect containers with privileged flags, root user processes, or socket mounts that allow container breakouts.
              </CardDescription>
            </CardHeader>
            <CardContent>
              {audit.container_risks.length === 0 ? (
                <div className="p-6 text-center space-y-2">
                  <CheckCircle2 className="size-7 text-emerald-400 mx-auto" />
                  <p className="text-sm font-semibold text-zinc-200">All containers adhere to baseline isolation.</p>
                  <p className="text-xs text-muted-foreground">
                    No privileged modes or dangerous host socket mounts were detected.
                  </p>
                </div>
              ) : (
                <div className="space-y-3">
                  {audit.container_risks.map((c) => (
                    <div
                      key={c.container_id}
                      className="p-4 rounded-xl border border-border/70 bg-background/50 space-y-2.5"
                    >
                      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                        <div className="flex items-center gap-2">
                          <code className="text-sm font-bold text-zinc-100">{c.name}</code>
                          <span className="text-xs text-muted-foreground">({c.image})</span>
                        </div>
                        <div className="flex flex-wrap items-center gap-1.5">
                          {c.is_privileged && (
                            <Badge variant="outline" className="border-rose-500/40 text-rose-400 text-[10px]">
                              Privileged
                            </Badge>
                          )}
                          {c.socket_mounted && (
                            <Badge variant="outline" className="border-amber-500/40 text-amber-400 text-[10px]">
                              docker.sock Mounted
                            </Badge>
                          )}
                          {c.host_network && (
                            <Badge variant="outline" className="border-sky-500/40 text-sky-400 text-[10px]">
                              --net=host
                            </Badge>
                          )}
                        </div>
                      </div>

                      <div className="space-y-1">
                        {c.risks.map((risk, i) => (
                          <div key={i} className="flex items-center gap-2 text-xs text-amber-300">
                            <AlertTriangle className="size-3 shrink-0" />
                            <span>{risk}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
