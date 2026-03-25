import { describe, it, expect } from 'vitest'
import { getFlameVariant, xpForLevel, xpProgressPercent } from './stars'

describe('getFlameVariant', () => {
  it('returns flame-grey for 0 streak (no streak)', () => {
    expect(getFlameVariant(0)).toBe('flame-grey')
  })

  it('returns flame-small for streak of 1', () => {
    expect(getFlameVariant(1)).toBe('flame-small')
  })

  it('returns flame-small for streak of 2', () => {
    expect(getFlameVariant(2)).toBe('flame-small')
  })

  it('returns flame-medium for streak of 3', () => {
    expect(getFlameVariant(3)).toBe('flame-medium')
  })

  it('returns flame-medium for streak of 6', () => {
    expect(getFlameVariant(6)).toBe('flame-medium')
  })

  it('returns flame-large for streak of 7', () => {
    expect(getFlameVariant(7)).toBe('flame-large')
  })

  it('returns flame-large for streak of 13', () => {
    expect(getFlameVariant(13)).toBe('flame-large')
  })

  it('returns flame-blue for streak of 14', () => {
    expect(getFlameVariant(14)).toBe('flame-blue')
  })

  it('returns flame-blue for streak of 29', () => {
    expect(getFlameVariant(29)).toBe('flame-blue')
  })

  it('returns flame-rainbow for streak of 30', () => {
    expect(getFlameVariant(30)).toBe('flame-rainbow')
  })

  it('returns flame-rainbow for streaks beyond 30', () => {
    expect(getFlameVariant(100)).toBe('flame-rainbow')
    expect(getFlameVariant(365)).toBe('flame-rainbow')
  })
})

describe('xpForLevel', () => {
  it('returns 0 for level 0 or below', () => {
    expect(xpForLevel(0)).toBe(0)
    expect(xpForLevel(-1)).toBe(0)
  })

  it('returns a positive value for level 1+', () => {
    expect(xpForLevel(1)).toBeGreaterThan(0)
    expect(xpForLevel(10)).toBeGreaterThan(xpForLevel(1))
  })
})

describe('xpProgressPercent', () => {
  it('returns 0 at the start of a level', () => {
    const threshold = xpForLevel(0)
    expect(xpProgressPercent(1, threshold)).toBe(0)
  })

  it('returns 100 when xp meets or exceeds the next level threshold', () => {
    const threshold = xpForLevel(1)
    expect(xpProgressPercent(1, threshold)).toBe(100)
  })

  it('returns a value between 0 and 100 for mid-level progress', () => {
    const low = xpForLevel(0)
    const high = xpForLevel(1)
    const mid = Math.round((low + high) / 2)
    const percent = xpProgressPercent(1, mid)
    expect(percent).toBeGreaterThan(0)
    expect(percent).toBeLessThan(100)
  })
})
