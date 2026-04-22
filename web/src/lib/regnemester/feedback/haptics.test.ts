// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { vibrate, vibrateCorrect, vibrateWrong, prefersReducedMotion } from './haptics'

function installMatchMedia(reduce: boolean) {
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
  vi.stubGlobal('matchMedia', vi.fn(() => mql))
  Object.defineProperty(window, 'matchMedia', { value: vi.fn(() => mql), configurable: true })
  return mql
}

describe('haptics', () => {
  beforeEach(() => {
    // Default: no prefers-reduced-motion, vibrate present.
    installMatchMedia(false)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    delete (navigator as { vibrate?: unknown }).vibrate
  })

  it('prefersReducedMotion returns true when media query matches', () => {
    installMatchMedia(true)
    expect(prefersReducedMotion()).toBe(true)
  })

  it('prefersReducedMotion returns false by default', () => {
    expect(prefersReducedMotion()).toBe(false)
  })

  it('vibrate forwards to navigator.vibrate when supported', () => {
    const fn = vi.fn(() => true)
    Object.defineProperty(navigator, 'vibrate', { value: fn, configurable: true })
    vibrate(100)
    expect(fn).toHaveBeenCalledWith(100)
  })

  it('vibrate is a no-op when prefers-reduced-motion is set', () => {
    installMatchMedia(true)
    const fn = vi.fn(() => true)
    Object.defineProperty(navigator, 'vibrate', { value: fn, configurable: true })
    vibrate(100)
    expect(fn).not.toHaveBeenCalled()
  })

  it('vibrate is a no-op when navigator.vibrate is missing', () => {
    // navigator.vibrate not defined — should silently skip.
    expect(() => vibrate([10, 10, 10])).not.toThrow()
  })

  it('vibrateCorrect fires a short tap', () => {
    const fn = vi.fn(() => true)
    Object.defineProperty(navigator, 'vibrate', { value: fn, configurable: true })
    vibrateCorrect()
    expect(fn).toHaveBeenCalledWith(20)
  })

  it('vibrateWrong fires a triple pulse', () => {
    const fn = vi.fn(() => true)
    Object.defineProperty(navigator, 'vibrate', { value: fn, configurable: true })
    vibrateWrong()
    expect(fn).toHaveBeenCalledWith([30, 40, 30])
  })

  it('vibrate swallows navigator.vibrate throwing', () => {
    const fn = vi.fn(() => { throw new Error('not allowed') })
    Object.defineProperty(navigator, 'vibrate', { value: fn, configurable: true })
    expect(() => vibrate(50)).not.toThrow()
  })
})
