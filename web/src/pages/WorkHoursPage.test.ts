import { describe, it, expect } from 'vitest'
import { calculateDayWithLivePunch, type WorkSettings } from './workHoursUtils'

const defaultSettings: WorkSettings = {
  standard_day_minutes: 450,
  lunch_minutes: 30,
  rounding_minutes: 30,
}

function makeDate(hours: number, minutes: number): Date {
  const d = new Date(2026, 3, 17, hours, minutes, 0, 0)
  return d
}

describe('calculateDayWithLivePunch', () => {
  it('returns null when now is before punchStart', () => {
    const result = calculateDayWithLivePunch(
      makeDate(9, 0),
      '10:00',
      [],
      false,
      [],
      defaultSettings,
    )
    expect(result).toBeNull()
  })

  it('calculates simple case: punch 10:00, now 14:00, lunch on, no deductions', () => {
    const result = calculateDayWithLivePunch(
      makeDate(14, 0),
      '10:00',
      [],
      true,
      [],
      defaultSettings,
    )
    expect(result).not.toBeNull()
    expect(result!.grossMinutes).toBe(240)
    expect(result!.lunchMinutes).toBe(30)
    expect(result!.deductionMinutes).toBe(0)
    expect(result!.netMinutes).toBe(210)
    expect(result!.reportedMinutes).toBe(210)
  })

  it('calculates without lunch', () => {
    const result = calculateDayWithLivePunch(
      makeDate(14, 0),
      '10:00',
      [],
      false,
      [],
      defaultSettings,
    )
    expect(result!.grossMinutes).toBe(240)
    expect(result!.lunchMinutes).toBe(0)
    expect(result!.netMinutes).toBe(240)
    expect(result!.reportedMinutes).toBe(240)
  })

  it('rounds down net to rounding interval', () => {
    const result = calculateDayWithLivePunch(
      makeDate(14, 20),
      '10:00',
      [],
      true,
      [],
      defaultSettings,
    )
    // gross=260, lunch=30, net=230, reported=floor(230/30)*30=210
    expect(result!.netMinutes).toBe(230)
    expect(result!.reportedMinutes).toBe(210)
  })

  it('includes deductions', () => {
    const result = calculateDayWithLivePunch(
      makeDate(14, 0),
      '10:00',
      [],
      true,
      [
        { id: 1, day_id: 1, name: 'Errand', minutes: 15 },
      ],
      defaultSettings,
    )
    // gross=240, lunch=30, deductions=15, net=195, reported=floor(195/30)*30=180
    expect(result!.grossMinutes).toBe(240)
    expect(result!.deductionMinutes).toBe(15)
    expect(result!.netMinutes).toBe(195)
    expect(result!.reportedMinutes).toBe(180)
  })

  it('includes completed sessions from earlier in the day', () => {
    const result = calculateDayWithLivePunch(
      makeDate(16, 0),
      '14:00',
      [
        { id: 1, day_id: 1, start_time: '08:00', end_time: '12:00', sort_order: 0, is_internal: false },
      ],
      true,
      [],
      defaultSettings,
    )
    // completed: 240min, current: 120min, gross=360, lunch=30, net=330, reported=330
    expect(result!.grossMinutes).toBe(360)
    expect(result!.netMinutes).toBe(330)
    expect(result!.reportedMinutes).toBe(330)
  })

  it('clamps net to zero when deductions exceed gross', () => {
    const result = calculateDayWithLivePunch(
      makeDate(10, 15),
      '10:00',
      [],
      true,
      [],
      defaultSettings,
    )
    // gross=15, lunch=30, net=max(15-30,0)=0, reported=0
    expect(result!.netMinutes).toBe(0)
    expect(result!.reportedMinutes).toBe(0)
  })

  it('handles custom rounding interval', () => {
    const settings: WorkSettings = { ...defaultSettings, rounding_minutes: 15 }
    const result = calculateDayWithLivePunch(
      makeDate(14, 20),
      '10:00',
      [],
      true,
      [],
      settings,
    )
    // gross=260, lunch=30, net=230, reported=floor(230/15)*15=225
    expect(result!.reportedMinutes).toBe(225)
  })

  it('reports standardMinutes from settings', () => {
    const result = calculateDayWithLivePunch(
      makeDate(14, 0),
      '10:00',
      [],
      false,
      [],
      defaultSettings,
    )
    expect(result!.standardMinutes).toBe(450)
  })
})
