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

export function useAllPRs() {
  const [data, setData] = useState<AllPRsData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    let currentController: AbortController | null = null

    async function fetchAllPRs() {
      currentController = new AbortController()
      try {
        const res = await fetch('/api/forge/prs/all', {
          credentials: 'include',
          signal: currentController.signal,
        })
        if (cancelled) return
        if (!res.ok) {
          const body = await res.json().catch(() => ({}))
          setError((body as { error?: string }).error ?? `HTTP ${res.status}`)
          return
        }
        const json: AllPRsData = await res.json()
        if (!cancelled) {
          setData(json)
          setError(null)
        }
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        if (!cancelled) {
          setLoading(false)
          timeoutId = setTimeout(() => void fetchAllPRs(), 30_000)
        }
      }
    }

    void fetchAllPRs()
    return () => {
      cancelled = true
      currentController?.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [])

  return { data, loading, error }
}
