// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useCalendarEvents } from './useCalendarEvents'
import type { CalendarEvent, ViewMode } from '../components/calendar/types'

// ── i18n mock ─────────────────────────────────────────────────────────────────
// Return the key verbatim so error assertions are deterministic. The t function
// must be a stable reference (matching real react-i18next) so useCallback deps
// that include t don't invalidate on every render.
vi.mock('react-i18next', () => {
  const t = (key: string) => key
  const i18n = { language: 'en' }
  return { useTranslation: () => ({ t, i18n }) }
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

// ── Controllable fetch mock ───────────────────────────────────────────────────
//
// Each call records its url + signal and exposes resolve/reject so tests can
// drive the in-flight request by hand. Aborting the request's signal rejects
// the pending promise with an AbortError, mirroring the real fetch contract the
// hook relies on for race-safety.
interface FetchCall {
  url: string
  signal: AbortSignal | undefined
  resolve: (value: { ok: boolean; status?: number; json: () => Promise<unknown> }) => void
  reject: (reason: unknown) => void
}

function installFetch(): FetchCall[] {
  const calls: FetchCall[] = []
  const fetchMock = vi.fn((url: string, opts?: { signal?: AbortSignal }) => {
    return new Promise((resolve, reject) => {
      const signal = opts?.signal
      calls.push({ url, signal, resolve, reject })
      if (signal) {
        signal.addEventListener(
          'abort',
          () => reject(new DOMException('Aborted', 'AbortError')),
          { once: true },
        )
      }
    })
  })
  vi.stubGlobal('fetch', fetchMock)
  return calls
}

function okJson(data: unknown) {
  return { ok: true, json: () => Promise.resolve(data) }
}

const USER = { id: 1 }
const RANGE_A = new Date(2026, 3, 8) // Apr 8 2026
const RANGE_B = new Date(2026, 4, 8) // May 8 2026

function makeEvent(id: string): CalendarEvent {
  return {
    id,
    calendar_id: 'primary',
    title: `Event ${id}`,
    start_time: '2026-04-08T10:00:00Z',
    end_time: '2026-04-08T11:00:00Z',
    all_day: false,
    status: 'confirmed',
  }
}

function renderEvents(initial: { viewMode: ViewMode; rangeStart: Date; user: { id: number } | null }) {
  return renderHook(
    ({ viewMode, rangeStart, user }) => useCalendarEvents(viewMode, rangeStart, user),
    { initialProps: initial },
  )
}

describe('useCalendarEvents', () => {
  it('starts loading and does not fetch without a user', () => {
    const calls = installFetch()
    const { result } = renderEvents({ viewMode: 'month', rangeStart: RANGE_A, user: null })
    expect(result.current.loading).toBe(true)
    expect(calls.length).toBe(0)
  })

  it('populates events and clears loading on a successful fetch', async () => {
    const calls = installFetch()
    const { result } = renderEvents({ viewMode: 'month', rangeStart: RANGE_A, user: USER })

    expect(result.current.loading).toBe(true)
    expect(calls.length).toBe(1)
    expect(calls[0].url).toContain('/api/calendar/events')

    const ev = makeEvent('1')
    await act(async () => {
      calls[0].resolve(okJson({ events: [ev] }))
    })

    expect(result.current.loading).toBe(false)
    expect(result.current.events).toEqual([ev])
    expect(result.current.error).toBeNull()
    expect(result.current.syncErrors).toEqual([])
  })

  it('sets the error message and stops loading on a failed fetch', async () => {
    const calls = installFetch()
    const { result } = renderEvents({ viewMode: 'month', rangeStart: RANGE_A, user: USER })

    await act(async () => {
      calls[0].resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })
    })

    expect(result.current.error).toBe('calendar.errors.failedToLoad')
    expect(result.current.loading).toBe(false)
  })

  it('aborts the in-flight request and never applies a stale response when inputs change', async () => {
    const calls = installFetch()
    const { result, rerender } = renderEvents({ viewMode: 'month', rangeStart: RANGE_A, user: USER })

    expect(calls.length).toBe(1)
    const firstSignal = calls[0].signal

    // Change rangeStart while the first request is still in flight.
    await act(async () => {
      rerender({ viewMode: 'month', rangeStart: RANGE_B, user: USER })
    })

    // The first request must have been aborted, and a second one started.
    expect(firstSignal?.aborted).toBe(true)
    expect(calls.length).toBe(2)

    // A late stale response for the first request must never overwrite state.
    await act(async () => {
      calls[0].resolve(okJson({ events: [makeEvent('stale')] }))
    })
    expect(result.current.events).toEqual([])

    // The newer request's response is applied.
    await act(async () => {
      calls[1].resolve(okJson({ events: [makeEvent('fresh')] }))
    })
    expect(result.current.events).toEqual([makeEvent('fresh')])
    expect(result.current.loading).toBe(false)
  })

  it('aborts the pending request on unmount', async () => {
    const calls = installFetch()
    const { unmount } = renderEvents({ viewMode: 'month', rangeStart: RANGE_A, user: USER })

    expect(calls.length).toBe(1)
    const signal = calls[0].signal
    expect(signal?.aborted).toBe(false)

    unmount()
    expect(signal?.aborted).toBe(true)
  })

  it('aborts a refetch-initiated request on unmount', async () => {
    const calls = installFetch()
    const { result, unmount } = renderEvents({ viewMode: 'month', rangeStart: RANGE_A, user: USER })

    // Resolve the initial fetch so we can call refetch.
    await act(async () => {
      calls[0].resolve(okJson({ events: [] }))
    })

    // Start a refetch (no external signal chained).
    act(() => { void result.current.refetch() })
    expect(calls.length).toBe(2)
    const refetchSignal = calls[1].signal
    expect(refetchSignal?.aborted).toBe(false)

    unmount()
    expect(refetchSignal?.aborted).toBe(true)
  })

  it('refetch() re-runs the current query without changing inputs', async () => {
    const calls = installFetch()
    const { result } = renderEvents({ viewMode: 'month', rangeStart: RANGE_A, user: USER })

    await act(async () => {
      calls[0].resolve(okJson({ events: [makeEvent('1')] }))
    })
    expect(calls.length).toBe(1)

    // Fire refetch without awaiting it — the request stays in flight until we
    // resolve it below.
    act(() => { void result.current.refetch() })
    expect(calls.length).toBe(2)
    expect(calls[1].url).toContain('/api/calendar/events')
    expect(calls[1].url).not.toContain('sync=true')

    await act(async () => {
      calls[1].resolve(okJson({ events: [makeEvent('2')] }))
    })
    expect(result.current.events).toEqual([makeEvent('2')])
  })

  it('refetch(true) requests a server-side sync and surfaces sync errors', async () => {
    const calls = installFetch()
    const { result } = renderEvents({ viewMode: 'month', rangeStart: RANGE_A, user: USER })

    await act(async () => {
      calls[0].resolve(okJson({ events: [] }))
    })

    act(() => { void result.current.refetch(true) })
    expect(calls[1].url).toContain('sync=true')

    const syncErrors = [{ calendar_id: 'primary', message: 'boom' }]
    await act(async () => {
      calls[1].resolve(okJson({ events: [], sync_errors: syncErrors }))
    })
    await waitFor(() => expect(result.current.syncErrors).toEqual(syncErrors))
  })
})
