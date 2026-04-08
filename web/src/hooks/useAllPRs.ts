import { useState, useEffect } from 'react'
import type { OpenPR } from './useForgeStatus'

export interface ExternalPR {
  number: number
  title: string
  anvil: string
  branch: string
  base_branch: string
  author: string
  url: string
  is_draft: boolean
}

export interface AllPRsData {
  forge_prs: OpenPR[]
  external_prs: ExternalPR[]
}

export function useAllPRs(enabled: boolean) {
  const [data, setData] = useState<AllPRsData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)

  function refetch() {
    setRefreshKey(k => k + 1)
  }

  useEffect(() => {
    if (!enabled) return

    const controller = new AbortController()
    let timeoutId: ReturnType<typeof setTimeout> | undefined

    async function fetchAllPRs() {
      try {
        const res = await fetch('/api/forge/prs/all', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (controller.signal.aborted) return
        if (!res.ok) {
          const body = await res.json().catch(() => ({}))
          setError((body as { error?: string }).error ?? `HTTP ${res.status}`)
          return
        }
        const json: AllPRsData = await res.json()
        if (!controller.signal.aborted) {
          setData(json)
          setError(null)
        }
      } catch (err) {
        if (controller.signal.aborted) return
        if (err instanceof Error && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          timeoutId = setTimeout(() => void fetchAllPRs(), 30_000)
        }
      }
    }

    setLoading(true)
    setError(null)
    void fetchAllPRs()
    return () => {
      controller.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [enabled, refreshKey])

  return { data, loading, error, refetch }
}
