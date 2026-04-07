import { useTranslation } from 'react-i18next'
import { LogIn } from 'lucide-react'
import ClockWeatherWidget from './ClockWeatherWidget'
import CalendarWidget from './CalendarWidget'

export default function GuestTodayView() {
  const { t } = useTranslation('today')
  return (
    <>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.weather')}</h2>
        <ClockWeatherWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.calendar')}</h2>
        <CalendarWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <div className="flex flex-col items-center gap-3 py-4">
          <LogIn size={24} className="text-gray-400" />
          <p className="text-sm text-gray-400 text-center">{t('guest.loginPrompt')}</p>
          <a
            href="/api/auth/google/login"
            className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 transition-colors"
          >
            {t('guest.signIn')}
          </a>
        </div>
      </div>
    </>
  )
}
