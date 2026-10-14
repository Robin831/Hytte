import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest'
import {
  applyRedemption,
  calculateDay,
  daysSinceLocalDate,
  isPunchStale,
  liveSegmentMinutes,
  parseHHMM,
  parseWorkSettings,
  DEFAULT_WORK_SETTINGS,
  sessionMinutes,
  type WorkDeduction,
  type WorkSession,
  type WorkSettings,
} from './calculateDay'

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

// Mirrors DefaultSettings() in internal/workhours/models.go.
const defaultSettings: WorkSettings = {
  standard_day_minutes: 450,
  lunch_minutes: 30,
  rounding_minutes: 30,
}

let nextId = 1
function session(start: string, end: string, crossesMidnight = false): WorkSession {
  const id = nextId++
  return { id, day_id: 1, start_time: start, end_time: end, sort_order: id, is_internal: false, crosses_midnight: crossesMidnight }
}

function deduction(name: string, minutes: number): WorkDeduction {
  return { id: nextId++, day_id: 1, name, minutes }
}

function makeDate(hours: number, minutes: number): Date {
  return new Date(2026, 3, 17, hours, minutes, 0, 0)
}

// One case per CalculateDay test in internal/workhours/calculator_test.go, with
// the same inputs and expected outputs.
describe('calculateDay (pinned to calculator_test.go)', () => {
  it('TestCalculateDay_BasicSession', () => {
    const sum = calculateDay([session('06:00', '15:00')], [], defaultSettings, { lunch: true })
    expect(sum.gross).toBe(540)
    expect(sum.lunch).toBe(30)
    expect(sum.net).toBe(510)
    expect(sum.reported).toBe(510)
    expect(sum.reported / 60).toBe(8.5)
    expect(sum.remainder).toBe(0)
  })

  it('TestCalculateDay_Remainder', () => {
    const sum = calculateDay([session('06:00', '14:07')], [], defaultSettings, { lunch: true })
    expect(sum.gross).toBe(487)
    expect(sum.net).toBe(457)
    expect(sum.reported).toBe(450)
    expect(sum.reported / 60).toBe(7.5)
    expect(sum.remainder).toBe(7)
  })

  it('TestCalculateDay_MultipleSessions (lunch off)', () => {
    const sum = calculateDay([session('06:00', '08:00'), session('08:45', '15:00')], [], defaultSettings, { lunch: false })
    expect(sum.gross).toBe(495)
    expect(sum.lunch).toBe(0)
    expect(sum.net).toBe(495)
    expect(sum.reported).toBe(480)
    expect(sum.remainder).toBe(15)
  })

  it('TestCalculateDay_WithCustomDeductions', () => {
    const sum = calculateDay([session('06:00', '15:00')], [deduction('Kindergarten', 15)], defaultSettings, { lunch: true })
    expect(sum.net).toBe(495)
    expect(sum.deductions).toBe(15)
  })

  it('TestCalculateDay_NoSessions', () => {
    const sum = calculateDay([], [], defaultSettings, { lunch: false })
    expect(sum.gross).toBe(0)
    expect(sum.net).toBe(0)
    expect(sum.reported).toBe(0)
  })

  it('TestCalculateDay_NetClampedToZero', () => {
    const sum = calculateDay([session('09:00', '09:20')], [], defaultSettings, { lunch: true })
    expect(sum.net).toBe(0)
  })

  it('TestCalculateDay_CrossesMidnightCountsOnStartDate', () => {
    const sum = calculateDay([session('22:00', '02:00', true)], [], defaultSettings)
    expect(sum.gross).toBe(240)
    expect(sum.net).toBe(240)
    expect(sum.reported).toBe(240)
    expect(sum.remainder).toBe(0)
  })

  it('TestCalculateDay_MixedWrappedAndNormalSessions', () => {
    const sum = calculateDay([session('09:00', '17:00'), session('22:00', '01:15', true)], [], defaultSettings, { lunch: true })
    expect(sum.gross).toBe(675)
    expect(sum.net).toBe(645)
    expect(sum.reported).toBe(630)
    expect(sum.remainder).toBe(15)
  })
})

