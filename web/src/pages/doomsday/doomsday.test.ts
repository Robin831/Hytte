import { describe, it, expect } from 'vitest'
import {
  centuryAnchor, diagnose, isLeap, monthDoomsday, oddPlus11, parseIso, randomDate, solve, weekdayOf, yearDoomsday,
  type SimpleDate, type Weekday,
} from './doomsday'

describe('doomsday method', () => {
  it('has the right century anchors', () => {
    expect([1600, 1700, 1800, 1900, 2000, 2100, 2200].map(centuryAnchor)).toEqual([2, 0, 5, 3, 2, 0, 5])
  })

  it('walks Odd + 11 for 2026 (doomsday Saturday)', () => {
    const o = oddPlus11(2026)
    expect(o).toMatchObject({ t: 26, oddFirst: false, half: 13, oddSecond: true, afterSecond: 24, mod: 3, forward: 4 })
    expect(yearDoomsday(2026)).toBe(6)
  })

  it('knows leap years and month doomsdays', () => {
    expect([1900, 2000, 2024, 2026].map(isLeap)).toEqual([false, true, true, false])
    expect(monthDoomsday(1, false)).toBe(3)
    expect(monthDoomsday(2, true)).toBe(29)
    expect(monthDoomsday(9, false)).toBe(5)
  })

  it('agrees with the calendar for every day from 1600 to 2299', () => {
    for (let y = 1600; y <= 2299; y++) {
      for (let m = 1; m <= 12; m++) {
        for (const d of [1, 13, 28]) {
          const date = { y, m, d }
          expect(solve(date).answer, `${y}-${m}-${d}`).toBe(weekdayOf(date))
        }
      }
    }
  })

  it('solves famous dates', () => {
    expect(solve({ y: 1814, m: 5, d: 17 }).answer).toBe(2) // Tuesday
    expect(solve({ y: 1969, m: 7, d: 20 }).answer).toBe(0) // Sunday
    expect(solve({ y: 2026, m: 12, d: 24 }).answer).toBe(4) // Thursday
  })
})

describe('diagnose', () => {
  it('spots a leap-year slip in January', () => {
    const date: SimpleDate = { y: 2024, m: 1, d: 10 }
    const right = solve(date).answer
    // Using 3 Jan instead of 4 Jan in a leap year lands one day later.
    expect(diagnose(date, ((right + 1) % 7) as Weekday)).toBe('leap')
    expect(diagnose(date, right)).toBeNull()
  })

  it('spots a wrong century anchor', () => {
    const date: SimpleDate = { y: 1987, m: 6, d: 6 }
    const s = solve(date)
    const wrong = ((centuryAnchor(2087) + s.odd.forward) % 7) as Weekday
    expect(diagnose(date, wrong)).toBe('anchor')
  })

  it('falls back to off-by-one or unknown', () => {
    const date: SimpleDate = { y: 2026, m: 8, d: 8 } // Saturday, on a doomsday
    expect(['offByOne', 'month', 'noSubtract', 'secondEleven', 'firstEleven', 'anchor']).toContain(diagnose(date, 0))
  })
})

describe('helpers', () => {
  it('draws valid dates in range', () => {
    let seed = 1
    const rand = () => (seed = (seed * 16807) % 2147483647) / 2147483647
    for (let i = 0; i < 500; i++) {
      const d = randomDate(5, rand)
      expect(d.y).toBeGreaterThanOrEqual(1600)
      expect(d.y).toBeLessThanOrEqual(2299)
      expect(parseIso(`${d.y}-${String(d.m).padStart(2, '0')}-${String(d.d).padStart(2, '0')}`)).toEqual(d)
    }
    expect(parseIso('2023-02-29')).toBeNull()
  })
})
