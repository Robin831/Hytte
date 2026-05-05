import { useTranslation } from 'react-i18next'
import { Lightbulb } from 'lucide-react'

export default function Suggestions() {
  const { t } = useTranslation('common')

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <header className="flex items-center gap-3 mb-6">
        <Lightbulb size={24} className="text-yellow-400" />
        <h1 className="text-2xl font-semibold text-white">{t('nav.suggestions')}</h1>
      </header>
      <p className="text-gray-400">{t('suggestions.placeholder')}</p>
    </div>
  )
}
