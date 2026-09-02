export interface WorkSession {
  id: number
  day_id: number
  start_time: string
  end_time: string
  sort_order: number
  is_internal: boolean
  /** true when end_time falls on the day after start_time (e.g. 22:00 → 02:00). */
  crosses_midnight: boolean
}

export interface WorkDeduction {
  id: number
  day_id: number
  name: string
  minutes: number
  preset_id?: number | null
}

export interface WorkSettings {
  standard_day_minutes: number
  lunch_minutes: number
  rounding_minutes: number
}

export interface LiveEstimate {
  grossMinutes: number
  lunchMinutes: number
  deductionMinutes: number
  netMinutes: number
  reportedMinutes: number
  standardMinutes: number
}

const MINUTES_PER_DAY = 24 * 60

function parseHHMM(t: string): number | null {
  const parts = t.split(':')
  if (parts.length !== 2) return null
  const h = Number(parts[0])
  const m = Number(parts[1])
  if (!Number.isInteger(h) || !Number.isInteger(m)) return null
  if (h < 0 || h > 23 || m < 0 || m > 59) return null
  return h * 60 + m
}

/**
 * Whole calendar days from a YYYY-MM-DD date to the local date of `now`.
 * Uses UTC midnights so a DST transition in between can't skew the count.
 * Returns null when the date is malformed.
 */
function daysSinceLocalDate(date: string, now: Date): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  if (!m) return null
  const year = Number(m[1])
  const month = Number(m[2])
  const day = Number(m[3])
  const start = new Date(Date.UTC(year, month - 1, day))
  // Reject values that rolled over (e.g. 2026-02-31 → March 3).
  if (start.getUTCFullYear() !== year || start.getUTCMonth() !== month - 1 || start.getUTCDate() !== day) {
    return null
  }
  const today = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate())
  return Math.round((today - start.getTime()) / (MINUTES_PER_DAY * 60_000))
}

/**
 * Duration of a session in minutes, mirroring the server-side rule: a session
 * flagged as crossing midnight ends on the following day, so a full day is
 * added before subtracting. Returns null when either time is malformed.
 */
export function sessionMinutes(session: Pick<WorkSession, 'start_time' | 'end_time' | 'crosses_midnight'>): number | null {
  const startMins = parseHHMM(session.start_time)
  const endMins = parseHHMM(session.end_time)
  if (startMins === null || endMins === null) return null
  const end = session.crosses_midnight ? endMins + MINUTES_PER_DAY : endMins
  return Math.max(end - startMins, 0)
}

/**
 * Live estimate for a day with an open punch-in.
 *
 * `punchDate` is the YYYY-MM-DD the punch-in was recorded on. When it is
 * supplied, the elapsed time spans the calendar days between it and `now`, so a
 * shift started at 22:00 and still open at 01:00 keeps counting instead of
 * looking like a start in the future. Without it a clock time earlier than the
 * start is ambiguous — a wrapped session and a genuinely future start are
 * indistinguishable — so null is returned as before.
 */
export function calculateDayWithLivePunch(
  now: Date,
  punchStart: string,
  sessions: WorkSession[],
  lunch: boolean,
  deductions: WorkDeduction[],
  settings: WorkSettings,
  punchDate?: string | null,
): LiveEstimate | null {
  const startMins = parseHHMM(punchStart)
  if (startMins === null) return null
  const nowMins = now.getHours() * 60 + now.getMinutes()

  const elapsedDays = punchDate ? daysSinceLocalDate(punchDate, now) : null
  const elapsed = nowMins - startMins + (elapsedDays ?? 0) * MINUTES_PER_DAY
  if (elapsed < 0) return null

  let gross = elapsed
  for (const s of sessions) {
    const duration = sessionMinutes(s)
    if (duration === null) return null
    gross += duration
  }

  const lunchMin = lunch ? settings.lunch_minutes : 0
  let customMin = 0
  for (const d of deductions) {
    customMin += d.minutes
  }

  const net = Math.max(gross - lunchMin - customMin, 0)
  const rounding = settings.rounding_minutes > 0 ? settings.rounding_minutes : 30
  const reportedMin = Math.floor(net / rounding) * rounding

  return {
    grossMinutes: gross,
    lunchMinutes: lunchMin,
    deductionMinutes: customMin,
    netMinutes: net,
    reportedMinutes: reportedMin,
    standardMinutes: settings.standard_day_minutes,
  }
}
