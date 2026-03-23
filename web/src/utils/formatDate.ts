import i18n from '../i18n'

function toDate(date: Date | string): Date {
  const d = typeof date === 'string' ? new Date(date) : date
  if (isNaN(d.getTime())) throw new RangeError(`Invalid date: ${date}`)
  return d
}

export function formatDate(date: Date | string, options?: Intl.DateTimeFormatOptions): string {
  const locale = i18n.language
  const opts: Intl.DateTimeFormatOptions = { ...options }
  // Force Gregorian calendar for Thai to avoid Buddhist Era year confusion
  if (locale === 'th' || locale.startsWith('th-')) {
    opts.calendar = 'gregory'
  }
  return toDate(date).toLocaleDateString(locale, opts)
}

export function formatTime(date: Date | string, options?: Intl.DateTimeFormatOptions): string {
  const locale = i18n.language
  const opts: Intl.DateTimeFormatOptions = { ...options }
  // Force Gregorian calendar for Thai to avoid Buddhist Era year confusion
  if (locale === 'th' || locale.startsWith('th-')) {
    opts.calendar = 'gregory'
  }
  return toDate(date).toLocaleTimeString(locale, opts)
}

export function formatNumber(n: number, options?: Intl.NumberFormatOptions): string {
  return n.toLocaleString(i18n.language, options)
}