describe('calculateDay balance and settings', () => {
  it('reports balance as reported minus the standard day', () => {
    // Same inputs as TestCalculateDay_Remainder: 450 reported vs 450 standard.
    expect(calculateDay([session('06:00', '14:07')], [], defaultSettings, { lunch: true }).balance).toBe(0)
    // TestCalculateDay_MultipleSessions: 480 reported → +30.
    expect(calculateDay([session('06:00', '08:00'), session('08:45', '15:00')], [], defaultSettings).balance).toBe(30)
    // An empty day is a full standard day short.
    expect(calculateDay([], [], defaultSettings).balance).toBe(-450)
  })

  it('defaults lunch to off, like the Go zero value', () => {
    const sum = calculateDay([session('06:00', '15:00')], [], defaultSettings)
    expect(sum.lunch).toBe(0)
    expect(sum.net).toBe(540)
  })

  it('falls back to a 30-minute rounding when the setting is not positive', () => {
    const sum = calculateDay([session('06:00', '14:07')], [], { ...defaultSettings, rounding_minutes: 0 }, { lunch: true })
    expect(sum.reported).toBe(450)
    expect(sum.remainder).toBe(7)
  })

  it('rounds down to a custom interval', () => {
    const sum = calculateDay([session('06:00', '14:07')], [], { ...defaultSettings, rounding_minutes: 15 }, { lunch: true })
    expect(sum.reported).toBe(450)
    expect(sum.remainder).toBe(7)
    const sum2 = calculateDay([session('06:00', '14:22')], [], { ...defaultSettings, rounding_minutes: 15 }, { lunch: true })
    expect(sum2.net).toBe(472)
    expect(sum2.reported).toBe(465)
    expect(sum2.remainder).toBe(7)
  })

  it('throws on a malformed session time, where Go returns an error', () => {
    expect(() => calculateDay([session('nope', '16:00')], [], defaultSettings)).toThrow()
  })
})

describe('applyRedemption (mirrors applyRedemptionToSummary)', () => {
  it('credits redeemed flex into reported and balance only', () => {
    // 08:00-15:00 = 420 gross, lunch 30 -> net 390, reported 390, balance -60.
    const calc = calculateDay([session('08:00', '15:00')], [], defaultSettings, { lunch: true })
    expect(calc.balance).toBe(-60)
    const credited = applyRedemption(calc, 60)
    expect(credited).toEqual({ ...calc, reported: 450, balance: 0 })
  })

  it('leaves the summary alone for zero or negative redemptions', () => {
    const calc = calculateDay([session('08:00', '15:00')], [], defaultSettings, { lunch: true })
    expect(applyRedemption(calc, 0)).toEqual(calc)
    expect(applyRedemption(calc, -30)).toEqual(calc)
  })
})

describe('calculateDay with an open punch-in', () => {
  it('adds a live segment from the punch start to now', () => {
    const sum = calculateDay([], [], defaultSettings, { lunch: true, openPunchStart: '10:00', now: makeDate(14, 0) })
    // gross=240, lunch=30, net=210, reported=210
    expect(sum.gross).toBe(240)
    expect(sum.lunch).toBe(30)
    expect(sum.net).toBe(210)
    expect(sum.reported).toBe(210)
    expect(sum.balance).toBe(210 - 450)
  })

  it('rounds the live total down and keeps the remainder', () => {
    const sum = calculateDay([], [], defaultSettings, { openPunchStart: '08:00', now: makeDate(10, 17) })
    expect(sum.net).toBe(137)
    expect(sum.reported).toBe(120)
    expect(sum.remainder).toBe(17)
  })

  it('adds completed sessions and deductions to the live segment', () => {
    const sum = calculateDay(
      [session('06:00', '10:00')],
      [deduction('Break', 15)],
      defaultSettings,
      { lunch: true, openPunchStart: '11:00', now: makeDate(14, 30) },
    )
    // gross=240+210=450, net=450-30-15=405, reported=390
    expect(sum.gross).toBe(450)
    expect(sum.deductions).toBe(15)
    expect(sum.net).toBe(405)
    expect(sum.reported).toBe(390)
    expect(sum.remainder).toBe(15)
  })

  it('counts a completed wrapped session once alongside the live segment', () => {
    const sum = calculateDay([session('22:00', '02:00', true)], [], defaultSettings, { openPunchStart: '14:00', now: makeDate(16, 0) })
    expect(sum.gross).toBe(360)
  })

  it('clamps a live segment that starts after now to zero', () => {
    const sum = calculateDay([session('06:00', '08:00')], [], defaultSettings, { openPunchStart: '10:00', now: makeDate(9, 0) })
    expect(sum.gross).toBe(120)
  })

  it('wraps past midnight when the punch-in is dated yesterday', () => {
    const sum = calculateDay([], [deduction('Break', 15)], defaultSettings, {
      lunch: true,
      openPunchStart: '22:00',
      openPunchDate: '2026-04-16',
      now: makeDate(2, 30),
    })
    // gross=270, lunch=30, deductions=15, net=225, reported=210
    expect(sum.gross).toBe(270)
    expect(sum.net).toBe(225)
    expect(sum.reported).toBe(210)
  })
})

