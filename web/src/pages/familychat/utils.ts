// formatRelative renders an ISO timestamp as a localized relative-time string
// (e.g. "2 minutes ago"). Falls back to an empty string for invalid input.
// The caller is responsible for creating and memoizing the rtf instance.
export function formatRelative(iso: string, rtf: Intl.RelativeTimeFormat, justNow: string): string {
  if (!iso) return ''
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const now = Date.now()
  const diffSec = Math.round((then - now) / 1000)
  const abs = Math.abs(diffSec)
  if (abs < 30) return justNow
  if (abs < 60) return rtf.format(diffSec, 'second')
  if (abs < 60 * 60) return rtf.format(Math.round(diffSec / 60), 'minute')
  if (abs < 60 * 60 * 24) return rtf.format(Math.round(diffSec / 3600), 'hour')
  if (abs < 60 * 60 * 24 * 7) return rtf.format(Math.round(diffSec / 86400), 'day')
  if (abs < 60 * 60 * 24 * 30) return rtf.format(Math.round(diffSec / (86400 * 7)), 'week')
  if (abs < 60 * 60 * 24 * 365) return rtf.format(Math.round(diffSec / (86400 * 30)), 'month')
  return rtf.format(Math.round(diffSec / (86400 * 365)), 'year')
}

// formatFileSize renders a byte count as a human-readable string using decimal
// (SI) units: 1 KB = 1000 B, 1 MB = 1 000 000 B. One decimal is shown for
// values below 10 (e.g. "9.5 MB"). Used by attachment chips and download links.
export function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return ''
  if (bytes < 1000) return `${bytes} B`
  const units = ['KB', 'MB', 'GB']
  let value = bytes / 1000
  let i = 0
  while (value >= 1000 && i < units.length - 1) {
    value /= 1000
    i++
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[i]}`
}

