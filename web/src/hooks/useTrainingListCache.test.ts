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

function primeRaw(userId: string, snapshot: Record<string, unknown> | string) {
  window.sessionStorage.setItem(
    trainingListCacheKey(userId),
    typeof snapshot === 'string' ? snapshot : JSON.stringify(snapshot),
  )
}

function validSnapshot(userId: string, overrides: Partial<TrainingListSnapshot> = {}) {
  return {
    version: TRAINING_LIST_CACHE_VERSION,
    userId,
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
    writeTrainingListCache('1', {
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
    writeTrainingListCache('1', { workouts: [makeWorkout(1)], nextCursor: 'c1', scrollY: 10 })
    writeTrainingListCache('1', { scrollY: 900 })

    const snapshot = readTrainingListCache('1')
    expect(snapshot!.scrollY).toBe(900)
    expect(snapshot!.workouts.map(w => w.id)).toEqual([1])
    expect(snapshot!.nextCursor).toBe('c1')
  })

  it('scopes snapshots per user', () => {
    writeTrainingListCache('1', { workouts: [makeWorkout(1)] })
    writeTrainingListCache('2', { workouts: [makeWorkout(7)] })

    expect(readTrainingListCache('1')!.workouts.map(w => w.id)).toEqual([1])
    expect(readTrainingListCache('2')!.workouts.map(w => w.id)).toEqual([7])
    expect(readTrainingListCache('3')).toBeNull()
  })

  it('ignores a snapshot whose stored userId does not match the reader', () => {
    primeRaw('2', validSnapshot('1'))
    expect(readTrainingListCache('2')).toBeNull()
    // The mismatched entry is dropped rather than left to fail every read.
    expect(window.sessionStorage.getItem(trainingListCacheKey('2'))).toBeNull()
  })

  it('ignores a snapshot written by a different cache version', () => {
    primeRaw('1', validSnapshot('1', { version: TRAINING_LIST_CACHE_VERSION + 1 }))
    expect(readTrainingListCache('1')).toBeNull()
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

    expect(() => writeTrainingListCache('1', { workouts: [makeWorkout(1)] })).not.toThrow()
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
    writeTrainingListCache('', { workouts: [makeWorkout(1)] })
    expect(window.sessionStorage.length).toBe(0)
    expect(readTrainingListCache('')).toBeNull()
  })

  it('clears one user, or every user when called with no id', () => {
    writeTrainingListCache('1', { workouts: [makeWorkout(1)] })
    writeTrainingListCache('2', { workouts: [makeWorkout(2)] })

    clearTrainingListCache('1')
    expect(readTrainingListCache('1')).toBeNull()
    expect(readTrainingListCache('2')).not.toBeNull()

    clearTrainingListCache()
    expect(readTrainingListCache('2')).toBeNull()
  })

  it('leaves unrelated sessionStorage keys alone when sweeping', () => {
    window.sessionStorage.setItem('unrelated', 'keep me')
    writeTrainingListCache('1', { workouts: [makeWorkout(1)] })

    clearTrainingListCache()

    expect(window.sessionStorage.getItem('unrelated')).toBe('keep me')
  })

  it('binds the helpers to a user id through the hook', () => {
    const { result } = renderHook(() => useTrainingListCache(1))

    act(() => {
      result.current.write({ workouts: [makeWorkout(5)], nextCursor: 'c5', scrollY: 50 })
    })
    expect(result.current.read()!.workouts.map(w => w.id)).toEqual([5])
    expect(readTrainingListCache('1')!.nextCursor).toBe('c5')

    act(() => {
      result.current.clear()
    })
    expect(result.current.read()).toBeNull()
  })

  it('returns a stable object for the same user and a new one when the user changes', () => {
    const { result, rerender } = renderHook(({ id }: { id: number }) => useTrainingListCache(id), {
      initialProps: { id: 1 },
    })
    const first = result.current
    rerender({ id: 1 })
    expect(result.current).toBe(first)

    rerender({ id: 2 })
    expect(result.current).not.toBe(first)

    act(() => {
      result.current.write({ workouts: [makeWorkout(9)] })
    })
    expect(readTrainingListCache('2')!.workouts.map(w => w.id)).toEqual([9])
    expect(readTrainingListCache('1')).toBeNull()
  })

  it('is a no-op for a null user', () => {
    const { result } = renderHook(() => useTrainingListCache(null))
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
