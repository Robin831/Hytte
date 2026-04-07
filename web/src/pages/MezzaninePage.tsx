import { useTranslation } from 'react-i18next'
import MezzanineLayout from '../components/mezzanine/MezzanineLayout'

function SidebarPlaceholder() {
  const { t } = useTranslation('forge')
  return (
    <div className="p-4 text-sm text-gray-500">
      {t('mezzanine.sidebarPlaceholder')}
    </div>
  )
}

function FloorPlaceholder() {
  const { t } = useTranslation('forge')
  return (
    <div className="flex items-center justify-center h-full text-gray-500">
      {t('mezzanine.floorPlaceholder')}
    </div>
  )
}

export default function MezzaninePage() {
  return (
    <MezzanineLayout sidebar={<SidebarPlaceholder />}>
      <FloorPlaceholder />
    </MezzanineLayout>
  )
}
