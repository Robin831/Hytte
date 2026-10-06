// Local calendar-date helpers. Date-only values (`YYYY-MM-DD`) describe a day
// on the user's wall calendar, so they must be built from and parsed into
// local components — `toISOString()` and `new Date('YYYY-MM-DD')` both use UTC
// and shift the day by one for users away from UTC+0.

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

/** Formats `d` (default: now) as a local `YYYY-MM-DD` date string. */
export function localDateString(d: Date = new Date()): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})$/

/**
 * Parses a `YYYY-MM-DD` string as local midnight on that day. Anything else
 * (e.g. a full ISO timestamp) falls back to `new Date(s)`.
 */
export function parseLocalDate(s: string): Date {
  const m = DATE_ONLY.exec(s)
  if (!m) return new Date(s)
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
}
