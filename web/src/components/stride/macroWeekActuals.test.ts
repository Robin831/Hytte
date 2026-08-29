import { describe, it, expect } from 'vitest'
import { actualsByWeek } from './macroWeekActuals'
import { weeksRemaining } from './macroPlan'
import type { WeekSummary } from '../../types/stride'

function summary(overrides: Partial<WeekSummary> & { week_start: string }): WeekSummary {
  return {
    plan_id: 1,
    week_end: '',
    phase: 'base',
    sessions_planned: 5,
    sessions_completed: 4,
    completion_rate: 80,
    ...overrides,
  }
}

describe('actualsByWeek', () => {
  it('indexes weeks by Monday and converts metres to kilometres', () => {
    const actuals = actualsByWeek([
      summary({ week_start: '2026-01-05', total_distance_meters: 52400, sessions_completed: 5 }),
      summary({ week_start: '2026-01-12', total_distance_meters: 41000, sessions_completed: 3 }),
    ])

    expect(actuals.get('2026-01-05')).toEqual({ km: 52.4, sessionsCompleted: 5, sessionsPlanned: 5 })
    expect(actuals.get('2026-01-12')?.km).toBe(41)
  })

  it('leaves a week with no history entry undefined rather than zero', () => {
    const actuals = actualsByWeek([summary({ week_start: '2026-01-05', total_distance_meters: 10000 })])

    expect(actuals.has('2026-01-12')).toBe(false)
    expect(actuals.get('2026-01-12')).toBeUndefined()
  })

  it('treats a missing distance as zero kilometres', () => {
    const actuals = actualsByWeek([summary({ week_start: '2026-01-05' })])

    expect(actuals.get('2026-01-05')?.km).toBe(0)
  })

  it('keeps the first entry when a week is repeated across pages', () => {
    const actuals = actualsByWeek([
      summary({ week_start: '2026-01-05', total_distance_meters: 52000 }),
      summary({ week_start: '2026-01-05', total_distance_meters: 12000 }),
    ])

    expect(actuals.get('2026-01-05')?.km).toBe(52)
    expect(actuals.size).toBe(1)
  })

  it('keeps history from outside the block — the caller only looks up its own weeks', () => {
    const actuals = actualsByWeek([
      summary({ week_start: '2025-06-02', total_distance_meters: 30000 }),
      summary({ week_start: '2026-01-05', total_distance_meters: 50000 }),
    ])

    expect(actuals.size).toBe(2)
  })

  it('returns an empty map for no history', () => {
    expect(actualsByWeek([]).size).toBe(0)
  })
})

describe('weeksRemaining', () => {
  it('counts whole weeks between two Mondays', () => {
    expect(weeksRemaining('2026-01-05', '2026-03-02')).toBe(8)
  })

  it('is zero when the block ends in the week in progress', () => {
    expect(weeksRemaining('2026-01-05', '2026-01-05')).toBe(0)
  })

  it('is negative once the horizon is behind the current week', () => {
    expect(weeksRemaining('2026-03-02', '2026-01-05')).toBe(-8)
  })

  it('is not thrown off by a DST change between the two Mondays', () => {
    // Europe/Oslo moves the clocks on 2026-03-29, inside this span.
    expect(weeksRemaining('2026-03-23', '2026-04-06')).toBe(2)
  })

  it('returns 0 for an unparsable week key rather than NaN', () => {
    expect(weeksRemaining('not-a-date', '2026-03-02')).toBe(0)
  })
})
