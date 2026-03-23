import { useTranslation } from 'react-i18next'

export default function CalendarPage() {
  const { t } = useTranslation('common')

  return (
    <div className="p-6">
      <h1 className="text-2xl font-bold mb-4">{t('calendar.title')}</h1>
      <p className="text-gray-400">{t('calendar.comingSoon')}</p>
    </div>
  )
}
