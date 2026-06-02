import type { DaySummary, LeaveType, WorkDay } from './types'

// ── Time/format helpers ─────────────────────────────────────────────────────

export function formatMins(mins: number): string {
  const abs = Math.abs(mins)
  const h = Math.floor(abs / 60)
  const m = abs % 60
  const prefix = mins < 0 ? '-' : ''
  return `${prefix}${h}:${m.toString().padStart(2, '0')}`
}

export function formatHours(minutes: number): string {
  return (minutes / 60).toFixed(1) + 'h'
}

export function localDateStr(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

export function getInitialDate(holidays: Set<string>): string {
  const d = new Date()
  while (d.getDay() === 0 || d.getDay() === 6 || holidays.has(localDateStr(d))) {
    d.setDate(d.getDate() - 1)
  }
  return localDateStr(d)
}

export function prevWeekday(date: string, holidays: Set<string>): string {
  const d = new Date(date + 'T12:00:00')
  d.setDate(d.getDate() - 1)
  while (d.getDay() === 0 || d.getDay() === 6 || holidays.has(localDateStr(d))) {
    d.setDate(d.getDate() - 1)
  }
  return localDateStr(d)
}

export function nextWeekday(date: string, holidays: Set<string>): string {
  const d = new Date(date + 'T12:00:00')
  d.setDate(d.getDate() + 1)
  while (d.getDay() === 0 || d.getDay() === 6 || holidays.has(localDateStr(d))) {
    d.setDate(d.getDate() + 1)
  }
  return localDateStr(d)
}

export function addWeeks(date: string, weeks: number): string {
  const d = new Date(date + 'T12:00:00')
  d.setDate(d.getDate() + weeks * 7)
  return localDateStr(d)
}

export function addMonths(monthStr: string, n: number): string {
  const [y, m] = monthStr.split('-').map(Number)
  const d = new Date(y, m - 1 + n, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

export function dateToMonthStr(date: string): string {
  return date.substring(0, 7)
}

// Returns session range (earliest start, latest end) for a day
export function sessionRange(day: WorkDay | undefined): { start: string; end: string } | null {
  if (!day || !day.sessions || day.sessions.length === 0) return null
  const sorted = [...day.sessions].sort((a, b) => a.start_time.localeCompare(b.start_time))
  const start = sorted[0].start_time
  const end = sorted.reduce((mx, s) => (s.end_time > mx ? s.end_time : mx), sorted[0].end_time)
  return { start, end }
}

// Returns the 5 Mon-Fri dates for the week starting at weekStart
export function weekDays(weekStart: string): string[] {
  const result: string[] = []
  const d = new Date(weekStart + 'T12:00:00')
  for (let i = 0; i < 5; i++) {
    result.push(localDateStr(d))
    d.setDate(d.getDate() + 1)
  }
  return result
}

// Builds a month grid of weeks (Mon–Sun), with null for out-of-month cells
export function buildMonthGrid(monthStr: string): (string | null)[][] {
  const year = parseInt(monthStr.split('-')[0])
  const month = parseInt(monthStr.split('-')[1]) - 1
  const numDays = new Date(year, month + 1, 0).getDate()

  // Offset for Monday-based week (Mon=0 ... Sun=6)
  let startDow = new Date(year, month, 1).getDay() - 1
  if (startDow < 0) startDow = 6

  const cells: (string | null)[] = Array(startDow).fill(null)
  for (let d = 1; d <= numDays; d++) {
    cells.push(`${monthStr}-${String(d).padStart(2, '0')}`)
  }
  while (cells.length % 7 !== 0) cells.push(null)

  const weeks: (string | null)[][] = []
  for (let i = 0; i < cells.length; i += 7) {
    weeks.push(cells.slice(i, i + 7))
  }
  return weeks
}

// Count Mon-Fri days in month up to today (or end of month if past), excluding holidays
export function countWorkdaysUpToNow(monthStr: string, holidays?: Set<string>): number {
  const year = parseInt(monthStr.split('-')[0])
  const month = parseInt(monthStr.split('-')[1]) - 1
  const numDays = new Date(year, month + 1, 0).getDate()
  const today = new Date()
  let count = 0
  for (let d = 1; d <= numDays; d++) {
    const date = new Date(year, month, d)
    if (date > today) break
    const dow = date.getDay()
    if (dow === 0 || dow === 6) continue
    if (holidays?.has(localDateStr(date))) continue
    count++
  }
  return count
}

// Count all Mon-Fri days in a full month (used for past/future months), excluding holidays
export function countWorkdaysInMonth(mStr: string, holidays?: Set<string>): number {
  if (!mStr || mStr.length < 7) return 0
  const parts = mStr.split('-')
  if (parts.length < 2) return 0
  const year = Number(parts[0])
  const month = Number(parts[1])
  if (!Number.isFinite(year) || !Number.isFinite(month) || month < 1 || month > 12) return 0

  const start = new Date(year, month - 1, 1)
  const end = new Date(year, month, 1)
  let workdays = 0

  for (let d = new Date(start); d < end; d.setDate(d.getDate() + 1)) {
    const dow = d.getDay()
    if (dow === 0 || dow === 6) continue
    if (holidays?.has(localDateStr(d))) continue
    workdays++
  }

  return workdays
}

// Returns Tailwind classes for a calendar cell based on reported hours
export function dayCellClass(summary: DaySummary | undefined, isWeekend: boolean, isHoliday?: boolean, leaveType?: LeaveType): string {
  if (isWeekend || isHoliday) return 'bg-gray-900/30 text-gray-600'
  if (leaveType === 'vacation') return 'bg-purple-900/60 text-purple-200'
  if (leaveType === 'sick') return 'bg-orange-900/60 text-orange-200'
  if (leaveType === 'personal') return 'bg-teal-900/60 text-teal-200'
  if (leaveType === 'public_holiday') return 'bg-gray-900/30 text-gray-600'
  if (!summary || summary.reported_minutes === 0) return 'bg-gray-800/40 text-gray-400'
  if (summary.reported_minutes > summary.standard_minutes) return 'bg-blue-900/60 text-blue-200'
  if (summary.reported_minutes === summary.standard_minutes) return 'bg-green-900/60 text-green-200'
  return 'bg-amber-900/60 text-amber-200'
}

// ── Norwegian public holidays ──────────────────────────────────────────────

function getEasterSunday(year: number): Date {
  const a = year % 19
  const b = Math.floor(year / 100)
  const c = year % 100
  const d = Math.floor(b / 4)
  const e = b % 4
  const f = Math.floor((b + 8) / 25)
  const g = Math.floor((b - f + 1) / 3)
  const h = (19 * a + b - d - g + 15) % 30
  const i = Math.floor(c / 4)
  const k = c % 4
  const l = (32 + 2 * e + 2 * i - h - k) % 7
  const m = Math.floor((a + 11 * h + 22 * l) / 451)
  const month = Math.floor((h + l - 7 * m + 114) / 31)
  const day = ((h + l - 7 * m + 114) % 31) + 1
  return new Date(year, month - 1, day)
}

function addDays(date: Date, n: number): Date {
  const r = new Date(date)
  r.setDate(r.getDate() + n)
  return r
}

// Returns a Map of YYYY-MM-DD → Norwegian holiday name for the given year
export function getNorwegianHolidays(year: number): Map<string, string> {
  const m = new Map<string, string>()
  const fmt = (d: Date) => localDateStr(d)
  m.set(`${year}-01-01`, 'Nyttårsdag')
  m.set(`${year}-05-01`, 'Arbeidernes dag')
  m.set(`${year}-05-17`, 'Grunnlovsdagen')
  m.set(`${year}-12-25`, '1. juledag')
  m.set(`${year}-12-26`, '2. juledag')
  const easter = getEasterSunday(year)
  m.set(fmt(addDays(easter, -3)), 'Skjærtorsdag')
  m.set(fmt(addDays(easter, -2)), 'Langfredag')
  m.set(fmt(easter), '1. påskedag')
  m.set(fmt(addDays(easter, 1)), '2. påskedag')
  m.set(fmt(addDays(easter, 39)), 'Kristi himmelfartsdag')
  m.set(fmt(addDays(easter, 49)), '1. pinsedag')
  m.set(fmt(addDays(easter, 50)), '2. pinsedag')
  return m
}

// Build a holiday set covering the given year and its adjacent years so that
// day-by-day navigation across year boundaries (Dec↔Jan) never loses holidays.
export function buildNavHolidaySet(year: number): Set<string> {
  return new Set<string>([
    ...getNorwegianHolidays(year - 1).keys(),
    ...getNorwegianHolidays(year).keys(),
    ...getNorwegianHolidays(year + 1).keys(),
  ])
}

export function currentTimeHHMM(): string {
  const now = new Date()
  return `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`
}
