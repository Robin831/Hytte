import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Users, Square } from 'lucide-react'
import type { WorkerInfo } from '../hooks/useForgeStatus'
import ConfirmDialog from './ConfirmDialog'
import { CollapsiblePanelHeader } from './CollapsiblePanelHeader'
import { usePanelCollapse } from '../hooks/usePanelCollapse'

interface WorkersCardProps {
  workers: WorkerInfo[]
  showToast: (message: string, type: 'success' | 'error') => void
  selectedWorkerId: string | null
  onSelectWorker: (id: string) => void
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

export default function WorkersCard({ workers, showToast, selectedWorkerId, onSelectWorker }: WorkersCardProps) {
  const { t } = useTranslation('forge')
  const [killing, setKilling] = useState<Record<string, boolean>>({})
  const [confirmKill, setConfirmKill] = useState<WorkerInfo | null>(null)
  const [isOpen, toggle] = usePanelCollapse('workers')

  const active = workers.filter(w => w.status === 'pending' || w.status === 'running')

  async function handleKill(worker: WorkerInfo) {
    setConfirmKill(null)
    setKilling(prev => ({ ...prev, [worker.id]: true }))
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
      setKilling(prev => ({ ...prev, [worker.id]: false }))
    }
  }

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <CollapsiblePanelHeader
        isOpen={isOpen}
        toggle={toggle}
        panelId="workers-panel"
        icon={<Users size={18} className="text-blue-400 shrink-0" />}
        title={t('workers.title')}
        trailing={
          <span className="text-xs text-gray-500">
            {t('workers.activeCount', { count: active.length })}
          </span>
        }
      />

      <div id="workers-panel" hidden={!isOpen}>
      {active.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('workers.empty')}</p>
      ) : (
        <div className="divide-y divide-gray-700/40">
          {/* Header row — hidden on smallest screens, shown from sm up */}
          <div className="hidden sm:grid grid-cols-[minmax(0,1fr)_8rem_6rem_8rem_5rem] gap-3 px-5 py-2 text-xs font-medium text-gray-500 uppercase tracking-wide">
            <span>{t('workers.colBead')}</span>
            <span>{t('workers.colPhase')}</span>
            <span>{t('workers.colDuration')}</span>
            <span>{t('workers.colProvider')}</span>
            <span />
          </div>

          {active.map(worker => {
            const isSelected = selectedWorkerId === worker.id
            return (
              <div
                key={worker.id}
                aria-current={isSelected || undefined}
                role="button"
                tabIndex={0}
                aria-label={t('workers.selectLabel', { id: worker.bead_id })}
                onClick={() => onSelectWorker(worker.id)}
                onKeyDown={event => {
                  if (event.key === 'Enter' || event.key === ' ' || event.key === 'Spacebar') {
                    event.preventDefault()
                    onSelectWorker(worker.id)
                  }
                }}
                className={`grid grid-cols-1 sm:grid-cols-[minmax(0,1fr)_8rem_6rem_8rem_5rem] gap-1 sm:gap-3 px-5 py-4 min-h-[44px] items-start sm:items-center cursor-pointer transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-amber-400 focus-visible:ring-inset
                  ${isSelected
                    ? 'bg-amber-900/20 border-l-2 border-amber-500'
                    : 'border-l-2 border-transparent hover:bg-gray-700/30'
                  }`}
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

                <div className="flex items-center justify-end">
                  <button
                    type="button"
                    onClick={e => { e.stopPropagation(); setConfirmKill(worker) }}
                    disabled={!!killing[worker.id]}
                    aria-label={t('workers.killLabel', { id: worker.bead_id })}
                    className="flex items-center gap-1.5 min-h-[44px] min-w-[44px] px-3 rounded-lg text-sm font-medium transition-colors
                      bg-red-600/20 text-red-400 border border-red-600/30
                      hover:bg-red-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    <Square size={14} />
                    <span className="sm:hidden">{t('workers.kill')}</span>
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}
      </div>

      <ConfirmDialog
        open={confirmKill !== null}
        title={t('workers.killConfirmTitle')}
        message={t('workers.killConfirmMessage', { id: confirmKill?.bead_id ?? '' })}
        confirmLabel={t('workers.kill')}
        destructive
        onConfirm={() => { if (confirmKill) void handleKill(confirmKill) }}
        onCancel={() => setConfirmKill(null)}
      />
    </div>
  )
}
