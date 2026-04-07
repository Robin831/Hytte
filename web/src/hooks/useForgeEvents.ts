import { useState, useEffect, useRef, useCallback } from 'react'
import type { WorkerEvent } from '../components/LiveActivity'

interface UseForgeEventsOptions {
  maxEvents?: number
  pollInterval?: number
}

/**
 * Subscribes to Forge events via SSE (/api/forge/activity/stream) with a
 * polling fallback (/api/forge/events). Returns events in chronological order
 * (oldest first). An initial fetch seeds the list before live updates arrive.
 * All incoming events are deduplicated by id so the initial fetch and the SSE
 * initial batch cannot race each other into dropping or duplicating events.
 */
export function useForgeEvents({ maxEvents = 200, pollInterval = 3000 }: UseForgeEventsOptions = {}) {
  const [events, setEvents] = useState<WorkerEvent[]>([])
  const lastSeenIdRef = useRef(0)
  const esRef = useRef<EventSource | null>(null)
  const pollingRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const fallbackActiveRef = useRef(false)

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
      return merged.slice(-maxEvents)
    })
  }, [maxEvents])

  useEffect(() => {
    const abortController = new AbortController()
    const pollingInFlightRef = { current: false }

    function startPolling() {
      if (fallbackActiveRef.current) return
      fallbackActiveRef.current = true
      pollingRef.current = setInterval(() => {
        if (pollingInFlightRef.current) return
        pollingInFlightRef.current = true
        fetch('/api/forge/events?limit=50', { credentials: 'include', signal: abortController.signal })
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
          mergeEvents([event])
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
    fetch('/api/forge/events?limit=50', { credentials: 'include', signal: abortController.signal })
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
  }, [mergeEvents, pollInterval])

  return { events }
}
