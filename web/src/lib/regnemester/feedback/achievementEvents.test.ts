// @vitest-environment happy-dom
import { describe, it, expect, afterEach, vi } from 'vitest'
import type { UnlockedAchievement } from '../achievements/types'
import {
  emitAchievementUnlock,
  subscribeAchievementUnlock,
  _resetAchievementHandlers,
} from './achievementEvents'

function makeAch(code: string): UnlockedAchievement {
  return {
    code,
    title: code,
    description: '',
    tier: 'marathon',
    unlocked_at: '2026-04-22T00:00:00Z',
  }
}

describe('achievementEvents', () => {
  afterEach(() => {
    _resetAchievementHandlers()
  })

  it('delivers emitted items to all subscribers', () => {
    const a = vi.fn()
    const b = vi.fn()
    subscribeAchievementUnlock(a)
    subscribeAchievementUnlock(b)
    emitAchievementUnlock([makeAch('streak_25'), makeAch('marathon_sub_5')])
    expect(a).toHaveBeenCalledTimes(1)
    expect(b).toHaveBeenCalledTimes(1)
    expect(a.mock.calls[0][0]).toHaveLength(2)
  })

  it('skips empty or nullish payloads', () => {
    const h = vi.fn()
    subscribeAchievementUnlock(h)
    emitAchievementUnlock([])
    emitAchievementUnlock(null)
    emitAchievementUnlock(undefined)
    expect(h).not.toHaveBeenCalled()
  })

  it('unsubscribe removes the listener', () => {
    const h = vi.fn()
    const off = subscribeAchievementUnlock(h)
    off()
    emitAchievementUnlock([makeAch('streak_25')])
    expect(h).not.toHaveBeenCalled()
  })

  it('isolates listeners from each other when one throws', () => {
    const bad = vi.fn(() => { throw new Error('boom') })
    const good = vi.fn()
    subscribeAchievementUnlock(bad)
    subscribeAchievementUnlock(good)
    expect(() => emitAchievementUnlock([makeAch('streak_25')])).not.toThrow()
    expect(good).toHaveBeenCalled()
  })

  it('passes a copied array so listeners cannot mutate caller state', () => {
    const input = [makeAch('streak_25')]
    let received: UnlockedAchievement[] | null = null
    subscribeAchievementUnlock(items => { received = items })
    emitAchievementUnlock(input)
    expect(received).not.toBe(input)
    expect(received).toEqual(input)
  })
})
