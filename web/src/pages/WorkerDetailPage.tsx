import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  ArrowLeft,
  Cpu,
  Terminal,
  DollarSign,
  Activity,
  Check,
  X,
  Square,
  Clock,
} from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import type { AnchorHTMLAttributes } from 'react'
import { useWorkerDetail } from '../hooks/useWorkerDetail'
import type { LogEntry } from '../hooks/useWorkerDetail'
import WorkerLogModal from '../components/WorkerLogModal'
import ConfirmDialog from '../components/ConfirmDialog'
import { formatTime, formatDateTime } from '../utils/formatDate'

const SCROLL_THRESHOLD = 20

const PHASE_ORDER = ['queue', 'schematic', 'smith', 'temper', 'warden', 'pr', 'merged']

function formatDuration(startedAt: string, completedAt?: string): string {
  const start = new Date(startedAt).getTime()
  if (isNaN(start)) return '—'
  const end = completedAt ? new Date(completedAt).getTime() : Date.now()
  if (isNaN(end)) return '—'
  const elapsed = Math.floor((end - start) / 1000)
  if (elapsed < 60) return `${elapsed}s`
  const mins = Math.floor(elapsed / 60)
  const secs = elapsed % 60
  if (mins < 60) return `${mins}m ${secs}s`
  const hours = Math.floor(mins / 60)
  const remainMins = mins % 60
  return `${hours}h ${remainMins}m`
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`
  return String(n)
}

function formatCost(v: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  }).format(v)
}

function getSafeHref(href?: string): string | undefined {
  if (!href) return undefined
  try {
    const url = new URL(href, 'http://localhost')
    const protocol = url.protocol.toLowerCase()
    return ['http:', 'https:', 'mailto:'].includes(protocol) ? href : undefined
  } catch {
    return undefined
  }
}

const markdownLinkComponents = {
  a: ({ href, children }: AnchorHTMLAttributes<HTMLAnchorElement>) => {
    const safeHref = getSafeHref(typeof href === 'string' ? href : undefined)
    if (!safeHref) return <span>{children}</span>
    return (
      <a href={safeHref} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  },
}

function hasCodeFence(text: string): boolean {
  return text.includes('```')
}

function classifyLevel(type: string, message: string, level?: string): 'success' | 'failure' | 'info' {
  const t = type?.toLowerCase() ?? ''
  const l = level?.toLowerCase() ?? ''
  const m = message?.toLowerCase() ?? ''
  if (l === 'error' || t.includes('fail') || t.includes('error') || m.includes('failed')) return 'failure'
  if (l === 'success' || t.includes('pass') || t.includes('merged') || t.includes('done') || t.includes('success') || t.includes('complete')) return 'success'
  return 'info'
}

const levelDotStyles: Record<string, string> = {
  success: 'bg-green-500',
  failure: 'bg-red-500',
  info: 'bg-blue-500',
}

