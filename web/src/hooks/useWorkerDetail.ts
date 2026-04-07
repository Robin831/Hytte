import { useState, useEffect, useMemo } from 'react'
import type { WorkerInfo } from './useForgeStatus'
import type { WorkerEvent } from '../components/LiveActivity'

export interface LogEntry {
  seq: number
  type: 'tool_use' | 'text' | 'think'
  name: string
  content: string
  status: 'success' | 'error' | ''
}

interface BeadCost {
  bead_id: string
  estimated_cost: number
  input_tokens: number
  output_tokens: number
  cache_read: number
  cache_write: number
}

export interface WorkerDetailData {
  worker: WorkerInfo | null
  logEntries: LogEntry[]
  events: WorkerEvent[]
  cost: BeadCost | null
  loading: boolean
  error: string | null
}

const MAX_LOG_ENTRIES = 500

export function useWorkerDetail(workerId: string): WorkerDetailData {
  const [worker, setWorker] = useState<WorkerInfo | null>(null)
  const [logEntries, setLogEntries] = useState<LogEntry[]>([])
  const [allEvents, setAllEvents] = useState<WorkerEvent[]>([])
  const [cost, setCost] = useState<BeadCost | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Fetch worker info
  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    const controller = new AbortController()

    async function fetchWorker() {
      try {
        const res = await fetch('/api/forge/workers', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (cancelled) return
        if (!res.ok) {
          setError(`HTTP ${res.status}`)
          return
        }
        const workers: WorkerInfo[] = await res.json()
        if (cancelled) return
        const match = workers.find(w => w.id === workerId)
        setWorker(match ?? null)
        if (!match) {
          setError(null)
        } else {
          setError(null)
        }
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        if (!cancelled) {
          setLoading(false)
          timeoutId = setTimeout(() => void fetchWorker(), 5000)
        }
      }
    }

    void fetchWorker()
    return () => {
      cancelled = true
      controller.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [workerId])

  // Fetch parsed logs
  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    const controller = new AbortController()

    async function fetchLogs() {
      try {
        const res = await fetch(
          `/api/forge/workers/${encodeURIComponent(workerId)}/log/parsed?tail=${MAX_LOG_ENTRIES}`,
          { credentials: 'include', signal: controller.signal },
        )
        if (cancelled) return
        if (!res.ok) return
        const data: unknown = await res.json()
        if (cancelled || !Array.isArray(data)) return
        const knownTypes = new Set(['tool_use', 'text', 'think'])
        const valid: LogEntry[] = data
          .filter((item): item is Record<string, unknown> =>
            item !== null && typeof item === 'object',
          )
          .filter(item => knownTypes.has(item.type as string))
          .map(item => ({
            seq: typeof item.seq === 'number' ? item.seq : 0,
            type: item.type as LogEntry['type'],
            name: typeof item.name === 'string' ? item.name : '',
            content: typeof item.content === 'string' ? item.content : '',
            status: (
              item.status === 'success' || item.status === 'error'
                ? item.status
                : ''
            ) as '' | 'success' | 'error',
          }))
          .slice(-MAX_LOG_ENTRIES)
        setLogEntries(valid)
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
      } finally {
        if (!cancelled) {
          const isActive = worker?.status === 'pending' || worker?.status === 'running'
          if (isActive) {
            timeoutId = setTimeout(() => void fetchLogs(), 2000)
          }
        }
      }
    }

    void fetchLogs()
    return () => {
      cancelled = true
      controller.abort()
      if (timeoutId !== undefined) clearTimeout(timeoutId)
    }
  }, [workerId, worker?.status])

  // Fetch events (once, then filter by bead_id)
  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()

    async function fetchEvents() {
      try {
        const res = await fetch('/api/forge/events?limit=200', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (cancelled) return
        if (!res.ok) return
        const data: unknown = await res.json()
        if (cancelled || !Array.isArray(data)) return
        setAllEvents(data as WorkerEvent[])
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
      }
    }

    void fetchEvents()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [])

  // Fetch bead cost
  useEffect(() => {
    if (!worker?.bead_id) return
    let cancelled = false
    const controller = new AbortController()

    async function fetchCost() {
      try {
        const res = await fetch('/api/forge/costs/beads?days=90&limit=20', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (cancelled) return
        if (!res.ok) return
        const data: unknown = await res.json()
        if (cancelled || !Array.isArray(data)) return
        const match = (data as BeadCost[]).find(b => b.bead_id === worker!.bead_id)
        setCost(match ?? null)
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
      }
    }

    void fetchCost()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [worker?.bead_id])

  // Filter events for this worker's bead
  const events = useMemo(() => {
    if (!worker?.bead_id) return []
    return allEvents
      .filter(e => e.bead_id === worker.bead_id)
      .sort((a, b) => a.id - b.id)
  }, [allEvents, worker?.bead_id])

  return { worker, logEntries, events, cost, loading, error }
}
