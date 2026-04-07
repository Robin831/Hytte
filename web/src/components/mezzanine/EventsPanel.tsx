import { useState, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Filter } from 'lucide-react'
import type { WorkerEvent } from '../LiveActivity'
import { formatTime } from '../../utils/formatDate'
import { useForgeEvents } from '../../hooks/useForgeEvents'

// 'all' | 'errors' | 'prs' | 'anvil:<name>' — the tagged anvil: prefix keeps
// the type constrained so arbitrary strings cannot slip through accidentally.
type EventFilter = 'all' | 'errors' | 'prs' | `anvil:${string}`

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
  const { t } = useTranslation('forge')
  const [filter, setFilter] = useState<EventFilter>('all')

  // useForgeEvents connects to /api/forge/activity/stream (SSE) and falls back
  // to polling /api/forge/events. Events are returned in chronological order
  // (oldest first); we reverse them here for newest-first display.
  const { events: chronoEvents } = useForgeEvents({ maxEvents: MAX_EVENTS })
  const events = useMemo(() => [...chronoEvents].reverse(), [chronoEvents])

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
    // per-anvil filter: filter is `anvil:<name>`
    if (filter.startsWith('anvil:')) {
      const anvilName = filter.slice(6)
      return events.filter(e => e.anvil === anvilName)
    }
    return events
  }, [events, filter])

  return (
    <div className="flex flex-col rounded-lg border border-gray-700/50 bg-gray-900/60 overflow-hidden">
      <div className="flex items-center justify-between px-3 py-2 border-b border-gray-700/50">
        <h3 className="text-sm font-semibold text-gray-200">{t('mezzanine.events.title')}</h3>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Filter size={14} className="absolute left-2 top-1/2 -translate-y-1/2 text-gray-500 pointer-events-none" />
            <select
              value={filter}
              onChange={e => setFilter(e.target.value as EventFilter)}
              className="pl-7 pr-2 py-1 text-xs rounded bg-gray-800 border border-gray-700 text-gray-300 appearance-none cursor-pointer focus:outline-none focus:ring-1 focus:ring-blue-500"
              aria-label={t('mezzanine.events.filterLabel')}
            >
              <option value="all">{t('mezzanine.events.filterAll')}</option>
              <option value="errors">{t('mezzanine.events.filterErrors')}</option>
              <option value="prs">{t('mezzanine.events.filterPRs')}</option>
              {anvils.map(anvil => (
                <option key={anvil} value={`anvil:${anvil}`}>{anvil}</option>
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
                          {formatTime(event.timestamp, { hour: '2-digit', minute: '2-digit' })}
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
        <Link
          to="/forge/mezzanine/events"
          className="text-xs text-blue-400 hover:text-blue-300 hover:underline"
        >
          {t('mezzanine.events.viewAll')}
        </Link>
      </div>
    </div>
  )
}
