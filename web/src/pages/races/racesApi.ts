// Types and helpers for the race catalog (internal/races). Mirrors the Go
// types; the catalog is small enough to load whole and filter client-side.

export type Lang = 'nb' | 'en' | 'th'
export const LANGS: Lang[] = ['nb', 'en', 'th']

export type RaceStatus = 'open' | 'later' | 'closed'
export type EntryType = 'lottery' | 'fcfs' | 'qualifier' | 'unknown'
export type Travel = 'direct' | 'nearby' | 'none' | ''
export type DatePrecision = 'day' | 'approx' | 'early' | 'mid' | 'late' | 'month'
export type Series = 'majors' | 'emc' | 'superhalfs'
export type DeadlineKind =
  | 'entry_opens' | 'entry_closes' | 'lottery_opens' | 'lottery_closes' | 'lottery_results'
  | 'payment_due' | 'price_increase' | 'waitlist_closes' | 'other'
export type WatchState =
  | 'watching' | 'planning' | 'lottery_entered' | 'got_place' | 'not_selected'
  | 'registered' | 'completed' | 'skipped'

export const STATUSES: RaceStatus[] = ['open', 'later', 'closed']
export const ENTRY_TYPES: EntryType[] = ['lottery', 'fcfs', 'qualifier', 'unknown']
export const TRAVELS: Travel[] = ['direct', 'nearby', 'none', '']
export const PRECISIONS: DatePrecision[] = ['day', 'approx', 'early', 'mid', 'late', 'month']
export const SERIES: Series[] = ['majors', 'emc', 'superhalfs']
export const DEADLINE_KINDS: DeadlineKind[] = [
  'entry_opens', 'entry_closes', 'lottery_opens', 'lottery_closes', 'lottery_results',
  'payment_due', 'price_increase', 'waitlist_closes', 'other',
]
// Pipeline order: the order lanes appear in on "My races".
export const WATCH_STATES: WatchState[] = [
  'registered', 'got_place', 'lottery_entered', 'planning', 'watching',
  'not_selected', 'completed', 'skipped',
]

export interface EventText {
  place: string
  participants: string
  course: string
  travel: string
  how: string
  price: string
}

export interface DeadlineText {
  what: string
}

export interface Deadline {
  id: number
  event_id: number
  kind: DeadlineKind
  due_date: string
  date_precision: DatePrecision
  due_time: string
  tz: string
  due_at?: string
  expected: boolean
  texts: Partial<Record<Lang, DeadlineText>>
}

export interface RaceEvent {
  id: number
  slug: string
  name: string
  edition_year: number
  race_date: string
  date_precision: DatePrecision
  country: string
  distance_m: number
  status: RaceStatus
  entry_type: EntryType
  travel: Travel
  url: string
  series: Series[]
  texts: Partial<Record<Lang, EventText>>
  checked_at: string
  created_at: string
  updated_at: string
  deadlines: Deadline[]
}

export interface Watch {
  event_id: number
  state: WatchState
  notes: string
  stride_race_id: number | null
  created_at: string
  updated_at: string
}

export interface Change {
  id: number
  event_id: number
  field: string
  old_value: string
  new_value: string
  source: string
  created_at: string
}

export interface ResearchRun {
  id: number
  kind: 'race' | 'discover' | 'time'
  event_id: number | null
  event_name?: string
  trigger: 'manual' | 'scheduled'
  status: 'running' | 'done' | 'failed' | 'skipped'
  started_at: string
  finished_at: string
  cost_usd: number
  changes: number
  summary: string
  sources: string[]
  error: string
}

export interface SpendDay {
  date: string
  cost_usd: number
  runs: number
}

export interface SpendStats {
  today_usd: number
  last_7_days_usd: number
  month_to_date_usd: number
  last_30_days_usd: number
  all_time_usd: number
  race_checks_30d: number
  discoveries_30d: number
  avg_race_check_usd: number
  changes_30d: number
  daily: SpendDay[]
}

export interface ResearchSettings {
  enabled: boolean
  discovery_enabled: boolean
  model: string
  daily_budget_usd: number
  monthly_budget_usd: number
  nightly_max_races: number
  home_city: string
  home_airport: string
}

export interface FamilyWatch {
  user_id: number
  name: string
  picture: string
  state: WatchState
}

export interface HomeBase {
  city: string
  airport: string
}

export interface LedgerEntry {
  event_id: number
  name: string
  edition_year: number
  race_date: string
  entered_at: string
  outcome: 'won' | 'lost' | 'pending'
  decided_at: string
}

export interface Ledger {
  entries: LedgerEntry[]
  entered: number
  won: number
  lost: number
  pending: number
  streaks: { race: string; losses: number }[]
}

