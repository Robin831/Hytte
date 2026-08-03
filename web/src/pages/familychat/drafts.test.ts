// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { getDraft, setDraft, clearDraft, clearAllDrafts, draftKey, DRAFT_KEY_PREFIX, __resetDrafts } from './drafts'

const U = 1

describe('familychat drafts', () => {
  beforeEach(() => { __resetDrafts() })
  afterEach(() => { vi.restoreAllMocks(); __resetDrafts() })

  it('round-trips a draft for a conversation', () => {
    setDraft(U, 1, 'half typed')
    expect(getDraft(U, 1)).toBe('half typed')
  })

  it('keeps drafts isolated per conversation id', () => {
    setDraft(U, 1, 'for one')
    setDraft(U, 2, 'for two')
    expect(getDraft(U, 1)).toBe('for one')
    expect(getDraft(U, 2)).toBe('for two')
    expect(getDraft(U, 3)).toBe('')
  })

  it('preserves the exact text, including surrounding whitespace', () => {
    setDraft(U, 1, '  padded text  ')
    expect(getDraft(U, 1)).toBe('  padded text  ')
  })

  it('namespaces the localStorage key per user and conversation', () => {
    setDraft(U, 42, 'stored')
    expect(draftKey(U, 42)).toBe(`${DRAFT_KEY_PREFIX}${U}:42`)
    expect(localStorage.getItem(draftKey(U, 42))).toBe('stored')
  })

  it('isolates drafts by user id', () => {
    setDraft(1, 5, 'user one draft')
    setDraft(2, 5, 'user two draft')
    expect(getDraft(1, 5)).toBe('user one draft')
    expect(getDraft(2, 5)).toBe('user two draft')
  })

  it('survives a reload — a draft written earlier is read back from storage', () => {
    setDraft(U, 7, 'still here')
    // __resetDrafts drops the in-memory mirror but we re-seed storage to
    // stand in for a fresh page load with the same localStorage.
    const stored = localStorage.getItem(draftKey(U, 7))
    __resetDrafts()
    localStorage.setItem(draftKey(U, 7), stored!)
    expect(getDraft(U, 7)).toBe('still here')
  })

  it('removes a stored draft when the body becomes empty or whitespace-only', () => {
    setDraft(U, 1, 'something')
    setDraft(U, 1, '   ')
    expect(getDraft(U, 1)).toBe('')
    expect(localStorage.getItem(draftKey(U, 1))).toBeNull()

    setDraft(U, 2, 'something else')
    setDraft(U, 2, '')
    expect(getDraft(U, 2)).toBe('')
    expect(localStorage.getItem(draftKey(U, 2))).toBeNull()
  })

  it('stores nothing for a blank body', () => {
    setDraft(U, 1, '  ')
    expect(localStorage.getItem(draftKey(U, 1))).toBeNull()
    expect(getDraft(U, 1)).toBe('')
  })

  it('clearDraft removes the draft from storage and memory', () => {
    setDraft(U, 1, 'to be sent')
    clearDraft(U, 1)
    expect(getDraft(U, 1)).toBe('')
    expect(localStorage.getItem(draftKey(U, 1))).toBeNull()
  })

  it('ignores a falsy conversation id', () => {
    setDraft(U, 0, 'nowhere')
    expect(getDraft(U, 0)).toBe('')
    expect(() => clearDraft(U, 0)).not.toThrow()
  })

  it('ignores a falsy user id', () => {
    setDraft(0, 1, 'nowhere')
    expect(getDraft(0, 1)).toBe('')
    expect(() => clearDraft(0, 1)).not.toThrow()
  })

  it('clearAllDrafts wipes all user drafts from storage', () => {
    setDraft(1, 10, 'user one')
    setDraft(2, 20, 'user two')
    clearAllDrafts()
    expect(getDraft(1, 10)).toBe('')
    expect(getDraft(2, 20)).toBe('')
    expect(localStorage.getItem(draftKey(1, 10))).toBeNull()
    expect(localStorage.getItem(draftKey(2, 20))).toBeNull()
  })

  it('prefers storage over memory so cross-tab writes are visible', () => {
    setDraft(U, 1, 'tab A')
    // Simulate tab B writing a newer value directly to storage.
    localStorage.setItem(draftKey(U, 1), 'tab B newer')
    expect(getDraft(U, 1)).toBe('tab B newer')
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
      expect(() => setDraft(U, 1, 'private mode')).not.toThrow()
      expect(getDraft(U, 1)).toBe('private mode')
      // No memory entry for 2 — the throwing read is swallowed.
      expect(getDraft(U, 2)).toBe('')
      expect(() => clearDraft(U, 1)).not.toThrow()
      expect(getDraft(U, 1)).toBe('')
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
