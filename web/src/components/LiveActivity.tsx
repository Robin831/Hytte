import { useState, useEffect, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Activity, Terminal, Cpu } from 'lucide-react'

export interface WorkerEvent {
  type: string
  phase: string
  bead: string
  message: string
  timestamp: string
  level: string
  worker_id?: string
}

const levelClass: Record<string, string> = {
  error: 'text-red-400 bg-red-900/20 border-l-2 border-red-600',
  warn: 'text-amber-400 bg-amber-900/20 border-l-2 border-amber-600',
  warning: 'text-amber-400 bg-amber-900/20 border-l-2 border-amber-600',
  info: 'text-blue-400 bg-blue-900/20 border-l-2 border-blue-600',
  debug: 'text-gray-400 bg-gray-900/10 border-l-2 border-gray-600',
  success: 'text-green-400 bg-green-900/20 border-l-2 border-green-600',
}

const levelBadgeClass: Record<string, string> = {
  error: 'text-red-400',
  warn: 'text-amber-400',
  warning: 'text-amber-400',
  info: 'text-blue-400',
  debug: 'text-gray-500',
  success: 'text-green-400',
}

const SCROLL_THRESHOLD = 20

export default function LiveActivity() {
  const { t } = useTranslation('forge')

  const [events, setEvents] = useState<WorkerEvent[]>([])
  const [currentPhase, setCurrentPhase] = useState<string>('')
  const [currentBead, setCurrentBead] = useState<string>('')
  const [logLines, setLogLines] = useState<string[]>([])
  const [activeWorkerId, setActiveWorkerId] = useState<string | null>(null)
  const [eventUserScrolledUp, setEventUserScrolledUp] = useState(false)
  const [logUserScrolledUp, setLogUserScrolledUp] = useState(false)

  const eventBottomRef = useRef<HTMLDivElement>(null)
  const logBottomRef = useRef<HTMLDivElement>(null)
  const eventContainerRef = useRef<HTMLDivElement>(null)
  const logContainerRef = useRef<HTMLDivElement>(null)
  const pollingIntervalRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const logPollingIntervalRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const esRef = useRef<EventSource | null>(null)
  const fallbackActiveRef = useRef(false)

  const applyEvents = useCallback((incoming: WorkerEvent[]) => {
    if (incoming.length === 0) return
    setEvents(prev => [...prev, ...incoming].slice(-200))
    const latest = incoming[incoming.length - 1]
    if (latest.phase) setCurrentPhase(latest.phase)
    if (latest.bead) setCurrentBead(latest.bead)
    if (latest.worker_id) setActiveWorkerId(latest.worker_id)
  }, [])

  // SSE connection with polling fallback
  useEffect(() => {
    function startPolling() {
      if (fallbackActiveRef.current) return
      fallbackActiveRef.current = true
      pollingIntervalRef.current = setInterval(() => {
        fetch('/api/forge/events', { credentials: 'include' })
          .then(res => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
          .then((data: unknown) => {
            if (Array.isArray(data) && data.length > 0) {
              applyEvents(data as WorkerEvent[])
            }
          })
          .catch(() => {
            // ignore poll errors — transient failures are expected
          })
      }, 2000)
    }

    try {
      const es = new EventSource('/api/forge/activity/stream')
      esRef.current = es
      es.onmessage = (e: MessageEvent<string>) => {
        try {
          const event = JSON.parse(e.data) as WorkerEvent
          applyEvents([event])
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

    return () => {
      esRef.current?.close()
      esRef.current = null
      if (pollingIntervalRef.current !== undefined) {
        clearInterval(pollingIntervalRef.current)
      }
    }
  }, [applyEvents])

  // Poll worker log when activeWorkerId is known
  useEffect(() => {
    if (logPollingIntervalRef.current !== undefined) {
      clearInterval(logPollingIntervalRef.current)
      logPollingIntervalRef.current = undefined
    }
    if (!activeWorkerId) return

    function fetchLog() {
      fetch(`/api/forge/workers/${activeWorkerId}/log`, { credentials: 'include' })
        .then(res => (res.ok ? res.text() : Promise.reject(new Error(`HTTP ${res.status}`))))
        .then(text => {
          setLogLines(text.split('\n'))
        })
        .catch(() => {
          // ignore log fetch errors — worker may have finished
        })
    }

    fetchLog()
    logPollingIntervalRef.current = setInterval(fetchLog, 2000)

    return () => {
      if (logPollingIntervalRef.current !== undefined) {
        clearInterval(logPollingIntervalRef.current)
      }
    }
  }, [activeWorkerId])

  // Auto-scroll event log unless user scrolled up
  useEffect(() => {
    if (!eventUserScrolledUp) {
      eventBottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [events, eventUserScrolledUp])

  // Auto-scroll log output unless user scrolled up
  useEffect(() => {
    if (!logUserScrolledUp) {
      logBottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [logLines, logUserScrolledUp])

  function handleEventScroll() {
    const el = eventContainerRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop <= el.clientHeight + SCROLL_THRESHOLD
    setEventUserScrolledUp(!atBottom)
  }

  function handleLogScroll() {
    const el = logContainerRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop <= el.clientHeight + SCROLL_THRESHOLD
    setLogUserScrolledUp(!atBottom)
  }

  function scrollEventToBottom() {
    setEventUserScrolledUp(false)
    eventBottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  function scrollLogToBottom() {
    setLogUserScrolledUp(false)
    logBottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden flex flex-col">
      {/* Header */}
      <div className="flex items-center gap-2 px-5 py-4 border-b border-gray-700/50">
        <Activity size={18} className="text-blue-400 shrink-0" />
        <h2 className="text-sm font-medium text-gray-300">{t('liveActivity.title')}</h2>
        {currentBead && (
          <span className="ml-auto text-xs font-mono text-gray-400 bg-gray-700/50 px-2 py-0.5 rounded truncate max-w-[160px]">
            {currentBead}
          </span>
        )}
      </div>

      {/* Current phase status bar */}
      {(currentPhase || currentBead) && (
        <div className="flex items-center gap-2 px-5 py-2 bg-gray-900/30 border-b border-gray-700/30">
          <Cpu size={14} className="text-amber-400 shrink-0" />
          <span className="text-xs text-gray-400">{t('liveActivity.phase')}:</span>
          <span className="text-xs text-amber-300 font-medium">{currentPhase || '—'}</span>
        </div>
      )}

      {/* Worker log output panel */}
      {activeWorkerId && (
        <div className="flex flex-col border-b border-gray-700/50">
          <div className="flex items-center gap-2 px-5 py-2 bg-gray-900/20 border-b border-gray-700/20">
            <Terminal size={14} className="text-green-400 shrink-0" />
            <span className="text-xs text-gray-400">{t('liveActivity.workerOutput')}</span>
            {logUserScrolledUp && (
              <button
                type="button"
                onClick={scrollLogToBottom}
                className="ml-auto text-xs text-blue-400 hover:text-blue-300 transition-colors"
              >
                {t('liveActivity.scrollToBottom')}
              </button>
            )}
          </div>
          <div
            ref={logContainerRef}
            onScroll={handleLogScroll}
            className="max-h-48 overflow-y-auto bg-gray-950 px-4 py-2"
          >
            {logLines.length === 0 || (logLines.length === 1 && logLines[0] === '') ? (
              <p className="text-xs text-gray-600 py-2">{t('liveActivity.noOutput')}</p>
            ) : (
              <pre className="text-xs text-gray-300 font-mono whitespace-pre-wrap break-all leading-relaxed">
                {logLines.join('\n')}
              </pre>
            )}
            <div ref={logBottomRef} />
          </div>
        </div>
      )}

      {/* Event log sub-header */}
      <div className="flex items-center gap-2 px-5 py-2 bg-gray-900/10 border-b border-gray-700/20">
        <span className="text-xs text-gray-500">{t('liveActivity.eventLog')}</span>
        {events.length > 0 && (
          <span className="text-xs text-gray-600 ml-1">
            {t('liveActivity.eventCount', { count: events.length })}
          </span>
        )}
        {eventUserScrolledUp && (
          <button
            type="button"
            onClick={scrollEventToBottom}
            className="ml-auto text-xs text-blue-400 hover:text-blue-300 transition-colors"
          >
            {t('liveActivity.scrollToBottom')}
          </button>
        )}
      </div>

      {/* Event list */}
      {events.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('liveActivity.noEvents')}</p>
      ) : (
        <div
          ref={eventContainerRef}
          onScroll={handleEventScroll}
          className="max-h-64 overflow-y-auto divide-y divide-gray-700/40"
        >
          {events.map((event, idx) => {
            const lk = (event.level || event.type || 'info').toLowerCase()
            const rowClass = levelClass[lk] ?? 'text-gray-300 bg-gray-900/10 border-l-2 border-gray-700'
            const badgeClass = levelBadgeClass[lk] ?? 'text-gray-400'
            return (
              <div
                key={`${event.timestamp}-${lk}-${idx}`}
                className={`px-5 py-2 flex flex-col gap-0.5 ${rowClass}`}
              >
                <div className="flex items-center gap-2">
                  <span className={`text-xs font-semibold uppercase tracking-wide ${badgeClass}`}>
                    {lk}
                  </span>
                  {event.bead && (
                    <span className="text-xs font-mono text-gray-500 truncate max-w-[100px]">
                      {event.bead}
                    </span>
                  )}
                  {event.phase && (
                    <span className="text-xs text-gray-600 truncate">{event.phase}</span>
                  )}
                  <span className="ml-auto text-xs text-gray-500 shrink-0">
                    {(() => {
                      const d = new Date(event.timestamp)
                      if (isNaN(d.getTime())) return event.timestamp || '—'
                      return new Intl.DateTimeFormat(undefined, {
                        hour: '2-digit',
                        minute: '2-digit',
                        second: '2-digit',
                      }).format(d)
                    })()}
                  </span>
                </div>
                {event.message && (
                  <p className="text-sm text-gray-300 break-words">{event.message}</p>
                )}
              </div>
            )
          })}
          <div ref={eventBottomRef} />
        </div>
      )}
    </div>
  )
}