describe('liveSegmentMinutes', () => {
  // makeDate pins the clock to 2026-04-17, so a punch on 2026-04-16 is "yesterday".
  it('measures a same-day segment', () => {
    expect(liveSegmentMinutes('10:00', makeDate(14, 0), null)).toBe(240)
  })

  it('returns null for a start later than now without a proven wrap', () => {
    expect(liveSegmentMinutes('10:00', makeDate(9, 0), null)).toBeNull()
    expect(liveSegmentMinutes('10:00', makeDate(9, 0), '2026-04-17')).toBeNull()
  })

  it('returns null for a malformed start', () => {
    expect(liveSegmentMinutes('nope', makeDate(9, 0), null)).toBeNull()
  })

  it('keeps counting an open session that started the previous evening', () => {
    expect(liveSegmentMinutes('22:00', makeDate(1, 0), '2026-04-16')).toBe(180)
  })

  it('does not add a day when the clock has already passed the start time', () => {
    expect(liveSegmentMinutes('08:00', makeDate(10, 0), '2026-04-16')).toBe(120)
  })

  it('never adds more than the single day the punch-out could record', () => {
    expect(liveSegmentMinutes('00:05', makeDate(23, 59), '2026-04-16')).toBe(23 * 60 + 59 - 5)
  })

  it('reads zero at exactly the start time, whichever day the punch-in is dated', () => {
    expect(liveSegmentMinutes('09:00', makeDate(9, 0), '2026-04-17')).toBe(0)
    expect(liveSegmentMinutes('09:00', makeDate(9, 0), '2026-04-16')).toBe(0)
    expect(liveSegmentMinutes('09:00', makeDate(9, 0), null)).toBe(0)
    // 09:01 punches out as a plain 1-minute session...
    expect(liveSegmentMinutes('09:00', makeDate(9, 1), '2026-04-16')).toBe(1)
    // ...and 08:59 as a wrapped one, one minute short of the full day.
    expect(liveSegmentMinutes('09:00', makeDate(8, 59), '2026-04-16')).toBe(24 * 60 - 1)
  })

  it('gives up on a punch-in older than the allowed wrap instead of counting the nights', () => {
    expect(liveSegmentMinutes('08:00', makeDate(10, 0), '2026-04-15')).toBeNull()
    expect(liveSegmentMinutes('08:00', makeDate(10, 0), '2026-01-02')).toBeNull()
  })

  it('falls back to same-day behaviour when the punch date is malformed', () => {
    expect(liveSegmentMinutes('10:00', makeDate(9, 0), 'not-a-date')).toBeNull()
    expect(liveSegmentMinutes('10:00', makeDate(14, 0), '2026-02-31')).toBe(240)
  })

  it('still wraps a shift that started before a spring-forward midnight', () => {
    // Punched in 22:00 on 2026-03-29, still open at 01:00 on the 30th.
    expect(liveSegmentMinutes('22:00', new Date(2026, 2, 30, 1, 0, 0, 0), '2026-03-29')).toBe(180)
  })
})

