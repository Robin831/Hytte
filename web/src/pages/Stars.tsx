import { useTranslation } from 'react-i18next'
import { Star } from 'lucide-react'

export default function Stars() {
  const { t } = useTranslation('common')

  return (
    <div className="p-6 max-w-2xl mx-auto">
      <div className="flex items-center gap-3 mb-6">
        <Star size={24} className="text-yellow-400" />
        <h1 className="text-2xl font-semibold text-white">{t('stars.title')}</h1>
      </div>

      <div className="p-8 text-center bg-gray-800/50 rounded-lg border border-gray-700">
        <Star size={48} className="text-yellow-400 mx-auto mb-4" />
        <p className="text-gray-300 text-lg">{t('stars.comingSoon')}</p>
      </div>
    </div>
  )
}
