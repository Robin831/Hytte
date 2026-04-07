import { useState } from 'react'
import { useForgeWorkers, useForgeStatus, useForgeQueue } from '../hooks/useForgeStatus'
import MezzanineLayout from '../components/mezzanine/MezzanineLayout'
import WorkerPanelGrid from '../components/mezzanine/WorkerPanelGrid'
import QueueSidebar from '../components/mezzanine/QueueSidebar'
import PipelineBar from '../components/mezzanine/PipelineBar'
import BeadDetailModal from '../components/BeadDetailModal'

export default function MezzaninePage() {
  const { workers } = useForgeWorkers()
  const { status } = useForgeStatus()
  const { beads: queueBeads } = useForgeQueue()
  const [selectedBeadId, setSelectedBeadId] = useState<string | null>(null)

  return (
    <MezzanineLayout sidebar={<QueueSidebar onBeadClick={setSelectedBeadId} />}>
      <div className="flex flex-col gap-4">
        <PipelineBar
          workers={workers}
          openPRs={status?.open_prs}
          queueBeads={queueBeads}
          onBeadClick={setSelectedBeadId}
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
    </MezzanineLayout>
  )
}
