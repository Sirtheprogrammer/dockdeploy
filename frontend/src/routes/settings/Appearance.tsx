import { Activity, Boxes, Check, Palette, Rocket, Server, Shield, Sparkles, Terminal } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useTheme } from '@/lib/theme'
import { cn } from '@/lib/utils'

export function Appearance() {
  const { theme, setTheme, themes, iconPack, setIconPack, iconPacks } = useTheme()

  return (
    <div className="space-y-8 max-w-5xl">
      <div>
        <h3 className="text-lg font-medium tracking-tight">Appearance & Interface Customization</h3>
        <p className="text-muted-foreground text-sm">
          Customize your Dockdeploy control plane with distinct color themes, icon pack aesthetics, and display options.
        </p>
      </div>

      {/* Theme Selection */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Palette className="size-5 text-primary" />
            <CardTitle>Color Themes</CardTitle>
          </div>
          <CardDescription>
            Choose a color palette suited to your workspace environment. Changes are applied instantly.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {themes.map((t) => {
              const isSelected = theme === t.id
              return (
                <button
                  key={t.id}
                  type="button"
                  onClick={() => setTheme(t.id)}
                  className={cn(
                    'group relative flex flex-col text-left rounded-xl border p-4 transition-all duration-150 cursor-pointer',
                    isSelected
                      ? 'border-primary ring-2 ring-primary/40 bg-accent/30 shadow-md'
                      : 'border-border/80 bg-card hover:border-primary/50 hover:bg-accent/15',
                  )}
                >
                  {/* Color Palette Preview Swatch */}
                  <div
                    className="relative mb-3 h-24 w-full overflow-hidden rounded-lg border shadow-inner p-2.5 flex flex-col justify-between"
                    style={{ backgroundColor: t.preview.bg, borderColor: t.preview.border }}
                  >
                    <div className="flex items-center justify-between">
                      <div
                        className="flex items-center gap-1.5 px-2 py-0.5 rounded text-[10px] font-medium"
                        style={{ backgroundColor: t.preview.card, color: t.preview.primary }}
                      >
                        <Boxes className="size-3" />
                        <span>dockdeploy</span>
                      </div>
                      <div
                        className="size-3 rounded-full"
                        style={{ backgroundColor: t.preview.primary }}
                      />
                    </div>

                    <div className="flex items-center gap-2">
                      <div
                        className="h-5 px-2 rounded flex items-center text-[9px] font-semibold text-white"
                        style={{ backgroundColor: t.preview.primary }}
                      >
                        Action
                      </div>
                      <div
                        className="h-5 px-1.5 rounded flex items-center text-[9px] font-mono"
                        style={{ backgroundColor: t.preview.accent, color: t.preview.primary }}
                      >
                        status: ok
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center justify-between gap-2">
                    <span className="font-semibold text-sm tracking-tight text-foreground flex items-center gap-1.5">
                      {t.name}
                      {isSelected && (
                        <span className="bg-primary text-primary-foreground size-4 rounded-full flex items-center justify-center">
                          <Check className="size-2.5" />
                        </span>
                      )}
                    </span>
                    <Badge variant={t.isDark ? 'outline' : 'default'} className="text-[10px] py-0 px-1.5 uppercase font-mono">
                      {t.isDark ? 'Dark' : 'Light'}
                    </Badge>
                  </div>
                  <p className="text-muted-foreground mt-1 text-xs leading-relaxed">
                    {t.description}
                  </p>
                </button>
              )
            })}
          </div>
        </CardContent>
      </Card>

      {/* Icon Pack Selection */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Sparkles className="size-5 text-primary" />
            <CardTitle>Icon Packs</CardTitle>
          </div>
          <CardDescription>
            Select an iconography style that matches your workflow and visual preference.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {iconPacks.map((pack) => {
              const isSelected = iconPack === pack.id
              return (
                <button
                  key={pack.id}
                  type="button"
                  onClick={() => setIconPack(pack.id)}
                  className={cn(
                    'group relative flex flex-col text-left rounded-xl border p-4 transition-all duration-150 cursor-pointer',
                    isSelected
                      ? 'border-primary ring-2 ring-primary/40 bg-accent/30 shadow-md'
                      : 'border-border/80 bg-card hover:border-primary/50 hover:bg-accent/15',
                  )}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <span className="font-semibold text-sm tracking-tight text-foreground">
                        {pack.name}
                      </span>
                      {isSelected && (
                        <span className="bg-primary text-primary-foreground size-4 rounded-full flex items-center justify-center">
                          <Check className="size-2.5" />
                        </span>
                      )}
                    </div>
                    <Badge variant="outline" className="text-[10px] font-mono">
                      {pack.id}
                    </Badge>
                  </div>

                  <p className="text-muted-foreground mt-1 text-xs">
                    {pack.description}
                  </p>

                  {/* Icon Glyphs Sample Bar */}
                  <div
                    data-icon-pack={pack.id}
                    className="mt-4 flex items-center justify-between rounded-lg bg-surface/80 p-3 border border-border/50 text-foreground"
                  >
                    <div className="flex items-center gap-3">
                      <Server className="size-4.5 text-primary" />
                      <Rocket className="size-4.5 text-emerald-400" />
                      <Terminal className="size-4.5 text-sky-400" />
                      <Shield className="size-4.5 text-amber-400" />
                      <Activity className="size-4.5 text-indigo-400" />
                    </div>
                    <span className="text-[11px] font-mono text-muted-foreground">
                      Sample
                    </span>
                  </div>
                </button>
              )
            })}
          </div>
        </CardContent>
      </Card>

      {/* Terminal & Popups Integration */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Terminal className="size-5 text-primary" />
            <CardTitle>Detachable Windows & Popups</CardTitle>
          </div>
          <CardDescription>
            Multi-monitor support for terminals and assistant workflows.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 text-sm text-muted-foreground">
          <p>
            You can detach any server interactive terminal into an independent browser popup window, identical to the AI Assistant tab.
          </p>
          <div className="rounded-lg border bg-surface p-3 font-mono text-xs flex items-center justify-between">
            <span className="text-foreground">Direct URL: /terminal-popup?serverId=&lt;SERVER_ID&gt;</span>
            <Badge variant="outline">Standalone window</Badge>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
