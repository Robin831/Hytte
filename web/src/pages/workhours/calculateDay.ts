/**
 * Day math for the Work Hours feature.
 *
 * The source of truth is `CalculateDay` in internal/workhours/calculator.go.
 * This module is its TypeScript mirror and the ONLY frontend implementation of
 * the day math (session durations, lunch, deductions, clamping, rounding,
 * remainder and balance). The two must change together, and
 * calculateDay.test.ts replays every CalculateDay case from calculator_test.go
 * with the same inputs and expected outputs to keep them pinned.
 *
 * The only addition over the Go version is the optional live segment for an
 * open punch-in, which the server never sees until the punch-out is saved.
 */

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

/** Result of {@link calculateDay}; every field is in minutes, like Go's DaySummary. */
export interface DaySummaryCalc {
  gross: number
  lunch: number
  deductions: number
  net: number
  /** net rounded down to the settings rounding interval. */
  reported: number
  /** net - reported; goes to the flex pool. */
  remainder: number
  /** reported - standard day. */
  balance: number
}

const MINUTES_PER_DAY = 24 * 60

/** Fallback rounding interval when the setting is missing or not positive (Go: 30). */
const DEFAULT_ROUNDING_MINUTES = 30

/**
 * How many calendar days an open punch-in may wrap over before the live
 * estimate is treated as stale. One day covers the intended case — a shift
 * started in the evening and still running after midnight. A punch-in that is
 * older than that was almost certainly forgotten, and counting the sleeping
 * hours in between would produce a meaningless running total.
 */
const MAX_LIVE_PUNCH_WRAP_DAYS = 1

// Mirrors strconv.Atoi: optional sign, then decimal digits only.
const ATOI_RE = /^[+-]?\d+$/

/**
 * Go's DefaultSettings (internal/workhours/calculator.go). Frozen because it is
 * shared: Go returns a fresh value per call, so a mutation here must not leak
 * into later DayView mounts or parseWorkSettings fallbacks.
 */
export const DEFAULT_WORK_SETTINGS: Readonly<WorkSettings> = Object.freeze({
  standard_day_minutes: 450,
  lunch_minutes: 30,
  rounding_minutes: 30,
})

/**
 * Builds WorkSettings from the user's preferences, mirroring Go's loadSettings
 * (internal/workhours/handlers.go): each value must parse as strconv.Atoi
 * would, standard day and rounding must be positive and lunch non-negative;
 * anything else falls back to the default.
 */
export function parseWorkSettings(prefs: Record<string, string | undefined>): WorkSettings {
  const parse = (val: string | undefined, fallback: number, ok: (n: number) => boolean): number => {
    if (val === undefined || !ATOI_RE.test(val)) return fallback
    const n = Number(val)
    return Number.isSafeInteger(n) && ok(n) ? n : fallback
  }
  return {
    standard_day_minutes: parse(prefs.work_hours_standard_day, DEFAULT_WORK_SETTINGS.standard_day_minutes, n => n > 0),
    lunch_minutes: parse(prefs.work_hours_lunch_minutes, DEFAULT_WORK_SETTINGS.lunch_minutes, n => n >= 0),
    rounding_minutes: parse(prefs.work_hours_rounding, DEFAULT_WORK_SETTINGS.rounding_minutes, n => n > 0),
  }
}

/**
 * Parses "HH:MM" into minutes since midnight, mirroring Go's parseHHMM. Input
 * Go rejects with an error (wrong shape, non-numeric parts, hour outside 0-23,
 * minute outside 0-59) yields NaN.
 */
export function parseHHMM(s: string): number {
  const idx = s.indexOf(':')
  if (idx < 0) return NaN
  const hs = s.slice(0, idx)
  const ms = s.slice(idx + 1)
  if (!ATOI_RE.test(hs) || !ATOI_RE.test(ms)) return NaN
  const h = Number(hs)
  const m = Number(ms)
  if (h < 0 || h > 23 || m < 0 || m > 59) return NaN
  return h * 60 + m
}

/**
 * Duration of a session in minutes, mirroring Go's SessionDurationMinutes: a
 * session flagged as crossing midnight ends on the following day, so a full
 * day is added before subtracting. Returns null when either time is malformed
 * (where Go returns an error).
 */
export function sessionMinutes(session: Pick<WorkSession, 'start_time' | 'end_time' | 'crosses_midnight'>): number | null {
  const startMins = parseHHMM(session.start_time)
  const endMins = parseHHMM(session.end_time)
  if (Number.isNaN(startMins) || Number.isNaN(endMins)) return null
  const end = session.crosses_midnight ? endMins + MINUTES_PER_DAY : endMins
  return Math.max(end - startMins, 0)
}

/**
 * Whole calendar days from a YYYY-MM-DD date to the local date of `now`.
 * Uses UTC midnights so a DST transition in between can't skew the count.
 * Returns null when the date is malformed.
 */
