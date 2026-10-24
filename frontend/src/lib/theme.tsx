/* eslint-disable react-refresh/only-export-components */
import React, { createContext, useContext, useEffect, useState } from 'react'

export type ThemeId = 'dark' | 'light' | 'cyberpunk' | 'nord' | 'dracula' | 'emerald'
export type IconPackId = 'lucide' | 'bold' | 'sharp' | 'duotone'

export interface ThemeMeta {
  id: ThemeId
  name: string
  description: string
  isDark: boolean
  preview: {
    bg: string
    card: string
    primary: string
    accent: string
    border: string
  }
}

export interface IconPackMeta {
  id: IconPackId
  name: string
  description: string
  styleDescription: string
}

export const THEMES: ThemeMeta[] = [
  {
    id: 'dark',
    name: 'Zinc Dark',
    description: 'Classic deep charcoal interface designed for day-long operations.',
    isDark: true,
    preview: {
      bg: '#18181b',
      card: '#27272a',
      primary: '#38bdf8',
      accent: '#3f3f46',
      border: '#3f3f46',
    },
  },
  {
    id: 'light',
    name: 'Daylight',
    description: 'Clean, high-contrast crisp daylight aesthetic with slate accents.',
    isDark: false,
    preview: {
      bg: '#f8fafc',
      card: '#ffffff',
      primary: '#0284c7',
      accent: '#e2e8f0',
      border: '#cbd5e1',
    },
  },
  {
    id: 'cyberpunk',
    name: 'Midnight Cyber',
    description: 'Futuristic high-tech palette with electric cyan and neon violet glow.',
    isDark: true,
    preview: {
      bg: '#080c14',
      card: '#101726',
      primary: '#06b6d4',
      accent: '#a855f7',
      border: '#1e293b',
    },
  },
  {
    id: 'nord',
    name: 'Nordic Frost',
    description: 'Arctic-inspired polar night slate with cold ice blues and snow white.',
    isDark: true,
    preview: {
      bg: '#242933',
      card: '#2e3440',
      primary: '#88c0d0',
      accent: '#81a1c1',
      border: '#434c5e',
    },
  },
  {
    id: 'dracula',
    name: 'Dracula',
    description: 'Vampiric dark theme with gothic purple, neon pink and radioactive green.',
    isDark: true,
    preview: {
      bg: '#1e1f29',
      card: '#282a36',
      primary: '#bd93f9',
      accent: '#ff79c6',
      border: '#44475a',
    },
  },
  {
    id: 'emerald',
    name: 'Forest Emerald',
    description: 'Subtle deep evergreen terminal look with vibrant mint and jade green.',
    isDark: true,
    preview: {
      bg: '#0b1612',
      card: '#12231c',
      primary: '#10b981',
      accent: '#34d399',
      border: '#1d382d',
    },
  },
]

export const ICON_PACKS: IconPackMeta[] = [
  {
    id: 'lucide',
    name: 'Standard Lucide',
    description: 'Standard modern outline icons with balanced 2px line geometry.',
    styleDescription: 'Clean 2px stroke, smooth round caps and joints.',
  },
  {
    id: 'bold',
    name: 'Minimal Bold',
    description: 'Weighted high-contrast strokes with punchy presence.',
    styleDescription: 'Thicker 2.6px stroke, bold silhouette.',
  },
  {
    id: 'sharp',
    name: 'Tech Terminal',
    description: 'Crisp geometric edges with fine precision stroke and boxy joints.',
    styleDescription: 'Sharp miter joints, 1.7px stroke, technical aesthetic.',
  },
  {
    id: 'duotone',
    name: 'Duotone Glow',
    description: 'Two-tone soft shaded styling with subtle primary color fill.',
    styleDescription: 'Dual-opacity layered fill with tinted accents.',
  },
]

const THEME_STORAGE_KEY = 'dockdeploy:theme'
const ICON_PACK_STORAGE_KEY = 'dockdeploy:icon_pack'

interface ThemeContextValue {
  theme: ThemeId
  setTheme: (theme: ThemeId) => void
  iconPack: IconPackId
  setIconPack: (pack: IconPackId) => void
  themes: ThemeMeta[]
  iconPacks: IconPackMeta[]
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = useState<ThemeId>(() => {
    if (typeof window === 'undefined') return 'dark'
    const stored = localStorage.getItem(THEME_STORAGE_KEY) as ThemeId | null
    if (stored && THEMES.some((t) => t.id === stored)) {
      return stored
    }
    return 'dark'
  })

  const [iconPack, setIconPackState] = useState<IconPackId>(() => {
    if (typeof window === 'undefined') return 'lucide'
    const stored = localStorage.getItem(ICON_PACK_STORAGE_KEY) as IconPackId | null
    if (stored && ICON_PACKS.some((p) => p.id === stored)) {
      return stored
    }
    return 'lucide'
  })

  useEffect(() => {
    const root = document.documentElement
    root.setAttribute('data-theme', theme)

    const meta = THEMES.find((t) => t.id === theme)
    if (meta?.isDark) {
      root.classList.add('dark')
    } else {
      root.classList.remove('dark')
    }

    try {
      localStorage.setItem(THEME_STORAGE_KEY, theme)
    } catch {
      // ignore storage write errors
    }
  }, [theme])

  useEffect(() => {
    const root = document.documentElement
    root.setAttribute('data-icon-pack', iconPack)

    try {
      localStorage.setItem(ICON_PACK_STORAGE_KEY, iconPack)
    } catch {
      // ignore storage write errors
    }
  }, [iconPack])

  const setTheme = (nextTheme: ThemeId) => {
    setThemeState(nextTheme)
  }

  const setIconPack = (nextPack: IconPackId) => {
    setIconPackState(nextPack)
  }

  return (
    <ThemeContext.Provider
      value={{
        theme,
        setTheme,
        iconPack,
        setIconPack,
        themes: THEMES,
        iconPacks: ICON_PACKS,
      }}
    >
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  const ctx = useContext(ThemeContext)
  if (!ctx) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return ctx
}
