// Stale-while-revalidate cache for weather forecasts.
//
// The last successful `ForecastResponse` for each viewed location is persisted to
// localStorage so revisits can render real numbers instantly while a fresh forecast
// is fetched in the background. A single localStorage key holds a JSON array of
// entries; array order encodes recency (oldest first, most-recently-used last) so
// the store can be bounded with simple LRU eviction.

const STORAGE_KEY = 'weather:forecastCache'

// Maximum number of distinct locations to retain. Adding a new location beyond this
// evicts the oldest (least-recently-used) entry.
const MAX_ENTRIES = 5

export interface ForecastCacheEntry<T = unknown> {
  /** `lat,lon` identifier for the cached location. */
  key: string
  /** The last successful forecast response for this location. */
  response: T
  /** Epoch milliseconds at which the response was cached. */
  lastUpdated: number
}

/** Build the cache key for a location from its coordinates. */
export function keyFor(lat: number, lon: number): string {
  return `${lat},${lon}`
}

/** Type guard for a single well-formed cache entry. */
function isValidEntry(item: unknown): item is ForecastCacheEntry {
  return (
    typeof item === 'object' &&
    item !== null &&
    typeof (item as ForecastCacheEntry).key === 'string' &&
    typeof (item as ForecastCacheEntry).lastUpdated === 'number' &&
    Number.isFinite((item as ForecastCacheEntry).lastUpdated) &&
    'response' in (item as ForecastCacheEntry) &&
    (item as ForecastCacheEntry).response != null
  )
}

/**
 * Read the stored entry array, tolerating missing/corrupt/non-array data.
 * Returns only well-formed entries; anything malformed is treated as empty.
 */
function readEntries(): ForecastCacheEntry[] {
  let raw: string | null
  try {
    raw = localStorage.getItem(STORAGE_KEY)
  } catch {
    return []
  }
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.filter(isValidEntry)
  } catch {
    return []
  }
}

/**
 * Read the cached forecast for a location, or null when there is no valid entry.
 * Never throws — corrupt or missing data falls back to null.
 */
export function readForecastCache<T = unknown>(lat: number, lon: number): ForecastCacheEntry<T> | null {
  const key = keyFor(lat, lon)
  const entry = readEntries().find((e) => e.key === key)
  return (entry as ForecastCacheEntry<T> | undefined) ?? null
}

/**
 * Persist a successful forecast response for a location, refreshing its timestamp.
 * Moves the location to the most-recently-used end and evicts the oldest entries
 * beyond MAX_ENTRIES. Never throws — storage/serialization errors are ignored.
 */
export function writeForecastCache<T = unknown>(lat: number, lon: number, response: T): void {
  const key = keyFor(lat, lon)
  // Drop any existing entry for this key, then append it as most-recently-used.
  const entries = readEntries().filter((e) => e.key !== key)
  entries.push({ key, response, lastUpdated: Date.now() })
  // Evict from the oldest (front) end until within the bound.
  while (entries.length > MAX_ENTRIES) {
    entries.shift()
  }
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(entries))
  } catch {
    // Quota exceeded or serialization failure — caching is best-effort.
  }
}
