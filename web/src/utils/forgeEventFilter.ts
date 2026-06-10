import type { WorkerEvent } from '../components/LiveActivity'

// EventFilter is the Mezzanine events panel filter selection. The tagged
// `anvil:` prefix keeps the type constrained so arbitrary strings cannot slip
// through accidentally.
export type EventFilter = 'all' | 'errors' | 'prs' | `anvil:${string}`

// ForgeEventFilterParams are the server query params understood by
// /api/forge/events (and /api/forge/events/page) for the panel's filters.
export interface ForgeEventFilterParams {
  level?: string
  group?: string
  anvil?: string
}

// classifyLevel maps an event to a display level. The failure branch mirrors
// the backend `level=error` predicate in internal/forge/db.go
// (appendEventFilters) so client and server agree on what counts as an error.
export function classifyLevel(
  event: Pick<WorkerEvent, 'type' | 'level' | 'message'>,
): 'success' | 'failure' | 'info' {
  const type = event.type?.toLowerCase() ?? ''
  const level = event.level?.toLowerCase() ?? ''
  const message = event.message?.toLowerCase() ?? ''

  if (level === 'error' || type.includes('fail') || type.includes('error') || message.includes('failed')) {
    return 'failure'
  }
  if (
    level === 'success' ||
    type.includes('pass') ||
    type.includes('merged') ||
    type.includes('done') ||
    type.includes('success') ||
    type.includes('complete')
  ) {
    return 'success'
  }
  return 'info'
}

// isPRGroupEvent mirrors the backend `group=prs` predicate: the event type
// references a pull request, merge, warden, or review.
export function isPRGroupEvent(event: Pick<WorkerEvent, 'type'>): boolean {
  const type = event.type?.toLowerCase() ?? ''
  return (
    type.includes('pr') ||
    type.includes('merge') ||
    type.includes('warden') ||
    type.includes('review')
  )
}

// filterToParams maps a UI filter selection to the server query params. 'all'
// returns undefined, meaning no server-side filtering (default behavior).
export function filterToParams(filter: EventFilter): ForgeEventFilterParams | undefined {
  if (filter === 'errors') return { level: 'error' }
  if (filter === 'prs') return { group: 'prs' }
  if (filter.startsWith('anvil:')) return { anvil: filter.slice(6) }
  return undefined
}

// eventMatchesParams decides whether a live (SSE/poll) event belongs in the
// current server-filtered view, so live updates stay consistent with the server
// query without forcing a refetch on every event.
export function eventMatchesParams(event: WorkerEvent, params: ForgeEventFilterParams): boolean {
  if (params.level === 'error' && classifyLevel(event) !== 'failure') return false
  if (params.group === 'prs' && !isPRGroupEvent(event)) return false
  if (params.anvil && event.anvil !== params.anvil) return false
  return true
}
