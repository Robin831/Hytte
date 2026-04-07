import { useState, useEffect, useMemo, useRef } from 'react'
import type { WorkerInfo } from './useForgeStatus'
import type { WorkerEvent } from '../components/LiveActivity'
import { useForgeEvents } from './useForgeEvents'

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
  const [cost, setCost] = useState<BeadCost | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Ref to avoid stale closures when polling reads worker status
  const workerRef = useRef(worker)
  useEffect(() => {
    workerRef.current = worker
  })

  // Fetch worker info, polling while active
  useEffect(() => {
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    const controller = new AbortController()

    async function fetchWorker() {
      let match: WorkerInfo | undefined
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
        match = workers.find(w => w.id === workerId)
        setWorker(match ?? null)
        setError(null)
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        if (!cancelled) {
          setLoading(false)
          // Only continue polling while the worker is found and still active.
          // If match is undefined the worker wasn't found — stop polling.
          const stillActive =
            match !== undefined &&
            (match.status === 'pending' || match.status === 'running')
          if (stillActive) {
            timeoutId = setTimeout(() => void fetchWorker(), 5000)
          }
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

  // Fetch parsed logs, polling while worker is active
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
          // Read from ref to get fresh status (avoids stale closure)
          const currentStatus = workerRef.current?.status
          const isActive = currentStatus === 'pending' || currentStatus === 'running'
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

  // Live events via SSE + polling fallback (same as the Mezzanine dashboard)
  const { events: allEvents } = useForgeEvents()

  // Fetch bead cost via dedicated per-bead endpoint
  const beadId = worker?.bead_id
  useEffect(() => {
    if (!beadId) return
    let cancelled = false
    const controller = new AbortController()

    async function fetchCost() {
      try {
        const res = await fetch(
          `/api/forge/costs/beads/${encodeURIComponent(beadId!)}`,
          { credentials: 'include', signal: controller.signal },
        )
        if (cancelled) return
        if (!res.ok) return
        const data: unknown = await res.json()
        if (cancelled || typeof data !== 'object' || data === null) return
        setCost(data as BeadCost)
      } catch (err) {
        if (cancelled || (err instanceof Error && err.name === 'AbortError')) return
      }
    }

    void fetchCost()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [beadId])

  // Filter live events for this worker's bead
  const events = useMemo(() => {
    if (!beadId) return []
    return allEvents
      .filter(e => e.bead_id === beadId)
      .sort((a, b) => a.id - b.id)
  }, [allEvents, beadId])

  return { worker, logEntries, events, cost, loading, error }
}
