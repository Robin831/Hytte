import { useState, useEffect, useCallback, useRef } from 'react'
import type { Race, StrideMutationResult } from '../types/stride'

/** Body for POST /api/stride/races and PUT /api/stride/races/{id}. */
export interface RaceInput {
  name: string
  date: string
  distance_m: number
  target_time: number | null
  priority: 'A' | 'B' | 'C'
  notes: string
  // Only sent on an edit: the update endpoint writes every column, so
  // omitting the recorded result would wipe it.
  result_time?: number | null
}

export interface UseStrideRacesResult {
  races: Race[]
  loading: boolean
  /** Re-read the race list. Aborted on unmount unless a signal is passed. */
  reload: (signal?: AbortSignal) => Promise<void>
  /** Create a race (no id) or replace one (with id). Does not reload the list. */
  saveRace: (input: RaceInput, id?: number) => Promise<StrideMutationResult>
  /** Delete a race and drop it from the list. */
  deleteRace: (id: number) => Promise<StrideMutationResult>
}

/**
 * Owns the Stride race calendar: the list fetch (aborted on unmount) plus the
 * create/update/delete calls behind the race form.
 */
export function useStrideRaces(): UseStrideRacesResult {
  const [races, setRaces] = useState<Race[]>([])
  const [loading, setLoading] = useState(true)
  // Lives for as long as the hook is mounted; reloads triggered by the caller
  // use it so a response that lands after unmount is dropped.
  const lifetimeRef = useRef<AbortController | null>(null)

  const loadRaces = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/stride/races', { credentials: 'include', signal })
      if (!res.ok) {
        throw new Error(`Failed to load races: ${res.status} ${res.statusText}`)
      }
      const data = await res.json()
      if (!signal?.aborted) {
        setRaces(data.races ?? [])
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load races', error)
    } finally {
      if (!signal?.aborted) {
        setLoading(false)
      }
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    lifetimeRef.current = controller
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    void loadRaces(controller.signal)
    return () => {
      controller.abort()
      if (lifetimeRef.current === controller) lifetimeRef.current = null
    }
  }, [loadRaces])

  const reload = useCallback(
    (signal?: AbortSignal) => loadRaces(signal ?? lifetimeRef.current?.signal),
    [loadRaces],
  )

  const saveRace = useCallback(async (input: RaceInput, id?: number): Promise<StrideMutationResult> => {
    try {
      const res = await fetch(id != null ? `/api/stride/races/${id}` : '/api/stride/races', {
        method: id != null ? 'PUT' : 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        return { ok: false, status: res.status, error: data.error }
      }
      return { ok: true }
    } catch {
      return { ok: false }
    }
  }, [])

  const deleteRace = useCallback(async (id: number): Promise<StrideMutationResult> => {
    try {
      const res = await fetch(`/api/stride/races/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        return { ok: false, status: res.status, error: data.error }
      }
      setRaces(prev => prev.filter(r => r.id !== id))
      return { ok: true }
    } catch {
      return { ok: false }
    }
  }, [])

  return { races, loading, reload, saveRace, deleteRace }
}
