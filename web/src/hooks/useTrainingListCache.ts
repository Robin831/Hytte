import { useMemo } from 'react'
import type { Workout } from '../types/training'

// Snapshot of the paginated training list, kept per user, per active filter and
// per tab so that navigating into a workout detail and back restores the pages
// the user already paged in instead of resetting to page 1. The filter key is
// the serialized list query (`sport=running&tag=long&q=intervals`), which is
// also what the URL carries, so a filtered list restores exactly like the
// unfiltered one.
export interface TrainingListSnapshot {
  version: number
  userId: string
  filterKey: string
  savedAt: number
  workouts: Workout[]
  nextCursor: string | null
  latestWorkoutId: number | null
  scrollY: number
}

// Bumping the version retires every snapshot written by an older build: it is
// part of the key (old entries are never read again) and is re-checked on read.
// v2 added the per-filter key segment.
export const TRAINING_LIST_CACHE_VERSION = 2

// A snapshot older than this is discarded, so a tab left open for hours does not
// present a stale list as if it were fresh. Every write refreshes the timestamp,
// so the TTL measures idle time rather than session length.
export const TRAINING_LIST_CACHE_TTL_MS = 5 * 60 * 1000

// Filters are now part of the key, so one user can accumulate a snapshot per
// filter combination they try. Cap how many are kept and evict the oldest, so a
// session of filter-fiddling cannot fill sessionStorage with lists nobody will
// come back to.
export const MAX_SNAPSHOTS_PER_USER = 4

const KEY_PREFIX = 'hytte:training-list:'
const VERSION_PREFIX = `${KEY_PREFIX}v${TRAINING_LIST_CACHE_VERSION}:`

// Both segments are percent-encoded: a filter key is a query string full of `&`
// and `=`, and encoding it (along with the user id) keeps `:` exclusively a
// separator, so no combination of values can be read as a different key.
function userKeyPrefix(userId: string): string {
  return `${VERSION_PREFIX}${encodeURIComponent(userId)}:`
}

export function trainingListCacheKey(userId: string, filterKey = ''): string {
  return `${userKeyPrefix(userId)}${encodeURIComponent(filterKey)}`
}

// getStorage returns sessionStorage, or null when it is unavailable — private
// mode and hardened browser settings make even *touching* the property throw, so
// every access in this module goes through here.
function getStorage(): Storage | null {
  try {
    if (typeof window === 'undefined') return null
    return window.sessionStorage ?? null
  } catch {
    return null
  }
}

function removeKey(storage: Storage, key: string) {
  try {
    storage.removeItem(key)
  } catch {
    // Best effort — a storage that refuses removal also refuses reads, and read
    // failures already fall back to "no snapshot".
  }
}

// keysWithPrefix lists the stored keys under a prefix. Enumeration itself can
// throw on a hostile storage, in which case whatever was collected so far is
// returned — the callers only ever remove keys, so a short list just means a
// smaller sweep.
function keysWithPrefix(storage: Storage, prefix: string): string[] {
  const keys: string[] = []
  try {
    for (let i = 0; i < storage.length; i++) {
      const key = storage.key(i)
      if (key && key.startsWith(prefix)) keys.push(key)
    }
  } catch {
    // Fall through with what we have.
  }
  return keys
}

function isSnapshot(
  value: unknown,
  userId: string,
  filterKey: string,
): value is TrainingListSnapshot {
  if (!value || typeof value !== 'object') return false
  const s = value as Partial<TrainingListSnapshot>
  return (
    s.version === TRAINING_LIST_CACHE_VERSION &&
    s.userId === userId &&
    s.filterKey === filterKey &&
    typeof s.savedAt === 'number' &&
    Array.isArray(s.workouts) &&
    (s.nextCursor === null || typeof s.nextCursor === 'string') &&
    (s.latestWorkoutId === null || typeof s.latestWorkoutId === 'number') &&
    typeof s.scrollY === 'number'
  )
}

/**
 * readTrainingListCache returns the snapshot for a user and filter, or null when
 * there is none, it belongs to another user, filter or cache version, it is
 * older than the TTL, or the stored payload is unusable. Anything unusable is
 * dropped on the way out so a corrupt entry cannot keep failing every future
 * read.
 */
export function readTrainingListCache(
  userId: string,
  filterKey = '',
): TrainingListSnapshot | null {
  const storage = getStorage()
  if (!storage || !userId) return null
  const key = trainingListCacheKey(userId, filterKey)
  let raw: string | null
  try {
    raw = storage.getItem(key)
  } catch {
    return null
  }
  if (!raw) return null
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!isSnapshot(parsed, userId, filterKey)) {
      removeKey(storage, key)
      return null
    }
    // A negative age means the clock moved backwards since the write; treat that
    // as untrustworthy rather than as an eternally fresh snapshot.
    const age = Date.now() - parsed.savedAt
    if (age > TRAINING_LIST_CACHE_TTL_MS || age < -TRAINING_LIST_CACHE_TTL_MS) {
      removeKey(storage, key)
      return null
    }
    return parsed
  } catch {
    removeKey(storage, key)
    return null
  }
}

