// Stale-while-revalidate cache for weather forecasts.
//
// The last successful `ForecastResponse` for each viewed location is persisted to
// localStorage so revisits can render real numbers instantly while a fresh forecast
// is fetched in the background. A single localStorage key holds a JSON array of
// entries; array order encodes recency (oldest first, most-recently-used last) so
// the store can be bounded with simple LRU eviction. Reads promote the accessed
// entry to the tail so actively viewed locations are not evicted.
//
// The storage key is scoped by user ID to prevent cross-account data leakage on
// shared browsers. Anonymous sessions share a single "anon" namespace.

const STORAGE_KEY_PREFIX = 'weather:forecastCache'

function storageKey(userId?: number): string {
  return userId != null ? `${STORAGE_KEY_PREFIX}:${userId}` : `${STORAGE_KEY_PREFIX}:anon`
}

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
function readEntries(userId?: number): ForecastCacheEntry[] {
  let raw: string | null
  try {
    raw = localStorage.getItem(storageKey(userId))
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
 * Promotes the accessed entry to the most-recently-used position so actively
 * viewed locations are not evicted. Never throws — corrupt or missing data falls
 * back to null.
 */
export function readForecastCache<T = unknown>(lat: number, lon: number, userId?: number): ForecastCacheEntry<T> | null {
  const key = keyFor(lat, lon)
  const entries = readEntries(userId)
  const idx = entries.findIndex((e) => e.key === key)
  if (idx === -1) return null
  // Promote to tail (most-recently-used) if not already there.
  if (idx < entries.length - 1) {
    const [entry] = entries.splice(idx, 1)
    entries.push(entry)
    try {
      localStorage.setItem(storageKey(userId), JSON.stringify(entries))
    } catch {
      // Best-effort — promotion failure doesn't affect the read.
    }
  }
  return entries[entries.length - 1] as ForecastCacheEntry<T>
}

/**
 * Persist a successful forecast response for a location, refreshing its timestamp.
 * Moves the location to the most-recently-used end and evicts the oldest entries
 * beyond MAX_ENTRIES. Never throws — storage/serialization errors are ignored.
 */
export function writeForecastCache<T = unknown>(lat: number, lon: number, response: T, userId?: number): void {
  const key = keyFor(lat, lon)
  // Drop any existing entry for this key, then append it as most-recently-used.
  const entries = readEntries(userId).filter((e) => e.key !== key)
  entries.push({ key, response, lastUpdated: Date.now() })
  // Evict from the oldest (front) end until within the bound.
  while (entries.length > MAX_ENTRIES) {
    entries.shift()
  }
  try {
    localStorage.setItem(storageKey(userId), JSON.stringify(entries))
  } catch {
    // Quota exceeded or serialization failure — caching is best-effort.
  }
}
