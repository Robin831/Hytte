import type { Suggestion } from '../components/suggestions/SuggestionCard'

// Client-side serial queue for "Plan it" requests. Planning a suggestion runs
// the Claude CLI server-side for up to two minutes, and the plan endpoint has
// no server-side queue — concurrent requests would all run at once. This store
// serializes them: exactly one plan request is in flight at a time and the
// rest wait their turn.
//
// The store lives at module level, outside React, so the queue keeps draining
// while the user navigates elsewhere in the app. Only closing or reloading the
// tab drops it: the in-flight request still completes server-side, but queued
// entries are lost. Components subscribe via useSyncExternalStore.

export interface PlanQueueEntry {
  suggestion: Suggestion
  // Feedback text snapshotted at enqueue time.
  feedback: string
}

export interface FailedPlanEntry extends PlanQueueEntry {
  error: string
}

export interface PlanQueueState {
  /** The entry whose plan request is in flight, or null when idle. */
  active: PlanQueueEntry | null
  /** Entries waiting their turn, in FIFO order. */
  queued: PlanQueueEntry[]
  /** Entries whose plan request failed; kept for retry/dismiss. */
  failed: FailedPlanEntry[]
  /** Plans completed successfully since the tab was loaded (or last cleared). */
  doneCount: number
}

const emptyState: PlanQueueState = {
  active: null,
  queued: [],
  failed: [],
  doneCount: 0,
}

let state: PlanQueueState = emptyState
const listeners = new Set<() => void>()
const completionListeners = new Set<(updated: Suggestion) => void>()

function setState(patch: Partial<PlanQueueState>): void {
  state = { ...state, ...patch }
  for (const listener of listeners) listener()
}

export function subscribePlanQueue(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function getPlanQueueState(): PlanQueueState {
  return state
}

/**
 * Register a callback fired after each successful plan, with the
 * server-updated suggestion. This is how the page refetches its lists and
 * expands the freshly planned card without polling the state snapshot.
 */
export function subscribePlanCompleted(
  listener: (updated: Suggestion) => void,
): () => void {
  completionListeners.add(listener)
  return () => {
    completionListeners.delete(listener)
  }
}

export function isPlanEnqueued(id: number): boolean {
  return (
    state.active?.suggestion.id === id ||
    state.queued.some(e => e.suggestion.id === id)
  )
}

/**
 * Add a suggestion to the plan queue. No-op if it is already queued or being
 * planned. Re-enqueueing a previously failed suggestion clears its failure
 * entry (this is the retry path).
 */
export function enqueuePlan(suggestion: Suggestion, feedback: string): void {
  if (isPlanEnqueued(suggestion.id)) return
  setState({
    queued: [...state.queued, { suggestion, feedback }],
    failed: state.failed.filter(f => f.suggestion.id !== suggestion.id),
  })
  void processNext()
}

/** Remove a not-yet-started entry from the queue. The in-flight one cannot be
 * removed — the server is already planning it. */
export function removeQueuedPlan(id: number): void {
  setState({ queued: state.queued.filter(e => e.suggestion.id !== id) })
}

export function dismissFailedPlan(id: number): void {
  setState({ failed: state.failed.filter(e => e.suggestion.id !== id) })
}

/** Clear the session success counter (the panel's idle summary line). */
export function clearPlanQueueDone(): void {
  setState({ doneCount: 0 })
}

async function processNext(): Promise<void> {
  if (state.active || state.queued.length === 0) return
  const [next, ...rest] = state.queued
  setState({ active: next, queued: rest })
  try {
    const res = await fetch(`/api/suggestions/${next.suggestion.id}/plan`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(next.feedback ? { feedback: next.feedback } : {}),
    })
    if (!res.ok) {
      let msg = ''
      try {
        const body = (await res.json()) as { error?: string }
        if (body?.error) msg = body.error
      } catch {
        // keep the generic fallback
      }
      throw new Error(msg || `HTTP ${res.status}`)
    }
    const updated = (await res.json()) as Suggestion
    setState({
      active: null,
      doneCount: state.doneCount + 1,
    })
    for (const listener of completionListeners) listener(updated)
  } catch (err) {
    setState({
      active: null,
      failed: [
        ...state.failed,
        { ...next, error: err instanceof Error ? err.message : String(err) },
      ],
    })
  }
  // Keep draining regardless of how this entry ended.
  void processNext()
}

/** Test-only: reset all queue state. Subscribed listeners stay registered. */
export function resetPlanQueueForTest(): void {
  state = emptyState
  for (const listener of listeners) listener()
}
