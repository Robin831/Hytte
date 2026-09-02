import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest'
import {
  calculateDayWithLivePunch,
  daysSinceLocalDate,
  isPunchStale,
  sessionMinutes,
  type WorkSettings,
} from './workHoursUtils'

// CI runs under UTC, where local and UTC midnights coincide and the deliberate
// local/UTC mix in daysSinceLocalDate collapses into a no-op. Pin a DST-observing
// zone so the "a DST transition can't skew the count" claim is actually exercised.
// Node re-reads process.env.TZ on assignment (v16+), so stubbing it here takes
// effect even though dates have already been constructed by then — but that is a
// runtime detail, so the suite below proves the pin instead of assuming it.
beforeAll(() => {
  vi.stubEnv('TZ', 'Europe/Oslo')
})

afterAll(() => {
  vi.unstubAllEnvs()
})

describe('timezone pinning', () => {
  it('really runs in the DST-observing zone the DST cases below assume', () => {
    // Without this the spring-forward cases would pass under UTC for the wrong
    // reason — UTC has no transition to mishandle. Fail loudly instead.
    expect(new Date(2026, 6, 1).getTimezoneOffset()).toBe(-120) // CEST, summer
    expect(new Date(2026, 0, 1).getTimezoneOffset()).toBe(-60) // CET, winter
  })
})

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
      null,
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
      null,
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
      null,
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
      null,
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
      null,
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
        { id: 1, day_id: 1, start_time: '08:00', end_time: '12:00', sort_order: 0, is_internal: false, crosses_midnight: false },
      ],
      true,
      [],
      defaultSettings,
      null,
    )
    // completed: 240min, current: 120min, gross=360, lunch=30, net=330, reported=330
    expect(result!.grossMinutes).toBe(360)
    expect(result!.netMinutes).toBe(330)
    expect(result!.reportedMinutes).toBe(330)
  })

  it('clamps net to zero when lunch exceeds gross', () => {
    const result = calculateDayWithLivePunch(
      makeDate(10, 15),
      '10:00',
      [],
      true,
      [],
      defaultSettings,
      null,
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
      null,
    )
    // gross=260, lunch=30, net=230, reported=floor(230/15)*15=225
    expect(result!.reportedMinutes).toBe(225)
  })

  it('ignores completed sessions with end_time at or before start_time', () => {
    const result = calculateDayWithLivePunch(
      makeDate(16, 0),
      '14:00',
      [
        { id: 1, day_id: 1, start_time: '12:00', end_time: '11:00', sort_order: 0, is_internal: false, crosses_midnight: false },
      ],
      true,
      [],
      defaultSettings,
      null,
    )
    // invalid completed session contributes 0min, current=120min, gross=120, lunch=30, net=90, reported=90
    expect(result!.grossMinutes).toBe(120)
    expect(result!.netMinutes).toBe(90)
    expect(result!.reportedMinutes).toBe(90)
  })

  it('reports standardMinutes from settings', () => {
    const result = calculateDayWithLivePunch(
      makeDate(14, 0),
      '10:00',
      [],
      false,
      [],
      defaultSettings,
      null,
    )
    expect(result!.standardMinutes).toBe(450)
  })
})

