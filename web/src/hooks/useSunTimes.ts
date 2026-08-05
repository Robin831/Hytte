import { useEffect, useState } from 'react'
import type { RecentLocation } from '../recentLocations'

export interface SunTimes {
  /** RFC3339 timestamp, absent on polar day/night. */
  sunrise?: string
  /** RFC3339 timestamp, absent on polar day/night. */
  sunset?: string
  daylightSeconds: number
  polarDay: boolean
  polarNight: boolean
}

/**
 * Fetch sunrise/sunset/daylight for a location.
 *
 * The backend computes these locally and caches them per day, so this is cheap
 * and only re-runs when the location changes. Results are tagged with the
 * location they belong to, so the previous location's times are never shown
 * under a newly selected one while its request is still in flight.
 */
export function useSunTimes(location: RecentLocation | null, enabled = true): SunTimes | null {
  const [entry, setEntry] = useState<{ data: SunTimes; locationKey: string } | null>(null)

  const lat = location?.lat
  const lon = location?.lon
  const locationKey = lat !== undefined && lon !== undefined ? `${lat},${lon}` : null

  useEffect(() => {
    if (!enabled || locationKey === null) return

    let cancelled = false
    fetch(`/api/weather/sun?lat=${lat}&lon=${lon}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (!cancelled) setEntry(data ? { data: data as SunTimes, locationKey } : null)
      })
      .catch((err: unknown) => {
        console.warn('Failed to fetch sun data:', err)
        if (!cancelled) setEntry(null)
      })

    return () => {
      cancelled = true
    }
  }, [enabled, locationKey, lat, lon])

  return entry && entry.locationKey === locationKey ? entry.data : null
}