export interface RaceResult {
  id: number
  person_name: string
  event_id: number | null
  race_name: string
  race_date: string
  date_exact: boolean
  distance_m: number
  city: string
  country: string
  finish_seconds: number | null
  time_source: '' | 'email' | 'strava' | 'workout' | 'results_site' | 'manual' | 'catalog'
  time_url: string
  bib: string
  status: 'pending' | 'confirmed'
  confidence: '' | 'high' | 'medium' | 'low'
  source: string
  evidence: { thread_id?: string; url?: string; date?: string; from?: string; what?: string }[]
  notes: string
  series_key: string
  created_at: string
  updated_at: string
}

export interface PersonalBest {
  distance: '3k' | '5k' | '10k' | 'half' | 'marathon'
  result_id: number
  finish_seconds: number
  race_name: string
  race_date: string
  count: number
}

export interface HallSummary {
  pbs: PersonalBest[]
  races: number
  countries: string[]
  per_year: Record<string, number>
  first_year: number
  pending: number
}

export type ResultInput = Pick<RaceResult, 'person_name' | 'race_name' | 'race_date' | 'distance_m' | 'city' | 'country' |
  'finish_seconds' | 'bib' | 'notes'> & Partial<Pick<RaceResult, 'event_id' | 'status' | 'confidence' | 'time_source' | 'time_url' | 'evidence' | 'date_exact'>>

export interface SeriesFinish {
  race_key: string
  year: number
  finish_seconds: number | null
  source: 'manual' | 'auto'
}

export interface SeriesProgress {
  key: 'majors' | 'emc'
  required: number
  done: number
  races: { key: string; name: string; finishes: SeriesFinish[]; next_event_id: number | null }[]
}

export interface ResearchLog {
  runs: ResearchRun[]
  stats: SpendStats
  settings: ResearchSettings
  models: string[]
  config_error?: string
}

export interface StrideRace {
  id: number
  name: string
  date: string
  distance_m: number
  target_time: number | null
  priority: 'A' | 'B' | 'C'
}

export type EventInput = Omit<RaceEvent, 'id' | 'slug' | 'checked_at' | 'created_at' | 'updated_at' | 'deadlines'>
export type DeadlineInput = Omit<Deadline, 'id' | 'event_id' | 'due_at'>

export const HALF_M = 21097
export const MARATHON_M = 42195

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    credentials: 'include',
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
  })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || `HTTP ${res.status}`)
  }
  return res.json() as Promise<T>
}

export function fetchRaces(signal?: AbortSignal) {
  return request<{ events: RaceEvent[]; watches: Watch[]; rates?: Record<string, number>; family?: Record<string, FamilyWatch[]>; home?: HomeBase }>('/api/races', { signal })
}

export function fetchRace(id: number, signal?: AbortSignal) {
  return request<{ event: RaceEvent; changes: Change[]; watch: Watch | null; rates?: Record<string, number>; research?: ResearchRun | null; family?: FamilyWatch[]; home?: HomeBase }>(`/api/races/${id}`, { signal })
}

export function startResearch(id: number) {
  return request<{ run: ResearchRun }>(`/api/races/${id}/research`, { method: 'POST' })
}

export function fetchResearchLog(signal?: AbortSignal) {
  return request<ResearchLog>('/api/races/research/runs', { signal })
}