export default function WorkerDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { t } = useTranslation('forge')
  const { worker, logEntries, events, cost, loading, error } = useWorkerDetail(id ?? '')
  const [logModalOpen, setLogModalOpen] = useState(false)
  const [killing, setKilling] = useState(false)
  const [confirmKill, setConfirmKill] = useState(false)
  const [userScrolledUp, setUserScrolledUp] = useState(false)
  const [, setTick] = useState(0)
  const logContainerRef = useRef<HTMLDivElement>(null)

  const isActive = worker?.status === 'pending' || worker?.status === 'running'

  // Tick for duration display
  useEffect(() => {
    if (!isActive) return
    const interval = setInterval(() => setTick(n => n + 1), 1000)
    return () => clearInterval(interval)
  }, [isActive])

  // Auto-scroll log
  useEffect(() => {
    if (!userScrolledUp) {
      const el = logContainerRef.current
      if (el) el.scrollTop = el.scrollHeight
    }
  }, [logEntries, userScrolledUp])

  const handleLogScroll = useCallback(() => {
    const el = logContainerRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop <= el.clientHeight + SCROLL_THRESHOLD
    setUserScrolledUp(!atBottom)
  }, [])

  async function handleKill() {
    if (!worker) return
    setConfirmKill(false)
    setKilling(true)
    try {
      const res = await fetch(`/api/forge/workers/${encodeURIComponent(worker.id)}/kill`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        console.error((data as { error?: string }).error ?? `HTTP ${res.status}`)
      }
    } catch {
      // kill failed silently
    } finally {
      setKilling(false)
    }
  }

  // Derive phase timeline from events
  const phaseTimeline = (() => {
    if (!worker) return []
    const phases: { phase: string; timestamp: string }[] = []
    const seen = new Set<string>()

    // Add started_at as the initial phase
    if (worker.started_at) {
      const initialPhase = 'queue'
      phases.push({ phase: initialPhase, timestamp: worker.started_at })
      seen.add(initialPhase)
    }

    // Extract phases from events
    for (const event of events) {
      if (event.phase && !seen.has(event.phase)) {
        seen.add(event.phase)
        phases.push({ phase: event.phase, timestamp: event.timestamp })
      }
    }

    // Ensure current phase is included
    if (worker.phase && !seen.has(worker.phase)) {
      phases.push({ phase: worker.phase, timestamp: worker.updated_at ?? worker.started_at })
    }

    // Sort by known phase order
    phases.sort((a, b) => {
      const ai = PHASE_ORDER.indexOf(a.phase)
      const bi = PHASE_ORDER.indexOf(b.phase)
      if (ai !== -1 && bi !== -1) return ai - bi
      if (ai !== -1) return -1
      if (bi !== -1) return 1
      return 0
    })

    return phases
  })()

  const currentPhaseIndex = worker?.phase ? PHASE_ORDER.indexOf(worker.phase) : -1

  if (loading) {
    return (
      <div className="p-4 sm:p-6 max-w-5xl mx-auto">
        <div className="animate-pulse space-y-6">
          <div className="h-8 bg-gray-800 rounded w-48" />
          <div className="h-24 bg-gray-800 rounded-lg" />
          <div className="h-64 bg-gray-800 rounded-lg" />
        </div>
      </div>
    )
  }

  if (error && !worker) {
    return (
      <div className="p-4 sm:p-6 max-w-5xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Link to="/forge/mezzanine" className="text-gray-400 hover:text-white" aria-label={t('workerDetail.backToMezzanine')}>
            <ArrowLeft size={20} />
          </Link>
          <h1 className="text-xl font-bold text-white">{t('workerDetail.title')}</h1>
        </div>
        <p className="text-gray-400">{error}</p>
      </div>
    )
  }

  if (!worker) {
    return (
      <div className="p-4 sm:p-6 max-w-5xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Link to="/forge/mezzanine" className="text-gray-400 hover:text-white" aria-label={t('workerDetail.backToMezzanine')}>
            <ArrowLeft size={20} />
          </Link>
          <h1 className="text-xl font-bold text-white">{t('workerDetail.title')}</h1>
        </div>
        <p className="text-gray-400">{t('workerDetail.notFound')}</p>
      </div>
    )
  }

  const statusColor = worker.status === 'running' || worker.status === 'pending'
    ? 'text-green-400'
    : worker.status === 'failed'
      ? 'text-red-400'
      : 'text-gray-400'

  return (
    <div className="p-4 sm:p-6 max-w-5xl mx-auto space-y-6">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Link to="/forge/mezzanine" className="text-gray-400 hover:text-white" aria-label={t('workerDetail.backToMezzanine')}>
          <ArrowLeft size={20} />
        </Link>
        <Cpu size={20} className="text-amber-400" />
        <h1 className="text-xl font-bold text-white truncate">
          {worker.bead_id}
        </h1>
        {isActive && (
          <button
            type="button"
            onClick={() => setConfirmKill(true)}
            disabled={killing}
            aria-label={t('workers.killLabel', { id: worker.bead_id })}
            className="ml-auto flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm transition-colors
              bg-red-600/20 text-red-400 border border-red-600/30
              hover:bg-red-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Square size={14} />
            <span className="hidden sm:inline">{t('workers.kill')}</span>
          </button>
        )}
      </div>

      {/* Worker info card */}
      <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-4">
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
          <div>
            <span className="text-xs text-gray-500">{t('workerDetail.status')}</span>
            <p className={`font-medium capitalize ${statusColor}`}>{worker.status}</p>
          </div>
          <div>
            <span className="text-xs text-gray-500">{t('workerDetail.phase')}</span>
            <p className="text-amber-300 font-medium capitalize">{worker.phase || '—'}</p>
          </div>
          <div>
            <span className="text-xs text-gray-500">{t('workerDetail.anvil')}</span>
            <p className="text-gray-200">{worker.anvil}</p>
          </div>
          <div>
            <span className="text-xs text-gray-500">{t('workerDetail.duration')}</span>
            <p className="text-gray-200 tabular-nums">
              {formatDuration(worker.started_at, worker.completed_at)}
            </p>
          </div>
          {worker.title && (
            <div className="col-span-2 sm:col-span-4">
              <span className="text-xs text-gray-500">{t('workerDetail.beadTitle')}</span>
              <p className="text-gray-200 truncate">{worker.title}</p>
            </div>
          )}
          <div>
            <span className="text-xs text-gray-500">{t('workerDetail.started')}</span>
            <p className="text-gray-400 text-xs">{formatDateTime(worker.started_at)}</p>
          </div>
          {worker.completed_at && (
            <div>
              <span className="text-xs text-gray-500">{t('workerDetail.completed')}</span>
              <p className="text-gray-400 text-xs">{formatDateTime(worker.completed_at)}</p>
            </div>
          )}
          {worker.pr_number > 0 && (
            <div>
              <span className="text-xs text-gray-500">{t('workerDetail.pr')}</span>
              <p className="text-blue-400">#{worker.pr_number}</p>
            </div>
          )}
          <div>
            <span className="text-xs text-gray-500">{t('workerDetail.branch')}</span>
            <p className="text-gray-400 font-mono text-xs truncate">{worker.branch}</p>
          </div>
        </div>
      </div>

      {/* Phase Timeline */}
      <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-4">
        <h2 className="text-sm font-semibold text-gray-200 mb-4 flex items-center gap-1.5">
          <Clock size={14} />
          {t('workerDetail.phaseTimeline')}
        </h2>

        <div className="flex items-center gap-0 overflow-x-auto pb-2">
          {PHASE_ORDER.map((phase, i) => {
            const phaseEntry = phaseTimeline.find(p => p.phase === phase)
            const isCurrent = worker.phase === phase
            const isPast = currentPhaseIndex >= 0 && PHASE_ORDER.indexOf(phase) < currentPhaseIndex
            const isReached = isCurrent || isPast || !!phaseEntry

            return (
              <div key={phase} className="flex items-center">
                {i > 0 && (
                  <div className={`w-6 sm:w-10 h-0.5 ${isPast ? 'bg-green-500' : isCurrent ? 'bg-amber-500' : 'bg-gray-700'}`} />
                )}
                <div className="flex flex-col items-center gap-1 min-w-[56px] sm:min-w-[72px]">
                  <div
                    className={`w-6 h-6 rounded-full flex items-center justify-center text-xs font-medium border-2 ${
                      isCurrent
                        ? 'bg-amber-500/20 border-amber-500 text-amber-400'
                        : isPast
                          ? 'bg-green-500/20 border-green-500 text-green-400'
                          : isReached
                            ? 'bg-blue-500/20 border-blue-500 text-blue-400'
                            : 'bg-gray-800 border-gray-600 text-gray-600'
                    }`}
                  >
                    {isPast ? <Check size={12} /> : (i + 1)}
                  </div>
                  <span className={`text-xs capitalize ${isCurrent ? 'text-amber-400 font-medium' : isReached ? 'text-gray-300' : 'text-gray-600'}`}>
                    {t(`mezzanine.pipeline.stages.${phase}`, phase)}
                  </span>
                  {phaseEntry && (
                    <span className="text-[10px] text-gray-500 tabular-nums">
                      {formatTime(phaseEntry.timestamp, { hour: '2-digit', minute: '2-digit' })}
                    </span>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </div>

      {/* Token Usage & Cost */}
      {cost && (
        <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-4">
          <h2 className="text-sm font-semibold text-gray-200 mb-3 flex items-center gap-1.5">
            <DollarSign size={14} />
            {t('workerDetail.tokenUsage')}
          </h2>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-4">
            <div>
              <span className="text-xs text-gray-500">{t('workerDetail.totalCost')}</span>
              <p className="text-lg font-semibold text-white">{formatCost(cost.estimated_cost)}</p>
            </div>
            <div>
              <span className="text-xs text-gray-500">{t('workerDetail.inputTokens')}</span>
              <p className="text-gray-200 font-medium">{formatTokens(cost.input_tokens)}</p>
            </div>
            <div>
              <span className="text-xs text-gray-500">{t('workerDetail.outputTokens')}</span>
              <p className="text-gray-200 font-medium">{formatTokens(cost.output_tokens)}</p>
            </div>
            <div>
              <span className="text-xs text-gray-500">{t('workerDetail.cacheRead')}</span>
              <p className="text-gray-200 font-medium">{formatTokens(cost.cache_read)}</p>
            </div>
            <div>
              <span className="text-xs text-gray-500">{t('workerDetail.cacheWrite')}</span>
              <p className="text-gray-200 font-medium">{formatTokens(cost.cache_write)}</p>
            </div>
          </div>
        </div>
      )}

      {/* Parsed Log Output */}
      <div className="bg-gray-800 rounded-lg border border-gray-700/50 overflow-hidden">
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-700/50">
          <h2 className="text-sm font-semibold text-gray-200 flex items-center gap-1.5">
            <Terminal size={14} />
            {t('workerDetail.log')}
            <span className="text-xs text-gray-500 font-normal ml-1">
              ({logEntries.length})
            </span>
          </h2>
          <div className="flex items-center gap-2">
            {userScrolledUp && isActive && (
              <button
                type="button"
                onClick={() => {
                  setUserScrolledUp(false)
                  const el = logContainerRef.current
                  if (el) el.scrollTop = el.scrollHeight
                }}
                className="text-xs text-blue-400 hover:text-blue-300 transition-colors"
              >
                {t('liveActivity.scrollToBottom')}
              </button>
            )}
            <button
              type="button"
              onClick={() => setLogModalOpen(true)}
              className="text-xs text-blue-400 hover:text-blue-300 hover:underline transition-colors"
            >
              {t('workerDetail.viewRawLog')}
            </button>
          </div>
        </div>
        <div
          ref={logContainerRef}
          onScroll={handleLogScroll}
          role="log"
          aria-live="polite"
          className="max-h-96 overflow-y-auto bg-gray-950 divide-y divide-gray-800/60"
        >
          {logEntries.length === 0 ? (
            <p className="text-xs text-gray-600 py-3 px-4">{t('liveActivity.noOutput')}</p>
          ) : (
            logEntries.map(entry => (
              <LogEntryRow key={entry.seq} entry={entry} t={t} />
            ))
          )}
        </div>
      </div>

      {/* Related Events */}
      <div className="bg-gray-800 rounded-lg border border-gray-700/50 overflow-hidden">
        <div className="px-4 py-3 border-b border-gray-700/50">
          <h2 className="text-sm font-semibold text-gray-200 flex items-center gap-1.5">
            <Activity size={14} />
            {t('workerDetail.relatedEvents')}
            <span className="text-xs text-gray-500 font-normal ml-1">
              ({events.length})
            </span>
          </h2>
        </div>
        <div className="max-h-64 overflow-y-auto">
          {events.length === 0 ? (
            <p className="px-4 py-4 text-sm text-gray-500 text-center">
              {t('workerDetail.noEvents')}
            </p>
          ) : (
            <ul className="divide-y divide-gray-800/50">
              {events.map(event => {
                const level = classifyLevel(event.type, event.message, event.level)
                return (
                  <li key={event.id} className="px-4 py-2 text-sm">
                    <div className="flex items-start gap-2">
                      <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${levelDotStyles[level]}`} />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-baseline gap-2">
                          <span className="text-xs text-gray-500 tabular-nums shrink-0">
                            {formatTime(event.timestamp, { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                          </span>
                          <span className="text-xs font-medium text-gray-400 shrink-0">
                            {event.type}
                          </span>
                          {event.phase && (
                            <span className="text-xs text-amber-400/70 capitalize shrink-0">
                              {event.phase}
                            </span>
                          )}
                        </div>
                        <p className="text-gray-300 truncate">{event.message}</p>
                      </div>
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </div>
      </div>

      <WorkerLogModal
        open={logModalOpen}
        onClose={() => setLogModalOpen(false)}
        workerId={worker.id}
        beadId={worker.bead_id}
      />

      <ConfirmDialog
        open={confirmKill}
        title={t('workers.killConfirmTitle')}
        message={t('workers.killConfirmMessage', { id: worker.bead_id })}
        confirmLabel={t('workers.kill')}
        destructive
        onConfirm={() => void handleKill()}
        onCancel={() => setConfirmKill(false)}
      />
    </div>
  )
}

function LogEntryRow({ entry, t }: { entry: LogEntry; t: (key: string) => string }) {
  if (entry.type === 'tool_use') {
    return (
      <div className="py-1.5 px-4 flex flex-col gap-0.5">
        <div className="flex items-center gap-2">
          <span className="text-xs font-mono font-semibold text-purple-300 bg-purple-900/30 px-1.5 py-0.5 rounded">
            {entry.name}
          </span>
          {entry.status === 'success' && <Check size={12} className="text-green-400 shrink-0" />}
          {entry.status === 'error' && <X size={12} className="text-red-400 shrink-0" />}
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
