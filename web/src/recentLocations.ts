export interface RecentLocation {
  name: string
  lat: number
  lon: number
}

const STORAGE_KEY = 'recent_locations'
const MAX_RECENT = 10

// Names of default locations shown when there is no history.
// Coordinates come from the backend /api/weather/locations endpoint (single source of truth).
export const DEFAULT_LOCATION_NAMES = ['Oslo', 'Bergen', 'Trondheim']

export function isValidRecentLocation(item: unknown): item is RecentLocation {
  if (
    typeof item !== 'object' ||
    item === null ||
    typeof (item as RecentLocation).name !== 'string' ||
    typeof (item as RecentLocation).lat !== 'number' ||
    typeof (item as RecentLocation).lon !== 'number'
  ) {
    return false
  }

  const { lat, lon } = item as RecentLocation

  if (!Number.isFinite(lat) || !Number.isFinite(lon)) {
    return false
  }

  if (lat < -90 || lat > 90) {
    return false
  }

  if (lon < -180 || lon > 180) {
    return false
  }

  return true
}

/** Build default recent locations by resolving names against API-provided cities. */
export function buildDefaultLocations(knownLocations: RecentLocation[]): RecentLocation[] {
  const locMap = new Map(knownLocations.map((l) => [l.name, l]))
  const defaults = DEFAULT_LOCATION_NAMES.map((name) => locMap.get(name)).filter(
    (l): l is RecentLocation => l !== undefined,
  )
  return defaults.length > 0 ? defaults : knownLocations.slice(0, 3)
}

/**
 * Load recent locations from localStorage.
 * Returns null when nothing is stored (first visit) — caller should use
 * buildDefaultLocations() once the API responds.
 */
export function loadRecentLocations(): RecentLocation[] | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed) || parsed.length === 0) return null
    // Validate shape
    const valid = parsed.filter(isValidRecentLocation)
    return valid.length > 0 ? valid.slice(0, MAX_RECENT) : null
  } catch {
    return null
  }
}

export function saveRecentLocations(locations: RecentLocation[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(locations.slice(0, MAX_RECENT)))
  } catch {
    // localStorage may be unavailable; ignore.
  }
}

/** Adds a location to the front of the recents list, deduplicating by full identity (name+lat+lon). */
export function addRecentLocation(
  locations: RecentLocation[],
  location: RecentLocation,
): RecentLocation[] {
  const filtered = locations.filter(
    (l) => !(l.name === location.name && l.lat === location.lat && l.lon === location.lon),
  )
  return [location, ...filtered].slice(0, MAX_RECENT)
}

/** Oslo fallback coordinates used when no location has been saved. */
export const OSLO: RecentLocation = { name: 'Oslo', lat: 59.9139, lon: 10.7522 }

/**
 * Resolve the active location from localStorage.
 * Prefers lat+lon matching when the stored value is full JSON (avoids duplicate-name collisions).
 * Falls back to name matching for legacy string values, then to the first recent, then Oslo.
 */
export function resolveLocation(): RecentLocation {
  try {
    const stored = localStorage.getItem('weather_location')
    const recents = loadRecentLocations()
    if (stored) {
      // Prefer full-JSON storage with lat+lon matching (Rule 40)
      try {
        const parsed = JSON.parse(stored) as unknown
        if (isValidRecentLocation(parsed)) {
          const loc = parsed as RecentLocation
          if (recents) {
            const found = recents.find((l) => l.lat === loc.lat && l.lon === loc.lon)
            if (found) return found
          }
          // Stored location not in recents list — return it directly (valid coordinates)
          return loc
        }
      } catch {
        // Not JSON — fall through to legacy name-only matching
      }
      // Legacy: stored value is a plain name string
      if (recents) {
        const found = recents.find((l) => l.name === stored)
        if (found) return found
      }
    }
    if (recents && recents.length > 0) return recents[0]
  } catch {
    // localStorage unavailable
  }
  return OSLO
}

/** Parse a recent_locations JSON string from the backend preference. */
export function parseRecentLocationsPreference(value: string): RecentLocation[] | null {
  try {
    const parsed = JSON.parse(value) as unknown
    if (!Array.isArray(parsed)) return null
    const valid = parsed.filter(isValidRecentLocation)
    return valid.length > 0 ? valid.slice(0, MAX_RECENT) : null
  } catch {
    return null
  }
}