export function saveResearchSettings(settings: ResearchSettings) {
  return request<{ settings: ResearchSettings }>('/api/races/research/settings', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
}

export function linkStride(id: number, priority: 'A' | 'B' | 'C', targetTime: number | null) {
  return request<{ watch: Watch; stride_race: StrideRace }>(`/api/races/${id}/stride`, {
    method: 'POST',
    body: JSON.stringify({ priority, target_time: targetTime }),
  })
}

export function unlinkStride(id: number, deleteRace: boolean) {
  return request<{ watch: Watch }>(`/api/races/${id}/stride${deleteRace ? '?delete=1' : ''}`, { method: 'DELETE' })
}

export function syncCalendar() {
  return request<{ result: { created: number; updated: number; deleted: number; kept: number } }>(
    '/api/races/calendar/sync', { method: 'POST' })
}

/** "3:15:00" / "1:45" / "95:00" → seconds; null when blank or unreadable. */
export function parseDuration(text: string): number | null {
  const parts = text.trim().split(':').map(p => p.trim())
  if (parts.length < 2 || parts.length > 3 || parts.some(p => !/^\d+$/.test(p))) return null
  const nums = parts.map(Number)
  const [h, m, s] = parts.length === 3 ? nums : [0, nums[0], nums[1]]
  if (m > 59 && parts.length === 3) return null
  if (s > 59) return null
  const total = h * 3600 + m * 60 + s
  return total > 0 ? total : null
}

/** Seconds → "3:15:00". */
export function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.round(seconds % 60)
  return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

export function fetchLedger(signal?: AbortSignal) {
  return request<Ledger>('/api/races/ledger', { signal })
}

export function fetchSeries(signal?: AbortSignal) {
  return request<{ series: SeriesProgress[] }>('/api/races/series', { signal })
}

export function saveFinish(raceKey: string, year: number, finishSeconds: number | null) {
  return request<{ status: string }>('/api/races/series/finishes', {
    method: 'POST',
    body: JSON.stringify({ race_key: raceKey, year, finish_seconds: finishSeconds }),
  })
}

export function deleteFinish(raceKey: string, year: number) {
  return request<{ status: string }>(`/api/races/series/finishes?race_key=${encodeURIComponent(raceKey)}&year=${year}`, { method: 'DELETE' })
}

export function fetchResults(signal?: AbortSignal) {
  return request<{ results: RaceResult[]; summary: HallSummary }>('/api/races/results', { signal })
}

export function createResult(input: ResultInput) {
  return request<{ result: RaceResult }>('/api/races/results', { method: 'POST', body: JSON.stringify(input) })
}

export function updateResult(id: number, input: ResultInput) {
  return request<{ result: RaceResult }>(`/api/races/results/${id}`, { method: 'PUT', body: JSON.stringify(input) })
}

export function deleteResult(id: number) {
  return request<{ status: string }>(`/api/races/results/${id}`, { method: 'DELETE' })
}

export function confirmResults(body: { ids?: number[]; all_high?: boolean }) {
  return request<{ confirmed: number }>('/api/races/results/confirm', { method: 'POST', body: JSON.stringify(body) })
}

export function lookupTime(id: number) {
  return request<{ run: ResearchRun }>(`/api/races/results/${id}/lookup`, { method: 'POST' })
}

export function lookupMissingTimes() {
  return request<{ queued: number }>('/api/races/results/lookup-missing', { method: 'POST' })
}

/** The result as an input, for edits that keep everything else. */
export function resultToInput(r: RaceResult): ResultInput {
  return {
    person_name: r.person_name, race_name: r.race_name, race_date: r.race_date, distance_m: r.distance_m, city: r.city,
    country: r.country, finish_seconds: r.finish_seconds, bib: r.bib, notes: r.notes, event_id: r.event_id, status: r.status,
    confidence: r.confidence, time_source: r.time_source, time_url: r.time_url, evidence: r.evidence, date_exact: r.date_exact,
  }
}

export function startDiscovery() {
  return request<{ run: ResearchRun }>('/api/races/research/discover', { method: 'POST' })
}

/** Races added to the catalog in the last two weeks get a "new" badge. */
export function isNewRace(e: Pick<RaceEvent, 'created_at'>, now = Date.now()): boolean {
  const t = Date.parse(e.created_at)
  return Number.isFinite(t) && now - t < 14 * 86_400_000
}

export function setWatch(id: number, state: WatchState, notes: string) {
  return request<{ watch: Watch }>(`/api/races/${id}/watch`, {
    method: 'PUT',
    body: JSON.stringify({ state, notes }),
  })
}

export function deleteWatch(id: number) {
  return request<{ status: string }>(`/api/races/${id}/watch`, { method: 'DELETE' })
}

export function createRace(input: EventInput) {
  return request<{ event: RaceEvent }>('/api/races', { method: 'POST', body: JSON.stringify(input) })
}

export function updateRace(id: number, input: EventInput) {
  return request<{ event: RaceEvent }>(`/api/races/${id}`, { method: 'PUT', body: JSON.stringify(input) })
}

export function deleteRace(id: number) {
  return request<{ status: string }>(`/api/races/${id}`, { method: 'DELETE' })
}

export function createDeadline(eventId: number, input: DeadlineInput) {
  return request<{ deadline: Deadline }>(`/api/races/${eventId}/deadlines`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function updateDeadline(id: number, input: DeadlineInput) {
  return request<{ deadline: Deadline }>(`/api/races/deadlines/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

export function deleteDeadline(id: number) {
  return request<{ status: string }>(`/api/races/deadlines/${id}`, { method: 'DELETE' })
}

/** Maps an i18next language ("nb", "nb-NO", "no", "th") to a catalog language. */
export function toLang(language: string | undefined): Lang {
  const base = (language || 'en').toLowerCase().split('-')[0]
  if (base === 'nb' || base === 'no' || base === 'nn') return 'nb'
  if (base === 'th') return 'th'
  return 'en'
}

/** Picks the viewer's language, falling back to English, then Norwegian, then Thai. */
export function pickText<T>(texts: Partial<Record<Lang, T>> | undefined, lang: Lang): T | undefined {
  if (!texts) return undefined
  for (const l of [lang, 'en', 'nb', 'th'] as Lang[]) {
    if (texts[l]) return texts[l]
  }
  return undefined
}

/**
 * Intl locale per UI language. Thai is pinned to the Gregorian calendar —
 * th-TH defaults to the Buddhist era, which would print 2027 as 2570.
 */
export function intlLocale(lang: Lang): string {
  if (lang === 'nb') return 'nb-NO'
  if (lang === 'th') return 'th-TH-u-ca-gregory'
  return 'en-GB'
}

function parseDate(date: string): Date {
  // Noon UTC keeps the calendar day stable in every viewer time zone.
  return new Date(date + 'T12:00:00Z')
}

/** Formats a race/deadline date honoring its precision; `prefix` renders early/mid/late/approx. */
export function formatFuzzyDate(
  date: string,
  precision: DatePrecision,
  lang: Lang,
  prefix: (precision: 'early' | 'mid' | 'late' | 'approx', text: string) => string,
): string {
  const d = parseDate(date)
  const locale = intlLocale(lang)
  const monthYear = new Intl.DateTimeFormat(locale, { month: 'short', year: 'numeric', timeZone: 'UTC' }).format(d)
  const full = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' }).format(d)
  switch (precision) {
    case 'month':
      return monthYear
    case 'early':
    case 'mid':
    case 'late':
      return prefix(precision, monthYear)
    case 'approx':
      return prefix('approx', full)
    default:
      return full
  }
}

/** Day and short month for a race's date block; null day when the date is fuzzy. */
export function dateBlock(event: Pick<RaceEvent, 'race_date' | 'date_precision'>, lang: Lang) {
  const d = parseDate(event.race_date)
  const month = new Intl.DateTimeFormat(intlLocale(lang), { month: 'short', timeZone: 'UTC' }).format(d)
  return { month, day: event.date_precision === 'day' ? String(d.getUTCDate()) : null }
}

/** Formats an exact instant in the viewer's own time zone, with the zone name. */
export function formatInstant(iso: string, lang: Lang): string {
  return new Intl.DateTimeFormat(intlLocale(lang), {
    weekday: 'short', day: 'numeric', month: 'short', year: 'numeric',
    hour: '2-digit', minute: '2-digit', timeZoneName: 'short',
  }).format(new Date(iso))
}

/** Whole days from today (local) to a date; negative when past. */
export function daysUntil(date: string, today = new Date()): number {
  const start = Date.UTC(today.getFullYear(), today.getMonth(), today.getDate())
  const [y, m, d] = date.split('-').map(Number)
  return Math.round((Date.UTC(y, m - 1, d) - start) / 86_400_000)
}

/** The deadline's effective moment for sorting and "is it past" checks. */
export function deadlineMoment(d: Deadline): number {
  if (d.due_at) return new Date(d.due_at).getTime()
  // An all-day deadline lasts until the end of its day.
  const [y, m, day] = d.due_date.split('-').map(Number)
  return new Date(y, m - 1, day, 23, 59, 59).getTime()
}

/** The next deadline that hasn't passed, if any. */
export function nextDeadline(event: RaceEvent, now = Date.now()): Deadline | undefined {
  return event.deadlines
    .filter(d => deadlineMoment(d) >= now)
    .sort((a, b) => deadlineMoment(a) - deadlineMoment(b))[0]
}

/** Country code to its flag emoji ("NO" → 🇳🇴). */
export function flag(country: string): string {
  if (!/^[A-Z]{2}$/.test(country)) return ''
  return String.fromCodePoint(...[...country].map(c => 0x1f1e6 + c.charCodeAt(0) - 65))
}

export function distanceKind(m: number): 'half' | 'marathon' | 'other' {
  if (Math.abs(m - HALF_M) < 200) return 'half'
  if (Math.abs(m - MARATHON_M) < 300) return 'marathon'
  return 'other'
}

export function emptyText(): EventText {
  return { place: '', participants: '', course: '', travel: '', how: '', price: '' }
}

export function eventToInput(e: RaceEvent): EventInput {
  return {
    name: e.name,
    edition_year: e.edition_year,
    race_date: e.race_date,
    date_precision: e.date_precision,
    country: e.country,
    distance_m: e.distance_m,
    status: e.status,
    entry_type: e.entry_type,
    travel: e.travel,
    url: e.url,
    series: [...e.series],
    texts: Object.fromEntries(LANGS.map(l => [l, { ...emptyText(), ...e.texts[l] }])),
  }
}
