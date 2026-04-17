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

export function calculateDayWithLivePunch(
  now: Date,
  punchStart: string,
  sessions: WorkSession[],
  lunch: boolean,
  deductions: WorkDeduction[],
  settings: WorkSettings,
): LiveEstimate | null {
  const [sh, sm] = punchStart.split(':').map(Number)
  const startMins = sh * 60 + sm
  const nowMins = now.getHours() * 60 + now.getMinutes()
  if (nowMins < startMins) return null

  let gross = nowMins - startMins
  for (const s of sessions) {
    const [sSh, sSm] = s.start_time.split(':').map(Number)
    const [sEh, sEm] = s.end_time.split(':').map(Number)
    gross += sEh * 60 + sEm - (sSh * 60 + sSm)
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
