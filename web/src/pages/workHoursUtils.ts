import { parseHHMM, type WorkSession } from './workhours/calculateDay'

// The day math (durations, lunch, deductions, rounding, live punch estimate)
// lives in ./workhours/calculateDay, the single frontend mirror of the Go
// calculator. These re-exports keep existing imports resolving.
export type { WorkSession, WorkDeduction, WorkSettings, LiveEstimate } from './workhours/calculateDay'
export { daysSinceLocalDate, isPunchStale, sessionMinutes } from './workhours/calculateDay'

const MINUTES_PER_DAY = 24 * 60

/**
 * Finds sessions on the same day whose wall-clock times overlap, mirroring the
 * server-side overlap rule so stored overlaps (e.g. rows saved before the
 * server started rejecting them) can be flagged in the UI.
 *
 * Intervals are half-open ([start, end)), so back-to-back sessions do not
 * overlap. Each session is placed on the timeline of the day it started on
 * (a crosses-midnight session ends at end + 24h), and the internal flag is
 * ignored. Malformed times and empty sessions (end not after start) never
 * count as overlapping.
 *
 * Returns the ids of every overlapping session and each overlapping pair,
 * ordered by start time (earlier session first).
 */
export function findSessionOverlaps(sessions: WorkSession[]): {
  ids: Set<number>
  pairs: Array<[WorkSession, WorkSession]>
} {
  const bounded: Array<{ session: WorkSession; start: number; end: number }> = []
  for (const session of sessions) {
    const start = parseHHMM(session.start_time)
    const endMins = parseHHMM(session.end_time)
    if (Number.isNaN(start) || Number.isNaN(endMins)) continue
    const end = session.crosses_midnight ? endMins + MINUTES_PER_DAY : endMins
    if (end <= start) continue
    bounded.push({ session, start, end })
  }
  bounded.sort((a, b) => a.start - b.start || a.end - b.end || a.session.id - b.session.id)

  const ids = new Set<number>()
  const pairs: Array<[WorkSession, WorkSession]> = []
  for (let i = 0; i < bounded.length; i++) {
    const a = bounded[i]
    // Sorted by start, so once a later session starts at or after a's end,
    // none of the following ones can overlap a either.
    for (let j = i + 1; j < bounded.length && bounded[j].start < a.end; j++) {
      const b = bounded[j]
      ids.add(a.session.id)
      ids.add(b.session.id)
      pairs.push([a.session, b.session])
    }
  }
  return { ids, pairs }
}