describe('parseHHMM (pinned to TestParseHHMM)', () => {
  it('parses valid times', () => {
    expect(parseHHMM('00:00')).toBe(0)
    expect(parseHHMM('06:00')).toBe(360)
    expect(parseHHMM('23:59')).toBe(1439)
    expect(parseHHMM('08:30')).toBe(510)
  })

  it('returns NaN where Go returns an error', () => {
    expect(parseHHMM('invalid')).toBeNaN()
    expect(parseHHMM('25:00')).toBeNaN()
    expect(parseHHMM('08:60')).toBeNaN()
    expect(parseHHMM(':30')).toBeNaN()
    expect(parseHHMM('08:')).toBeNaN()
    expect(parseHHMM('8.5:00')).toBeNaN()
    expect(parseHHMM('08:00:00')).toBeNaN()
  })
})

describe('sessionMinutes (pinned to TestSessionDurationMinutes)', () => {
  it('measures an ordinary session', () => {
    expect(sessionMinutes(session('08:00', '16:00'))).toBe(480)
  })

  it('adds a full day when the session crosses midnight', () => {
    expect(sessionMinutes(session('22:00', '02:00', true))).toBe(240)
  })

  it('handles a wrapped session that ends exactly at midnight', () => {
    expect(sessionMinutes(session('23:30', '00:00', true))).toBe(30)
  })

  it('returns zero for a wrapped range without the flag', () => {
    expect(sessionMinutes(session('22:00', '02:00'))).toBe(0)
  })

  it('returns zero for a zero-length session', () => {
    expect(sessionMinutes(session('09:00', '09:00'))).toBe(0)
  })

  it('returns null for malformed times', () => {
    expect(sessionMinutes(session('nope', '16:00'))).toBeNull()
    expect(sessionMinutes(session('08:00', '25:00'))).toBeNull()
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

  it('agrees with the live segment it gates', () => {
    for (const date of ['2026-04-17', '2026-04-16', '2026-04-15', '2026-01-02']) {
      const stale = isPunchStale(date, makeDate(10, 0))
      const segment = liveSegmentMinutes('08:00', makeDate(10, 0), date)
      expect(segment === null).toBe(stale)
    }
  })
})

// Mirrors loadSettings in internal/workhours/handlers.go: Atoi parsing,
// standard day and rounding > 0, lunch >= 0, anything else keeps the default.
describe('parseWorkSettings', () => {
  it('returns the defaults when no preference is set', () => {
    expect(parseWorkSettings({})).toEqual(DEFAULT_WORK_SETTINGS)
    expect(DEFAULT_WORK_SETTINGS).toEqual({ standard_day_minutes: 450, lunch_minutes: 30, rounding_minutes: 30 })
    expect(Object.isFrozen(DEFAULT_WORK_SETTINGS)).toBe(true)
  })

  it('uses valid stored values', () => {
    expect(parseWorkSettings({
      work_hours_standard_day: '480',
      work_hours_lunch_minutes: '45',
      work_hours_rounding: '15',
    })).toEqual({ standard_day_minutes: 480, lunch_minutes: 45, rounding_minutes: 15 })
  })

  it('accepts a zero lunch and a leading sign like Atoi', () => {
    expect(parseWorkSettings({ work_hours_lunch_minutes: '0' }).lunch_minutes).toBe(0)
    expect(parseWorkSettings({ work_hours_standard_day: '+420' }).standard_day_minutes).toBe(420)
  })

  it('rejects a negative lunch', () => {
    expect(parseWorkSettings({ work_hours_lunch_minutes: '-15' }).lunch_minutes).toBe(30)
  })

  it.each(['0', '-30'])('rejects a non-positive standard day and rounding (%s)', v => {
    const s = parseWorkSettings({ work_hours_standard_day: v, work_hours_rounding: v })
    expect(s.standard_day_minutes).toBe(450)
    expect(s.rounding_minutes).toBe(30)
  })

  it.each(['', 'abc', '45abc', '4.5', ' 45', '45 ', '1e2', '0x1f'])('rejects input Atoi rejects (%j)', v => {
    expect(parseWorkSettings({
      work_hours_standard_day: v,
      work_hours_lunch_minutes: v,
      work_hours_rounding: v,
    })).toEqual(DEFAULT_WORK_SETTINGS)
  })
})
