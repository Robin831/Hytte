import i18n from '../i18n'

function toDate(date: Date | string): Date {
  const d = typeof date === 'string' ? new Date(date) : date
  if (isNaN(d.getTime())) throw new RangeError(`Invalid date: ${date}`)
  return d
}

export function formatDate(date: Date | string, options?: Intl.DateTimeFormatOptions): string {
  return toDate(date).toLocaleDateString(i18n.language, options)
}

export function formatTime(date: Date | string, options?: Intl.DateTimeFormatOptions): string {
  return toDate(date).toLocaleTimeString(i18n.language, options)
}

export function formatNumber(n: number, options?: Intl.NumberFormatOptions): string {
  return n.toLocaleString(i18n.language, options)
}
