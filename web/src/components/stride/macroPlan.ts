import type { MacroWeek } from '../../types/stride'

// Shared helpers for reading a macro block's week rows. The timeline, the
// long-term plan card and the weekly header all have to agree on which week the
// athlete is in, so the lookups live in one place.
//
// Everything that decides *which* week a date belongs to compares YYYY-MM-DD
// week keys rather than millisecond offsets: adding 7 * 24 h to a Monday lands
// an hour either side of the next Monday across a DST change, which is exactly
// the boundary these lookups sit on.

export const MS_PER_WEEK = 7 * 24 * 60 * 60 * 1000

// Parses a YYYY-MM-DD Monday as local midnight. Appending the time is what
// keeps it local — `new Date('2026-01-05')` would be parsed as UTC and land on
// the previous day west of Greenwich.
export function mondayToDate(weekStart: string): Date {
  return new Date(`${weekStart}T00:00:00`)
}

function dateKey(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

// The week key (Monday, YYYY-MM-DD) the given date falls in, in local time.
export function mondayKeyOf(date: Date): string {
  const d = new Date(date.getFullYear(), date.getMonth(), date.getDate())
  d.setDate(d.getDate() - ((d.getDay() + 6) % 7))
  return dateKey(d)
}

// The week key `weeks` weeks after weekStart. setDate walks calendar days, so
// the result stays a local midnight Monday across DST changes.
export function addWeeks(weekStart: string, weeks: number): string {
  const d = mondayToDate(weekStart)
  d.setDate(d.getDate() + weeks * 7)
  return dateKey(d)
}

// The block week a weekly plan materialises, matched on its Monday. Used by the
// week header, where the plan on screen is not always the current week.
export function macroWeekForWeekStart(weeks: MacroWeek[], weekStart: string): MacroWeek | null {
  return weeks.find(w => w.week_start === weekStart) ?? null
}

// The block week containing `date`, or null when the date falls outside the
// horizon (a block that starts next Monday, or one that has already ended).
export function macroWeekForDate(weeks: MacroWeek[], date: Date): MacroWeek | null {
  return macroWeekForWeekStart(weeks, mondayKeyOf(date))
}

// One run of consecutive weeks sharing a value — a four-week build collapses
// into a single run rather than four entries.
export interface MacroWeekRun {
  value: string
  startWeek: MacroWeek
  weeks: number
}

// Collapses consecutive weeks sharing the same value of `pick` into runs.
export function groupWeekRuns(weeks: MacroWeek[], pick: (week: MacroWeek) => string): MacroWeekRun[] {
  const runs: MacroWeekRun[] = []
  for (const week of weeks) {
    const value = pick(week)
    const last = runs[runs.length - 1]
    if (last && last.value === value) {
      last.weeks += 1
    } else {
      runs.push({ value, startWeek: week, weeks: 1 })
    }
  }
  return runs
}

// Sorts a block's weeks by Monday without mutating the caller's array. The
// server already returns them in order; the copy makes that independent of it.
export function sortMacroWeeks(weeks: MacroWeek[]): MacroWeek[] {
  return [...weeks].sort((a, b) => a.week_start.localeCompare(b.week_start))
}

// How many whole weeks of the block are still ahead of `fromWeek`, both
// arguments being Monday week keys. Mirrors the backend's weeksRemaining
// (internal/stride/macro_schedule.go): the difference is floored, so a horizon
// that ends this week reads as 0 rather than as a week the athlete still has.
//
// The day difference is rounded before the division because mondayToDate builds
// local midnights and a DST change puts an hour either side of a whole day.
export function weeksRemaining(fromWeek: string, endWeek: string): number {
  const from = mondayToDate(fromWeek)
  const end = mondayToDate(endWeek)
  if (isNaN(from.getTime()) || isNaN(end.getTime())) return 0
  const days = Math.round((end.getTime() - from.getTime()) / (24 * 60 * 60 * 1000))
  return Math.floor(days / 7)
}

// Left-edge accent per macro phase. The timeline, the mesocycle strip and the
// week list all colour a phase the same way, so the palette lives here rather
// than being re-declared next to each of them.
const PHASE_ACCENT: Record<string, string> = {
  base: 'border-blue-500/60',
  build: 'border-green-500/60',
  peak: 'border-orange-500/60',
  taper: 'border-red-500/60',
  race: 'border-yellow-500/60',
  recovery: 'border-purple-500/60',
}

// The border class for a phase, falling back to a neutral edge for a phase the
// palette does not know (a block written before a phase was renamed).
export function phaseAccent(phase: string): string {
  return PHASE_ACCENT[phase] ?? 'border-gray-600'
}
