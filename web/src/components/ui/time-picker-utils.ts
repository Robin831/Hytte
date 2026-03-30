/**
 * Parse a loosely-typed time string to HH:MM.
 * Accepts: "0600" → "06:00", "630" → "06:30", "9" → "09:00",
 *          "06:30" → "06:30", "9:5" → "09:05"
 */
export function parseTimeInput(raw: string): string | null {
  const trimmed = raw.trim()
  if (!trimmed) return null

  // Already looks like HH:MM or H:M — validate and return
  const colonMatch = trimmed.match(/^(\d{1,2}):(\d{1,2})$/)
  if (colonMatch) {
    const h = parseInt(colonMatch[1], 10)
    const m = parseInt(colonMatch[2], 10)
    if (h >= 0 && h <= 23 && m >= 0 && m <= 59) {
      return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`
    }
    return null
  }

  // Digits only
  const digits = trimmed.replace(/\D/g, '')
  if (digits.length === 0) return null

  let h: number, m: number

  if (digits.length <= 2) {
    // "9" → 09:00, "09" → 09:00
    h = parseInt(digits, 10)
    m = 0
  } else if (digits.length === 3) {
    // "630" → 6:30, "930" → 9:30
    h = parseInt(digits[0], 10)
    m = parseInt(digits.slice(1), 10)
  } else {
    // "0600" → 06:00, "1430" → 14:30
    h = parseInt(digits.slice(0, 2), 10)
    m = parseInt(digits.slice(2, 4), 10)
  }

  if (h < 0 || h > 23 || m < 0 || m > 59) return null
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`
}

/** Move a HH:MM time by `deltaMinutes` (positive or negative), wrapping around midnight. */
export function adjustTime(time: string, deltaMinutes: number): string {
  const [h, m] = time.split(':').map(Number)
  const totalMins = ((h * 60 + m + deltaMinutes) % (24 * 60) + 24 * 60) % (24 * 60)
  const newH = Math.floor(totalMins / 60)
  const newM = totalMins % 60
  return `${String(newH).padStart(2, '0')}:${String(newM).padStart(2, '0')}`
}
