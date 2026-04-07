import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import type { BeadDependency, BeadDetail } from '../types/forge'

export type { BeadDependency, BeadDetail }

export interface WorkerInfo {
  id: string
  bead_id: string
  anvil: string
  branch: string
  pid: number
  status: string
  phase: string
  title: string
  started_at: string
  completed_at?: string
  updated_at?: string
  log_path: string
  pr_number: number
}

export interface StuckBead {
  bead_id: string
  anvil: string
  retry_count: number
  next_retry?: string
  needs_human: boolean
  clarification_needed: boolean
  last_error: string
  updated_at: string
  dispatch_failures: number
}

export interface WorkerSummary {
  active: number
  completed: number
}

export interface OpenPR {
  id: number
  number: number
  title: string
  anvil: string
  bead_id: string
  branch: string
  ci_passing: boolean
  ci_pending: boolean
  has_approval: boolean
  changes_requested: boolean
  is_conflicting: boolean
  has_unresolved_threads: boolean
  has_pending_reviews: boolean
  bellows_managed: boolean
  ci_fix_count: number
  review_fix_count: number
}

export interface TodayStats {
  cost: number
  beads_processed: number
  prs_created: number
}

export interface ForgeEvent {
  type: 'error' | 'success' | 'info' | 'warning'
  message: string
  bead_id?: string
  anvil?: string
  timestamp: string
}

export interface QueuedBead {
  bead_id: string
  title: string
  priority?: number
  section?: string
}

export interface AnvilQueue {
  anvil: string
  beads: QueuedBead[]
}

export interface ForgeStatus {
  daemon_healthy: boolean
  daemon_error?: string
  workers: WorkerSummary
  worker_list: WorkerInfo[]
  prs_open: number
  queue_ready: number
  needs_human: number
  stuck: StuckBead[]
  open_prs?: OpenPR[]
  today_stats?: TodayStats
  recent_events?: ForgeEvent[]
  queue?: AnvilQueue[]
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

// useForgeWorkers fetches the worker list directly from /api/forge/workers,
// which reads from state.db without going through the IPC socket. Because
// the /api/forge/status endpoint performs an IPC-based health check that can
// be slow or blocked, workers are fetched independently to keep the UI responsive.
export function useForgeWorkers() {
  const { t } = useTranslation('forge')
  const [workers, setWorkers] = useState<WorkerInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    let currentController: AbortController | null = null

    async function fetchWorkers() {
      currentController = new AbortController()
      let stopPolling = false
      try {
        const res = await fetch('/api/forge/workers', { credentials: 'include', signal: currentController.signal })
        if (cancelled) return
        if (res.status === 404) {
          if (!cancelled) {
            setWorkers([])
          }
          stopPolling = true
          return
        }
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          if (!cancelled) {
            setError((data as { error?: string }).error ?? `HTTP ${res.status}`)
          }
        } else {
          const data: WorkerInfo[] = await res.json()
          if (!cancelled) {
            setWorkers(data)
            setError(null)
          }
        }
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
        if (!cancelled) {
          setError(err instanceof Error ? err.message : t('unknownError'))
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
          if (!stopPolling) {
            timeoutId = setTimeout(() => void fetchWorkers(), 5000)
          }
        }
      }
    }

    void fetchWorkers()
    return () => {
      cancelled = true
      currentController?.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [t])

  return { workers, loading, error }
}

export interface FullQueueBead {
  bead_id: string
  anvil: string
  title: string
  priority: number
  status: string
  section: string
}

// useForgeQueue polls /api/forge/queue/all for all queued beads with section info.
export function useForgeQueue() {
  const { t } = useTranslation('forge')
  const [beads, setBeads] = useState<FullQueueBead[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    let currentController: AbortController | null = null

    async function fetchQueue() {
      currentController = new AbortController()
      let stopPolling = false
      try {
        const res = await fetch('/api/forge/queue/all', { credentials: 'include', signal: currentController.signal })
        if (cancelled) return
        if (res.status === 404) {
          if (!cancelled) {
            setBeads([])
          }
          stopPolling = true
          return
        }
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          if (!cancelled) {
            setError((data as { error?: string }).error ?? `HTTP ${res.status}`)
          }
        } else {
          const data: FullQueueBead[] = await res.json()
          if (!cancelled) {
            setBeads(data)
            setError(null)
          }
        }
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
        if (!cancelled) {
          setError(err instanceof Error ? err.message : t('unknownError'))
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
          if (!stopPolling) {
            timeoutId = setTimeout(() => void fetchQueue(), 5000)
          }
        }
      }
    }

    void fetchQueue()
    return () => {
      cancelled = true
      currentController?.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [t])

  return { beads, loading, error }
}

