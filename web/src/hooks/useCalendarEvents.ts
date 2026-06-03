import { useCallback, useEffect, useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { type CalendarEvent, type ViewMode } from '../components/calendar/types'
import { buildEventsUrl } from './calendarUrl'

export interface SyncError {
  calendar_id: string
  message: string
}

interface EventsResponse {
  events?: CalendarEvent[]
  sync_errors?: SyncError[]
}

export interface UseCalendarEventsResult {
  events: CalendarEvent[]
  loading: boolean
  error: string | null
  syncErrors: SyncError[]
  /** Re-run the current query without changing inputs. Pass `true` to request a server-side sync. */
  refetch: (sync?: boolean) => Promise<void>
}

/**
 * Owns the calendar events data layer: fetch lifecycle, AbortController
 * plumbing, signal chaining, and the events/loading/error/syncErrors state.
 *
 * Changing `viewMode`, `rangeStart`, or `user` triggers a refetch and aborts
 * any in-flight request, so a stale response can never overwrite a newer one.
 * Unmounting aborts the pending request.
 */
export function useCalendarEvents(
  viewMode: ViewMode,
  rangeStart: Date,
  user: { id: number } | null,
): UseCalendarEventsResult {
  const { t } = useTranslation('common')

  const [events, setEvents] = useState<CalendarEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [syncErrors, setSyncErrors] = useState<SyncError[]>([])

  const fetchControllerRef = useRef<AbortController | null>(null)

  const fetchEvents = useCallback(async (sync = false, signal?: AbortSignal) => {
    if (!user) return
    // Cancel any previous in-flight fetch so stale responses can't overwrite newer results
    fetchControllerRef.current?.abort()
    const ctl = new AbortController()
    fetchControllerRef.current = ctl
    // Chain caller's signal (e.g. effect cleanup) so it propagates to our controller
    if (signal) signal.addEventListener('abort', () => ctl.abort(), { once: true })
    setLoading(true)
    try {
      const url = buildEventsUrl(viewMode, rangeStart, sync)
      const res = await fetch(url, { credentials: 'include', signal: ctl.signal })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data: EventsResponse = await res.json()
      setEvents(data.events ?? [])
      // sync_errors only appears on sync requests; clear on any fetch so the
      // chip vanishes after a successful sync or a plain reload.
      setSyncErrors(sync ? (data.sync_errors ?? []) : [])
      setError(null)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(t('calendar.errors.failedToLoad'))
      console.error('Failed to load calendar events:', err)
    } finally {
      if (!ctl.signal.aborted) setLoading(false)
    }
  }, [user, rangeStart, viewMode, t])

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- setLoading(true) inside fetchEvents is intentional to show the spinner as soon as a fetch starts
    void fetchEvents(false, controller.signal)
    return () => {
      controller.abort()
      fetchControllerRef.current?.abort()
    }
  }, [user, fetchEvents])

  const refetch = useCallback((sync = false) => fetchEvents(sync), [fetchEvents])

  return { events, loading, error, syncErrors, refetch }
}
