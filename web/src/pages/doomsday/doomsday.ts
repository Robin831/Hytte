// The Doomsday method (John Conway) for finding the weekday of any date in
// your head, with the "Odd + 11" way of finding a year's doomsday.
//
// Weekdays are numbered the way the method counts: Sunday = 0 … Saturday =
// 6 (Sunday is "Noneday"). The answer buttons are shown Monday first.

export type Weekday = 0 | 1 | 2 | 3 | 4 | 5 | 6

/** Monday-first order for the answer buttons. */
export const WEEK_MONDAY_FIRST: Weekday[] = [1, 2, 3, 4, 5, 6, 0]

export interface SimpleDate {
  y: number
  m: number // 1–12
  d: number
}

const mod7 = (n: number): Weekday => (((n % 7) + 7) % 7) as Weekday

export function isLeap(y: number): boolean {
  return (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0
}

export function daysInMonth(y: number, m: number): number {
  return [31, isLeap(y) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][m - 1]
}

/** The true weekday (the answer key), from the calendar itself. */
export function weekdayOf(date: SimpleDate): Weekday {
  return new Date(Date.UTC(date.y, date.m - 1, date.d)).getUTCDay() as Weekday
}

/**
 * Century anchors repeat every 400 years: 1600/2000 → Tuesday, 1700/2100 →
 * Sunday, 1800/2200 → Friday, 1900/2300 → Wednesday.
 */
export function centuryAnchor(y: number): Weekday {
  return ([2, 0, 5, 3] as Weekday[])[((Math.floor(y / 100) % 4) + 4) % 4]
}

/** The steps of "Odd + 11" for the last two digits of the year. */
export interface OddPlus11 {
  t: number // last two digits
  oddFirst: boolean // t was odd, so 11 was added
  afterFirst: number
  half: number
  oddSecond: boolean // the half was odd, so 11 was added
  afterSecond: number
  mod: number // afterSecond mod 7
  forward: number // 7 − mod (mod 7): days to count forward from the anchor
}

export function oddPlus11(y: number): OddPlus11 {
  const t = ((y % 100) + 100) % 100
  const oddFirst = t % 2 === 1
  const afterFirst = oddFirst ? t + 11 : t
  const half = afterFirst / 2
  const oddSecond = half % 2 === 1
  const afterSecond = oddSecond ? half + 11 : half
  const mod = afterSecond % 7
  return { t, oddFirst, afterFirst, half, oddSecond, afterSecond, mod, forward: (7 - mod) % 7 }
}

/** The year's doomsday: the anchor counted forward by Odd + 11. */
export function yearDoomsday(y: number): Weekday {
  return mod7(centuryAnchor(y) + oddPlus11(y).forward)
}

/**
 * A date in each month that always falls on the year's doomsday:
 * 3/1 (4/1 in leap years), 28/2 (29/2), 14/3 (pi day), 4/4, 9/5, 6/6,
 * 11/7, 8/8, 5/9, 10/10, 7/11, 12/12.
 */
export function monthDoomsday(m: number, leap: boolean): number {
  return [leap ? 4 : 3, leap ? 29 : 28, 14, 4, 9, 6, 11, 8, 5, 10, 7, 12][m - 1]
}

/** The full worked solution for a date. */
export interface Solution {
  date: SimpleDate
  leap: boolean
  anchor: Weekday
  odd: OddPlus11
  yearDay: Weekday
  monthDay: number // the doomsday date in the month
  diff: number // date − doomsday date
  diffMod: number // diff reduced mod 7 (0–6)
  answer: Weekday
}

export function solve(date: SimpleDate): Solution {
  const leap = isLeap(date.y)
  const anchor = centuryAnchor(date.y)
  const odd = oddPlus11(date.y)
  const yearDay = mod7(anchor + odd.forward)
  const monthDay = monthDoomsday(date.m, leap)
  const diff = date.d - monthDay
  const diffMod = mod7(diff)
  return { date, leap, anchor, odd, yearDay, monthDay, diff, diffMod, answer: mod7(yearDay + diff) }
}

// --- random dates ------------------------------------------------------------

export type Level = 1 | 2 | 3 | 4 | 5

/** Years each level draws from. */
export const LEVEL_YEARS: Record<Level, [number, number]> = {
  1: [1950, 2049],
  2: [1900, 2099],
  3: [2000, 2030],
  4: [1900, 2099],
  5: [1600, 2299],
}

export function randomDate(level: Level, rand: () => number = Math.random): SimpleDate {
  const [lo, hi] = LEVEL_YEARS[level]
  const y = lo + Math.floor(rand() * (hi - lo + 1))
  const m = 1 + Math.floor(rand() * 12)
  const d = 1 + Math.floor(rand() * daysInMonth(y, m))
  return { y, m, d }
}

// --- mistake diagnosis ---------------------------------------------------------

export type Slip =
  | 'leap' // used the non-leap (or leap) January/February doomsday
  | 'anchor' // used a neighbouring century's anchor
  | 'firstEleven' // forgot (or wrongly added) the first +11
  | 'secondEleven' // forgot (or wrongly added) the second +11
  | 'noSubtract' // counted forward by the mod instead of 7 − mod
  | 'month' // used a neighbouring month's doomsday date
  | 'offByOne' // counted one day too far or too short
  | 'unknown'

/**
 * Guesses which step went wrong: replays the method with one typical slip
 * at a time and returns the first that produces the wrong answer given.
 */
export function diagnose(date: SimpleDate, guess: Weekday): Slip | null {
  const s = solve(date)
  if (guess === s.answer) return null
  const finish = (yearDay: number, monthDay: number) => mod7(yearDay + date.d - monthDay)

  // Leap-year slip in January/February.
  if (date.m <= 2 && finish(s.yearDay, monthDoomsday(date.m, !s.leap)) === guess) return 'leap'
  // Wrong century anchor.
  for (const c of [-100, 100]) {
    if (finish(mod7(centuryAnchor(date.y + c) + s.odd.forward), s.monthDay) === guess) return 'anchor'
  }
  // Odd + 11 slips.
  const replay = (flipFirst: boolean, flipSecond: boolean, noSubtract: boolean) => {
    const t = s.odd.t
    const first = (s.odd.oddFirst !== flipFirst) ? t + 11 : t
    if (first % 2 !== 0) return null // halving an odd number: not a plausible path
    const half = first / 2
    const second = ((half % 2 === 1) !== flipSecond) ? half + 11 : half
    const m = second % 7
    const forward = noSubtract ? m : (7 - m) % 7
    return finish(mod7(s.anchor + forward), s.monthDay)
  }
  if (replay(true, false, false) === guess) return 'firstEleven'
  if (replay(false, true, false) === guess) return 'secondEleven'
  if (replay(false, false, true) === guess) return 'noSubtract'
  // A neighbouring month's doomsday date.
  for (const m of [date.m - 1, date.m + 1]) {
    if (m >= 1 && m <= 12 && finish(s.yearDay, monthDoomsday(m, s.leap)) === guess) return 'month'
  }
  if (mod7(s.answer + 1) === guess || mod7(s.answer - 1) === guess) return 'offByOne'
  return 'unknown'
}

/** Formats a date as YYYY-MM-DD (what the server stores). */
export function isoDate(d: SimpleDate): string {
  return `${String(d.y).padStart(4, '0')}-${String(d.m).padStart(2, '0')}-${String(d.d).padStart(2, '0')}`
}

/** Parses YYYY-MM-DD, or null. */
export function parseIso(s: string): SimpleDate | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s.trim())
  if (!m) return null
  const date = { y: Number(m[1]), m: Number(m[2]), d: Number(m[3]) }
  if (date.m < 1 || date.m > 12 || date.d < 1 || date.d > daysInMonth(date.y, date.m)) return null
  return date
}
