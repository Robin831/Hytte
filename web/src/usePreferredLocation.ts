import { useState, useEffect } from 'react'
import { resolveLocation, isValidRecentLocation } from './recentLocations'
import type { RecentLocation } from './recentLocations'

/**
 * Resolves the user's preferred weather location.
 * Starts with the localStorage/Oslo fallback, then asynchronously fetches the
 * user's saved `weather_location` preference from the server and resolves it to
 * coordinates via `/api/weather/locations`.
 */
export function usePreferredLocation(): RecentLocation {
  const [location, setLocation] = useState<RecentLocation>(resolveLocation)

  useEffect(() => {
    let cancelled = false

    async function fetchServerLocation() {
      try {
        const prefsRes = await fetch('/api/settings/preferences', { credentials: 'include' })
        if (!prefsRes.ok || cancelled) return

        const prefsData = (await prefsRes.json()) as {
          preferences?: { weather_location?: string; home_location?: string }
        }
        const locationName =
          prefsData?.preferences?.weather_location || prefsData?.preferences?.home_location
        if (!locationName || cancelled) return

        const locsRes = await fetch('/api/weather/locations')
        if (!locsRes.ok || cancelled) return
        const locsData = (await locsRes.json()) as { locations?: RecentLocation[] }
        const locs = locsData.locations ?? []
        const matched = locs.find((l) => l.name === locationName)
        if (matched && isValidRecentLocation(matched) && !cancelled) {
          setLocation(matched)
        }
      } catch {
        // Best-effort; localStorage/Oslo fallback is already the initial state.
      }
    }

    void fetchServerLocation()
    return () => {
      cancelled = true
    }
  }, [])

  return location
}
