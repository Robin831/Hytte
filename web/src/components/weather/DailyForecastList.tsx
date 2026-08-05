import { useTranslation } from 'react-i18next'
import { Droplets, Wind } from 'lucide-react'
import type { DayForecast } from '../../lib/weatherForecast'
import { getWeatherIcon } from '../../weatherUtils'

/** Seven-day outlook, one row per day. */
export default function DailyForecastList({ days }: { days: DayForecast[] }) {
  const { t } = useTranslation('weather')

  return (
    <section className="bg-gray-800 rounded-xl p-6">
      <h2 className="text-lg font-semibold mb-4">{t('page.sevenDayForecast')}</h2>
      <div className="space-y-3">
        {days.map((day) => (
          <div
            key={day.date}
            className="flex items-center justify-between bg-gray-700/50 rounded-lg px-4 py-3"
          >
            <div className="flex items-center gap-3 w-24">
              <span className="text-sm font-medium">{day.dayName}</span>
            </div>
            <div className="flex items-center gap-2 text-blue-400">
              {getWeatherIcon(day.symbolCode, 20)}
            </div>
            <div className="flex items-center gap-1 w-16 justify-end">
              <Droplets size={12} className="text-blue-400" />
              <span className="text-xs text-gray-400">{day.precipitation} mm</span>
            </div>
            <div className="flex items-center gap-1 w-16 justify-end">
              <Wind size={12} className="text-gray-400" />
              <span className="text-xs text-gray-400">{day.windSpeed} m/s</span>
            </div>
            <div className="flex items-center gap-2 w-20 justify-end">
              <span className="text-sm text-gray-400">{day.tempMin}°</span>
              <span className="text-sm font-medium">{day.tempMax}°</span>
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}
