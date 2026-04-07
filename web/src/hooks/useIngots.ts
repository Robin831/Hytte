import { useState, useEffect, useCallback } from 'react'

export interface Ingot {
  bead_id: string
  title: string
  anvil: string
  status: string
  phase: string
  started_at: string
  completed_at?: string
  duration_seconds?: number
  pr_number: number
  worker_id: string
}

export interface IngotMetrics {
  total_beads: number
  success_count: number
  failure_count: number
  running_count: number
  cancelled_count: number
  success_rate: number
  avg_duration_seconds: number
}

export interface IngotsParams {
  search?: string
  status?: string
  from?: string
  to?: string
  page: number
  pageSize: number
}

interface IngotsResult {
  ingots: Ingot[]
  total: number
  metrics: IngotMetrics
}

interface UseIngotsReturn {
  ingots: Ingot[]
  total: number
  metrics: IngotMetrics | null
  loading: boolean
  error: string | null
  refresh: () => void
}

const emptyMetrics: IngotMetrics = {
  total_beads: 0,
  success_count: 0,
  failure_count: 0,
  running_count: 0,
  cancelled_count: 0,
  success_rate: 0,
  avg_duration_seconds: 0,
}

export function useIngots(params: IngotsParams): UseIngotsReturn {
  const [ingots, setIngots] = useState<Ingot[]>([])
  const [total, setTotal] = useState(0)
  const [metrics, setMetrics] = useState<IngotMetrics | null>(null)
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
        if (params.status) qs.set('status', params.status)
        if (params.from) qs.set('from', params.from)
        if (params.to) qs.set('to', params.to)

        const res = await fetch(`/api/forge/ingots?${qs.toString()}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const data = (await res.json()) as IngotsResult
        if (!cancelled) {
          setIngots(data.ingots ?? [])
          setTotal(data.total ?? 0)
          setMetrics(data.metrics ?? emptyMetrics)
        }
      } catch (err) {
        if (cancelled) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Failed to load ingots')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void load()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [params.search, params.status, params.from, params.to, params.page, params.pageSize, refreshKey])

  return { ingots, total, metrics, loading, error, refresh }
}
