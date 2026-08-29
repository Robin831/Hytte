import type { WeekSummary } from '../../types/stride'

// What actually happened in one week of a macro block, read off the plan
// history. A week the athlete has not trained yet has no entry at all, which is
// what lets the week list tell "0 km done" apart from "not due yet".
export interface MacroWeekActual {
  km: number
  sessionsCompleted: number
  sessionsPlanned: number
}

// Indexes /api/stride/history weeks by their Monday so a macro week can look up
// its own actuals in O(1).
//
// History pages can overlap (the list paginates while the block list re-reads
// page 0) and a week can in principle appear twice; the first entry wins, since
// the API returns the newest page first and a later page repeating a week is
// the stale copy. Distance arrives in metres and is converted once here so no
// caller has to remember the unit.
export function actualsByWeek(weeks: WeekSummary[]): Map<string, MacroWeekActual> {
  const byWeek = new Map<string, MacroWeekActual>()
  for (const week of weeks) {
    if (!week.week_start || byWeek.has(week.week_start)) continue
    byWeek.set(week.week_start, {
      km: (week.total_distance_meters ?? 0) / 1000,
      sessionsCompleted: week.sessions_completed ?? 0,
      sessionsPlanned: week.sessions_planned ?? 0,
    })
  }
  return byWeek
}