describe('calculateDayWithLivePunch across midnight', () => {
  // makeDate pins the clock to 2026-04-17, so a punch on 2026-04-16 is "yesterday".
  it('keeps counting an open session that started the previous evening', () => {
    const result = calculateDayWithLivePunch(
      makeDate(1, 0),
      '22:00',
      [],
      false,
      [],
      defaultSettings,
      '2026-04-16',
    )
    expect(result).not.toBeNull()
    // 22:00 -> 01:00 the next day is 3h
    expect(result!.grossMinutes).toBe(180)
    expect(result!.netMinutes).toBe(180)
  })

  it('subtracts lunch and deductions from a wrapped in-progress session', () => {
    const result = calculateDayWithLivePunch(
      makeDate(2, 30),
      '22:00',
      [],
      true,
      [{ id: 1, day_id: 1, name: 'Break', minutes: 15 }],
      defaultSettings,
      '2026-04-16',
    )
    // gross=270, lunch=30, deductions=15, net=225, reported=floor(225/30)*30=210
    expect(result!.grossMinutes).toBe(270)
    expect(result!.netMinutes).toBe(225)
    expect(result!.reportedMinutes).toBe(210)
  })

  it('does not add a day when the clock has already passed the start time', () => {
    const result = calculateDayWithLivePunch(
      makeDate(10, 0),
      '08:00',
      [],
      false,
      [],
      defaultSettings,
      '2026-04-16',
    )
    // Punching out now records 08:00 -> 10:00 as a non-wrapping 2h session
    // (endTime > punchStart), so the estimate must say the same thing.
    expect(result!.grossMinutes).toBe(120)
  })

  it('never adds more than the single day the punch-out could record', () => {
    // Punched in just after midnight yesterday, still open late today: the
    // clock has not wrapped relative to the start, so this is a same-clock
    // measurement, not 47h of running total.
    const result = calculateDayWithLivePunch(
      makeDate(23, 59),
      '00:05',
      [],
      false,
      [],
      defaultSettings,
      '2026-04-16',
    )
    expect(result!.grossMinutes).toBe(23 * 60 + 59 - 5)
  })

  it('reads zero at exactly the start time, whichever day the punch-in is dated', () => {
    // The wrap needs a strictly negative difference: handlePunchOut refuses an
    // end time equal to the start as a zero-length punch, so at equality there
    // is no recorded session for the estimate to disagree with. One minute
    // either side the two line up exactly.
    expect(
      calculateDayWithLivePunch(makeDate(9, 0), '09:00', [], false, [], defaultSettings, '2026-04-17')!.grossMinutes,
    ).toBe(0)
    expect(
      calculateDayWithLivePunch(makeDate(9, 0), '09:00', [], false, [], defaultSettings, '2026-04-16')!.grossMinutes,
    ).toBe(0)
    expect(
      calculateDayWithLivePunch(makeDate(9, 0), '09:00', [], false, [], defaultSettings, null)!.grossMinutes,
    ).toBe(0)
    // 09:01 punches out as a plain 1-minute session...
    expect(
      calculateDayWithLivePunch(makeDate(9, 1), '09:00', [], false, [], defaultSettings, '2026-04-16')!.grossMinutes,
    ).toBe(1)
    // ...and 08:59 as a wrapped one, one minute short of the full day.
    expect(
      calculateDayWithLivePunch(makeDate(8, 59), '09:00', [], false, [], defaultSettings, '2026-04-16')!.grossMinutes,
    ).toBe(24 * 60 - 1)
  })

  it('gives up on a punch-in older than the allowed wrap instead of counting the nights', () => {
    // Two calendar days back: forgotten, not a running shift.
    expect(
      calculateDayWithLivePunch(makeDate(10, 0), '08:00', [], false, [], defaultSettings, '2026-04-15'),
    ).toBeNull()
    expect(
      calculateDayWithLivePunch(makeDate(10, 0), '08:00', [], false, [], defaultSettings, '2026-01-02'),
    ).toBeNull()
  })

  it('still rejects a start later today than the current time', () => {
    const result = calculateDayWithLivePunch(
      makeDate(9, 0),
      '10:00',
      [],
      false,
      [],
      defaultSettings,
      '2026-04-17',
    )
    expect(result).toBeNull()
  })

  it('falls back to same-day behaviour when the punch date is malformed', () => {
    expect(calculateDayWithLivePunch(makeDate(9, 0), '10:00', [], false, [], defaultSettings, 'not-a-date')).toBeNull()
    expect(
      calculateDayWithLivePunch(makeDate(14, 0), '10:00', [], false, [], defaultSettings, '2026-02-31')!.grossMinutes,
    ).toBe(240)
  })

  it('adds completed sessions to a wrapped in-progress session', () => {
    const result = calculateDayWithLivePunch(
      makeDate(1, 0),
      '22:00',
      [
        { id: 1, day_id: 1, start_time: '08:00', end_time: '12:00', sort_order: 0, is_internal: false, crosses_midnight: false },
      ],
      false,
      [],
      defaultSettings,
      '2026-04-16',
    )
    expect(result!.grossMinutes).toBe(240 + 180)
  })
})

