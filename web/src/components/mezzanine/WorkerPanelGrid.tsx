import { useTranslation } from 'react-i18next'
import type { WorkerInfo } from '../../hooks/useForgeStatus'
import { useToast } from '../../hooks/useToast'
import ToastList from '../ToastList'
import WorkerPanel from './WorkerPanel'
import IdleSlot from './IdleSlot'

interface WorkerPanelGridProps {
  workers: WorkerInfo[]
  maxSlots?: number
  onBeadClick?: (beadId: string) => void
}

export default function WorkerPanelGrid({ workers, maxSlots = 3, onBeadClick }: WorkerPanelGridProps) {
  const { t } = useTranslation('forge')
  const { toasts, showToast } = useToast()

  const activeWorkers = workers.filter(w => w.status === 'pending' || w.status === 'running')
  const idleCount = Math.max(0, maxSlots - activeWorkers.length)

  return (
    <div className="flex flex-col gap-4">
      <ToastList toasts={toasts} />

      {activeWorkers.length === 0 && idleCount === 0 ? (
        <p className="text-sm text-gray-500 text-center py-8">
          {t('mezzanine.noWorkers')}
        </p>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {activeWorkers.map(worker => (
            <WorkerPanel
              key={worker.id}
              worker={worker}
              showToast={showToast}
              onBeadClick={onBeadClick}
            />
          ))}
          {Array.from({ length: idleCount }, (_, i) => (
            <IdleSlot key={`idle-${i}`} slotIndex={activeWorkers.length + i} />
          ))}
        </div>
      )}
    </div>
  )
}
