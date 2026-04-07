import i18n from '../i18n'

function toDate(date: Date | string): Date {
  const d = typeof date === 'string' ? new Date(date) : date
  if (isNaN(d.getTime())) throw new RangeError(`Invalid date: ${date}`)
  return d
}

// Force Gregorian calendar for Thai to avoid Buddhist Era year confusion
function resolveLocaleOptions(options?: Intl.DateTimeFormatOptions): { locale: string; opts: Intl.DateTimeFormatOptions } {
  const locale = i18n.language
  const opts: Intl.DateTimeFormatOptions = { ...options }
  if (locale === 'th' || locale.startsWith('th-')) {
    opts.calendar = 'gregory'
  }
  return { locale, opts }
}

export function formatDate(date: Date | string, options?: Intl.DateTimeFormatOptions): string {
  const { locale, opts } = resolveLocaleOptions(options)
  return toDate(date).toLocaleDateString(locale, opts)
}

export function formatTime(date: Date | string, options?: Intl.DateTimeFormatOptions): string {
  const { locale, opts } = resolveLocaleOptions({ ...options, hour12: false })
  return toDate(date).toLocaleTimeString(locale, opts)
}

export function formatDateTime(date: Date | string, options?: Intl.DateTimeFormatOptions): string {
  const { locale, opts } = resolveLocaleOptions({ ...options, hour12: false })
  return toDate(date).toLocaleString(locale, opts)
}

export function formatNumber(n: number, options?: Intl.NumberFormatOptions): string {
  return n.toLocaleString(i18n.language, options)
}

/** Returns local date as YYYY-MM-DD (avoids UTC drift from toISOString). */
export function toLocalDateString(date?: Date): string {
  const d = date ?? new Date()
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}
