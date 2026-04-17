export interface WorkSession {
  id: number
  day_id: number
  start_time: string
  end_time: string
  sort_order: number
  is_internal: boolean
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

function parseHHMM(t: string): number | null {
  const parts = t.split(':')
  if (parts.length !== 2) return null
  const h = Number(parts[0])
  const m = Number(parts[1])
  if (!Number.isInteger(h) || !Number.isInteger(m)) return null
  if (h < 0 || h > 23 || m < 0 || m > 59) return null
  return h * 60 + m
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
    const sMins = parseHHMM(s.start_time)
    const eMins = parseHHMM(s.end_time)
    if (sMins === null || eMins === null) return null
    const sessionDuration = eMins - sMins
    gross += Math.max(sessionDuration, 0)
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
