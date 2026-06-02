import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'

export interface Note {
  id: number
  user_id: number
  title: string
  content: string
  tags: string[]
  created_at: string
  updated_at: string
}

export type ViewMode = 'edit' | 'preview'

/** Payload for creating (no id) or updating (with id) a note. */
export interface NoteInput {
  id?: number
  title: string
  content: string
  tags: string[]
}

export interface UseNotesResult {
  notes: Note[]
  allTags: string[]
  loading: boolean
  error: string
  /** Create (POST when id is absent) or update (PUT when id is present). Returns the saved note, or null on failure. */
  save: (input: NoteInput) => Promise<Note | null>
  /** Delete a note by id. Returns true on success, false on failure. */
  remove: (id: number) => Promise<boolean>
  /** Re-trigger the list + tag fetches. */
  refresh: () => void
}

/**
 * Owns the notes data layer: list/tag fetching (with abort handling) plus
 * save/delete mutations. `search` and `activeTag` drive the list query; an
 * empty `activeTag` means "all tags".
 */
export function useNotes(search: string, activeTag: string): UseNotesResult {
  const { t } = useTranslation('notes')
  const [notes, setNotes] = useState<Note[]>([])
  const [allTags, setAllTags] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refreshKey, setRefreshKey] = useState(0)

  const refresh = useCallback(() => setRefreshKey(k => k + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      try {
        const params = new URLSearchParams()
        if (search) params.set('search', search)
        if (activeTag) params.set('tag', activeTag)
        const res = await fetch(`/api/notes?${params}`, { credentials: 'include', signal: controller.signal })
        if (!res.ok) throw new Error(t('errors.failedToLoad'))
        const data = await res.json()
        setNotes(data.notes ?? [])
        setError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [search, activeTag, refreshKey, t])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/notes/tags', { credentials: 'include', signal: controller.signal })
        if (!res.ok) return
        const data = await res.json()
        setAllTags(data.tags ?? [])
      } catch {
        // non-critical
      }
    })()
    return () => { controller.abort() }
  }, [refreshKey])

  const save = useCallback(async (input: NoteInput): Promise<Note | null> => {
    setError('')
    const isUpdate = input.id != null
    const body = JSON.stringify({ title: input.title, content: input.content, tags: input.tags })
    try {
      const res = await fetch(isUpdate ? `/api/notes/${input.id}` : '/api/notes', {
        method: isUpdate ? 'PUT' : 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body,
      })
      if (!res.ok) {
        let msg = isUpdate ? t('errors.failedToSave') : t('errors.failedToCreate')
        try { const data = await res.json(); msg = data.error ?? msg } catch { /* non-JSON body */ }
        throw new Error(msg)
      }
      const data = await res.json()
      refresh()
      return data.note as Note
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.saveFailed'))
      return null
    }
  }, [t, refresh])

  const remove = useCallback(async (id: number): Promise<boolean> => {
    try {
      const res = await fetch(`/api/notes/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error ?? t('errors.failedToDelete'))
      }
      refresh()
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.deleteFailed'))
      return false
    }
  }, [t, refresh])

  return { notes, allTags, loading, error, save, remove, refresh }
}
