// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// Mock canvas-confetti before importing the module under test so we can
// observe what options get passed without the module trying to touch the
// real canvas during tests. Use vi.hoisted so the mock handle exists when
// vi.mock runs during module resolution.
const { confettiMock } = vi.hoisted(() => ({
  confettiMock: vi.fn((_opts?: { colors?: string[]; particleCount?: number; origin?: { x?: number; y?: number }; [k: string]: unknown }) => Promise.resolve(undefined)),
}))

vi.mock('canvas-confetti', () => ({
  default: Object.assign(confettiMock, { reset: vi.fn(), create: vi.fn() }),
}))

import { burst, screenShake, blitzIntensityForScore } from './confetti'

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

describe('confetti', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    setReducedMotion(false)
    confettiMock.mockClear()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('burst calls canvas-confetti with the requested palette', () => {
    burst('medium', 'golden')
    expect(confettiMock).toHaveBeenCalledTimes(1)
    const opts = confettiMock.mock.calls[0][0]
    expect(opts?.colors).toContain('#facc15')
    expect(opts?.particleCount).toBeGreaterThan(0)
  })

  it('burst with intensity "full" fires from both bottom corners', () => {
    burst('full', 'rainbow')
    expect(confettiMock).toHaveBeenCalledTimes(2)
    const origins = confettiMock.mock.calls.map(c => c[0]?.origin)
    expect(origins.map(o => o?.x)).toEqual([0, 1])
  })

  it('burst is a no-op under prefers-reduced-motion', () => {
    setReducedMotion(true)
    burst('large', 'default')
    expect(confettiMock).not.toHaveBeenCalled()
  })

  it('screenShake adds and later removes the shake class', () => {
    const el = document.createElement('div')
    screenShake(el)
    expect(el.classList.contains('regnemester-screen-shake')).toBe(true)
    vi.advanceTimersByTime(599)
    expect(el.classList.contains('regnemester-screen-shake')).toBe(true)
    vi.advanceTimersByTime(2)
    expect(el.classList.contains('regnemester-screen-shake')).toBe(false)
  })

  it('screenShake is a no-op under prefers-reduced-motion', () => {
    setReducedMotion(true)
    const el = document.createElement('div')
    screenShake(el)
    expect(el.classList.contains('regnemester-screen-shake')).toBe(false)
  })

  it('screenShake tolerates a null root', () => {
    expect(() => screenShake(null)).not.toThrow()
  })

  it('blitzIntensityForScore maps score thresholds correctly', () => {
    expect(blitzIntensityForScore(0)).toBe('small')
    expect(blitzIntensityForScore(119)).toBe('small')
    expect(blitzIntensityForScore(120)).toBe('medium')
    expect(blitzIntensityForScore(199)).toBe('medium')
    expect(blitzIntensityForScore(200)).toBe('large')
    expect(blitzIntensityForScore(500)).toBe('large')
  })
})
