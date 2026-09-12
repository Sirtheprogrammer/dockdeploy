import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** Merge conditional class names, letting later Tailwind utilities win. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** Format bytes for image sizes and disk usage. */
export function formatBytes(bytes: number, decimals = 1): string {
  if (bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, i)
  return `${i === 0 ? value : value.toFixed(decimals)} ${units[i]}`
}

/** Compact relative time ("4m ago"), for last-seen and run timestamps. */
export function formatRelative(input: string | number | Date): string {
  const then = new Date(input).getTime()
  if (Number.isNaN(then)) return '—'

  const seconds = Math.round((Date.now() - then) / 1000)
  const abs = Math.abs(seconds)
  if (abs < 10) return 'just now'

  const units: [size: number, suffix: string][] = [
    [86400 * 30, 'mo'],
    [86400, 'd'],
    [3600, 'h'],
    [60, 'm'],
    [1, 's'],
  ]
  const [size, suffix] = units.find(([s]) => abs >= s) ?? [1, 's']
  const label = `${Math.floor(abs / size)}${suffix}`
  return seconds < 0 ? `in ${label}` : `${label} ago`
}
