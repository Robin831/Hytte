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
    const controller = new AbortController()

    async function fetchStatus() {
      try {
        const res = await fetch('/api/forge/status', { credentials: 'include', signal: controller.signal })
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          setError((data as { error?: string }).error ?? `HTTP ${res.status}`)
          return
        }
        const data: ForgeStatus = await res.json()
        setStatus(data)
        setError(null)
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : t('unknownError'))
      } finally {
        setLoading(false)
      }
    }

    void fetchStatus()
    const id = setInterval(() => void fetchStatus(), 5000)
    return () => {
      controller.abort()
      clearInterval(id)
    }
  }, [t])

  return { status, error, loading }
}
