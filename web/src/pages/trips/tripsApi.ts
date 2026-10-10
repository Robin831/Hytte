// Types and helpers for trips (internal/trips). Every text is an I18n object
// with nb/en/th; the page shows the viewer's language and falls back.

export type Lang = 'nb' | 'en' | 'th'
export type I18n = Partial<Record<Lang, string>>
export type TripKind = 'race' | 'family' | 'holiday' | 'work' | 'other'
export const TRIP_KINDS: TripKind[] = ['race', 'family', 'holiday', 'work', 'other']

export interface Traveller { name: string; user_id?: number | null; child: boolean }
export interface Phase { key: string; title: I18n; start: string; end: string }
export interface Flight {
  phase: string; airline: string; flight_no: string; from: string; from_name: string; to: string; to_name: string
  dep_local: string; dep_tz: string; arr_local: string; arr_tz: string; booking_ref: string; seat: string; note: I18n
}
export interface Stay {
  phase: string; name: string; address: string; phone: string; check_in: string; check_out: string; tz: string
  booking_ref: string; room: string; price: string; note: I18n
}
export interface Transport { phase: string; title: I18n; when_local: string; tz: string; detail: I18n; ref: string; phone: string }
export interface Step { time: string; label: I18n; text: I18n; key: boolean }
export interface Day { date: string; title: I18n; summary: I18n; highlight: boolean; steps: Step[] }
export interface Contact { label: I18n; value: string; kind: 'phone' | 'ref' | 'email' | 'url' | 'text'; urgent: boolean; note: I18n }
export interface DocumentItem { title: I18n; status: 'done' | 'todo'; detail: I18n }
export interface Note { phase: string; title: I18n; body: I18n }
export interface Followup { title: I18n; detail: I18n; done: boolean }
export interface TripRace { name: string; date: string; bib: string; result: string }

export interface TripDoc {
  title: I18n; summary: I18n; route: string[]; travellers: Traveller[]; race?: TripRace | null; phases: Phase[]
  flights: Flight[]; stays: Stay[]; transport: Transport[]; days: Day[]; contacts: Contact[]; documents: DocumentItem[]
  notes: Note[]; followups: Followup[]
}

export interface CheckItem { id: number; title: I18n; detail: I18n; urgent: boolean; done: boolean; done_by_name: string; done_at: string; position: number }
export interface Checklist { id: number; phase: string; title: I18n; position: number; items: CheckItem[] }

export interface Trip {
  id: number; owner_id: number; kind: TripKind; start_date: string; end_date: string; home_tz: string; dest_tz: string
  share_family: boolean; race_event_id: number | null; result_id: number | null; doc: TripDoc; checklists: Checklist[]
  can_edit: boolean; created_at: string; updated_at: string
}

export interface TripSummary {
  id: number; kind: TripKind; title: I18n; start_date: string; end_date: string; dest_tz: string; route: string[]
  travellers: string[] | null; open: number; total: number
}

export type TripInput = Pick<Trip, 'kind' | 'start_date' | 'end_date' | 'home_tz' | 'dest_tz' | 'share_family' | 'race_event_id' | 'result_id' | 'doc'>

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { credentials: 'include', ...init, headers: init?.body ? { 'Content-Type': 'application/json' } : undefined })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error((data as { error?: string }).error || `HTTP ${res.status}`)
  }
  return res.json() as Promise<T>
}

export const fetchTrips = (signal?: AbortSignal) => request<{ trips: TripSummary[] }>('/api/trips', { signal })
export const fetchTrip = (id: number, signal?: AbortSignal) => request<{ trip: Trip }>(`/api/trips/${id}`, { signal })
export const createTrip = (input: TripInput) => request<{ id: number }>('/api/trips', { method: 'POST', body: JSON.stringify(input) })
export const updateTrip = (id: number, input: TripInput) => request<{ status: string }>(`/api/trips/${id}`, { method: 'PUT', body: JSON.stringify(input) })
export const deleteTrip = (id: number) => request<{ status: string }>(`/api/trips/${id}`, { method: 'DELETE' })
export const addChecklist = (tripId: number, title: I18n, phase = '') =>
  request<{ id: number }>(`/api/trips/${tripId}/checklists`, { method: 'POST', body: JSON.stringify({ title, phase }) })
export const deleteChecklist = (id: number) => request<{ status: string }>(`/api/trips/checklists/${id}`, { method: 'DELETE' })
export const addItem = (groupId: number, title: I18n, detail: I18n = {}, urgent = false) =>
  request<{ id: number }>(`/api/trips/checklists/${groupId}/items`, { method: 'POST', body: JSON.stringify({ title, detail, urgent }) })
export const setItemDone = (id: number, done: boolean) =>
  request<{ status: string }>(`/api/trips/items/${id}/done`, { method: 'POST', body: JSON.stringify({ done }) })
