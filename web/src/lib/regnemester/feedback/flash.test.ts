// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { flashCorrect, flashWrong } from './flash'

function setReducedMotion(reduce: boolean) {
  const mql = {
    matches: reduce,
    media: '(prefers-reduced-motion: reduce)',
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }
  Object.defineProperty(window, 'matchMedia', { value: vi.fn(() => mql), configurable: true })
}

describe('flash', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    setReducedMotion(false)
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('flashCorrect adds and later removes the correct class', () => {
    const el = document.createElement('div')
    flashCorrect(el)
    expect(el.classList.contains('regnemester-flash-correct')).toBe(true)
    vi.advanceTimersByTime(149)
    expect(el.classList.contains('regnemester-flash-correct')).toBe(true)
    vi.advanceTimersByTime(2)
    expect(el.classList.contains('regnemester-flash-correct')).toBe(false)
  })

  it('flashWrong adds and later removes the wrong class', () => {
    const el = document.createElement('div')
    flashWrong(el)
    expect(el.classList.contains('regnemester-flash-wrong')).toBe(true)
    vi.advanceTimersByTime(249)
    expect(el.classList.contains('regnemester-flash-wrong')).toBe(true)
    vi.advanceTimersByTime(2)
    expect(el.classList.contains('regnemester-flash-wrong')).toBe(false)
  })

  it('rapid re-trigger cancels the prior timer so the class sticks for the full new duration', () => {
    const el = document.createElement('div')
    flashCorrect(el)
    vi.advanceTimersByTime(100) // 50ms before original removal
    flashCorrect(el)
    // 100ms after the re-trigger — still well within the 150ms window.
    vi.advanceTimersByTime(100)
    expect(el.classList.contains('regnemester-flash-correct')).toBe(true)
    vi.advanceTimersByTime(100)
    expect(el.classList.contains('regnemester-flash-correct')).toBe(false)
  })

  it('is a no-op when prefers-reduced-motion is set', () => {
    setReducedMotion(true)
    const el = document.createElement('div')
    flashCorrect(el)
    flashWrong(el)
    expect(el.classList.contains('regnemester-flash-correct')).toBe(false)
    expect(el.classList.contains('regnemester-flash-wrong')).toBe(false)
  })

  it('tolerates a null element', () => {
    expect(() => flashCorrect(null)).not.toThrow()
    expect(() => flashWrong(null)).not.toThrow()
  })
})
