import { describe, it, expect } from 'vitest'
import { buildSeason, checkQualifying, formatMargin, type QualifyingStandard } from './season'
import { formatDuration, parseDuration, type RaceEvent, type StrideRace, type Watch, type WatchState } from './racesApi'
import { QUALIFYING_STANDARDS } from './qualifying'

function race(id: number, date: string, distance_m: number): RaceEvent {
  return {
    id, slug: `r${id}`, name: `Race ${id}`, edition_year: 2027, race_date: date, date_precision: 'day', country: 'NO',
    distance_m, status: 'open', entry_type: 'fcfs', travel: '', url: '', series: [], texts: {},
    scope: 'away', distances: [], place: '', lat: null, lng: null, source: '', source_id: '',
    checked_at: '', created_at: '', updated_at: '', deadlines: [],
  }
}
function watch(event_id: number, state: WatchState, stride_race_id: number | null = null): Watch {
  return { event_id, state, notes: '', stride_race_id, created_at: '', updated_at: '' }
}

describe('buildSeason', () => {
  const events = [
    race(1, '2027-04-11', 42195), // marathon
    race(2, '2027-05-02', 21097), // half 3 weeks later: fine
    race(3, '2027-05-30', 42195), // marathon 7 weeks after #1: marathonGap
    race(4, '2027-06-03', 21097), // 4 days later: sameWeek
    race(5, '2027-03-01', 42195), // past: left out
    race(6, '2027-09-26', 42195), // skipped: left out
    race(7, '2027-08-15', 42195), // A-marathon 11 weeks after #3 (also A)
  ]
  const watches = [
    watch(1, 'registered'), watch(2, 'planning'), watch(3, 'got_place', 30), watch(4, 'lottery_entered'),
    watch(5, 'registered'), watch(6, 'skipped'), watch(7, 'registered', 70),
  ]
  const stride: StrideRace[] = [
    { id: 30, name: 'Race 3', date: '2027-05-30', distance_m: 42195, target_time: null, priority: 'A' },
    { id: 70, name: 'Race 7', date: '2027-08-15', distance_m: 42195, target_time: null, priority: 'A' },
  ]

  it('orders upcoming season races with gaps, priorities and spacing warnings', () => {
    const items = buildSeason(events, watches, stride, '2027-04-01')
    expect(items.map(i => i.event.id)).toEqual([1, 2, 3, 4, 7])
    expect(items.map(i => i.committed)).toEqual([true, false, true, false, true])
    expect(items[1].weeksSincePrevious).toBe(3)
    expect(items[1].warnings).toEqual([])
    expect(items[2].warnings).toEqual(['marathonGap'])
    expect(items[2].priority).toBe('A')
    expect(items[3].warnings).toContain('sameWeek')
    expect(items[4].warnings).toEqual(['aRaceGap'])
  })

  it('warns about a marathon and a half under three weeks apart', () => {
    const items = buildSeason([race(1, '2027-04-11', 42195), race(2, '2027-04-25', 21097)],
      [watch(1, 'registered'), watch(2, 'registered')], [], '2027-01-01')
    expect(items[1].warnings).toEqual(['shortRecovery'])
  })
})

describe('checkQualifying', () => {
  const boston = QUALIFYING_STANDARDS.find(s => s.race === 'boston') as QualifyingStandard
  const london = QUALIFYING_STANDARDS.find(s => s.race === 'london') as QualifyingStandard

  it('picks the age group by age in the edition year and computes the margin', () => {
    // Born 1990 → 37 at Boston 2027 → men 35–39: 3:00:00.
    const [r] = checkQualifying([boston], 1990, 'male', 3 * 3600 - 5 * 60)
    expect(r.age).toBe(37)
    expect(r.group?.seconds).toBe(3 * 3600)
    expect(r.margin).toBe(-300)
    expect(formatMargin(r.margin!)).toBe('−5:00')
  })

  it('has standards for women and stays empty without a profile', () => {
    const [r] = checkQualifying([boston], 1995, 'female', null)
    expect(r.group?.seconds).toBe(3 * 3600 + 25 * 60) // women 18–34: 3:25
    expect(r.margin).toBeUndefined()
    expect(checkQualifying([london], null, 'male', 10000)[0].group).toBeUndefined()
  })

  it('ships verified, sourced standards for the big marathons', () => {
    for (const race of ['boston', 'berlin', 'chicago', 'nyc', 'sydney']) {
      const s = QUALIFYING_STANDARDS.find(x => x.race === race)
      expect(s?.verified, race).toBe(true)
      expect(s?.source, race).toMatch(/^https:\/\//)
      expect(s?.groups.length, race).toBeGreaterThan(0)
      expect(s?.note.nb && s?.note.en && s?.note.th, race).toBeTruthy()
    }
  })
})

describe('durations', () => {
  it('parses and formats race times', () => {
    expect(parseDuration('3:15:00')).toBe(11700)
    expect(parseDuration('1:45')).toBe(105) // M:SS
    expect(parseDuration(' 2:59:59 ')).toBe(10799)
    expect(parseDuration('')).toBeNull()
    expect(parseDuration('3:75:00')).toBeNull()
    expect(parseDuration('abc')).toBeNull()
    expect(formatDuration(11700)).toBe('3:15:00')
    expect(formatMargin(372)).toBe('+6:12')
    expect(formatMargin(-3725)).toBe('−1:02:05')
  })
})
