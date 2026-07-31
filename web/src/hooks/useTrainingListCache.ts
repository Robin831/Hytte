import { useMemo } from 'react'
import type { Workout } from '../types/training'

// Snapshot of the paginated training list, kept per user and per tab so that
// navigating into a workout detail and back restores the pages the user already
// paged in instead of resetting to page 1.
export interface TrainingListSnapshot {
  version: number
  userId: string
  savedAt: number
  workouts: Workout[]
  nextCursor: string | null
  latestWorkoutId: number | null
  scrollY: number
}

// Bumping the version retires every snapshot written by an older build: it is
// part of the key (old entries are never read again) and is re-checked on read.
export const TRAINING_LIST_CACHE_VERSION = 1

// A snapshot older than this is discarded, so a tab left open for hours does not
// present a stale list as if it were fresh. Every write refreshes the timestamp,
// so the TTL measures idle time rather than session length.
export const TRAINING_LIST_CACHE_TTL_MS = 5 * 60 * 1000

const KEY_PREFIX = 'hytte:training-list:'

export function trainingListCacheKey(userId: string): string {
  return `${KEY_PREFIX}v${TRAINING_LIST_CACHE_VERSION}:${userId}`
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

function isSnapshot(value: unknown, userId: string): value is TrainingListSnapshot {
  if (!value || typeof value !== 'object') return false
  const s = value as Partial<TrainingListSnapshot>
  return (
    s.version === TRAINING_LIST_CACHE_VERSION &&
    s.userId === userId &&
    typeof s.savedAt === 'number' &&
    Array.isArray(s.workouts) &&
    (s.nextCursor === null || typeof s.nextCursor === 'string') &&
    (s.latestWorkoutId === null || typeof s.latestWorkoutId === 'number') &&
    typeof s.scrollY === 'number'
  )
}

/**
 * readTrainingListCache returns the snapshot for a user, or null when there is
 * none, it belongs to another user or cache version, it is older than the TTL,
 * or the stored payload is unusable. Anything unusable is dropped on the way out
 * so a corrupt entry cannot keep failing every future read.
 */
export function readTrainingListCache(userId: string): TrainingListSnapshot | null {
  const storage = getStorage()
  if (!storage || !userId) return null
  const key = trainingListCacheKey(userId)
  let raw: string | null
  try {
    raw = storage.getItem(key)
  } catch {
    return null
  }
  if (!raw) return null
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!isSnapshot(parsed, userId)) {
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

/**
 * writeTrainingListCache merges a patch into the stored snapshot, so the list
 * and the scroll offset can be persisted independently. Quota and serialization
 * failures are swallowed: the cache is an optimization, never a requirement.
 */
export function writeTrainingListCache(userId: string, patch: TrainingListSnapshotPatch): void {
  const storage = getStorage()
  if (!storage || !userId) return
  const current = readTrainingListCache(userId)
  const next: TrainingListSnapshot = {
    version: TRAINING_LIST_CACHE_VERSION,
    userId,
    savedAt: Date.now(),
    workouts: patch.workouts ?? current?.workouts ?? [],
    nextCursor: patch.nextCursor !== undefined ? patch.nextCursor : current?.nextCursor ?? null,
    latestWorkoutId:
      patch.latestWorkoutId !== undefined ? patch.latestWorkoutId : current?.latestWorkoutId ?? null,
    scrollY: patch.scrollY ?? current?.scrollY ?? 0,
  }
  const key = trainingListCacheKey(userId)
  try {
    storage.setItem(key, JSON.stringify(next))
  } catch {
    // Most likely a quota error from a very long list. Drop the entry so the
    // next mount does a clean load rather than restoring a half-written one.
    removeKey(storage, key)
  }
}

/**
 * clearTrainingListCache removes one user's snapshot, or — with no argument —
 * every training-list snapshot in the tab. The sweep is what logout uses, so
 * signing in as a different user in the same tab can never surface the previous
 * user's workouts.
 */
export function clearTrainingListCache(userId?: string): void {
  const storage = getStorage()
  if (!storage) return
  if (userId) {
    removeKey(storage, trainingListCacheKey(userId))
    return
  }
  try {
    const keys: string[] = []
    for (let i = 0; i < storage.length; i++) {
      const key = storage.key(i)
      if (key && key.startsWith(KEY_PREFIX)) keys.push(key)
    }
    for (const key of keys) removeKey(storage, key)
  } catch {
    // Storage unavailable — nothing was written either, so nothing to clear.
  }
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
 * useTrainingListCache binds the snapshot helpers to a user id. The returned
 * object is referentially stable for a given user, so it is safe to use as an
 * effect dependency.
 */
export function useTrainingListCache(userId: string | number | null | undefined): TrainingListCache {
  const key = userId === null || userId === undefined ? '' : String(userId)
  return useMemo(
    () => ({
      read: () => readTrainingListCache(key),
      write: (patch: TrainingListSnapshotPatch) => writeTrainingListCache(key, patch),
      clear: () => clearTrainingListCache(key),
    }),
    [key],
  )
}
