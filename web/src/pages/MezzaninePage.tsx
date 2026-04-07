import { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useForgeWorkers, useForgeStatus, useForgeQueue } from '../hooks/useForgeStatus'
import { useToast } from '../hooks/useToast'
import MezzanineLayout from '../components/mezzanine/MezzanineLayout'
import WorkerPanelGrid from '../components/mezzanine/WorkerPanelGrid'
import QueueSidebar from '../components/mezzanine/QueueSidebar'
import PipelineBar from '../components/mezzanine/PipelineBar'
import BeadDetailModal from '../components/BeadDetailModal'
import ToastList from '../components/ToastList'

export default function MezzaninePage() {
  const { t } = useTranslation('forge')
  const { workers } = useForgeWorkers()
  const { status } = useForgeStatus()
  const { beads: queueBeads } = useForgeQueue()
  const [selectedBeadId, setSelectedBeadId] = useState<string | null>(null)
  const { toasts, showToast } = useToast()

  const handleMerge = useCallback(async (prId: number, prNumber: number) => {
    try {
      const res = await fetch(`/api/forge/prs/${prId}/merge`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        showToast((body as { error?: string }).error ?? t('readyToMerge.mergeError'), 'error')
        return
      }
      showToast(t('readyToMerge.mergeSuccess', { number: prNumber }), 'success')
    } catch {
      showToast(t('readyToMerge.mergeError'), 'error')
    }
  }, [showToast, t])

  return (
    <MezzanineLayout sidebar={<QueueSidebar onBeadClick={setSelectedBeadId} />}>
      <div className="flex flex-col gap-4">
        <PipelineBar
          workers={workers}
          openPRs={status?.open_prs}
          queueBeads={queueBeads}
          onBeadClick={setSelectedBeadId}
          onMerge={handleMerge}
        />

        <WorkerPanelGrid
          workers={workers}
          onBeadClick={setSelectedBeadId}
        />
      </div>

      <BeadDetailModal
        open={selectedBeadId !== null}
        beadId={selectedBeadId}
        onClose={() => setSelectedBeadId(null)}
      />

      <ToastList toasts={toasts} />
    </MezzanineLayout>
  )
}
