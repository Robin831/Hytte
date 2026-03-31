import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Activity } from 'lucide-react'
import type { ForgeEvent } from '../hooks/useForgeStatus'

interface RecentEventsCardProps {
  events: ForgeEvent[]
}

const eventTypeClass: Record<ForgeEvent['type'], string> = {
  error: 'text-red-400 bg-red-900/20 border-red-800/30',
  success: 'text-green-400 bg-green-900/20 border-green-800/30',
  info: 'text-blue-400 bg-blue-900/20 border-blue-800/30',
  warning: 'text-amber-400 bg-amber-900/20 border-amber-800/30',
}

const eventTypeBadgeClass: Record<ForgeEvent['type'], string> = {
  error: 'text-red-400',
  success: 'text-green-400',
  info: 'text-blue-400',
  warning: 'text-amber-400',
}

export default function RecentEventsCard({ events }: RecentEventsCardProps) {
  const { t } = useTranslation('forge')
  const bottomRef = useRef<HTMLDivElement>(null)

  const visible = events.slice(-20)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [events])

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-gray-700/50">
        <Activity size={18} className="text-blue-400 shrink-0" />
        <h2 className="text-sm font-medium text-gray-300">{t('recentEvents.title')}</h2>
        {visible.length > 0 && (
          <span className="ml-auto text-xs text-gray-500">
            {t('recentEvents.count', { total: visible.length })}
          </span>
        )}
      </div>

      {visible.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('recentEvents.empty')}</p>
      ) : (
        <div className="max-h-72 overflow-y-auto divide-y divide-gray-700/40">
          {visible.map((event, idx) => (
            <div
              key={`${event.timestamp}-${event.type}-${event.bead_id ?? ''}-${idx}`}
              className={`px-5 py-3 flex flex-col gap-0.5 border-l-2 ${eventTypeClass[event.type]}`}
            >
              <div className="flex items-center gap-2">
                <span className={`text-xs font-semibold uppercase tracking-wide ${eventTypeBadgeClass[event.type]}`}>
                  {t(`recentEvents.type.${event.type}`)}
                </span>
                {event.bead_id && (
                  <span className="text-xs font-mono text-gray-500 truncate">{event.bead_id}</span>
                )}
                <span className="ml-auto text-xs text-gray-500 shrink-0">
                  {new Intl.DateTimeFormat(undefined, {
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit',
                  }).format(new Date(event.timestamp))}
                </span>
              </div>
              <p className="text-sm text-gray-300 break-words">{event.message}</p>
              {event.anvil && (
                <p className="text-xs text-gray-500">{event.anvil}</p>
              )}
            </div>
          ))}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  )
}
