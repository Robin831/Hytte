import type { SimpleDate, Weekday } from './doomsday'

/** Intl locale for the app language (Thai with the Gregorian calendar). */
export function intlLocale(lang: string): string {
  if (lang.startsWith('th')) return 'th-TH-u-ca-gregory'
  if (lang.startsWith('nb')) return 'nb-NO'
  return 'en-GB'
}

const utc = (d: SimpleDate) => new Date(Date.UTC(d.y, d.m - 1, d.d))

/** Weekday name; 1 Jan 2023 was a Sunday (0). */
export function weekdayName(wd: Weekday, lang: string, style: 'long' | 'short' = 'long'): string {
  return new Date(Date.UTC(2023, 0, 1 + wd)).toLocaleDateString(intlLocale(lang), { weekday: style, timeZone: 'UTC' })
}

export function monthName(m: number, lang: string): string {
  return new Date(Date.UTC(2023, m - 1, 1)).toLocaleDateString(intlLocale(lang), { month: 'long', timeZone: 'UTC' })
}

/** A date without its weekday, e.g. "17 May 1814". */
export function dateText(d: SimpleDate, lang: string): string {
  const date = utc(d)
  date.setUTCFullYear(d.y) // years below 100 would otherwise shift
  return date.toLocaleDateString(intlLocale(lang), { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' })
}

/** Day and month only, e.g. "14 March". */
export function dayMonthText(day: number, m: number, lang: string): string {
  return new Date(Date.UTC(2023, m - 1, day)).toLocaleDateString(intlLocale(lang), { day: 'numeric', month: 'long', timeZone: 'UTC' })
}

/** "the 1900s" style century label: the first year of the century. */
export function centuryStart(y: number): number {
  return Math.floor(y / 100) * 100
}

/** Is the date in the past (for "was" vs "is")? */
export function isPast(d: SimpleDate, today = new Date()): boolean {
  return utc(d).getTime() < Date.UTC(today.getFullYear(), today.getMonth(), today.getDate())
}
