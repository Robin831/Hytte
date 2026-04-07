import { useState, useEffect, useCallback } from 'react'
import type { WorkerEvent } from '../components/LiveActivity'

export interface EventsPageParams {
  search?: string
  type?: string
  anvil?: string
  from?: string
  to?: string
  page: number
  pageSize: number
}

interface EventsPageResult {
  events: WorkerEvent[]
  total: number
}

interface UseEventsPageReturn {
  events: WorkerEvent[]
  total: number
  loading: boolean
  error: string | null
  refresh: () => void
}

export function useEventsPage(params: EventsPageParams): UseEventsPageReturn {
  const [events, setEvents] = useState<WorkerEvent[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)

  const refresh = useCallback(() => setRefreshKey(k => k + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    let cancelled = false

    async function load() {
      setLoading(true)
      setError(null)
      try {
        const qs = new URLSearchParams()
        qs.set('limit', String(params.pageSize))
        qs.set('offset', String((params.page - 1) * params.pageSize))
        if (params.search) qs.set('search', params.search)
        if (params.type) qs.set('type', params.type)
        if (params.anvil) qs.set('anvil', params.anvil)
        if (params.from) qs.set('from', params.from)
        if (params.to) qs.set('to', params.to)

        const res = await fetch(`/api/forge/events/page?${qs.toString()}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const data = (await res.json()) as EventsPageResult
        if (!cancelled) {
          setEvents(data.events ?? [])
          setTotal(data.total ?? 0)
        }
      } catch (err) {
        if (cancelled) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Failed to load events')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void load()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [params.search, params.type, params.anvil, params.from, params.to, params.page, params.pageSize, refreshKey])

  return { events, total, loading, error, refresh }
}
