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
    const valid = parsed.filter(
      (item: unknown): item is RecentLocation =>
        typeof item === 'object' &&
        item !== null &&
        typeof (item as RecentLocation).name === 'string' &&
        typeof (item as RecentLocation).lat === 'number' &&
        typeof (item as RecentLocation).lon === 'number',
    )
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

/** Adds a location to the front of the recents list, deduplicating by name. */
export function addRecentLocation(
  locations: RecentLocation[],
  location: RecentLocation,
): RecentLocation[] {
  const filtered = locations.filter((l) => l.name !== location.name)
  return [location, ...filtered].slice(0, MAX_RECENT)
}

/** Parse a recent_locations JSON string from the backend preference. */
export function parseRecentLocationsPreference(value: string): RecentLocation[] | null {
  try {
    const parsed = JSON.parse(value) as unknown
    if (!Array.isArray(parsed)) return null
    const valid = parsed.filter(
      (item: unknown): item is RecentLocation =>
        typeof item === 'object' &&
        item !== null &&
        typeof (item as RecentLocation).name === 'string' &&
        typeof (item as RecentLocation).lat === 'number' &&
        typeof (item as RecentLocation).lon === 'number',
    )
    return valid.length > 0 ? valid.slice(0, MAX_RECENT) : null
  } catch {
    return null
  }
}
