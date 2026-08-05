// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  enqueuePlan,
  getPlanQueueState,
  isPlanEnqueued,
  removeQueuedPlan,
  dismissFailedPlan,
  clearPlanQueueDone,
  subscribePlanCompleted,
  resetPlanQueueForTest,
} from './suggestionsPlanQueue'
import type { Suggestion } from '../components/suggestions/SuggestionCard'

function makeSuggestion(id: number): Suggestion {
  return {
    id,
    user_id: 1,
    generated_at: '2026-05-01T00:00:00Z',
    page_slug: 'dashboard',
    source: 'claude',
    type: 'addition',
    size: 's',
    title: `Suggestion ${id}`,
    body: 'Body',
    status: 'pending',
  }
}

interface Deferred {
  url: string
  body: unknown
  resolve: (res: Partial<Response>) => void
  reject: (err: Error) => void
}

// Fetch mock that parks every request in `requests` until the test resolves
// it — this is what lets the tests assert strict one-at-a-time processing.
let requests: Deferred[] = []

function deferredFetch() {
  return vi.fn((url: string, opts?: RequestInit) => {
    return new Promise<Response>((resolve, reject) => {
      requests.push({
        url: String(url),
        body: opts?.body ? JSON.parse(String(opts.body)) : undefined,
        resolve: r => resolve(r as Response),
        reject,
      })
    })
  })
}

function okResponse(payload: unknown): Partial<Response> {
  return { ok: true, status: 200, json: async () => payload }
}

function errorResponse(status: number, error?: string): Partial<Response> {
  return {
    ok: false,
    status,
    json: async () => {
      if (error === undefined) throw new Error('no body')
      return { error }
    },
  }
}

// Let queued promise callbacks run.
async function flush() {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
}

