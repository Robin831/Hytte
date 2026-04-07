import { useTranslation } from 'react-i18next'
import { CheckCircle, CalendarDays, Bus } from 'lucide-react'
import { Link } from 'react-router-dom'
import ClockWeatherWidget from './ClockWeatherWidget'
import NetatmoWidget from './NetatmoWidget'
import CalendarWidget from './CalendarWidget'
import WorkHoursWidget from './WorkHoursWidget'
import BusDepartureWidget from './BusDepartureWidget'
import MoonPhaseWidget from './MoonPhaseWidget'

export default function ParentTodayView() {
  const { t } = useTranslation('today')
  return (
    <>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.weather')}</h2>
        <ClockWeatherWidget />
        <div className="mt-2"><NetatmoWidget /></div>
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.calendar')}</h2>
        <CalendarWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.workHoursTitle')}</h2>
        <WorkHoursWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.transport')}</h2>
        <BusDepartureWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.skywatch')}</h2>
        <MoonPhaseWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('quickActions.title')}</h2>
        <div className="flex flex-wrap gap-2">
          <Link
            to="/family"
            className="inline-flex items-center gap-1.5 rounded-lg bg-gray-700 px-3 py-2 text-sm text-gray-200 hover:bg-gray-600 transition-colors"
          >
            <CheckCircle size={16} />
            {t('quickActions.approveChores')}
          </Link>
          <Link
            to="/calendar"
            className="inline-flex items-center gap-1.5 rounded-lg bg-gray-700 px-3 py-2 text-sm text-gray-200 hover:bg-gray-600 transition-colors"
          >
            <CalendarDays size={16} />
            {t('quickActions.checkSchedule')}
          </Link>
          <Link
            to="/transit"
            className="inline-flex items-center gap-1.5 rounded-lg bg-gray-700 px-3 py-2 text-sm text-gray-200 hover:bg-gray-600 transition-colors"
          >
            <Bus size={16} />
            {t('quickActions.busTimes')}
          </Link>
        </div>
      </div>
    </>
  )
}
