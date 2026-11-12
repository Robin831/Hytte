// Types and calls for Verksted (internal/verksted) — codename for the
// Christmas presents planner.

export const STATUSES = ['idea', 'planned', 'ordered', 'arrived', 'wrapped', 'given'] as const
export type GiftStatus = typeof STATUSES[number]

export interface Person {
  id: number
  name: string
  group: string
  user_id: number | null
  birth_year: number | null
  sizes: string
  notes: string
  archived: boolean
  budget_nok: number
}

export interface Wish {
  id: number
  person_id: number
  title: string
  url: string
  notes: string
  price_nok: number
  priority: number
  reserved: boolean
  reserved_by: number | null
  gift_id: number | null
}

export interface Gift {
  id: number
  season: number
  title: string
  notes: string
  url: string
  shop: string
  giver: string
  price_nok: number
  status: GiftStatus
  wish_id: number | null
  recipient_ids: number[]
}

export interface Overview {
  season: number
  seasons: number[]
  total_budget_nok: number
  people: Person[]
  wishes: Wish[]
  gifts: Gift[]
}

export type PersonInput = Partial<Pick<Person, 'name' | 'group' | 'birth_year' | 'sizes' | 'notes' | 'archived'>>
export type WishInput = Partial<Pick<Wish, 'person_id' | 'title' | 'url' | 'notes' | 'price_nok' | 'priority'>>
export type GiftInput = Partial<Pick<Gift, 'season' | 'title' | 'notes' | 'url' | 'shop' | 'giver' | 'price_nok' | 'status' | 'wish_id' | 'recipient_ids'>>

async function call<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    credentials: 'include',
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
  })
  if (!res.ok) {
    let msg = ''
    try {
      msg = ((await res.json()) as { error?: string }).error ?? ''
    } catch {
      // not JSON
    }
    throw new Error(msg || `HTTP ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

const json = (method: string, body: unknown): RequestInit => ({ method, body: JSON.stringify(body) })

export const api = {
  overview: (season: number | null, signal?: AbortSignal) =>
    call<Overview>(`/api/verksted${season ? `?season=${season}` : ''}`, { signal }),
  addPerson: (in_: PersonInput) => call<{ id: number }>('/api/verksted/people', json('POST', in_)),
  updatePerson: (id: number, in_: PersonInput) => call<void>(`/api/verksted/people/${id}`, json('PATCH', in_)),
  deletePerson: (id: number) => call<void>(`/api/verksted/people/${id}`, { method: 'DELETE' }),
  setPersonBudget: (id: number, season: number, budget: number) =>
    call<void>(`/api/verksted/people/${id}/budget`, json('PUT', { season, budget_nok: budget })),
  setTotalBudget: (season: number, budget: number) =>
    call<void>('/api/verksted/budget', json('PUT', { season, budget_nok: budget })),
  addWish: (in_: WishInput) => call<{ id: number }>('/api/verksted/wishes', json('POST', in_)),
  updateWish: (id: number, in_: WishInput) => call<void>(`/api/verksted/wishes/${id}`, json('PATCH', in_)),
  deleteWish: (id: number) => call<void>(`/api/verksted/wishes/${id}`, { method: 'DELETE' }),
  reserveWish: (id: number, reserved: boolean) => call<void>(`/api/verksted/wishes/${id}/reserve`, json('POST', { reserved })),
  addGift: (in_: GiftInput) => call<{ id: number }>('/api/verksted/gifts', json('POST', in_)),
  updateGift: (id: number, in_: GiftInput) => call<void>(`/api/verksted/gifts/${id}`, json('PATCH', in_)),
  deleteGift: (id: number) => call<void>(`/api/verksted/gifts/${id}`, { method: 'DELETE' }),
}

/** A gift shared by several people counts its price split evenly. */
export function shareOf(g: Gift): number {
  return g.recipient_ids.length > 0 ? g.price_nok / g.recipient_ids.length : g.price_nok
}

/** Bought means at least ordered. */
export function isBought(s: GiftStatus): boolean {
  return STATUSES.indexOf(s) >= STATUSES.indexOf('ordered')
}

/** Days until Christmas Eve of the season (0 on the day, negative after). */
export function daysToChristmasEve(season: number, today: Date = new Date()): number {
  const eve = new Date(season, 11, 24)
  const start = new Date(today.getFullYear(), today.getMonth(), today.getDate())
  return Math.round((eve.getTime() - start.getTime()) / 86_400_000)
}

/** Age the person turns in the season, if known. */
export function ageIn(p: Person, season: number): number | null {
  return p.birth_year ? season - p.birth_year : null
}
