// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import {
  useWizardDraft,
  readDraft,
  writeDraft,
  clearDraft,
  wizardDraftKey,
  WIZARD_DRAFT_VERSION,
  DEFAULT_DRAFT_DEBOUNCE_MS,
} from './useWizardDraft'

interface Draft {
  step: string
  values: string[]
}

const NAME = 'test-wizard'
const KEY = wizardDraftKey(NAME)

const draft: Draft = { step: 'stages', values: ['1.4', '2.8'] }

function primeRaw(payload: Record<string, unknown> | string) {
  window.localStorage.setItem(KEY, typeof payload === 'string' ? payload : JSON.stringify(payload))
}

function isDraft(data: unknown): data is Draft {
  const d = data as Draft
  return !!d && typeof d === 'object' && typeof d.step === 'string' && Array.isArray(d.values)
}

describe('readDraft / writeDraft / clearDraft', () => {
  beforeEach(() => {
    window.localStorage.clear()
    vi.restoreAllMocks()
  })

  it('round-trips a draft unchanged', () => {
    writeDraft(NAME, draft)
    expect(readDraft<Draft>(NAME)).toEqual(draft)
  })

  it('stamps the envelope with the schema version and a timestamp', () => {
    writeDraft(NAME, draft)
    const envelope = JSON.parse(window.localStorage.getItem(KEY)!)
    expect(envelope.version).toBe(WIZARD_DRAFT_VERSION)
    expect(typeof envelope.savedAt).toBe('string')
    expect(envelope.data).toEqual(draft)
  })

  it('returns null when there is no draft', () => {
    expect(readDraft<Draft>(NAME)).toBeNull()
  })

  it('ignores a draft written by an older version', () => {
    primeRaw({ version: WIZARD_DRAFT_VERSION - 1, savedAt: '2026-01-01T00:00:00.000Z', data: draft })
    expect(readDraft<Draft>(NAME)).toBeNull()
  })

  it('ignores a draft written by a newer version', () => {
    primeRaw({ version: WIZARD_DRAFT_VERSION + 1, savedAt: '2026-01-01T00:00:00.000Z', data: draft })
    expect(readDraft<Draft>(NAME)).toBeNull()
  })

  it('ignores an envelope with no version field', () => {
    primeRaw({ savedAt: '2026-01-01T00:00:00.000Z', data: draft })
    expect(readDraft<Draft>(NAME)).toBeNull()
  })

  it('overwrites an unreadable draft on the next write', () => {
    primeRaw({ version: WIZARD_DRAFT_VERSION - 1, savedAt: '2026-01-01T00:00:00.000Z', data: draft })
    writeDraft(NAME, draft)
    expect(readDraft<Draft>(NAME)).toEqual(draft)
  })

  it('returns null for malformed JSON instead of throwing', () => {
    primeRaw('{not json')
    expect(() => readDraft<Draft>(NAME)).not.toThrow()
    expect(readDraft<Draft>(NAME)).toBeNull()
  })

  it('returns null for a non-object payload', () => {
    primeRaw('42')
    expect(readDraft<Draft>(NAME)).toBeNull()
  })

  it('returns null when the envelope carries no data', () => {
    primeRaw({ version: WIZARD_DRAFT_VERSION, savedAt: '2026-01-01T00:00:00.000Z' })
    expect(readDraft<Draft>(NAME)).toBeNull()
  })

  it('returns null when the payload fails validation', () => {
    primeRaw({ version: WIZARD_DRAFT_VERSION, savedAt: '2026-01-01T00:00:00.000Z', data: { step: 1 } })
    expect(readDraft<Draft>(NAME, isDraft)).toBeNull()
  })

  it('returns a payload that passes validation', () => {
    writeDraft(NAME, draft)
    expect(readDraft<Draft>(NAME, isDraft)).toEqual(draft)
  })

  it('returns null when reading throws', () => {
    const getItem = vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError')
    })
    expect(readDraft<Draft>(NAME)).toBeNull()
    getItem.mockRestore()
  })

  it('swallows a storage write failure', () => {
    const setItem = vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => {
      throw new DOMException('quota', 'QuotaExceededError')
    })
    expect(() => writeDraft(NAME, draft)).not.toThrow()
    setItem.mockRestore()
    expect(readDraft<Draft>(NAME)).toBeNull()
  })

  it('clears the stored draft', () => {
    writeDraft(NAME, draft)
    clearDraft(NAME)
    expect(window.localStorage.getItem(KEY)).toBeNull()
    expect(readDraft<Draft>(NAME)).toBeNull()
  })

  it('swallows a storage removal failure', () => {
    const removeItem = vi.spyOn(window.localStorage, 'removeItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError')
    })
    expect(() => clearDraft(NAME)).not.toThrow()
    removeItem.mockRestore()
  })

  it('keeps drafts for different wizards apart', () => {
    writeDraft(NAME, draft)
    writeDraft('other-wizard', { step: 'review', values: [] })
    expect(readDraft<Draft>(NAME)).toEqual(draft)
    expect(readDraft<Draft>('other-wizard')!.step).toBe('review')
  })
})

