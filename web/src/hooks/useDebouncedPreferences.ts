import { useCallback, useEffect, useRef, useState } from 'react'

// Per-section save status shown next to a settings section header.
export type PrefSaveStatus = 'idle' | 'saving' | 'saved' | 'error'

interface UseDebouncedPreferencesOptions {
  // Debounce window in milliseconds before a queued batch is flushed.
  delayMs?: number
  // Maps a preference key to the id of the section it belongs to. The returned
  // id is used to key the per-section status map.
  sectionForKey: (key: string) => string
  // Called with the full preference map returned by the server after a
  // successful write, so the caller can update its local state.
  onSaved: (prefs: Record<string, string>) => void
  // Called once per failed batch (not once per key) so the caller can show a
  // single error toast.
  onErrorToast: () => void
  // Optional success toast, shown only for immediate saves that opt in.
  onSuccessToast?: () => void
}

export interface UseDebouncedPreferencesResult {
  // Per-section save status, keyed by the ids returned from sectionForKey.
  status: Record<string, PrefSaveStatus>
  // Queue a single preference change. Changes accumulate and flush as one
  // request delayMs after the last edit.
  queuePreference: (key: string, value: string) => void
  // Persist immediately (e.g. for toggles/selects). Drains any pending queued
  // changes into the same request. Resolves true on success, false on failure.
  saveNow: (updates: Record<string, string>, toastOnSuccess?: boolean) => Promise<boolean>
  // Flush any pending queued changes right away (e.g. on blur).
  flush: () => Promise<boolean>
}

const SAVED_RESET_MS = 2000

// useDebouncedPreferences batches preference writes to
// PUT /api/settings/preferences. Queued keys accumulate in local state and
// flush as a single request ~delayMs after the last edit, on an explicit
// flush(), or on unmount/navigation. Each affected section exposes a
// Saving… → Saved (or Error) status, and failures surface as one toast per
// batch rather than one per field.
export function useDebouncedPreferences(
  options: UseDebouncedPreferencesOptions,
): UseDebouncedPreferencesResult {
  const { delayMs = 500 } = options

  // Keep the latest callbacks in a ref so timers/unmount handlers never read a
  // stale closure.
  const optionsRef = useRef(options)
  useEffect(() => {
    optionsRef.current = options
  })

  const [status, setStatus] = useState<Record<string, PrefSaveStatus>>({})
  const pendingRef = useRef<Record<string, string>>({})
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const savedResetTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  const mountedRef = useRef(true)

  const sectionsOf = useCallback((keys: string[]): Set<string> => {
    const set = new Set<string>()
    for (const key of keys) set.add(optionsRef.current.sectionForKey(key))
    return set
  }, [])

  const setSectionStatus = useCallback((sections: Set<string>, value: PrefSaveStatus) => {
    setStatus((prev) => {
      const next = { ...prev }
      for (const section of sections) next[section] = value
      return next
    })
  }, [])

  // After a successful save, return the section to idle so the "Saved" badge
  // does not linger indefinitely.
  const scheduleSavedReset = useCallback((sections: Set<string>) => {
    for (const section of sections) {
      if (savedResetTimers.current[section]) clearTimeout(savedResetTimers.current[section])
      savedResetTimers.current[section] = setTimeout(() => {
        setStatus((prev) => (prev[section] === 'saved' ? { ...prev, [section]: 'idle' } : prev))
      }, SAVED_RESET_MS)
    }
  }, [])

  // Perform the actual write for a set of updates and drive the section status.
  const commit = useCallback(
    async (updates: Record<string, string>, toastOnSuccess = false): Promise<boolean> => {
      const keys = Object.keys(updates)
      if (keys.length === 0) return true
      const sections = sectionsOf(keys)
      setSectionStatus(sections, 'saving')
      try {
        const res = await fetch('/api/settings/preferences', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ preferences: updates }),
        })
        if (!res.ok) throw new Error(`save failed (${res.status})`)
        const data = await res.json()
        if (!mountedRef.current) return true
        optionsRef.current.onSaved(data.preferences || {})
        setSectionStatus(sections, 'saved')
        scheduleSavedReset(sections)
        if (toastOnSuccess) optionsRef.current.onSuccessToast?.()
        return true
      } catch {
        if (!mountedRef.current) return false
        setSectionStatus(sections, 'error')
        optionsRef.current.onErrorToast()
        return false
      }
    },
    [sectionsOf, setSectionStatus, scheduleSavedReset],
  )

  const flush = useCallback(async (): Promise<boolean> => {
    if (timerRef.current) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
    const snapshot = pendingRef.current
    pendingRef.current = {}
    return commit(snapshot)
  }, [commit])

  // flush is referenced from queuePreference's timer; keep it in a ref so the
  // timer always calls the latest version.
  const flushRef = useRef(flush)
  useEffect(() => {
    flushRef.current = flush
  })

  const queuePreference = useCallback(
    (key: string, value: string) => {
      pendingRef.current[key] = value
      // Show "Saving…" as soon as the user makes a change, even during the
      // debounce window.
      setSectionStatus(sectionsOf([key]), 'saving')
      if (timerRef.current) clearTimeout(timerRef.current)
      timerRef.current = setTimeout(() => {
        void flushRef.current()
      }, delayMs)
    },
    [delayMs, sectionsOf, setSectionStatus],
  )

  const saveNow = useCallback(
    async (updates: Record<string, string>, toastOnSuccess = false): Promise<boolean> => {
      if (timerRef.current) {
        clearTimeout(timerRef.current)
        timerRef.current = null
      }
      // Drain any queued edits into the same request so nothing is lost.
      const merged = { ...pendingRef.current, ...updates }
      pendingRef.current = {}
      return commit(merged, toastOnSuccess)
    },
    [commit],
  )

  // Flush pending changes before the component unmounts (e.g. navigation) so
  // no edits are silently lost. Uses keepalive so the request survives a page
  // teardown; no state is touched here since the component is gone.
  useEffect(() => {
    return () => {
      mountedRef.current = false
      if (timerRef.current) clearTimeout(timerRef.current)
      for (const timer of Object.values(savedResetTimers.current)) clearTimeout(timer)
      const pending = pendingRef.current
      pendingRef.current = {}
      if (Object.keys(pending).length === 0) return
      try {
        void fetch('/api/settings/preferences', {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ preferences: pending }),
          keepalive: true,
        }).catch(() => {})
      } catch {
        // Best-effort: nothing else we can do during teardown.
      }
    }
  }, [])

  return { status, queuePreference, saveNow, flush }
}
