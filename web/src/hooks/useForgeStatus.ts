import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

export interface ForgeStatus {
  daemon_healthy: boolean
  daemon_error?: string
  workers: {
    active: number
    completed: number
  }
  prs_open: number
  queue_ready: number
  needs_human: number
}

export function useForgeStatus() {
  const { t } = useTranslation('forge')
  const [status, setStatus] = useState<ForgeStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    let currentController: AbortController | null = null

    async function fetchStatus() {
      currentController = new AbortController()
      let stopPolling = false
      try {
        const res = await fetch('/api/forge/status', { credentials: 'include', signal: currentController.signal })
        if (cancelled) return
        if (res.status === 404) {
          setStatus(null)
          setError(t('featureDisabled'))
          stopPolling = true
          return
        }
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          setStatus(null)
          setError((data as { error?: string }).error ?? `HTTP ${res.status}`)
        } else {
          const data: ForgeStatus = await res.json()
          if (!cancelled) {
            setStatus(data)
            setError(null)
          }
        }
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
        setStatus(null)
        setError(err instanceof Error ? err.message : t('unknownError'))
      } finally {
        if (!cancelled) {
          setLoading(false)
          if (!stopPolling) {
            timeoutId = setTimeout(() => void fetchStatus(), 5000)
          }
        }
      }
    }

    void fetchStatus()
    return () => {
      cancelled = true
      currentController?.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [t])

  return { status, error, loading }
}