describe('useWizardDraft', () => {
  beforeEach(() => {
    window.localStorage.clear()
    vi.restoreAllMocks()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('exposes the stored draft as loaded on mount', () => {
    writeDraft(NAME, draft)
    const { result } = renderHook(() => useWizardDraft<Draft>(NAME, { validate: isDraft }))
    expect(result.current.loaded).toEqual(draft)
  })

  it('exposes null when the stored draft is from another version', () => {
    primeRaw({ version: WIZARD_DRAFT_VERSION + 1, savedAt: '2026-01-01T00:00:00.000Z', data: draft })
    const { result } = renderHook(() => useWizardDraft<Draft>(NAME))
    expect(result.current.loaded).toBeNull()
  })

  it('exposes null for malformed JSON without throwing', () => {
    primeRaw('{not json')
    expect(() => renderHook(() => useWizardDraft<Draft>(NAME))).not.toThrow()
    const { result } = renderHook(() => useWizardDraft<Draft>(NAME))
    expect(result.current.loaded).toBeNull()
  })

  it('does not re-read the draft after mount', () => {
    const { result, rerender } = renderHook(() => useWizardDraft<Draft>(NAME))
    expect(result.current.loaded).toBeNull()
    writeDraft(NAME, draft)
    rerender()
    expect(result.current.loaded).toBeNull()
  })

  it('debounces rapid saves into a single write', () => {
    const setItem = vi.spyOn(window.localStorage, 'setItem')
    const { result } = renderHook(() => useWizardDraft<Draft>(NAME))

    act(() => {
      result.current.save({ step: 'stages', values: ['1'] })
      result.current.save({ step: 'stages', values: ['1.'] })
      result.current.save({ step: 'stages', values: ['1.4'] })
    })
    expect(setItem).not.toHaveBeenCalled()

    act(() => { vi.advanceTimersByTime(DEFAULT_DRAFT_DEBOUNCE_MS) })
    expect(setItem).toHaveBeenCalledTimes(1)
    expect(readDraft<Draft>(NAME)!.values).toEqual(['1.4'])
  })

  it('honours a custom debounce window', () => {
    const { result } = renderHook(() => useWizardDraft<Draft>(NAME, { debounceMs: 1000 }))

    act(() => { result.current.save(draft) })
    act(() => { vi.advanceTimersByTime(999) })
    expect(readDraft<Draft>(NAME)).toBeNull()

    act(() => { vi.advanceTimersByTime(1) })
    expect(readDraft<Draft>(NAME)).toEqual(draft)
  })

  it('clear() drops a pending write so the draft does not come back', () => {
    const { result } = renderHook(() => useWizardDraft<Draft>(NAME))

    act(() => { result.current.save(draft) })
    act(() => { result.current.clear() })
    act(() => { vi.advanceTimersByTime(DEFAULT_DRAFT_DEBOUNCE_MS * 4) })

    expect(window.localStorage.getItem(KEY)).toBeNull()
  })

  it('clear() removes an already-written draft', () => {
    writeDraft(NAME, draft)
    const { result } = renderHook(() => useWizardDraft<Draft>(NAME))

    act(() => { result.current.clear() })
    expect(window.localStorage.getItem(KEY)).toBeNull()
  })

  it('flush() writes the pending draft immediately', () => {
    const { result } = renderHook(() => useWizardDraft<Draft>(NAME))

    act(() => { result.current.save(draft) })
    act(() => { result.current.flush() })
    expect(readDraft<Draft>(NAME)).toEqual(draft)

    // The flushed write is not repeated when the debounce window elapses.
    const setItem = vi.spyOn(window.localStorage, 'setItem')
    act(() => { vi.advanceTimersByTime(DEFAULT_DRAFT_DEBOUNCE_MS * 4) })
    expect(setItem).not.toHaveBeenCalled()
  })

  it('flush() is a no-op when nothing is pending', () => {
    const setItem = vi.spyOn(window.localStorage, 'setItem')
    const { result } = renderHook(() => useWizardDraft<Draft>(NAME))

    act(() => { result.current.flush() })
    expect(setItem).not.toHaveBeenCalled()
  })

  it('flushes a pending write on unmount', () => {
    const { result, unmount } = renderHook(() => useWizardDraft<Draft>(NAME))

    act(() => { result.current.save(draft) })
    unmount()

    expect(readDraft<Draft>(NAME)).toEqual(draft)
  })

  it('does not resurrect a cleared draft on unmount', () => {
    const { result, unmount } = renderHook(() => useWizardDraft<Draft>(NAME))

    act(() => { result.current.save(draft) })
    act(() => { result.current.clear() })
    unmount()

    expect(window.localStorage.getItem(KEY)).toBeNull()
  })
})
