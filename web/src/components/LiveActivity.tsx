import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { Activity, Terminal, Cpu, CheckCircle, Check, X } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import type { WorkerInfo } from '../hooks/useForgeStatus'
import { CollapsiblePanelHeader } from './CollapsiblePanelHeader'
import { usePanelCollapse } from '../hooks/usePanelCollapse'

// Backend Event fields from /api/forge/activity/stream and /api/forge/events
export interface WorkerEvent {
  id: number
  timestamp: string
  type: string
  message: string
  bead_id?: string
  anvil?: string
  // Optional, derived fields used by the UI
  phase?: string
  bead?: string
  level?: string
}

// Structured log entry from /api/forge/workers/{id}/log/parsed
interface LogEntry {
  seq: number
  type: 'tool_use' | 'text' | 'think'
  name: string
  content: string
  status: 'success' | 'error' | ''
}

interface LiveActivityProps {
  selectedWorker: WorkerInfo | null
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
const MAX_LOG_ENTRIES = 500

function hasCodeFence(text: string): boolean {
  return text.includes('```')
}

function getSafeHref(href?: string): string | undefined {
  if (!href) return undefined

  try {
    // Support both absolute and relative URLs by providing a base
    const url = new URL(href, 'http://localhost')
    const protocol = url.protocol.toLowerCase()
    const allowedProtocols = ['http:', 'https:', 'mailto:']

    return allowedProtocols.includes(protocol) ? href : undefined
  } catch {
    // Malformed URLs are treated as unsafe
    return undefined
  }
}

const markdownLinkComponents = {
  a: ({ href, children }: React.AnchorHTMLAttributes<HTMLAnchorElement>) => {
    const safeHref = getSafeHref(typeof href === 'string' ? href : undefined)

    if (!safeHref) {
      // Render as plain text if the URL is not allowed
      return <span>{children}</span>
    }

    return (
      <a href={safeHref} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  },
}

function LogEntryRow({ entry, t }: { entry: LogEntry; t: TFunction<'forge'> }) {
  if (entry.type === 'tool_use') {
    return (
      <div className="py-1.5 px-4 flex flex-col gap-0.5">
        <div className="flex items-center gap-2">
          <span className="text-xs font-mono font-semibold text-purple-300 bg-purple-900/30 px-1.5 py-0.5 rounded">
            {entry.name}
          </span>
          {entry.status === 'success' && (
            <Check size={12} className="text-green-400 shrink-0" />
          )}
          {entry.status === 'error' && (
            <X size={12} className="text-red-400 shrink-0" />
          )}
        </div>
        {entry.content && (
          <pre className="text-xs text-gray-400 font-mono whitespace-pre-wrap break-all leading-relaxed pl-1">
            {entry.content}
          </pre>
        )}
      </div>
    )
  }

  if (entry.type === 'think') {
    return (
      <div className="py-1.5 px-4 flex flex-col gap-0.5">
        <span className="text-xs text-gray-500 italic font-medium">{t('liveActivity.logPrefixThink')}</span>
        {entry.content && (
          <p className="text-xs text-gray-500 italic whitespace-pre-wrap break-words leading-relaxed pl-1">
            {entry.content}
          </p>
        )}
      </div>
    )
  }

  // type === 'text'
  return (
    <div className="py-1.5 px-4 flex flex-col gap-0.5">
      <span className="text-xs text-gray-400 font-medium">{t('liveActivity.logPrefixText')}</span>
      {entry.content && (
        hasCodeFence(entry.content) ? (
          <div className="text-xs text-gray-300 prose prose-invert prose-sm max-w-none pl-1
            [&_code]:text-xs [&_code]:font-mono [&_pre]:bg-gray-950 [&_pre]:p-2 [&_pre]:rounded
            [&_pre]:overflow-x-auto [&_pre]:text-xs [&_p]:my-0.5 [&_p]:text-gray-300">
            <ReactMarkdown components={markdownLinkComponents}>{entry.content}</ReactMarkdown>
          </div>
        ) : (
          <p className="text-xs text-gray-300 whitespace-pre-wrap break-words leading-relaxed pl-1">
            {entry.content}
          </p>
        )
      )}
    </div>
  )
}

export default function LiveActivity({ selectedWorker }: LiveActivityProps) {
  const { t } = useTranslation('forge')
  const [isOpen, toggle] = usePanelCollapse('live-activity')

  const [events, setEvents] = useState<WorkerEvent[]>([])
  const [logEntries, setLogEntries] = useState<LogEntry[]>([])
  const [eventUserScrolledUp, setEventUserScrolledUp] = useState(false)
  const [logUserScrolledUp, setLogUserScrolledUp] = useState(false)
  const [showPolls, setShowPolls] = useState(false)

  const eventBottomRef = useRef<HTMLDivElement>(null)
  const logBottomRef = useRef<HTMLDivElement>(null)
  const eventContainerRef = useRef<HTMLDivElement>(null)
  const logContainerRef = useRef<HTMLDivElement>(null)
  const pollingIntervalRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const esRef = useRef<EventSource | null>(null)
  const fallbackActiveRef = useRef(false)
  const lastSeenIdRef = useRef<number>(0)
  const logFetchingRef = useRef(false)

  // Derive worker details from the selectedWorker prop
  const isWorkerCompleted = selectedWorker
    ? selectedWorker.status !== 'pending' && selectedWorker.status !== 'running'
    : false
  const activeWorkerId =
    selectedWorker && !isWorkerCompleted ? selectedWorker.id : null
  const currentPhase = selectedWorker?.phase ?? ''
  const currentBead = selectedWorker?.bead_id ?? ''

  const visibleEvents = useMemo(
    () => (showPolls ? events : events.filter(e => e.type !== 'poll')),
    [events, showPolls]
  )

  const applyEvents = useCallback((incoming: WorkerEvent[]) => {
    if (incoming.length === 0) return
    setEvents(prev => [...prev, ...incoming].slice(-200))
  }, [])

  // SSE connection with polling fallback — paused when panel is collapsed
  useEffect(() => {
    if (!isOpen) return

    function startPolling() {
      if (fallbackActiveRef.current) return
      fallbackActiveRef.current = true
      pollingIntervalRef.current = setInterval(() => {
        fetch('/api/forge/events', { credentials: 'include' })
          .then(res => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
          .then((data: unknown) => {
            if (!Array.isArray(data) || data.length === 0) return
            // Events are returned newest-first; filter to only those newer than
            // last seen, then sort oldest-first before appending to avoid duplicates
            // and scrambled ordering.
            const newer = (data as WorkerEvent[]).filter(e => e.id > lastSeenIdRef.current)
            if (newer.length === 0) return
            const sorted = [...newer].sort((a, b) => a.id - b.id)
            lastSeenIdRef.current = sorted[sorted.length - 1].id
            applyEvents(sorted)
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
          if (event.id > lastSeenIdRef.current) {
            lastSeenIdRef.current = event.id
            applyEvents([event])
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

    return () => {
      esRef.current?.close()
      esRef.current = null
      fallbackActiveRef.current = false
      if (pollingIntervalRef.current !== undefined) {
        clearInterval(pollingIntervalRef.current)
        pollingIntervalRef.current = undefined
      }
    }
  }, [applyEvents, isOpen])

  // Poll worker log every 2 seconds when activeWorkerId is known and panel is open
  useEffect(() => {
    if (!activeWorkerId || !isOpen) return

    const controller = new AbortController()
    // Prevents a fetch that completed just before cleanup from updating state
    // for a previous worker (race between abort and .then() resolution).
    let cancelled = false

    const fetchLog = () => {
      // Skip this tick if a previous fetch is still in-flight to avoid
      // overlapping requests and out-of-order updates on slow networks.
      if (logFetchingRef.current) return
      logFetchingRef.current = true
      fetch(
        `/api/forge/workers/${encodeURIComponent(activeWorkerId)}/log/parsed?tail=${MAX_LOG_ENTRIES}`,
        {
          credentials: 'include',
          signal: controller.signal,
        }
      )
        .then(res => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
        .then((data: unknown) => {
          if (cancelled) return
          if (Array.isArray(data)) {
            const knownTypes = new Set<string>(['tool_use', 'text', 'think'])
            const validEntries: LogEntry[] = data
              .filter((item): item is Record<string, unknown> =>
                item !== null && typeof item === 'object'
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
            // Always apply the latest entries from the server so that changes
            // to any item (not just the last one) and any field (e.g. status)
            // are reflected in the UI.
            setLogEntries(validEntries)
          }
        })
        .catch((err: unknown) => {
          if (err instanceof Error && err.name === 'AbortError') return
          // ignore transient errors — poll will retry
        })
        .finally(() => {
          logFetchingRef.current = false
        })
    }

    fetchLog()
    const interval = setInterval(fetchLog, 2000)

    return () => {
      cancelled = true
      clearInterval(interval)
      controller.abort()
      setLogEntries([])
      setLogUserScrolledUp(false)
    }
  }, [activeWorkerId, isOpen])

  // Auto-scroll event log unless user scrolled up
  useEffect(() => {
    if (!eventUserScrolledUp) {
      eventBottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [visibleEvents, eventUserScrolledUp])

  // Auto-scroll log output unless user scrolled up
  useEffect(() => {
    if (!logUserScrolledUp) {
      logBottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [logEntries, logUserScrolledUp])

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
      <CollapsiblePanelHeader
        isOpen={isOpen}
        toggle={toggle}
        panelId="live-activity-panel"
        icon={<Activity size={18} className="text-blue-400 shrink-0" />}
        title={t('liveActivity.title')}
        trailing={
          <>
            {isWorkerCompleted && (
              <span className="flex items-center gap-1 text-xs text-green-400 bg-green-900/20 px-2 py-0.5 rounded">
                <CheckCircle size={12} />
                {t('liveActivity.completedWorker')}
              </span>
            )}
            {currentBead && (
              <span className="text-xs font-mono text-gray-400 bg-gray-700/50 px-2 py-0.5 rounded truncate max-w-[160px]">
                {currentBead}
              </span>
            )}
          </>
        }
      />

      <div id="live-activity-panel" hidden={!isOpen} className="flex flex-col">
      {/* Current phase status bar */}
      {(currentPhase || currentBead) && (
        <div className="flex items-center gap-2 px-5 py-2 bg-gray-900/30 border-b border-gray-700/30">
          <Cpu size={14} className="text-amber-400 shrink-0" />
          <span className="text-xs text-gray-400">{t('liveActivity.phase')}:</span>
          <span className="text-xs text-amber-300 font-medium">{currentPhase || '—'}</span>
        </div>
      )}

      {/* Parsed worker log output panel */}
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
            className="max-h-80 overflow-y-auto bg-gray-950 divide-y divide-gray-800/60"
          >
            {logEntries.length === 0 ? (
              <p className="text-xs text-gray-600 py-3 px-4">{t('liveActivity.noOutput')}</p>
            ) : (
              logEntries.map((entry) => (
                <LogEntryRow key={entry.seq} entry={entry} t={t} />
              ))
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
            {t('liveActivity.eventCount', { total: visibleEvents.length })}
          </span>
        )}
        <button
          type="button"
          onClick={() => setShowPolls(p => !p)}
          className={`ml-auto text-xs transition-colors ${showPolls ? 'text-blue-400 hover:text-blue-300' : 'text-gray-600 hover:text-gray-400'}`}
          aria-pressed={showPolls}
          aria-label={t('liveActivity.showPolls')}
        >
          {t('liveActivity.showPolls')}
        </button>
        {eventUserScrolledUp && (
          <button
            type="button"
            onClick={scrollEventToBottom}
            className="text-xs text-blue-400 hover:text-blue-300 transition-colors"
          >
            {t('liveActivity.scrollToBottom')}
          </button>
        )}
      </div>

      {/* Event list */}
      {visibleEvents.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">
          {events.length > 0 ? t('liveActivity.allFiltered') : t('liveActivity.noEvents')}
        </p>
      ) : (
        <div
          ref={eventContainerRef}
          onScroll={handleEventScroll}
          className="max-h-64 overflow-y-auto divide-y divide-gray-700/40"
        >
          {visibleEvents.map((event, idx) => {
            const lk = (event.level || event.type || 'info').toLowerCase()
            const rowClass = levelClass[lk] ?? 'text-gray-300 bg-gray-900/10 border-l-2 border-gray-700'
            const badgeClass = levelBadgeClass[lk] ?? 'text-gray-400'
            return (
              <div
                key={`${event.id}-${idx}`}
                className={`px-5 py-2 flex flex-col gap-0.5 ${rowClass}`}
              >
                <div className="flex items-center gap-2">
                  <span className={`text-xs font-semibold uppercase tracking-wide ${badgeClass}`}>
                    {lk}
                  </span>
                  {event.bead_id && (
                    <span className="text-xs font-mono text-gray-500 truncate max-w-[100px]">
                      {event.bead_id}
                    </span>
                  )}
                  {event.anvil && (
                    <span className="text-xs text-gray-600 truncate">{event.anvil}</span>
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
    </div>
  )
}
