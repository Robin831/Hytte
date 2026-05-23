// Parses a strict "H:MM:SS" target-time string to seconds. Returns null for
// empty/whitespace-only input or any string that does not match the format
// exactly (non-negative hours, minutes 0–59, seconds 0–59). Two-part inputs
// like "25:00" are intentionally rejected to avoid the historical ambiguity
// where they were silently treated as hours+minutes and saved as 25 hours.
export function parseTargetTime(s: string): number | null {
  const trimmed = s.trim()
  if (!trimmed) return null
  const match = trimmed.match(/^(\d+):(\d{2}):(\d{2})$/)
  if (!match) return null
  const hours = Number(match[1])
  const minutes = Number(match[2])
  const seconds = Number(match[3])
  if (!Number.isFinite(hours) || minutes > 59 || seconds > 59) return null
  const totalSeconds = hours * 3600 + minutes * 60 + seconds
  if (!Number.isSafeInteger(totalSeconds)) return null
  return totalSeconds
}
