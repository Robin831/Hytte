import { useTranslation } from 'react-i18next'
import { Users } from 'lucide-react'
import type { WorkerInfo } from '../hooks/useForgeStatus'

interface WorkersCardProps {
  workers: WorkerInfo[]
}

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

export default function WorkersCard({ workers }: WorkersCardProps) {
  const { t } = useTranslation('forge')

  const active = workers.filter(w => w.status === 'pending' || w.status === 'running')

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-gray-700/50">
        <Users size={18} className="text-blue-400 shrink-0" />
        <h2 className="text-sm font-medium text-gray-300">{t('workers.title')}</h2>
        <span className="ml-auto text-xs text-gray-500">
          {t('workers.activeCount', { count: active.length })}
        </span>
      </div>

      {active.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('workers.empty')}</p>
      ) : (
        <div className="divide-y divide-gray-700/40">
          {/* Header row — hidden on smallest screens, shown from sm up */}
          <div className="hidden sm:grid grid-cols-[minmax(0,1fr)_8rem_6rem_8rem] gap-3 px-5 py-2 text-xs font-medium text-gray-500 uppercase tracking-wide">
            <span>{t('workers.colBead')}</span>
            <span>{t('workers.colPhase')}</span>
            <span>{t('workers.colDuration')}</span>
            <span>{t('workers.colProvider')}</span>
          </div>

          {active.map(worker => (
            <div
              key={worker.id}
              className="grid grid-cols-1 sm:grid-cols-[minmax(0,1fr)_8rem_6rem_8rem] gap-1 sm:gap-3 px-5 py-4 min-h-[44px] items-start sm:items-center"
            >
              {/* Bead ID + title stacked on mobile */}
              <div className="flex flex-col gap-0.5 min-w-0">
                <span className="text-sm font-mono text-amber-400 truncate">{worker.bead_id}</span>
                {worker.title && (
                  <span className="text-xs text-gray-500 truncate">{worker.title}</span>
                )}
              </div>

              <div className="flex items-center gap-1">
                <span className="sm:hidden text-xs text-gray-500">{t('workers.colPhase')}:</span>
                <span className="text-sm text-gray-300 capitalize">{worker.phase || '—'}</span>
              </div>

              <div className="flex items-center gap-1">
                <span className="sm:hidden text-xs text-gray-500">{t('workers.colDuration')}:</span>
                <span className="text-sm text-gray-300 tabular-nums">{formatDuration(worker.started_at)}</span>
              </div>

              <div className="flex items-center gap-1">
                <span className="sm:hidden text-xs text-gray-500">{t('workers.colProvider')}:</span>
                <span className="text-sm text-gray-300 truncate">{worker.anvil || '—'}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
