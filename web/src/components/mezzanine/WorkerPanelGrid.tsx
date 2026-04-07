import { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import type { WorkerInfo } from '../../hooks/useForgeStatus'
import WorkerPanel from './WorkerPanel'
import IdleSlot from './IdleSlot'

interface ToastMessage {
  id: number
  message: string
  type: 'success' | 'error'
}

interface WorkerPanelGridProps {
  workers: WorkerInfo[]
  maxSlots?: number
  onBeadClick?: (beadId: string) => void
}

let toastCounter = 0

export default function WorkerPanelGrid({ workers, maxSlots = 3, onBeadClick }: WorkerPanelGridProps) {
  const { t } = useTranslation('forge')
  const [toasts, setToasts] = useState<ToastMessage[]>([])

  const showToast = useCallback((message: string, type: 'success' | 'error') => {
    const id = ++toastCounter
    setToasts(prev => [...prev, { id, message, type }])
    setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id))
    }, 4000)
  }, [])

  const activeWorkers = workers.filter(w => w.status === 'pending' || w.status === 'running')
  const idleCount = Math.max(0, maxSlots - activeWorkers.length)

  return (
    <div className="flex flex-col gap-4">
      {/* Toast overlay */}
      {toasts.length > 0 && (
        <div className="fixed top-4 right-4 z-50 flex flex-col gap-2">
          {toasts.map(toast => (
            <div
              key={toast.id}
              className={`px-4 py-2 rounded-lg text-sm font-medium shadow-lg ${
                toast.type === 'success'
                  ? 'bg-green-900/90 text-green-200 border border-green-700/50'
                  : 'bg-red-900/90 text-red-200 border border-red-700/50'
              }`}
            >
              {toast.message}
            </div>
          ))}
        </div>
      )}

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
