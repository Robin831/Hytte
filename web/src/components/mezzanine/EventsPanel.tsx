import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Filter } from 'lucide-react'
import type { WorkerEvent } from '../LiveActivity'

type EventFilter = 'all' | 'errors' | 'prs' | string

interface EventsPanelProps {
  onBeadClick?: (beadId: string) => void
}

function classifyLevel(event: WorkerEvent): 'success' | 'failure' | 'info' {
  const type = event.type?.toLowerCase() ?? ''
  const level = event.level?.toLowerCase() ?? ''
  const message = event.message?.toLowerCase() ?? ''

  if (level === 'error' || type.includes('fail') || type.includes('error') || message.includes('failed')) {
    return 'failure'
  }
  if (
    level === 'success' ||
    type.includes('pass') ||
    type.includes('merged') ||
    type.includes('done') ||
    type.includes('success') ||
    type.includes('complete')
  ) {
    return 'success'
  }
  return 'info'
}

const levelStyles: Record<string, string> = {
  success: 'border-l-2 border-green-500 bg-green-900/10',
  failure: 'border-l-2 border-red-500 bg-red-900/10',
  info: 'border-l-2 border-blue-500 bg-blue-900/10',
}

const levelDotStyles: Record<string, string> = {
  success: 'bg-green-500',
  failure: 'bg-red-500',
  info: 'bg-blue-500',
}

const MAX_EVENTS = 100

