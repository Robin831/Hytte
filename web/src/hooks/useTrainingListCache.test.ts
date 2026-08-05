// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import {
  useTrainingListCache,
  readTrainingListCache,
  writeTrainingListCache,
  clearTrainingListCache,
  trainingListCacheKey,
  isReloadNavigation,
  TRAINING_LIST_CACHE_VERSION,
  TRAINING_LIST_CACHE_TTL_MS,
  MAX_SNAPSHOTS_PER_USER,
  type TrainingListSnapshot,
} from './useTrainingListCache'
import type { Workout } from '../types/training'

const makeWorkout = (id: number): Workout => ({
  id,
  user_id: 1,
  sport: 'running',
  title: `Run ${id}`,
  started_at: '2026-01-01T08:00:00Z',
  duration_seconds: 1800,
  distance_meters: 5000,
  avg_heart_rate: 150,
  max_heart_rate: 170,
  avg_pace_sec_per_km: 360,
  avg_cadence: 0,
  calories: 300,
  ascent_meters: 0,
  descent_meters: 0,
  fit_file_hash: '',
  analysis_status: '',
  title_source: '',
  created_at: '2026-01-01T08:00:00Z',
  tags: [],
})

// A filter key is the serialized list query, so it carries the separators that
// make key construction interesting.
const RUNNING = 'sport=running&tag=long&q=intervals'

function primeRaw(
  userId: string,
  snapshot: Record<string, unknown> | string,
  filterKey = '',
) {
  window.sessionStorage.setItem(
    trainingListCacheKey(userId, filterKey),
    typeof snapshot === 'string' ? snapshot : JSON.stringify(snapshot),
  )
}

function validSnapshot(
  userId: string,
  overrides: Partial<TrainingListSnapshot> = {},
  filterKey = '',
) {
  return {
    version: TRAINING_LIST_CACHE_VERSION,
    userId,
    filterKey,
    savedAt: Date.now(),
    workouts: [makeWorkout(1)],
    nextCursor: 'cursor-1',
    latestWorkoutId: 1,
    scrollY: 420,
    ...overrides,
  }
}

