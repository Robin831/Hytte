import { describe, it, expect } from 'vitest'
import {
  type Deadline, type RaceEvent,
  daysUntil, distanceKind, flag, formatFuzzyDate, intlLocale, nextDeadline, pickText, toLang,
} from './racesApi'

const prefix = (p: string, text: string) => `${p}:${text}`

function deadline(over: Partial<Deadline>): Deadline {
  return {
    id: 1, event_id: 1, kind: 'entry_closes', due_date: '2027-01-01', date_precision: 'day',
    due_time: '', tz: '', expected: false, texts: {}, ...over,
  }
}

describe('racesApi helpers', () => {
  it('maps i18next languages to catalog languages', () => {
    expect(toLang('nb-NO')).toBe('nb')
    expect(toLang('no')).toBe('nb')
    expect(toLang('th')).toBe('th')
    expect(toLang('en-US')).toBe('en')
    expect(toLang('de')).toBe('en')
    expect(toLang(undefined)).toBe('en')
  })

  it('falls back through en, nb, th when the viewer language is missing', () => {
    expect(pickText({ nb: 'norsk', en: 'english' }, 'th')).toBe('english')
    expect(pickText({ nb: 'norsk' }, 'en')).toBe('norsk')
    expect(pickText({ th: 'ไทย' }, 'nb')).toBe('ไทย')
    expect(pickText(undefined, 'nb')).toBeUndefined()
  })

  it('formats Thai dates in the Gregorian calendar, not the Buddhist era', () => {
    expect(intlLocale('th')).toContain('ca-gregory')
    const text = formatFuzzyDate('2027-03-07', 'day', 'th', prefix)
    expect(text).toContain('2027')
    expect(text).not.toContain('2570')
  })

  it('honors date precision', () => {
    expect(formatFuzzyDate('2027-09-15', 'month', 'en', prefix)).toBe('Sept 2027')
    expect(formatFuzzyDate('2027-09-15', 'mid', 'en', prefix)).toBe('mid:Sept 2027')
    expect(formatFuzzyDate('2026-12-20', 'approx', 'en', prefix)).toBe('approx:20 Dec 2026')
    expect(formatFuzzyDate('2027-03-07', 'day', 'en', prefix)).toBe('7 Mar 2027')
  })

  it('counts whole days to a date from local today', () => {
    const today = new Date(2026, 9, 9, 23, 30) // 9 Oct 2026, late evening
    expect(daysUntil('2026-10-09', today)).toBe(0)
    expect(daysUntil('2026-10-10', today)).toBe(1)
    expect(daysUntil('2026-10-19', today)).toBe(10)
    expect(daysUntil('2026-10-01', today)).toBe(-8)
  })

  it('picks the next deadline that has not passed, using exact times when known', () => {
    const now = new Date('2026-10-19T00:30:00Z').getTime()
    const event = {
      deadlines: [
        // Sydney lottery closed at 23:00 UTC on the 18th — already past at 00:30 UTC.
        deadline({ id: 1, kind: 'lottery_closes', due_date: '2026-10-19', due_time: '10:00', tz: 'Australia/Sydney', due_at: '2026-10-18T23:00:00Z' }),
        deadline({ id: 2, kind: 'lottery_results', due_date: '2026-11-03' }),
        deadline({ id: 3, kind: 'payment_due', due_date: '2026-12-15' }),
      ],
    } as RaceEvent
    expect(nextDeadline(event, now)?.id).toBe(2)
  })

  it('classifies distances and renders flags', () => {
    expect(distanceKind(21097)).toBe('half')
    expect(distanceKind(42195)).toBe('marathon')
    expect(distanceKind(10000)).toBe('other')
    expect(flag('NO')).toBe('🇳🇴')
    expect(flag('nope')).toBe('')
  })
})