export type TrainingListSnapshotPatch = Partial<
  Pick<TrainingListSnapshot, 'workouts' | 'nextCursor' | 'latestWorkoutId' | 'scrollY'>
>

// savedAtOf reads just the timestamp off a stored entry, for ranking evictions.
// An entry that cannot be parsed counts as infinitely old, so corrupt leftovers
// are the first thing the cap throws away.
function savedAtOf(storage: Storage, key: string): number {
  try {
    const raw = storage.getItem(key)
    if (!raw) return Number.NEGATIVE_INFINITY
    const parsed = JSON.parse(raw) as Partial<TrainingListSnapshot> | null
    const savedAt = parsed?.savedAt
    return typeof savedAt === 'number' ? savedAt : Number.NEGATIVE_INFINITY
  } catch {
    return Number.NEGATIVE_INFINITY
  }
}

// evictOverCap makes room for the entry about to be written at `currentKey` by
// dropping this user's least recently written snapshots until the cap would hold
// with the new entry included. `currentKey` is excluded from the ranking whether
// or not it already exists, since it always survives.
function evictOverCap(storage: Storage, userId: string, currentKey: string): void {
  const others = keysWithPrefix(storage, userKeyPrefix(userId)).filter(k => k !== currentKey)
  const allowed = MAX_SNAPSHOTS_PER_USER - 1
  if (others.length <= allowed) return
  const ranked = others
    .map(key => ({ key, savedAt: savedAtOf(storage, key) }))
    // Not a subtraction: -Infinity - -Infinity is NaN, which would leave the
    // corrupt entries in an arbitrary place in the order.
    .sort((a, b) => (a.savedAt === b.savedAt ? 0 : a.savedAt < b.savedAt ? -1 : 1))
  for (let i = 0; i < others.length - allowed; i++) removeKey(storage, ranked[i].key)
}

/**
 * writeTrainingListCache merges a patch into the stored snapshot for a user and
 * filter, so the list and the scroll offset can be persisted independently.
 * Quota and serialization failures are swallowed: the cache is an optimization,
 * never a requirement.
 */
export function writeTrainingListCache(
  userId: string,
  filterKey: string,
  patch: TrainingListSnapshotPatch,
): void {
  const storage = getStorage()
  if (!storage || !userId) return
  const current = readTrainingListCache(userId, filterKey)
  const next: TrainingListSnapshot = {
    version: TRAINING_LIST_CACHE_VERSION,
    userId,
    filterKey,
    savedAt: Date.now(),
    workouts: patch.workouts ?? current?.workouts ?? [],
    nextCursor: patch.nextCursor !== undefined ? patch.nextCursor : current?.nextCursor ?? null,
    latestWorkoutId:
      patch.latestWorkoutId !== undefined ? patch.latestWorkoutId : current?.latestWorkoutId ?? null,
    scrollY: patch.scrollY ?? current?.scrollY ?? 0,
  }
  const key = trainingListCacheKey(userId, filterKey)
  evictOverCap(storage, userId, key)
  try {
    storage.setItem(key, JSON.stringify(next))
  } catch {
    // Most likely a quota error from a very long list. Drop the entry so the
    // next mount does a clean load rather than restoring a half-written one.
    removeKey(storage, key)
  }
}

/**
 * clearTrainingListCache removes every snapshot one user has across all their
 * filter combinations, or — with no argument — every training-list snapshot in
 * the tab, including ones left behind by older cache versions. The sweep is what
 * logout uses, so signing in as a different user in the same tab can never
 * surface the previous user's workouts.
 */
export function clearTrainingListCache(userId?: string): void {
  const storage = getStorage()
  if (!storage) return
  const prefix = userId ? userKeyPrefix(userId) : KEY_PREFIX
  for (const key of keysWithPrefix(storage, prefix)) removeKey(storage, key)
}

/**
 * isReloadNavigation reports whether the document was reached by a reload.
 * sessionStorage survives F5, so the snapshot alone cannot distinguish "came
 * back from a detail page" from "the user asked for a fresh page"; the
 * navigation type can. Unknown navigations are treated as not-a-reload.
 */
export function isReloadNavigation(): boolean {
  try {
    const entries = performance.getEntriesByType('navigation') as PerformanceNavigationTiming[]
    return entries[0]?.type === 'reload'
  } catch {
    return false
  }
}

export interface TrainingListCache {
  read: () => TrainingListSnapshot | null
  write: (patch: TrainingListSnapshotPatch) => void
  clear: () => void
}

/**
 * useTrainingListCache binds the snapshot helpers to a user id and a filter key.
 * The returned object is referentially stable for a given pair, so it is safe to
 * use as an effect dependency — and it changes identity when the filters change,
 * which is exactly when the page must look at a different snapshot. `clear()`
 * drops all of the user's snapshots, not just the current filter's.
 */
export function useTrainingListCache(
  userId: string | number | null | undefined,
  filterKey = '',
): TrainingListCache {
  const key = userId === null || userId === undefined ? '' : String(userId)
  return useMemo(
    () => ({
      read: () => readTrainingListCache(key, filterKey),
      write: (patch: TrainingListSnapshotPatch) => writeTrainingListCache(key, filterKey, patch),
      clear: () => clearTrainingListCache(key),
    }),
    [key, filterKey],
  )
}
