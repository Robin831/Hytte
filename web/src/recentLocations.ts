export interface RecentLocation {
  name: string
  lat: number
  lon: number
}

const STORAGE_KEY = 'recent_locations'
const MAX_RECENT = 10

// Default locations shown when there is no history.
export const DEFAULT_LOCATIONS: RecentLocation[] = [
  { name: 'Oslo', lat: 59.9139, lon: 10.7522 },
  { name: 'Bergen', lat: 60.3913, lon: 5.3221 },
  { name: 'Trondheim', lat: 63.4305, lon: 10.3951 },
]

export function loadRecentLocations(): RecentLocation[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return [...DEFAULT_LOCATIONS]
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed) || parsed.length === 0) return [...DEFAULT_LOCATIONS]
    // Validate shape
    const valid = parsed.filter(
      (item: unknown): item is RecentLocation =>
        typeof item === 'object' &&
        item !== null &&
        typeof (item as RecentLocation).name === 'string' &&
        typeof (item as RecentLocation).lat === 'number' &&
        typeof (item as RecentLocation).lon === 'number',
    )
    return valid.length > 0 ? valid.slice(0, MAX_RECENT) : [...DEFAULT_LOCATIONS]
  } catch {
    return [...DEFAULT_LOCATIONS]
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
