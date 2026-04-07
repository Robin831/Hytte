import { useState, useEffect, useCallback } from 'react'

export interface AnvilHealth {
  anvil: string
  active_workers: number
  open_prs: number
  queue_depth: number
  last_activity: string | null
}

interface UseAnvilHealthReturn {
  anvils: AnvilHealth[]
  loading: boolean
  error: string | null
  refresh: () => void
}

export function useAnvilHealth(): UseAnvilHealthReturn {
  const [anvils, setAnvils] = useState<AnvilHealth[]>([])
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
        const res = await fetch('/api/forge/anvils/health', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const data = (await res.json()) as AnvilHealth[]
        if (!cancelled) {
          setAnvils(data ?? [])
        }
      } catch (err) {
        if (cancelled) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Failed to load anvil health')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void load()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [refreshKey])

  return { anvils, loading, error, refresh }
}
