import { useState, useEffect, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Clock, Check } from 'lucide-react'
import { Dialog, DialogHeader, DialogBody } from '../ui/dialog'
import { formatTime, formatDateTime } from '../../utils/formatDate'

interface LifecycleEvent {
  id: number
  timestamp: string
  type: string
  message: string
  bead_id: string
  anvil: string
  phase?: string
  level?: string
}

const PHASE_ORDER = ['queue', 'schematic', 'smith', 'temper', 'warden', 'pr', 'merged']

function classifyLevel(event: LifecycleEvent): 'success' | 'failure' | 'info' {
  const type = event.type?.toLowerCase() ?? ''
  const level = event.level?.toLowerCase() ?? ''
  const message = event.message?.toLowerCase() ?? ''
  if (level === 'error' || type.includes('fail') || type.includes('error') || message.includes('failed')) return 'failure'
  if (level === 'success' || type.includes('pass') || type.includes('merged') || type.includes('done') || type.includes('success') || type.includes('complete')) return 'success'
  return 'info'
}

const levelDotStyles: Record<string, string> = {
  success: 'bg-green-500',
  failure: 'bg-red-500',
  info: 'bg-blue-500',
}

const levelLineStyles: Record<string, string> = {
  success: 'border-green-500/40',
  failure: 'border-red-500/40',
  info: 'border-blue-500/40',
}

interface BeadLifecycleModalProps {
  open: boolean
  onClose: () => void
  beadId: string | null
}

export default function BeadLifecycleModal({ open, onClose, beadId }: BeadLifecycleModalProps) {
  const { t } = useTranslation('forge')
  const titleId = useId()
  // Single state: tracks the most recently completed fetch
  const [fetched, setFetched] = useState<{
    beadId: string
    events: LifecycleEvent[]
    error: string | null
  } | null>(null)

  // All derived — no synchronous setState in effects
  const events = fetched?.beadId === beadId ? (fetched.events ?? []) : []
  const error = fetched?.beadId === beadId ? fetched.error : null
  const loading = open && !!beadId && fetched?.beadId !== beadId

  useEffect(() => {
    if (!open || !beadId) return
    const controller = new AbortController()

    fetch(`/api/forge/events/page?bead=${encodeURIComponent(beadId)}&limit=200`, {
      credentials: 'include',
      signal: controller.signal,
    })
      .then(res => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json()
      })
      .then((data: { events?: LifecycleEvent[] }) => {
        if (!controller.signal.aborted) {
          setFetched({ beadId, events: data.events ?? [], error: null })
        }
      })
      .catch(err => {
        if (err instanceof Error && err.name === 'AbortError') return
        setFetched({ beadId, events: [], error: err instanceof Error ? err.message : t('unknownError') })
      })

    return () => { controller.abort() }
  }, [open, beadId, t])

  // Derive phase timeline from events
  const phases = (() => {
    const seen = new Map<string, { first: string; last: string; count: number }>()
    for (const event of events) {
      const phase = event.phase || inferPhase(event.type)
      if (!phase) continue
      const existing = seen.get(phase)
      if (existing) {
        existing.last = event.timestamp
        existing.count++
      } else {
        seen.set(phase, { first: event.timestamp, last: event.timestamp, count: 1 })
      }
    }
    return PHASE_ORDER
      .filter(p => seen.has(p))
      .map(p => ({ phase: p, ...seen.get(p)! }))
  })()

  return (
    <Dialog open={open} onClose={onClose} maxWidth="max-w-2xl" aria-labelledby={titleId}>
      <DialogHeader
        id={titleId}
        title={t('mezzanine.lifecycle.title', { beadId: beadId ?? '' })}
        onClose={onClose}
      />
      <DialogBody>
        {loading && (
          <p className="text-sm text-gray-500 text-center py-4">{t('mezzanine.lifecycle.loading')}</p>
        )}
        {error && (
          <p className="text-sm text-red-400 text-center py-4">{error}</p>
        )}

        {!loading && !error && events.length === 0 && (
          <p className="text-sm text-gray-500 text-center py-4">{t('mezzanine.lifecycle.empty')}</p>
        )}

        {/* Phase summary bar */}
        {phases.length > 0 && (
          <div className="mb-4">
            <h3 className="text-xs font-medium text-gray-400 mb-2 flex items-center gap-1.5">
              <Clock size={12} />
              {t('mezzanine.lifecycle.phaseSummary')}
            </h3>
            <div className="flex items-center gap-0 overflow-x-auto pb-2">
              {phases.map((p, i) => (
                <div key={p.phase} className="flex items-center">
                  {i > 0 && <div className="w-6 h-0.5 bg-green-500" />}
                  <div className="flex flex-col items-center gap-1 min-w-[60px]">
                    <div className="w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-medium border-2 bg-green-500/20 border-green-500 text-green-400">
                      <Check size={10} />
                    </div>
                    <span className="text-[10px] text-gray-300 capitalize">
                      {t(`mezzanine.pipeline.stages.${p.phase}`, { defaultValue: p.phase })}
                    </span>
                    <span className="text-[9px] text-gray-500 tabular-nums">
                      {formatTime(p.first, { hour: '2-digit', minute: '2-digit' })}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Full event timeline */}
        {events.length > 0 && (
          <div className="relative">
            <div className="absolute left-3 top-0 bottom-0 w-px bg-gray-700/50" />
            <ul className="space-y-0">
              {events.map(event => {
                const level = classifyLevel(event)
                return (
                  <li key={event.id} className="relative pl-8 py-2">
                    <div className={`absolute left-[9px] top-3 h-2 w-2 rounded-full ${levelDotStyles[level]} ring-2 ring-gray-900`} />
                    {/* Connecting line to next event */}
                    <div className={`absolute left-[13px] top-5 bottom-0 w-px border-l ${levelLineStyles[level]}`} />
                    <div className="flex items-baseline gap-2 flex-wrap">
                      <span className="text-[10px] text-gray-500 tabular-nums shrink-0">
                        {formatDateTime(event.timestamp)}
                      </span>
                      <span className="text-xs font-medium text-gray-400">{event.type}</span>
                      {event.phase && (
                        <span className="text-[10px] text-amber-400/70 capitalize">{event.phase}</span>
                      )}
                    </div>
                    <p className="text-xs text-gray-300 mt-0.5">{event.message}</p>
                  </li>
                )
              })}
            </ul>
          </div>
        )}
      </DialogBody>
    </Dialog>
  )
}

function inferPhase(eventType: string): string | undefined {
  const t = eventType?.toLowerCase() ?? ''
  if (t.includes('queue') || t.includes('dispatch')) return 'queue'
  if (t.includes('schematic')) return 'schematic'
  if (t.includes('smith')) return 'smith'
  if (t.includes('temper')) return 'temper'
  if (t.includes('warden')) return 'warden'
  if (t.includes('pr') || t.includes('pull')) return 'pr'
  if (t.includes('merge')) return 'merged'
  return undefined
}