describe('daysSinceLocalDate', () => {
  it('counts whole calendar days back from the local date of now', () => {
    expect(daysSinceLocalDate('2026-04-17', makeDate(1, 0))).toBe(0)
    expect(daysSinceLocalDate('2026-04-16', makeDate(23, 0))).toBe(1)
    expect(daysSinceLocalDate('2026-04-10', makeDate(12, 0))).toBe(7)
  })

  it('returns a negative count for a date after today', () => {
    expect(daysSinceLocalDate('2026-04-18', makeDate(12, 0))).toBe(-1)
  })

  it('returns null for a malformed or non-existent date', () => {
    expect(daysSinceLocalDate('not-a-date', makeDate(12, 0))).toBeNull()
    expect(daysSinceLocalDate('2026-02-31', makeDate(12, 0))).toBeNull()
  })

  it('counts a spring-forward night as one whole day', () => {
    // Europe/Oslo springs forward on 2026-03-29, so local midnight on the 29th
    // and on the 30th are only 23h apart. A naive local-midnight subtraction
    // would floor that to 0 days.
    expect(daysSinceLocalDate('2026-03-29', new Date(2026, 2, 30, 12, 0, 0, 0))).toBe(1)
    expect(daysSinceLocalDate('2026-03-29', new Date(2026, 2, 30, 0, 30, 0, 0))).toBe(1)
    expect(daysSinceLocalDate('2026-03-28', new Date(2026, 2, 30, 12, 0, 0, 0))).toBe(2)
  })
})

describe('isPunchStale', () => {
  it('is false for a punch-in from today or last night', () => {
    expect(isPunchStale('2026-04-17', makeDate(12, 0))).toBe(false)
    expect(isPunchStale('2026-04-16', makeDate(1, 0))).toBe(false)
  })

  it('is true for a punch-in older than the allowed wrap', () => {
    expect(isPunchStale('2026-04-15', makeDate(10, 0))).toBe(true)
    expect(isPunchStale('2026-01-02', makeDate(10, 0))).toBe(true)
  })

  it('is false when there is no date to judge, or it is unparseable', () => {
    expect(isPunchStale(null, makeDate(10, 0))).toBe(false)
    expect(isPunchStale('not-a-date', makeDate(10, 0))).toBe(false)
  })

  it('agrees with the estimate it gates', () => {
    for (const date of ['2026-04-17', '2026-04-16', '2026-04-15', '2026-01-02']) {
      const stale = isPunchStale(date, makeDate(10, 0))
      const estimate = calculateDayWithLivePunch(makeDate(10, 0), '08:00', [], false, [], defaultSettings, date)
      expect(estimate === null).toBe(stale)
    }
  })
})

describe('calculateDayWithLivePunch across a DST transition', () => {
  it('still wraps a shift that started before a spring-forward midnight', () => {
    // Punched in 22:00 on 2026-03-29, still open at 01:00 on the 30th.
    const result = calculateDayWithLivePunch(
      new Date(2026, 2, 30, 1, 0, 0, 0),
      '22:00',
      [],
      false,
      [],
      defaultSettings,
      '2026-03-29',
    )
    expect(result!.grossMinutes).toBe(180)
  })
})

describe('sessionMinutes', () => {
  const base = { id: 1, day_id: 1, sort_order: 0, is_internal: false }

  it('measures an ordinary session', () => {
    expect(sessionMinutes({ ...base, start_time: '08:00', end_time: '16:00', crosses_midnight: false })).toBe(480)
  })

  it('adds a full day when the session crosses midnight', () => {
    expect(sessionMinutes({ ...base, start_time: '22:00', end_time: '02:00', crosses_midnight: true })).toBe(240)
  })

  it('handles a wrapped session that ends exactly at midnight', () => {
    expect(sessionMinutes({ ...base, start_time: '23:30', end_time: '00:00', crosses_midnight: true })).toBe(30)
  })

  it('returns zero for a wrapped range without the flag', () => {
    expect(sessionMinutes({ ...base, start_time: '22:00', end_time: '02:00', crosses_midnight: false })).toBe(0)
  })

  it('returns zero for a zero-length session', () => {
    expect(sessionMinutes({ ...base, start_time: '09:00', end_time: '09:00', crosses_midnight: false })).toBe(0)
  })

  it('returns null for malformed times', () => {
    expect(sessionMinutes({ ...base, start_time: 'nope', end_time: '16:00', crosses_midnight: false })).toBeNull()
    expect(sessionMinutes({ ...base, start_time: '08:00', end_time: '25:00', crosses_midnight: false })).toBeNull()
  })
})

describe('calculateDayWithLivePunch with wrapped sessions', () => {
  it('counts a completed wrapped session once', () => {
    const result = calculateDayWithLivePunch(
      makeDate(16, 0),
      '14:00',
      [
        { id: 1, day_id: 1, start_time: '22:00', end_time: '02:00', sort_order: 0, is_internal: false, crosses_midnight: true },
      ],
      false,
      [],
      defaultSettings,
      null,
    )
    // completed wrapped: 240min, current: 120min, gross=360
    expect(result!.grossMinutes).toBe(360)
    expect(result!.netMinutes).toBe(360)
  })
})
