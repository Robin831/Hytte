export const FRESH_THRESHOLD_MS = 30 * 60 * 1000
export const STALE_THRESHOLD_MS = 24 * 60 * 60 * 1000

export type FreshnessBucket = 'fresh' | 'stale' | 'dead' | 'never'

export function getAnvilFreshness(
  lastActivity: string | null | undefined,
  now: Date = new Date(),
): FreshnessBucket {
  if (lastActivity == null) return 'never'
  const parsed = new Date(lastActivity)
  if (isNaN(parsed.getTime())) return 'never'
  const ageMs = now.getTime() - parsed.getTime()
  if (ageMs < FRESH_THRESHOLD_MS) return 'fresh'
  if (ageMs < STALE_THRESHOLD_MS) return 'stale'
  return 'dead'
}
