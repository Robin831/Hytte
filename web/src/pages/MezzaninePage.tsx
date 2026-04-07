import { useState } from 'react'
import { useForgeWorkers } from '../hooks/useForgeStatus'
import MezzanineLayout from '../components/mezzanine/MezzanineLayout'
import WorkerPanelGrid from '../components/mezzanine/WorkerPanelGrid'
import QueueSidebar from '../components/mezzanine/QueueSidebar'
import BeadDetailModal from '../components/BeadDetailModal'

export default function MezzaninePage() {
  const { workers } = useForgeWorkers()
  const [selectedBeadId, setSelectedBeadId] = useState<string | null>(null)

  return (
    <MezzanineLayout sidebar={<QueueSidebar onBeadClick={setSelectedBeadId} />}>
      <WorkerPanelGrid
        workers={workers}
        onBeadClick={setSelectedBeadId}
      />

      <BeadDetailModal
        open={selectedBeadId !== null}
        beadId={selectedBeadId}
        onClose={() => setSelectedBeadId(null)}
      />
    </MezzanineLayout>
  )
}
