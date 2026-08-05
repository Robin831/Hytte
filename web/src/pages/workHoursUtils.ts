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

export function calculateDayWithLivePunch(
  now: Date,
  punchStart: string,
  sessions: WorkSession[],
  lunch: boolean,
  deductions: WorkDeduction[],
  settings: WorkSettings,
): LiveEstimate | null {
  const startMins = parseHHMM(punchStart)
  if (startMins === null) return null
  const nowMins = now.getHours() * 60 + now.getMinutes()
  if (nowMins < startMins) return null

  let gross = nowMins - startMins
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