export function daysSinceLocalDate(date: string, now: Date): number | null {
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
 * Whether an open punch-in is too old to keep estimating: it has been left
 * running for more calendar days than {@link MAX_LIVE_PUNCH_WRAP_DAYS} allows,
 * so it was forgotten rather than still ticking. This is the single definition
 * of staleness — {@link liveSegmentMinutes} bails on it, and callers use it to
 * tell that bail apart from an unusable start time.
 */
export function isPunchStale(punchDate: string | null, now: Date): boolean {
  if (!punchDate) return false
  const elapsedDays = daysSinceLocalDate(punchDate, now)
  return elapsedDays !== null && elapsedDays > MAX_LIVE_PUNCH_WRAP_DAYS
}

/**
 * Minutes elapsed on an open punch-in that started at `punchStart` (HH:MM),
 * measured up to `now`.
 *
 * `punchDate` is the YYYY-MM-DD the punch-in was recorded on, or null when it
 * is unknown. A clock time earlier than the start time only makes sense as a
 * session that ran past midnight, so exactly one day is added — and only when
 * `punchDate` confirms the punch-in belongs to an earlier calendar day. That
 * keeps a shift started at 22:00 and still open at 01:00 counting instead of
 * looking like a start in the future, while never adding time the punch-out
 * itself could not record: `handlePunchOut` marks a session as crossing
 * midnight only when the end time is strictly before the start time, and
 * refuses an end time equal to it as a zero-length punch. So the wrap needs a
 * strictly negative difference — at exactly the start time the segment reads
 * 0, and there is no recorded session for it to disagree with.
 *
 * Returns null when the segment cannot be measured: the start time is
 * malformed, the punch-in is stale (see {@link isPunchStale}), or the start is
 * later than `now` without a proven wrap (a wrapped session and a genuinely
 * future start are indistinguishable without `punchDate`).
 */
export function liveSegmentMinutes(punchStart: string, now: Date, punchDate: string | null = null): number | null {
  const startMins = parseHHMM(punchStart)
  if (Number.isNaN(startMins)) return null
  if (isPunchStale(punchDate, now)) return null
  const nowMins = now.getHours() * 60 + now.getMinutes()

  const elapsed = nowMins - startMins
  if (elapsed >= 0) return elapsed
  // The clock has wrapped. Only a punch-in dated to an earlier calendar day
  // explains that; dated today it is simply a start time in the future.
  const elapsedDays = punchDate ? daysSinceLocalDate(punchDate, now) : null
  if (elapsedDays === null || elapsedDays < 1) return null
  return elapsed + MINUTES_PER_DAY
}

export interface CalculateDayOptions {
  /** Whether the day's lunch box is checked. Defaults to false (Go's zero value). */
  lunch?: boolean
  /** HH:MM start of an open punch-in; adds a live segment from it to `now`. */
  openPunchStart?: string
  /** YYYY-MM-DD the open punch-in was recorded on, used to prove a midnight wrap. */
  openPunchDate?: string | null
  /** Clock used for the live segment. Defaults to the current time. */
  now?: Date
}

/**
 * Computes gross/net/reported minutes, remainder and balance for a work day.
 * Port of Go's CalculateDay (internal/workhours/calculator.go):
 *
 *	gross     = sum of session durations (+ live segment, if any)
 *	deduct    = lunch (if checked) + custom deductions
 *	net       = gross - deduct  (clamped to 0)
 *	reported  = floor(net / rounding) * rounding
 *	remainder = net - reported  → goes to flex pool
 *	balance   = reported - standard day
 *
 * Throws when a session has a malformed time, where Go returns an error. A live
 * segment that cannot be measured (see {@link liveSegmentMinutes}) counts as 0;
 * callers that need to distinguish that case check liveSegmentMinutes first.
 */
export function calculateDay(
  sessions: WorkSession[],
  deductions: WorkDeduction[],
  settings: WorkSettings,
  opts: CalculateDayOptions = {},
): DaySummaryCalc {
  let rounding = settings.rounding_minutes
  if (!(rounding > 0)) {
    rounding = DEFAULT_ROUNDING_MINUTES
  }

  let gross = 0
  for (const s of sessions) {
    const min = sessionMinutes(s)
    if (min === null) {
      throw new Error(`session ${s.id}: invalid time ${s.start_time}-${s.end_time}`)
    }
    gross += min
  }
  if (opts.openPunchStart !== undefined) {
    gross += liveSegmentMinutes(opts.openPunchStart, opts.now ?? new Date(), opts.openPunchDate ?? null) ?? 0
  }

  const lunch = opts.lunch ? settings.lunch_minutes : 0

  let custom = 0
  for (const d of deductions) {
    custom += d.minutes
  }

  const net = Math.max(gross - lunch - custom, 0)

  // Go's integer division truncates; net is never negative here.
  const reported = Math.trunc(net / rounding) * rounding
  const remainder = net - reported

  const balance = reported - settings.standard_day_minutes

  return {
    gross,
    lunch,
    deductions: custom,
    net,
    reported,
    remainder,
    balance,
  }
}

/**
 * Credits flex minutes redeemed on the day into reported and balance, as the
 * server does after CalculateDay. Port of Go's applyRedemptionToSummary
 * (internal/workhours/handlers.go); non-positive amounts are ignored.
 * Gross, net and remainder are untouched: redeemed time is not worked time.
 */
export function applyRedemption(calc: DaySummaryCalc, redeemedMinutes: number): DaySummaryCalc {
  if (!(redeemedMinutes > 0)) return calc
  return {
    ...calc,
    reported: calc.reported + redeemedMinutes,
    balance: calc.balance + redeemedMinutes,
  }
}
