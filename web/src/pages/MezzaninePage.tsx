import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useForgeWorkers } from '../hooks/useForgeStatus'
import MezzanineLayout from '../components/mezzanine/MezzanineLayout'
import WorkerPanelGrid from '../components/mezzanine/WorkerPanelGrid'
import BeadDetailModal from '../components/BeadDetailModal'

function SidebarPlaceholder() {
  const { t } = useTranslation('forge')
  return (
    <div className="p-4 text-sm text-gray-500">
      {t('mezzanine.sidebarPlaceholder')}
    </div>
  )
}

export default function MezzaninePage() {
  const { workers } = useForgeWorkers()
  const [selectedBeadId, setSelectedBeadId] = useState<string | null>(null)

  return (
    <MezzanineLayout sidebar={<SidebarPlaceholder />}>
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