export default function EventsPanel({ onBeadClick }: EventsPanelProps) {
  const { t, i18n } = useTranslation('forge')
  const [events, setEvents] = useState<WorkerEvent[]>([])
  const [filter, setFilter] = useState<EventFilter>('all')
  const lastSeenIdRef = useRef(0)
  const esRef = useRef<EventSource | null>(null)
  const pollingRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const fallbackActiveRef = useRef(false)

  const appendEvents = useCallback((newEvents: WorkerEvent[]) => {
    setEvents(prev => {
      const combined = [...newEvents, ...prev]
      return combined.slice(0, MAX_EVENTS)
    })
  }, [])

  useEffect(() => {
    function startPolling() {
      if (fallbackActiveRef.current) return
      fallbackActiveRef.current = true
      pollingRef.current = setInterval(() => {
        fetch('/api/forge/events?limit=50', { credentials: 'include' })
          .then(res => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
          .then((data: unknown) => {
            if (!Array.isArray(data) || data.length === 0) return
            const newer = (data as WorkerEvent[]).filter(e => e.id > lastSeenIdRef.current)
            if (newer.length === 0) return
            const sorted = [...newer].sort((a, b) => a.id - b.id)
            lastSeenIdRef.current = sorted[sorted.length - 1].id
            appendEvents(sorted.reverse())
          })
          .catch(() => {})
      }, 3000)
    }

    try {
      const es = new EventSource('/api/forge/activity/stream')
      esRef.current = es
      es.onmessage = (e: MessageEvent<string>) => {
        try {
          const event = JSON.parse(e.data) as WorkerEvent
          if (event.id > lastSeenIdRef.current) {
            lastSeenIdRef.current = event.id
            appendEvents([event])
          }
        } catch {
          // ignore unparseable SSE data
        }
      }
      es.onerror = () => {
        es.close()
        esRef.current = null
        startPolling()
      }
    } catch {
      startPolling()
    }

    // Also do an initial fetch to populate existing events
    fetch('/api/forge/events?limit=50', { credentials: 'include' })
      .then(res => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
      .then((data: unknown) => {
        if (!Array.isArray(data) || data.length === 0) return
        const sorted = [...(data as WorkerEvent[])].sort((a, b) => b.id - a.id)
        if (sorted.length > 0) {
          lastSeenIdRef.current = Math.max(lastSeenIdRef.current, sorted[0].id)
        }
        setEvents(sorted.slice(0, MAX_EVENTS))
      })
      .catch(() => {})

    return () => {
      esRef.current?.close()
      esRef.current = null
      fallbackActiveRef.current = false
      if (pollingRef.current !== undefined) {
        clearInterval(pollingRef.current)
        pollingRef.current = undefined
      }
    }
  }, [appendEvents])

  const anvils = useMemo(() => {
    const set = new Set<string>()
    for (const e of events) {
      if (e.anvil) set.add(e.anvil)
    }
    return [...set].sort()
  }, [events])

  const filtered = useMemo(() => {
    if (filter === 'all') return events
    if (filter === 'errors') return events.filter(e => classifyLevel(e) === 'failure')
    if (filter === 'prs') {
      return events.filter(e => {
        const type = e.type?.toLowerCase() ?? ''
        return type.includes('pr') || type.includes('merge') || type.includes('warden') || type.includes('review')
      })
    }
    // per-anvil filter
    return events.filter(e => e.anvil === filter)
  }, [events, filter])

  const formatTime = useCallback((timestamp: string) => {
    try {
      return new Intl.DateTimeFormat(i18n.language, {
        hour: '2-digit',
        minute: '2-digit',
      }).format(new Date(timestamp))
    } catch {
      return timestamp
    }
  }, [i18n.language])

  return (
    <div className="flex flex-col rounded-lg border border-gray-700/50 bg-gray-900/60 overflow-hidden">
      <div className="flex items-center justify-between px-3 py-2 border-b border-gray-700/50">
        <h3 className="text-sm font-semibold text-gray-200">{t('mezzanine.events.title')}</h3>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Filter size={14} className="absolute left-2 top-1/2 -translate-y-1/2 text-gray-500 pointer-events-none" />
            <select
              value={filter}
              onChange={e => setFilter(e.target.value)}
              className="pl-7 pr-2 py-1 text-xs rounded bg-gray-800 border border-gray-700 text-gray-300 appearance-none cursor-pointer focus:outline-none focus:ring-1 focus:ring-blue-500"
              aria-label={t('mezzanine.events.filterLabel')}
            >
              <option value="all">{t('mezzanine.events.filterAll')}</option>
              <option value="errors">{t('mezzanine.events.filterErrors')}</option>
              <option value="prs">{t('mezzanine.events.filterPRs')}</option>
              {anvils.map(anvil => (
                <option key={anvil} value={anvil}>{anvil}</option>
              ))}
            </select>
          </div>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto max-h-64 md:max-h-80">
        {filtered.length === 0 ? (
          <p className="px-3 py-4 text-sm text-gray-500 text-center">
            {t('mezzanine.events.empty')}
          </p>
        ) : (
          <ul className="divide-y divide-gray-800/50">
            {filtered.map(event => {
              const level = classifyLevel(event)
              const clickable = !!event.bead_id && !!onBeadClick
              return (
                <li
                  key={event.id}
                  className={[
                    'px-3 py-2 text-sm',
                    levelStyles[level],
                    clickable ? 'cursor-pointer hover:bg-gray-800/40' : '',
                  ].join(' ')}
                  onClick={clickable ? () => onBeadClick(event.bead_id!) : undefined}
                  role={clickable ? 'button' : undefined}
                  tabIndex={clickable ? 0 : undefined}
                  onKeyDown={clickable ? (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onBeadClick(event.bead_id!) } } : undefined}
                >
                  <div className="flex items-start gap-2">
                    <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${levelDotStyles[level]}`} />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-baseline gap-2">
                        <span className="text-xs text-gray-500 tabular-nums shrink-0">
                          {formatTime(event.timestamp)}
                        </span>
                        <span className="text-xs font-medium text-gray-400 shrink-0">
                          {event.type}
                        </span>
                        {event.anvil && (
                          <span className="text-xs text-gray-600 shrink-0">
                            {event.anvil}
                          </span>
                        )}
                      </div>
                      <p className="text-gray-300 truncate">{event.message}</p>
                      {event.bead_id && (
                        <span className="text-xs text-blue-400">{event.bead_id}</span>
                      )}
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </div>

      <div className="px-3 py-2 border-t border-gray-700/50">
        <a
          href="/forge"
          className="text-xs text-blue-400 hover:text-blue-300 hover:underline"
        >
          {t('mezzanine.events.viewAll')}
        </a>
      </div>
    </div>
  )
}
