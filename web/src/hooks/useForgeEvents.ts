import { useState, useEffect, useRef, useCallback } from 'react'
import type { WorkerEvent } from '../components/LiveActivity'
import { eventMatchesParams, type ForgeEventFilterParams } from '../utils/forgeEventFilter'

interface UseForgeEventsOptions {
  maxEvents?: number
  pollInterval?: number
  // When set, the initial fetch and polling fallback query the server with
  // these filter params so the entire event log is searched (not just the live
  // window), and live SSE/poll events are matched client-side to stay in the
  // filtered view. Omit (or pass undefined) for the default unfiltered stream.
  filter?: ForgeEventFilterParams
}

// When a server-side filter is active we fetch a deeper slice so the panel can
// surface older matching events; the unfiltered stream keeps the smaller window.
const FILTERED_FETCH_LIMIT = 200
const UNFILTERED_FETCH_LIMIT = 50

/**
 * Subscribes to Forge events via SSE (/api/forge/activity/stream) with a
 * polling fallback (/api/forge/events). Returns events in chronological order
 * (oldest first). An initial fetch seeds the list before live updates arrive.
 * All incoming events are deduplicated by id so the initial fetch and the SSE
 * initial batch cannot race each other into dropping or duplicating events.
 *
 * When `filter` is provided, the seed/poll fetches pass the filter params to
 * the server (which filters the full event log) and live events are matched
 * against the same params client-side so the returned list only ever contains
 * matching events, newest history included.
 */
export function useForgeEvents({ maxEvents = 200, pollInterval = 3000, filter }: UseForgeEventsOptions = {}) {
  const [events, setEvents] = useState<WorkerEvent[]>([])
  const lastSeenIdRef = useRef(0)
  const esRef = useRef<EventSource | null>(null)
  const pollingRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const fallbackActiveRef = useRef(false)

  // Destructure to primitive deps so the effect re-runs only when the filter
  // actually changes (the caller may pass a fresh object each render).
  const level = filter?.level ?? ''
  const group = filter?.group ?? ''
  const anvil = filter?.anvil ?? ''
  const hasFilter = !!(level || group || anvil)

  // When a filter is active we fetch a deeper slice (FILTERED_FETCH_LIMIT) so
  // older matches surface. Cap the retained list to at least that depth,
  // otherwise a smaller caller-supplied maxEvents (e.g. the panel's 100) would
  // silently truncate the deeper fetch and negate its benefit.
  const effectiveMax = hasFilter ? Math.max(maxEvents, FILTERED_FETCH_LIMIT) : maxEvents

  const mergeEvents = useCallback((incoming: WorkerEvent[]) => {
    if (incoming.length === 0) return
    setEvents(prev => {
      const byId = new Map<number, WorkerEvent>()
      for (const e of prev) byId.set(e.id, e)
      for (const e of incoming) byId.set(e.id, e)
      const merged = [...byId.values()].sort((a, b) => a.id - b.id)
      if (merged.length > 0) {
        lastSeenIdRef.current = Math.max(lastSeenIdRef.current, merged[merged.length - 1].id)
      }
      return merged.slice(-effectiveMax)
    })
  }, [effectiveMax])

  useEffect(() => {
    const abortController = new AbortController()
    const pollingInFlightRef = { current: false }

    // Reset when the filter changes so a previous (e.g. unfiltered) result set
    // cannot leak into the new filtered view.
    setEvents([])
    lastSeenIdRef.current = 0

    const params: ForgeEventFilterParams = { level, group, anvil }
    const fetchURL = () => {
      const qs = new URLSearchParams()
      qs.set('limit', String(hasFilter ? FILTERED_FETCH_LIMIT : UNFILTERED_FETCH_LIMIT))
      if (level) qs.set('level', level)
      if (group) qs.set('group', group)
      if (anvil) qs.set('anvil', anvil)
      return `/api/forge/events?${qs.toString()}`
    }
    // Live (SSE) events arrive unfiltered, so match them against the active
    // filter before merging. With no filter every event matches.
    const matchesFilter = (e: WorkerEvent) => !hasFilter || eventMatchesParams(e, params)

    function startPolling() {
      if (fallbackActiveRef.current) return
      fallbackActiveRef.current = true
      pollingRef.current = setInterval(() => {
        if (pollingInFlightRef.current) return
        pollingInFlightRef.current = true
        fetch(fetchURL(), { credentials: 'include', signal: abortController.signal })
          .then(res => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
          .then((data: unknown) => {
            if (!Array.isArray(data) || data.length === 0) return
            const newer = (data as WorkerEvent[]).filter(e => e.id > lastSeenIdRef.current)
            if (newer.length === 0) return
            mergeEvents(newer)
          })
          .catch(err => {
            if (err instanceof DOMException && err.name === 'AbortError') return
            console.warn('useForgeEvents: polling fetch failed', err)
          })
          .finally(() => { pollingInFlightRef.current = false })
      }, pollInterval)
    }

    try {
      const es = new EventSource('/api/forge/activity/stream')
      esRef.current = es
      es.onmessage = (e: MessageEvent<string>) => {
        try {
          const event = JSON.parse(e.data) as WorkerEvent
          if (matchesFilter(event)) mergeEvents([event])
        } catch {
          // ignore unparseable SSE data
        }
      }
      es.onerror = (err) => {
        console.warn('useForgeEvents: SSE connection error, falling back to polling', err)
        es.close()
        esRef.current = null
        startPolling()
      }
    } catch (err) {
      console.warn('useForgeEvents: SSE not available, falling back to polling', err)
      startPolling()
    }

    // Initial fetch to seed the list; merged with any SSE events already received
    // to avoid the race condition where SSE arrives before the fetch resolves.
    // The server already applied the filter, so merge the results as-is.
    fetch(fetchURL(), { credentials: 'include', signal: abortController.signal })
      .then(res => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
      .then((data: unknown) => {
        if (!Array.isArray(data) || data.length === 0) return
        mergeEvents(data as WorkerEvent[])
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        console.warn('useForgeEvents: initial fetch failed', err)
      })

    return () => {
      abortController.abort()
      esRef.current?.close()
      esRef.current = null
      fallbackActiveRef.current = false
      if (pollingRef.current !== undefined) {
        clearInterval(pollingRef.current)
        pollingRef.current = undefined
      }
    }
  }, [mergeEvents, pollInterval, level, group, anvil, hasFilter])

  return { events }
}
