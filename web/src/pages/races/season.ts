// Pure logic for the season planner and the qualifying check, kept apart
// from the components so it can be tested directly.
import { type RaceEvent, type StrideRace, type Watch, type WatchState, distanceKind } from './racesApi'

export const SEASON_STATES: WatchState[] = ['registered', 'got_place', 'lottery_entered', 'planning']
const COMMITTED: WatchState[] = ['registered', 'got_place']

export type SpacingWarning = 'sameWeek' | 'marathonGap' | 'shortRecovery' | 'aRaceGap'

export interface SeasonItem {
  event: RaceEvent
  watch: Watch
  committed: boolean
  priority?: 'A' | 'B' | 'C'
  weeksSincePrevious?: number
  warnings: SpacingWarning[]
}

function daysBetween(a: string, b: string): number {
  return Math.round((Date.parse(b + 'T12:00:00Z') - Date.parse(a + 'T12:00:00Z')) / 86_400_000)
}

/**
 * Upcoming races the user is committed to or considering, in date order,
 * with the gap to the previous one and spacing warnings: two races in one
 * week; marathons under 8 weeks apart; a marathon and a half under 3 weeks
 * apart; two A-priority marathons under 12 weeks apart.
 */
export function buildSeason(events: RaceEvent[], watches: Watch[], strideRaces: StrideRace[], today: string): SeasonItem[] {
  const byId = new Map(events.map(e => [e.id, e]))
  const stride = new Map(strideRaces.map(r => [r.id, r]))
  const items: SeasonItem[] = watches
    .filter(w => SEASON_STATES.includes(w.state))
    .map(w => ({ w, e: byId.get(w.event_id) }))
    .filter((x): x is { w: Watch; e: RaceEvent } => !!x.e && x.e.race_date >= today)
    .sort((a, b) => a.e.race_date.localeCompare(b.e.race_date) || a.e.name.localeCompare(b.e.name))
    .map(({ w, e }) => ({
      event: e,
      watch: w,
      committed: COMMITTED.includes(w.state),
      priority: w.stride_race_id != null ? stride.get(w.stride_race_id)?.priority : undefined,
      warnings: [],
    }))

  items.forEach((item, i) => {
    if (i === 0) return
    const kind = distanceKind(item.event.distance_m)
    for (let j = i - 1; j >= 0; j--) {
      const prev = items[j]
      const gap = daysBetween(prev.event.race_date, item.event.race_date)
      if (j === i - 1) item.weeksSincePrevious = Math.floor(gap / 7)
      if (gap > 12 * 7) break
      const prevKind = distanceKind(prev.event.distance_m)
      const add = (w: SpacingWarning) => { if (!item.warnings.includes(w)) item.warnings.push(w) }
      if (gap < 7) add('sameWeek')
      else if (kind === 'marathon' && prevKind === 'marathon' && gap < 8 * 7) add('marathonGap')
      else if (kind !== prevKind && (kind === 'marathon' || prevKind === 'marathon') && gap < 3 * 7) add('shortRecovery')
      if (kind === 'marathon' && prevKind === 'marathon' && item.priority === 'A' && prev.priority === 'A' && gap < 12 * 7) add('aRaceGap')
    }
  })
  return items
}

export interface StandardGroup {
  sex: 'male' | 'female' | 'nonbinary'
  min_age: number
  max_age: number
  seconds: number
}

export interface QualifyingStandard {
  race: string
  name: string
  edition: number
  kind: 'qualifier' | 'guaranteed_entry'
  eligibility: string
  age_rule: string
  window: string
  source: string
  verified: boolean
  groups: StandardGroup[]
  /** Approximate race date, for age on race day (YYYY-MM-DD). */
  race_date?: string
}

export interface QualifyingResult {
  standard: QualifyingStandard
  group?: StandardGroup
  age?: number
  /** prediction − standard, in seconds: negative = inside the standard. */
  margin?: number
}

/**
 * Compares a predicted marathon time with each standard for the runner's
 * age group. Age is taken from the birth year at the edition's race date,
 * so it can be a year off around the birthday.
 */
export function checkQualifying(
  standards: QualifyingStandard[],
  birthYear: number | null,
  sex: 'male' | 'female' | null,
  predictedSeconds: number | null,
): QualifyingResult[] {
  return standards.map(standard => {
    if (!birthYear || !sex || standard.groups.length === 0) return { standard }
    const raceYear = Number((standard.race_date ?? String(standard.edition)).slice(0, 4))
    const age = raceYear - birthYear
    const group = standard.groups.find(g => g.sex === sex && age >= g.min_age && age <= g.max_age)
    if (!group) return { standard, age }
    return { standard, age, group, margin: predictedSeconds != null ? predictedSeconds - group.seconds : undefined }
  })
}

/** "+4:12" / "−1:05:30" for a margin in seconds. */
export function formatMargin(seconds: number): string {
  const sign = seconds > 0 ? '+' : '−'
  const abs = Math.abs(Math.round(seconds))
  const h = Math.floor(abs / 3600)
  const m = Math.floor((abs % 3600) / 60)
  const s = abs % 60
  const mm = String(m).padStart(h ? 2 : 1, '0')
  return `${sign}${h ? h + ':' : ''}${mm}:${String(s).padStart(2, '0')}`
}
