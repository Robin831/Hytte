import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * Schema version of the stored envelope. Bump it whenever the shape of a
 * persisted draft changes — drafts written by any other version are ignored
 * on read and overwritten by the next save.
 */
export const WIZARD_DRAFT_VERSION = 1

export const DEFAULT_DRAFT_DEBOUNCE_MS = 400

export interface WizardDraftEnvelope<T> {
  version: number
  savedAt: string
  data: T
}

export function wizardDraftKey(name: string): string {
  return `hytte:wizard-draft:${name}`
}

/**
 * Read a draft. Never throws: a missing key, an unavailable localStorage
 * (Safari private mode), corrupt JSON, a version mismatch or a payload that
 * fails `validate` all yield null.
 */
export function readDraft<T>(name: string, validate?: (data: unknown) => data is T): T | null {
  let raw: string | null
  try {
    raw = window.localStorage.getItem(wizardDraftKey(name))
  } catch {
    return null
  }
  if (!raw) return null

  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object') return null

  const envelope = parsed as Partial<WizardDraftEnvelope<unknown>>
  if (envelope.version !== WIZARD_DRAFT_VERSION) return null
  if (envelope.data === undefined || envelope.data === null) return null
  if (validate && !validate(envelope.data)) return null

  return envelope.data as T
}

/** Write a draft. Storage failures (quota, private mode) are swallowed. */
export function writeDraft<T>(name: string, data: T): void {
  const envelope: WizardDraftEnvelope<T> = {
    version: WIZARD_DRAFT_VERSION,
    savedAt: new Date().toISOString(),
    data,
  }
  try {
    window.localStorage.setItem(wizardDraftKey(name), JSON.stringify(envelope))
  } catch {
    // Quota exceeded or storage denied — a lost draft must not break the wizard.
  }
}

export function clearDraft(name: string): void {
  try {
    window.localStorage.removeItem(wizardDraftKey(name))
  } catch {
    // Ignore — nothing sensible to do if storage is unavailable.
  }
}

export interface UseWizardDraftOptions<T> {
  debounceMs?: number
  validate?: (data: unknown) => data is T
}

export interface WizardDraft<T> {
  /** The draft found on mount, read exactly once. Null when there is none. */
  loaded: T | null
  /** Queue a debounced write. */
  save: (data: T) => void
  /** Drop any pending write and remove the stored draft. */
  clear: () => void
  /** Write any pending draft immediately (e.g. before navigating away). */
  flush: () => void
}

/**
 * Persist a wizard's state to localStorage under a single versioned key.
 *
 * The draft is read once on mount and handed back as `loaded` — restoring it
 * is up to the caller, so the wizard can start empty and offer the draft
 * behind a banner instead of silently resurrecting old input.
 */
export function useWizardDraft<T>(
  name: string,
  options: UseWizardDraftOptions<T> = {},
): WizardDraft<T> {
  const { debounceMs = DEFAULT_DRAFT_DEBOUNCE_MS, validate } = options

  // Read once, with the validator as it stood on the first render — the draft
  // is a mount-time snapshot, so later identity changes are irrelevant.
  const [loaded] = useState<T | null>(() => readDraft<T>(name, validate))

  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pendingRef = useRef<{ data: T } | null>(null)

  const cancelTimer = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }, [])

  const flush = useCallback(() => {
    cancelTimer()
    const pending = pendingRef.current
    pendingRef.current = null
    if (pending) writeDraft(name, pending.data)
  }, [cancelTimer, name])

  const save = useCallback(
    (data: T) => {
      pendingRef.current = { data }
      cancelTimer()
      timerRef.current = setTimeout(() => {
        timerRef.current = null
        const pending = pendingRef.current
        pendingRef.current = null
        if (pending) writeDraft(name, pending.data)
      }, debounceMs)
    },
    [cancelTimer, debounceMs, name],
  )

  // Dropping the pending write first is what stops a keystroke queued moments
  // before a save or a discard from resurrecting the draft we just removed.
  const clear = useCallback(() => {
    cancelTimer()
    pendingRef.current = null
    clearDraft(name)
  }, [cancelTimer, name])

  // Keep the latest flush in a ref so the unmount handler never reads a stale
  // closure.
  const flushRef = useRef(flush)
  useEffect(() => {
    flushRef.current = flush
  })

  // Flush on unmount so a draft typed in the last few hundred milliseconds
  // survives navigating away. After clear() there is nothing pending, so a
  // discarded or saved draft stays gone.
  useEffect(() => () => { flushRef.current() }, [])

  return { loaded, save, clear, flush }
}
