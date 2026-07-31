// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { getDraft, setDraft, clearDraft, draftKey, DRAFT_KEY_PREFIX, __resetDrafts } from './drafts'

describe('familychat drafts', () => {
  beforeEach(() => { __resetDrafts() })
  afterEach(() => { vi.restoreAllMocks(); __resetDrafts() })

  it('round-trips a draft for a conversation', () => {
    setDraft(1, 'half typed')
    expect(getDraft(1)).toBe('half typed')
  })

  it('keeps drafts isolated per conversation id', () => {
    setDraft(1, 'for one')
    setDraft(2, 'for two')
    expect(getDraft(1)).toBe('for one')
    expect(getDraft(2)).toBe('for two')
    expect(getDraft(3)).toBe('')
  })

  it('preserves the exact text, including surrounding whitespace', () => {
    setDraft(1, '  padded text  ')
    expect(getDraft(1)).toBe('  padded text  ')
  })

  it('namespaces the localStorage key per conversation', () => {
    setDraft(42, 'stored')
    expect(draftKey(42)).toBe(`${DRAFT_KEY_PREFIX}42`)
    expect(localStorage.getItem(draftKey(42))).toBe('stored')
  })

  it('survives a reload — a draft written earlier is read back from storage', () => {
    setDraft(7, 'still here')
    // __resetDrafts drops the in-memory mirror but we re-seed storage to
    // stand in for a fresh page load with the same localStorage.
    const stored = localStorage.getItem(draftKey(7))
    __resetDrafts()
    localStorage.setItem(draftKey(7), stored!)
    expect(getDraft(7)).toBe('still here')
  })

  it('removes a stored draft when the body becomes empty or whitespace-only', () => {
    setDraft(1, 'something')
    setDraft(1, '   ')
    expect(getDraft(1)).toBe('')
    expect(localStorage.getItem(draftKey(1))).toBeNull()

    setDraft(2, 'something else')
    setDraft(2, '')
    expect(getDraft(2)).toBe('')
    expect(localStorage.getItem(draftKey(2))).toBeNull()
  })

  it('stores nothing for a blank body', () => {
    setDraft(1, '  ')
    expect(localStorage.getItem(draftKey(1))).toBeNull()
    expect(getDraft(1)).toBe('')
  })

  it('clearDraft removes the draft from storage and memory', () => {
    setDraft(1, 'to be sent')
    clearDraft(1)
    expect(getDraft(1)).toBe('')
    expect(localStorage.getItem(draftKey(1))).toBeNull()
  })

  it('ignores a falsy conversation id', () => {
    setDraft(0, 'nowhere')
    expect(getDraft(0)).toBe('')
    expect(localStorage.getItem(draftKey(0))).toBeNull()
    expect(() => clearDraft(0)).not.toThrow()
  })

  it('falls back to memory when localStorage throws', () => {
    // Stands in for private mode / a blocked or full storage: every operation
    // throws. The composer must keep working for the current session.
    const throwing = {
      length: 0,
      getItem: vi.fn(() => { throw new Error('storage disabled') }),
      setItem: vi.fn(() => { throw new Error('quota exceeded') }),
      removeItem: vi.fn(() => { throw new Error('storage disabled') }),
      key: vi.fn(() => { throw new Error('storage disabled') }),
      clear: vi.fn(() => { throw new Error('storage disabled') }),
    }
    const original = Object.getOwnPropertyDescriptor(window, 'localStorage')
    Object.defineProperty(window, 'localStorage', { configurable: true, get: () => throwing })
    try {
      expect(() => setDraft(1, 'private mode')).not.toThrow()
      expect(getDraft(1)).toBe('private mode')
      // No memory entry for 2 — the throwing read is swallowed.
      expect(getDraft(2)).toBe('')
      expect(() => clearDraft(1)).not.toThrow()
      expect(getDraft(1)).toBe('')
      expect(() => __resetDrafts()).not.toThrow()

      expect(throwing.setItem).toHaveBeenCalled()
      expect(throwing.getItem).toHaveBeenCalled()
      expect(throwing.removeItem).toHaveBeenCalled()
    } finally {
      if (original) Object.defineProperty(window, 'localStorage', original)
      else Reflect.deleteProperty(window, 'localStorage')
    }
  })
})