export const deleteItem = (id: number) => request<{ status: string }>(`/api/trips/items/${id}`, { method: 'DELETE' })

export function toLang(language: string | undefined): Lang {
  const base = (language || 'en').toLowerCase().split('-')[0]
  if (base === 'nb' || base === 'no' || base === 'nn') return 'nb'
  if (base === 'th') return 'th'
  return 'en'
}

/** The viewer's language, falling back to en, nb, th. */
export function txt(t: I18n | undefined, lang: Lang): string {
  if (!t) return ''
  for (const l of [lang, 'en', 'nb', 'th'] as Lang[]) if (t[l]) return t[l]!
  return ''
}

export function intlLocale(lang: Lang): string {
  if (lang === 'nb') return 'nb-NO'
  if (lang === 'th') return 'th-TH-u-ca-gregory'
  return 'en-GB'
}

/**
 * The UTC instant of a wall-clock time ("2026-10-05T15:10") in a zone.
 * Uses the zone's offset at that moment (Intl), so DST is right.
 */
export function zonedInstant(local: string, tz: string): Date {
  const [d, t] = local.split('T')
  const [y, mo, da] = d.split('-').map(Number)
  const [h, mi] = (t || '00:00').split(':').map(Number)
  const guess = Date.UTC(y, mo - 1, da, h, mi)
  const offset = (at: number) => {
    const parts = Object.fromEntries(new Intl.DateTimeFormat('en-US', {
      timeZone: tz, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    }).formatToParts(new Date(at)).map(p => [p.type, p.value]))
    return Date.UTC(+parts.year, +parts.month - 1, +parts.day, +parts.hour, +parts.minute) - at
  }
  let ts = guess - offset(guess)
  ts = guess - offset(ts)
  return new Date(ts)
}

/** "2026-10-05" in a zone at an instant. */
export function dateIn(tz: string, at: Date): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit' }).format(at)
}

export function formatClock(tz: string, at: Date, lang: Lang): string {
  return new Intl.DateTimeFormat(intlLocale(lang), { timeZone: tz, hour: '2-digit', minute: '2-digit' }).format(at)
}

/** Weekday + date + time of a wall-clock time, as written (local to its place). */
export function formatLocal(local: string, lang: Lang, withTime = true): string {
  if (!local) return ''
  const [d, t] = local.split('T')
  const date = new Date(d + 'T12:00:00Z')
  const day = new Intl.DateTimeFormat(intlLocale(lang), { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' }).format(date)
  return withTime && t ? `${day} ${t}` : day
}

export function formatDate(d: string, lang: Lang): string {
  return new Intl.DateTimeFormat(intlLocale(lang), { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' })
    .format(new Date(d + 'T12:00:00Z'))
}

/** "1 d 4 t" style countdown parts. */
export function countdown(ms: number): { days: number; hours: number; minutes: number } {
  const m = Math.max(0, Math.floor(ms / 60000))
  return { days: Math.floor(m / 1440), hours: Math.floor((m % 1440) / 60), minutes: m % 60 }
}

export type TripState = 'before' | 'during' | 'after'

export function tripState(t: Pick<Trip, 'start_date' | 'end_date' | 'dest_tz'>, now: Date): TripState {
  const today = dateIn(t.dest_tz, now)
  if (today < t.start_date) return 'before'
  if (today > t.end_date) return 'after'
  return 'during'
}

/** The trip's first departure instant (first flight, else start of day at home). */
export function departureInstant(t: Trip): Date {
  const flights = [...t.doc.flights].filter(f => f.dep_local && f.dep_tz).sort((a, b) =>
    zonedInstant(a.dep_local, a.dep_tz).getTime() - zonedInstant(b.dep_local, b.dep_tz).getTime())
  if (flights[0]) return zonedInstant(flights[0].dep_local, flights[0].dep_tz)
  return zonedInstant(t.start_date + 'T00:00', t.home_tz)
}

export const emptyDoc = (): TripDoc => ({
  title: {}, summary: {}, route: [], travellers: [], race: null, phases: [], flights: [], stays: [], transport: [], days: [],
  contacts: [], documents: [], notes: [], followups: [],
})

export function tripToInput(t: Trip): TripInput {
  return {
    kind: t.kind, start_date: t.start_date, end_date: t.end_date, home_tz: t.home_tz, dest_tz: t.dest_tz,
    share_family: t.share_family, race_event_id: t.race_event_id, result_id: t.result_id, doc: structuredClone(t.doc),
  }
}

/** A text edited in one language: keep the others only if this one is unchanged. */
export function editI18n(prev: I18n | undefined, lang: Lang, value: string): I18n {
  if ((prev?.[lang] ?? '') === value) return prev ?? {}
  // Changing the text invalidates the other languages; the server fills them in again.
  return value.trim() ? { [lang]: value } : {}
}