describe('suggestionsPlanQueue', () => {
  beforeEach(() => {
    requests = []
    resetPlanQueueForTest()
    vi.stubGlobal('fetch', deferredFetch())
  })

  afterEach(() => {
    resetPlanQueueForTest()
    vi.unstubAllGlobals()
  })

  it('processes strictly one plan at a time, in FIFO order', async () => {
    enqueuePlan(makeSuggestion(1), '')
    enqueuePlan(makeSuggestion(2), '')
    enqueuePlan(makeSuggestion(3), '')
    await flush()

    // Only the first request has fired; two wait in the queue.
    expect(requests.map(r => r.url)).toEqual(['/api/suggestions/1/plan'])
    expect(getPlanQueueState().active?.suggestion.id).toBe(1)
    expect(getPlanQueueState().queued.map(e => e.suggestion.id)).toEqual([2, 3])

    requests[0].resolve(okResponse({ ...makeSuggestion(1), status: 'planned' }))
    await flush()

    expect(requests.map(r => r.url)).toEqual([
      '/api/suggestions/1/plan',
      '/api/suggestions/2/plan',
    ])
    expect(getPlanQueueState().active?.suggestion.id).toBe(2)
    expect(getPlanQueueState().doneCount).toBe(1)

    requests[1].resolve(okResponse({ ...makeSuggestion(2), status: 'planned' }))
    await flush()
    requests[2].resolve(okResponse({ ...makeSuggestion(3), status: 'planned' }))
    await flush()

    expect(getPlanQueueState()).toMatchObject({
      active: null,
      queued: [],
      failed: [],
      doneCount: 3,
    })
  })

  it('sends the snapshotted feedback, omitting it when empty', async () => {
    enqueuePlan(makeSuggestion(1), 'please keep it small')
    enqueuePlan(makeSuggestion(2), '')
    await flush()

    expect(requests[0].body).toEqual({ feedback: 'please keep it small' })

    requests[0].resolve(okResponse({ ...makeSuggestion(1), status: 'planned' }))
    await flush()

    expect(requests[1].body).toEqual({})
  })

  it('notifies completion listeners with the server-updated suggestion', async () => {
    const seen: Suggestion[] = []
    const unsubscribe = subscribePlanCompleted(s => seen.push(s))

    enqueuePlan(makeSuggestion(1), '')
    await flush()
    const updated = { ...makeSuggestion(1), status: 'planned' as const, plan: 'The plan' }
    requests[0].resolve(okResponse(updated))
    await flush()

    expect(seen).toHaveLength(1)
    expect(seen[0].plan).toBe('The plan')
    unsubscribe()
  })

  it('records a failure with the server error and keeps draining', async () => {
    enqueuePlan(makeSuggestion(1), '')
    enqueuePlan(makeSuggestion(2), '')
    await flush()

    requests[0].resolve(errorResponse(504, 'Claude timed out generating the plan'))
    await flush()

    const state = getPlanQueueState()
    expect(state.failed).toHaveLength(1)
    expect(state.failed[0].suggestion.id).toBe(1)
    expect(state.failed[0].error).toBe('Claude timed out generating the plan')
    // The queue moved on to the next entry despite the failure.
    expect(state.active?.suggestion.id).toBe(2)
    expect(state.doneCount).toBe(0)

    requests[1].resolve(okResponse({ ...makeSuggestion(2), status: 'planned' }))
    await flush()
    expect(getPlanQueueState().doneCount).toBe(1)
  })

  it('falls back to an HTTP status message when the error body is unreadable', async () => {
    enqueuePlan(makeSuggestion(1), '')
    await flush()
    requests[0].resolve(errorResponse(502))
    await flush()

    expect(getPlanQueueState().failed[0].error).toBe('HTTP 502')
  })

  it('records network failures', async () => {
    enqueuePlan(makeSuggestion(1), '')
    await flush()
    requests[0].reject(new Error('network down'))
    await flush()

    expect(getPlanQueueState().failed[0].error).toBe('network down')
    expect(getPlanQueueState().active).toBeNull()
  })

  it('ignores a duplicate enqueue of an already queued or active suggestion', async () => {
    enqueuePlan(makeSuggestion(1), '')
    enqueuePlan(makeSuggestion(2), '')
    await flush()

    enqueuePlan(makeSuggestion(1), 'again') // active
    enqueuePlan(makeSuggestion(2), 'again') // queued
    await flush()

    expect(requests).toHaveLength(1)
    expect(getPlanQueueState().queued.map(e => e.suggestion.id)).toEqual([2])
    expect(isPlanEnqueued(1)).toBe(true)
    expect(isPlanEnqueued(2)).toBe(true)
  })

  it('removes a queued entry without touching the active one', async () => {
    enqueuePlan(makeSuggestion(1), '')
    enqueuePlan(makeSuggestion(2), '')
    enqueuePlan(makeSuggestion(3), '')
    await flush()

    removeQueuedPlan(2)
    expect(getPlanQueueState().queued.map(e => e.suggestion.id)).toEqual([3])
    expect(getPlanQueueState().active?.suggestion.id).toBe(1)

    // Removing the active id is a no-op — the server is already planning it.
    removeQueuedPlan(1)
    expect(getPlanQueueState().active?.suggestion.id).toBe(1)

    requests[0].resolve(okResponse({ ...makeSuggestion(1), status: 'planned' }))
    await flush()
    // Suggestion 2 was removed, so 3 is next.
    expect(requests.map(r => r.url)).toEqual([
      '/api/suggestions/1/plan',
      '/api/suggestions/3/plan',
    ])
  })

  it('retrying a failed entry clears its failure record', async () => {
    enqueuePlan(makeSuggestion(1), 'ctx')
    await flush()
    requests[0].resolve(errorResponse(502, 'boom'))
    await flush()
    expect(getPlanQueueState().failed).toHaveLength(1)

    // Retry re-enqueues with the original feedback and drops the failure.
    const failed = getPlanQueueState().failed[0]
    enqueuePlan(failed.suggestion, failed.feedback)
    await flush()

    expect(getPlanQueueState().failed).toHaveLength(0)
    expect(requests).toHaveLength(2)
    expect(requests[1].body).toEqual({ feedback: 'ctx' })
  })

  it('dismisses failures and clears the done counter', async () => {
    enqueuePlan(makeSuggestion(1), '')
    enqueuePlan(makeSuggestion(2), '')
    await flush()
    requests[0].resolve(errorResponse(502, 'boom'))
    await flush()
    requests[1].resolve(okResponse({ ...makeSuggestion(2), status: 'planned' }))
    await flush()

    dismissFailedPlan(1)
    expect(getPlanQueueState().failed).toHaveLength(0)

    expect(getPlanQueueState().doneCount).toBe(1)
    clearPlanQueueDone()
    expect(getPlanQueueState().doneCount).toBe(0)
  })
})
