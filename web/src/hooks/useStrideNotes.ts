import { useState, useEffect, useCallback, useRef } from 'react'
import type { Note, NoteScope, StrideMutationResult } from '../types/stride'

/** Body for POST /api/stride/notes and PATCH /api/stride/notes/{id}. */
export interface NoteInput {
  content: string
  target_date: string
  scope: NoteScope
}

export interface UseStrideNotesResult {
  /** Notes no coach run has consumed yet. */
  notes: Note[]
  /** Notes already consumed by a coach run, kept as history. */
  consumedNotes: Note[]
  loading: boolean
  /** Re-read both lists. Aborted on unmount unless a signal is passed. */
  reload: (signal?: AbortSignal) => Promise<void>
  /** Create a note. Does not reload the lists. */
  createNote: (input: NoteInput) => Promise<StrideMutationResult>
  /** Update a note; a 409 means it was consumed meanwhile. Does not reload the lists. */
  updateNote: (id: number, input: NoteInput) => Promise<StrideMutationResult>
  /** Delete a note and drop it from whichever list holds it. */
  deleteNote: (id: number) => Promise<StrideMutationResult>
}

/**
 * Owns the Stride coach notes: one fetch of every note split into active and
 * consumed (aborted on unmount), plus create/update/delete.
 */
export function useStrideNotes(): UseStrideNotesResult {
  const [notes, setNotes] = useState<Note[]>([])
  const [consumedNotes, setConsumedNotes] = useState<Note[]>([])
  const [loading, setLoading] = useState(true)
  // Lives for as long as the hook is mounted; reloads triggered by the caller
  // use it so a response that lands after unmount is dropped.
  const lifetimeRef = useRef<AbortController | null>(null)

  const loadNotes = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/stride/notes?status=all', { credentials: 'include', signal })
      if (!res.ok) {
        throw new Error(`Failed to load notes: ${res.status} ${res.statusText}`)
      }
      const data = await res.json()
      if (!signal?.aborted) {
        const allNotes: Note[] = data.notes ?? []
        setNotes(allNotes.filter(n => !n.consumed_at))
        setConsumedNotes(allNotes.filter(n => !!n.consumed_at))
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load notes', error)
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
    void loadNotes(controller.signal)
    return () => {
      controller.abort()
      if (lifetimeRef.current === controller) lifetimeRef.current = null
    }
  }, [loadNotes])

  const reload = useCallback(
    (signal?: AbortSignal) => loadNotes(signal ?? lifetimeRef.current?.signal),
    [loadNotes],
  )

  const createNote = useCallback(async (input: NoteInput): Promise<StrideMutationResult> => {
    try {
      const res = await fetch('/api/stride/notes', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        console.error('Failed to create note', data.error ?? res.statusText)
        return { ok: false, status: res.status, error: data.error }
      }
      return { ok: true }
    } catch (error) {
      console.error('Failed to create note', error)
      return { ok: false }
    }
  }, [])

  const updateNote = useCallback(async (id: number, input: NoteInput): Promise<StrideMutationResult> => {
    try {
      const res = await fetch(`/api/stride/notes/${id}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        return { ok: false, status: res.status, error: data.error }
      }
      return { ok: true }
    } catch (error) {
      console.error('Failed to update note', error)
      return { ok: false }
    }
  }, [])

  const deleteNote = useCallback(async (id: number): Promise<StrideMutationResult> => {
    try {
      const res = await fetch(`/api/stride/notes/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        console.error('Failed to delete note', data.error)
        return { ok: false, status: res.status, error: data.error }
      }
      setNotes(prev => prev.filter(n => n.id !== id))
      setConsumedNotes(prev => prev.filter(n => n.id !== id))
      return { ok: true }
    } catch (error) {
      console.error('Failed to delete note', error)
      return { ok: false }
    }
  }, [])

  return { notes, consumedNotes, loading, reload, createNote, updateNote, deleteNote }
}
