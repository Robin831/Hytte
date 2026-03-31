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
    let timeoutId: ReturnType<typeof setTimeout>

    async function fetchStatus() {
      const controller = new AbortController()
      try {
        const res = await fetch('/api/forge/status', { credentials: 'include', signal: controller.signal })
        if (cancelled) return
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
          timeoutId = setTimeout(() => void fetchStatus(), 5000)
        }
      }
    }

    void fetchStatus()
    return () => {
      cancelled = true
      clearTimeout(timeoutId)
    }
  }, [t])

  return { status, error, loading }
}
