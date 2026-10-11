import { getJSON, source } from './useWidgetData'
import { toLocalDateString } from '../../utils/formatDate'
import { type TripSummary, fetchTrips, tripState } from '../../pages/trips/tripsApi'
import { type Deadline, type RaceEvent, type Watch, type WatchState, daysUntil, fetchRaces } from '../../pages/races/racesApi'
import type { DayPlan } from '../../types/stride'
import type { CalendarEvent } from '../calendar/types'
import { endOfDay, startOfDay } from '../calendar/types'

// The data behind the dashboard's widgets. Each source is fetched once and
// shared by every widget that shows it, the "next up" list included.

// --- trips --------------------------------------------------------------

export const tripsSource = source('trips', () => fetchTrips().then((d) => d.trips ?? []))

/** Ongoing trips first, then upcoming ones by start date; past trips are dropped. */
export function upcomingTrips(trips: TripSummary[], now: Date): TripSummary[] {
  return trips.filter((t) => tripState(t, now) !== 'after').sort((a, b) => a.start_date.localeCompare(b.start_date))
}

// --- races --------------------------------------------------------------

export const racesSource = source('races', () => fetchRaces())

/** Watch states that mean the race is still ahead of you. */
const ACTIVE: WatchState[] = ['registered', 'got_place', 'lottery_entered', 'planning', 'watching']
const RUNNING: WatchState[] = ['registered', 'got_place']

export interface WatchedRace {
  event: RaceEvent
  state: WatchState
  /** Entered and going, as opposed to watching or planning. */
  running: boolean
}

export interface RaceDeadline {
  event: RaceEvent
  deadline: Deadline
}

/** The races still ahead that are on the user's list, and their open deadlines, soonest first. */
export function watchedRaces(data: { events?: RaceEvent[] | null; watches?: Watch[] | null }): {
  races: WatchedRace[]
  deadlines: RaceDeadline[]
} {
  const stateOf = new Map((data.watches ?? []).map((w) => [w.event_id, w.state]))
  const races = (data.events ?? [])
    .flatMap((event) => {
      const state = stateOf.get(event.id)
      return state && ACTIVE.includes(state) && daysUntil(event.race_date) >= 0
        ? [{ event, state, running: RUNNING.includes(state) }]
        : []
    })
    .sort((a, b) => a.event.race_date.localeCompare(b.event.race_date))
  const deadlines = races
    .flatMap((r) => (r.event.deadlines ?? []).map((deadline) => ({ event: r.event, deadline })))
    .filter((d) => daysUntil(d.deadline.due_date) >= 0)
    .sort((a, b) => a.deadline.due_date.localeCompare(b.deadline.due_date))
  return { races, deadlines }
}

// --- Stride -------------------------------------------------------------

export interface StridePlan {
  id: number
  week_start: string
  week_end: string
  phase: string
  plan: DayPlan[] | null
}

/** A week without a plan answers 404, which is "nothing to show", not a failure. */
export const strideSource = source('stride', async (): Promise<StridePlan | null> => {
  const res = await fetch('/api/stride/plans/current', { credentials: 'include' })
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`stride plan: ${res.status}`)
  const data = (await res.json()) as { plan: StridePlan | null }
  return data.plan
})

/** Today's planned session, when it is not a rest day. */
export function todaysSession(plan: StridePlan | null) {
  const day = (plan?.plan ?? []).find((d) => d.date === toLocalDateString())
  return day && !day.rest_day ? (day.session ?? null) : null
}

// --- Wordfeud -----------------------------------------------------------

export interface WordfeudGame {
  id: number
  opponent: string
  scores: [number, number]
  is_my_turn: boolean
}

/** Asks Wordfeud's servers live, so it can be slow or fail without it meaning much. */
export const wordfeudSource = source('wordfeud', () =>
  getJSON<{ games: WordfeudGame[] | null }>('/api/wordfeud/games').then((d) => d.games ?? []),
)

// --- family chat --------------------------------------------------------

export interface Conversation {
  id: number
  name: string
  last_message_at: string
  unread_count: number
  last_message_preview: string
}

export const conversationsSource = source('familychat', () =>
  getJSON<{ conversations: Conversation[] | null }>('/api/familychat/conversations').then((d) => d.conversations ?? []),
)

// --- chores and stars ---------------------------------------------------

export interface Chore {
  id: number
  name: string
  /** Absent = not done yet, "pending" = waiting for a parent, "approved" = done. */
  completion_status?: string | null
}

/** Still to do: never started, sent back by a parent, or waiting for a teammate. */
export const isOpenChore = (c: Chore) =>
  !c.completion_status || c.completion_status === 'rejected' || c.completion_status === 'waiting_for_team'

export const myChoresSource = source('chores', () =>
  getJSON<{ chores: Chore[] | null }>('/api/allowance/my/chores').then((d) => d.chores ?? []),
)

/** How many completed chores wait for this parent's approval. */
export const pendingApprovalsSource = source('approvals', () =>
  getJSON<{ pending: unknown[] | null }>('/api/allowance/pending').then((d) => (d.pending ?? []).length),
)

export interface Stars {
  balance: number
  title: string
  streak: number
}

export const starsSource = source('stars', async (): Promise<Stars> => {
  const [balance, streaks] = await Promise.all([
    getJSON<{ current_balance: number; title: string }>('/api/stars/balance'),
    // The streak is a bonus line: the balance still shows without it.
    getJSON<{ daily_workout?: { current_count: number } }>('/api/stars/streaks').catch(() => null),
  ])
  return { balance: balance.current_balance, title: balance.title, streak: streaks?.daily_workout?.current_count ?? 0 }
})

// --- work hours ---------------------------------------------------------

export interface WorkHours {
  session: { date: string; start_time: string } | null
  summary: { net_minutes: number; standard_minutes: number } | null
}

export const workHoursSource = source('workhours', async (): Promise<WorkHours> => {
  const [punch, day] = await Promise.all([
    getJSON<{ session: WorkHours['session'] }>('/api/workhours/punch-session'),
    getJSON<{ summary: WorkHours['summary'] }>(`/api/workhours/day?date=${toLocalDateString()}`),
  ])
  return { session: punch.session ?? null, summary: day.summary ?? null }
})

// --- calendar -----------------------------------------------------------

const rfc3339 = (d: Date) => d.toISOString().replace(/\.\d{3}Z$/, 'Z')

/** Today's calendar events. */
export const todaysEventsSource = source('calendar-today', () => {
  const now = new Date()
  const range = `start=${encodeURIComponent(rfc3339(startOfDay(now)))}&end=${encodeURIComponent(rfc3339(endOfDay(now)))}`
  return getJSON<{ events: CalendarEvent[] | null }>(`/api/calendar/events?${range}`).then((d) => d.events ?? [])
})