describe('useTrainingListCache', () => {
  beforeEach(() => {
    window.sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('round-trips a snapshot', () => {
    writeTrainingListCache('1', '', {
      workouts: [makeWorkout(1), makeWorkout(2)],
      nextCursor: 'cursor-2',
      latestWorkoutId: 2,
      scrollY: 300,
    })

    const snapshot = readTrainingListCache('1')
    expect(snapshot).not.toBeNull()
    expect(snapshot!.workouts.map(w => w.id)).toEqual([1, 2])
    expect(snapshot!.nextCursor).toBe('cursor-2')
    expect(snapshot!.latestWorkoutId).toBe(2)
    expect(snapshot!.scrollY).toBe(300)
  })

  it('merges a partial write into the existing snapshot', () => {
    writeTrainingListCache('1', '', { workouts: [makeWorkout(1)], nextCursor: 'c1', scrollY: 10 })
    writeTrainingListCache('1', '', { scrollY: 900 })

    const snapshot = readTrainingListCache('1')
    expect(snapshot!.scrollY).toBe(900)
    expect(snapshot!.workouts.map(w => w.id)).toEqual([1])
    expect(snapshot!.nextCursor).toBe('c1')
  })

  it('scopes snapshots per user', () => {
    writeTrainingListCache('1', '', { workouts: [makeWorkout(1)] })
    writeTrainingListCache('2', '', { workouts: [makeWorkout(7)] })

    expect(readTrainingListCache('1')!.workouts.map(w => w.id)).toEqual([1])
    expect(readTrainingListCache('2')!.workouts.map(w => w.id)).toEqual([7])
    expect(readTrainingListCache('3')).toBeNull()
  })

  it('round-trips a snapshot under a filter key', () => {
    writeTrainingListCache('1', RUNNING, {
      workouts: [makeWorkout(3)],
      nextCursor: 'c3',
      scrollY: 120,
    })

    const snapshot = readTrainingListCache('1', RUNNING)
    expect(snapshot!.workouts.map(w => w.id)).toEqual([3])
    expect(snapshot!.nextCursor).toBe('c3')
    expect(snapshot!.scrollY).toBe(120)
  })

  it('keeps filter variants isolated from each other', () => {
    writeTrainingListCache('1', '', { workouts: [makeWorkout(1)] })
    writeTrainingListCache('1', 'sport=running', { workouts: [makeWorkout(2)] })
    writeTrainingListCache('1', 'sport=cycling', { workouts: [makeWorkout(3)] })

    expect(readTrainingListCache('1')!.workouts.map(w => w.id)).toEqual([1])
    expect(readTrainingListCache('1', 'sport=running')!.workouts.map(w => w.id)).toEqual([2])
    expect(readTrainingListCache('1', 'sport=cycling')!.workouts.map(w => w.id)).toEqual([3])
    expect(readTrainingListCache('1', 'sport=swimming')).toBeNull()
  })

  it('does not let a user id containing the separator collide with a filter key', () => {
    writeTrainingListCache('a:b', '', { workouts: [makeWorkout(1)] })
    writeTrainingListCache('a', 'b', { workouts: [makeWorkout(2)] })

    expect(trainingListCacheKey('a:b', '')).not.toBe(trainingListCacheKey('a', 'b'))
    expect(readTrainingListCache('a:b')!.workouts.map(w => w.id)).toEqual([1])
    expect(readTrainingListCache('a', 'b')!.workouts.map(w => w.id)).toEqual([2])
  })

  it('ignores a snapshot whose stored userId does not match the reader', () => {
    primeRaw('2', validSnapshot('1'))
    expect(readTrainingListCache('2')).toBeNull()
    // The mismatched entry is dropped rather than left to fail every read.
    expect(window.sessionStorage.getItem(trainingListCacheKey('2'))).toBeNull()
  })

  it('ignores a snapshot whose stored filterKey does not match the reader', () => {
    primeRaw('1', validSnapshot('1', {}, 'sport=cycling'), RUNNING)
    expect(readTrainingListCache('1', RUNNING)).toBeNull()
    expect(window.sessionStorage.getItem(trainingListCacheKey('1', RUNNING))).toBeNull()
  })

  it('ignores a snapshot written by a different cache version', () => {
    primeRaw('1', validSnapshot('1', { version: TRAINING_LIST_CACHE_VERSION + 1 }))
    expect(readTrainingListCache('1')).toBeNull()
  })

  it('never reads a snapshot left behind by the v1 key layout', () => {
    // Exactly what the v1 build wrote: no filter segment, version 1.
    window.sessionStorage.setItem('hytte:training-list:v1:1', JSON.stringify({
      version: 1,
      userId: '1',
      savedAt: Date.now(),
      workouts: [makeWorkout(1)],
      nextCursor: null,
      latestWorkoutId: 1,
      scrollY: 42,
    }))

    expect(readTrainingListCache('1')).toBeNull()
    expect(readTrainingListCache('1', RUNNING)).toBeNull()
  })

  it('ignores a snapshot older than the TTL', () => {
    primeRaw('1', validSnapshot('1', { savedAt: Date.now() - TRAINING_LIST_CACHE_TTL_MS - 1000 }))
    expect(readTrainingListCache('1')).toBeNull()
    expect(window.sessionStorage.getItem(trainingListCacheKey('1'))).toBeNull()
  })

  it('keeps a snapshot inside the TTL', () => {
    primeRaw('1', validSnapshot('1', { savedAt: Date.now() - (TRAINING_LIST_CACHE_TTL_MS - 1000) }))
    expect(readTrainingListCache('1')).not.toBeNull()
  })

  it('ignores a snapshot timestamped in the future', () => {
    primeRaw('1', validSnapshot('1', { savedAt: Date.now() + TRAINING_LIST_CACHE_TTL_MS * 2 }))
    expect(readTrainingListCache('1')).toBeNull()
  })

  it('returns null for corrupt JSON instead of throwing', () => {
    primeRaw('1', '{ not json at all')
    expect(() => readTrainingListCache('1')).not.toThrow()
    expect(readTrainingListCache('1')).toBeNull()
    expect(window.sessionStorage.getItem(trainingListCacheKey('1'))).toBeNull()
  })

  it('returns null for a payload with the wrong shape', () => {
    primeRaw('1', validSnapshot('1', { workouts: 'nope' as unknown as Workout[] }))
    expect(readTrainingListCache('1')).toBeNull()
  })

  it('swallows a storage write failure', () => {
    const setItem = vi.spyOn(window.sessionStorage, 'setItem').mockImplementation(() => {
      throw new DOMException('quota', 'QuotaExceededError')
    })

    expect(() => writeTrainingListCache('1', '', { workouts: [makeWorkout(1)] })).not.toThrow()
    expect(setItem).toHaveBeenCalled()
    setItem.mockRestore()
    expect(readTrainingListCache('1')).toBeNull()
  })

  it('returns null when reading throws', () => {
    const getItem = vi.spyOn(window.sessionStorage, 'getItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError')
    })

    expect(readTrainingListCache('1')).toBeNull()
    getItem.mockRestore()
  })

  it('does nothing without a user id', () => {
    writeTrainingListCache('', '', { workouts: [makeWorkout(1)] })
    expect(window.sessionStorage.length).toBe(0)
    expect(readTrainingListCache('')).toBeNull()
  })

  it('keeps only the newest snapshots per user once the cap is reached', () => {
    const now = Date.now()
    const nowSpy = vi.spyOn(Date, 'now')
    // One write per filter, each a second newer than the last.
    for (let i = 0; i < MAX_SNAPSHOTS_PER_USER; i++) {
      nowSpy.mockReturnValue(now + i * 1000)
      writeTrainingListCache('1', `sport=s${i}`, { workouts: [makeWorkout(i)] })
    }
    expect(readTrainingListCache('1', 'sport=s0')).not.toBeNull()

    // One past the cap: the oldest entry goes, everything newer stays.
    nowSpy.mockReturnValue(now + MAX_SNAPSHOTS_PER_USER * 1000)
    writeTrainingListCache('1', 'sport=overflow', { workouts: [makeWorkout(99)] })

    expect(readTrainingListCache('1', 'sport=s0')).toBeNull()
    for (let i = 1; i < MAX_SNAPSHOTS_PER_USER; i++) {
      expect(readTrainingListCache('1', `sport=s${i}`)).not.toBeNull()
    }
    expect(readTrainingListCache('1', 'sport=overflow')!.workouts.map(w => w.id)).toEqual([99])
    nowSpy.mockRestore()
  })

  it('does not count another user\'s snapshots towards the cap', () => {
    for (let i = 0; i < MAX_SNAPSHOTS_PER_USER; i++) {
      writeTrainingListCache('2', `sport=s${i}`, { workouts: [makeWorkout(i)] })
    }
    writeTrainingListCache('1', '', { workouts: [makeWorkout(50)] })
    writeTrainingListCache('1', 'sport=running', { workouts: [makeWorkout(51)] })

    expect(readTrainingListCache('1')).not.toBeNull()
    expect(readTrainingListCache('1', 'sport=running')).not.toBeNull()
    // The other user is at its own cap, untouched by these writes.
    for (let i = 0; i < MAX_SNAPSHOTS_PER_USER; i++) {
      expect(readTrainingListCache('2', `sport=s${i}`)).not.toBeNull()
    }
  })

  it('evicts an unparseable entry before a valid one', () => {
    window.sessionStorage.setItem(trainingListCacheKey('1', 'sport=broken'), '{ not json')
    for (let i = 0; i < MAX_SNAPSHOTS_PER_USER - 1; i++) {
      writeTrainingListCache('1', `sport=s${i}`, { workouts: [makeWorkout(i)] })
    }
    writeTrainingListCache('1', 'sport=fresh', { workouts: [makeWorkout(99)] })

    expect(window.sessionStorage.getItem(trainingListCacheKey('1', 'sport=broken'))).toBeNull()
    for (let i = 0; i < MAX_SNAPSHOTS_PER_USER - 1; i++) {
      expect(readTrainingListCache('1', `sport=s${i}`)).not.toBeNull()
    }
    expect(readTrainingListCache('1', 'sport=fresh')).not.toBeNull()
  })

  it('re-writing the same filter does not evict the other variants', () => {
    for (let i = 0; i < MAX_SNAPSHOTS_PER_USER; i++) {
      writeTrainingListCache('1', `sport=s${i}`, { workouts: [makeWorkout(i)] })
    }
    writeTrainingListCache('1', 'sport=s0', { scrollY: 10 })

    for (let i = 0; i < MAX_SNAPSHOTS_PER_USER; i++) {
      expect(readTrainingListCache('1', `sport=s${i}`)).not.toBeNull()
    }
  })

  it('clears every filter variant for one user, or every user when called with no id', () => {
    writeTrainingListCache('1', '', { workouts: [makeWorkout(1)] })
    writeTrainingListCache('1', 'sport=running', { workouts: [makeWorkout(2)] })
    writeTrainingListCache('1', RUNNING, { workouts: [makeWorkout(3)] })
    writeTrainingListCache('2', '', { workouts: [makeWorkout(4)] })
    writeTrainingListCache('2', 'sport=cycling', { workouts: [makeWorkout(5)] })

    clearTrainingListCache('1')
    expect(readTrainingListCache('1')).toBeNull()
    expect(readTrainingListCache('1', 'sport=running')).toBeNull()
    expect(readTrainingListCache('1', RUNNING)).toBeNull()
    expect(readTrainingListCache('2')).not.toBeNull()
    expect(readTrainingListCache('2', 'sport=cycling')).not.toBeNull()

    clearTrainingListCache()
    expect(readTrainingListCache('2')).toBeNull()
    expect(readTrainingListCache('2', 'sport=cycling')).toBeNull()
  })

  it('sweeps snapshots from older cache versions too', () => {
    window.sessionStorage.setItem('hytte:training-list:v1:1', 'legacy')
    writeTrainingListCache('1', 'sport=running', { workouts: [makeWorkout(1)] })

    clearTrainingListCache()

    expect(window.sessionStorage.getItem('hytte:training-list:v1:1')).toBeNull()
    expect(readTrainingListCache('1', 'sport=running')).toBeNull()
  })

  it('leaves unrelated sessionStorage keys alone when sweeping', () => {
    window.sessionStorage.setItem('unrelated', 'keep me')
    writeTrainingListCache('1', '', { workouts: [makeWorkout(1)] })

    clearTrainingListCache()

    expect(window.sessionStorage.getItem('unrelated')).toBe('keep me')
  })

  it('binds the helpers to a user id and filter key through the hook', () => {
    const { result } = renderHook(() => useTrainingListCache(1, RUNNING))

    act(() => {
      result.current.write({ workouts: [makeWorkout(5)], nextCursor: 'c5', scrollY: 50 })
    })
    expect(result.current.read()!.workouts.map(w => w.id)).toEqual([5])
    expect(readTrainingListCache('1', RUNNING)!.nextCursor).toBe('c5')
    // The unfiltered list is a different snapshot entirely.
    expect(readTrainingListCache('1')).toBeNull()

    act(() => {
      result.current.clear()
    })
    expect(result.current.read()).toBeNull()
  })

  it('clears all of the user\'s filter variants through the hook', () => {
    writeTrainingListCache('1', '', { workouts: [makeWorkout(1)] })
    writeTrainingListCache('1', 'sport=cycling', { workouts: [makeWorkout(2)] })
    const { result } = renderHook(() => useTrainingListCache(1, 'sport=running'))
    act(() => {
      result.current.write({ workouts: [makeWorkout(3)] })
    })

    act(() => {
      result.current.clear()
    })

    expect(readTrainingListCache('1')).toBeNull()
    expect(readTrainingListCache('1', 'sport=cycling')).toBeNull()
    expect(readTrainingListCache('1', 'sport=running')).toBeNull()
  })

  it('returns a stable object for the same user and filter, and a new one when either changes', () => {
    const { result, rerender } = renderHook(
      ({ id, filter }: { id: number; filter: string }) => useTrainingListCache(id, filter),
      { initialProps: { id: 1, filter: '' } },
    )
    const first = result.current
    rerender({ id: 1, filter: '' })
    expect(result.current).toBe(first)

    rerender({ id: 1, filter: 'sport=running' })
    expect(result.current).not.toBe(first)
    const filtered = result.current

    rerender({ id: 2, filter: 'sport=running' })
    expect(result.current).not.toBe(filtered)

    act(() => {
      result.current.write({ workouts: [makeWorkout(9)] })
    })
    expect(readTrainingListCache('2', 'sport=running')!.workouts.map(w => w.id)).toEqual([9])
    expect(readTrainingListCache('1', 'sport=running')).toBeNull()
  })

  it('is a no-op for a null user', () => {
    const { result } = renderHook(() => useTrainingListCache(null, 'sport=running'))
    act(() => {
      result.current.write({ workouts: [makeWorkout(1)] })
    })
    expect(result.current.read()).toBeNull()
    expect(window.sessionStorage.length).toBe(0)
  })
})

describe('isReloadNavigation', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('reports a reload navigation', () => {
    vi.spyOn(performance, 'getEntriesByType').mockReturnValue([
      { type: 'reload' } as unknown as PerformanceEntry,
    ])
    expect(isReloadNavigation()).toBe(true)
  })

  it('reports a normal navigation', () => {
    vi.spyOn(performance, 'getEntriesByType').mockReturnValue([
      { type: 'navigate' } as unknown as PerformanceEntry,
    ])
    expect(isReloadNavigation()).toBe(false)
  })

  it('treats an unavailable navigation entry as not a reload', () => {
    vi.spyOn(performance, 'getEntriesByType').mockImplementation(() => {
      throw new Error('unsupported')
    })
    expect(isReloadNavigation()).toBe(false)
  })
})
