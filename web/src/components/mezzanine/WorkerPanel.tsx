import { useState, useEffect, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { Cpu, Square, Terminal, Check, X } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import type { WorkerInfo } from '../../hooks/useForgeStatus'
import { CollapsiblePanelHeader } from '../CollapsiblePanelHeader'
import { usePanelCollapse } from '../../hooks/usePanelCollapse'
import ConfirmDialog from '../ConfirmDialog'

interface LogEntry {
  seq: number
  type: 'tool_use' | 'text' | 'think'
  name: string
  content: string
  status: 'success' | 'error' | ''
}

interface WorkerPanelProps {
  worker: WorkerInfo
  showToast: (message: string, type: 'success' | 'error') => void
  onBeadClick?: (beadId: string) => void
}

const MAX_LOG_ENTRIES = 500
const PREVIEW_LINES = 3
const SCROLL_THRESHOLD = 20

function formatDuration(startedAt: string): string {
  const start = new Date(startedAt).getTime()
  if (isNaN(start)) return '—'
  const elapsed = Math.floor((Date.now() - start) / 1000)
  if (elapsed < 60) return `${elapsed}s`
  const mins = Math.floor(elapsed / 60)
  const secs = elapsed % 60
  if (mins < 60) return `${mins}m ${secs}s`
  const hours = Math.floor(mins / 60)
  const remainMins = mins % 60
  return `${hours}h ${remainMins}m`
}

function hasCodeFence(text: string): boolean {
  return text.includes('```')
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
  a: ({ href, children }: React.AnchorHTMLAttributes<HTMLAnchorElement>) => {
    const safeHref = getSafeHref(typeof href === 'string' ? href : undefined)
    if (!safeHref) return <span>{children}</span>
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

function PreviewLines({ entries, t }: { entries: LogEntry[]; t: TFunction<'forge'> }) {
  const preview = entries.slice(-PREVIEW_LINES)
  if (preview.length === 0) {
    return (
      <p className="text-xs text-gray-600 px-4 py-2">{t('liveActivity.noOutput')}</p>
    )
  }
  return (
    <div className="divide-y divide-gray-800/40 opacity-70">
      {preview.map(entry => (
        <LogEntryRow key={entry.seq} entry={entry} t={t} />
      ))}
    </div>
  )
}

export default function WorkerPanel({ worker, showToast, onBeadClick }: WorkerPanelProps) {
  const { t } = useTranslation('forge')
  const panelId = `worker-panel-${worker.id}`
  const [isOpen, toggle] = usePanelCollapse(`mez-worker-${worker.id}`)
  const [logEntries, setLogEntries] = useState<LogEntry[]>([])
  const [killing, setKilling] = useState(false)
  const [confirmKill, setConfirmKill] = useState(false)
  const [userScrolledUp, setUserScrolledUp] = useState(false)
  const [duration, setDuration] = useState(() => formatDuration(worker.started_at))
  const logContainerRef = useRef<HTMLDivElement>(null)
  const logFetchingRef = useRef(false)

  const isActive = worker.status === 'pending' || worker.status === 'running'

  // Update duration every second for active workers, and reset when started_at changes
  useEffect(() => {
    setDuration(formatDuration(worker.started_at))
    if (!isActive) return
    const interval = setInterval(() => {
      setDuration(formatDuration(worker.started_at))
    }, 1000)
    return () => clearInterval(interval)
  }, [isActive, worker.started_at])

  // Poll parsed log
  useEffect(() => {
    if (!isActive) return

    const controller = new AbortController()
    let cancelled = false

    const fetchLog = () => {
      if (logFetchingRef.current) return
      logFetchingRef.current = true
      fetch(
        `/api/forge/workers/${encodeURIComponent(worker.id)}/log/parsed?tail=${MAX_LOG_ENTRIES}`,
        { credentials: 'include', signal: controller.signal }
      )
        .then(res => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
        .then((data: unknown) => {
          if (cancelled) return
          if (Array.isArray(data)) {
            const knownTypes = new Set(['tool_use', 'text', 'think'])
            const valid: LogEntry[] = data
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
            setLogEntries(valid)
          }
        })
        .catch((err: unknown) => {
          if (err instanceof Error && err.name === 'AbortError') return
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
    }
  }, [isActive, worker.id])

  // Auto-scroll when expanded
  useEffect(() => {
    if (!userScrolledUp && isOpen) {
      const el = logContainerRef.current
      if (el) el.scrollTop = el.scrollHeight
    }
  }, [logEntries, userScrolledUp, isOpen])

  const handleLogScroll = useCallback(() => {
    const el = logContainerRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop <= el.clientHeight + SCROLL_THRESHOLD
    setUserScrolledUp(!atBottom)
  }, [])

  async function handleKill() {
    setConfirmKill(false)
    setKilling(true)
    try {
      const res = await fetch(`/api/forge/workers/${encodeURIComponent(worker.id)}/kill`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t('workers.killSuccess', { id: worker.bead_id }), 'success')
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setKilling(false)
    }
  }

  const trailing = (
    <div className="flex items-center gap-2">
      <span className="text-xs text-gray-500 tabular-nums">{duration}</span>
      {isActive && (
        <button
          type="button"
          onClick={e => { e.stopPropagation(); setConfirmKill(true) }}
          disabled={killing}
          aria-label={t('workers.killLabel', { id: worker.bead_id })}
          className="flex items-center justify-center min-h-[32px] min-w-[32px] rounded-lg text-sm transition-colors
            bg-red-600/20 text-red-400 border border-red-600/30
            hover:bg-red-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Square size={14} />
        </button>
      )}
    </div>
  )

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden flex flex-col">
      <CollapsiblePanelHeader
        isOpen={isOpen}
        toggle={toggle}
        panelId={panelId}
        icon={<Cpu size={18} className="text-amber-400 shrink-0" />}
        title={
          <span className="flex items-center gap-2 min-w-0">
            <button
              type="button"
              onClick={e => { e.stopPropagation(); onBeadClick?.(worker.bead_id) }}
              aria-label={t('workers.viewBead', { id: worker.bead_id })}
              className="font-mono text-amber-400 hover:text-amber-300 hover:underline truncate transition-colors"
            >
              {worker.bead_id}
            </button>
            <span className="text-gray-600 hidden sm:inline">·</span>
            <span className="text-gray-500 truncate hidden sm:inline">{worker.anvil}</span>
          </span>
        }
        trailing={trailing}
        headingLevel={3}
      />

      <div id={panelId} hidden={!isOpen}>
        {/* Metadata row */}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-2 bg-gray-900/30 border-b border-gray-700/30 text-xs">
          <span className="text-gray-500">
            {t('workers.colPhase')}: <span className="text-amber-300 capitalize">{worker.phase || '—'}</span>
          </span>
          <span className="text-gray-500 sm:hidden">
            {t('mezzanine.workerPanel.anvil')}: <span className="text-gray-300">{worker.anvil || '—'}</span>
          </span>
          {worker.title && (
            <span className="text-gray-500 truncate max-w-[240px]" title={worker.title}>
              {worker.title}
            </span>
          )}
        </div>

        {/* Streaming output */}
        {isActive && (
          <div className="flex flex-col">
            <div className="flex items-center gap-2 px-5 py-2 bg-gray-900/20 border-b border-gray-700/20">
              <Terminal size={14} className="text-green-400 shrink-0" />
              <span className="text-xs text-gray-400">{t('liveActivity.workerOutput')}</span>
              {userScrolledUp && (
                <button
                  type="button"
                  onClick={() => {
                    setUserScrolledUp(false)
                    const el = logContainerRef.current
                    if (el) el.scrollTop = el.scrollHeight
                  }}
                  className="ml-auto text-xs text-blue-400 hover:text-blue-300 transition-colors"
                >
                  {t('liveActivity.scrollToBottom')}
                </button>
              )}
            </div>
            <div
              ref={logContainerRef}
              onScroll={handleLogScroll}
              role="log"
              aria-live="polite"
              className="max-h-80 overflow-y-auto bg-gray-950 divide-y divide-gray-800/60"
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
        )}
      </div>

      {/* Preview when collapsed — show last few lines */}
      {!isOpen && isActive && (
        <div className="border-t border-gray-700/30 bg-gray-900/20">
          <PreviewLines entries={logEntries} t={t} />
        </div>
      )}

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
