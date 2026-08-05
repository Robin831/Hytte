import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TimeseriesEntry } from '../../lib/weatherForecast'
import { getWeatherIcon } from '../../weatherUtils'
import { formatDate, formatTime } from '../../utils/formatDate'

type HourlyRange = 12 | 24 | 48

const RANGES: HourlyRange[] = [12, 24, 48]

/** Horizontally scrolling hour-by-hour preview with a 12/24/48-hour range toggle. */
export default function HourlyStrip({ timeseries }: { timeseries: TimeseriesEntry[] }) {
  const { t } = useTranslation('weather')
  const [hourlyRange, setHourlyRange] = useState<HourlyRange>(12)

  return (
    <section className="bg-gray-800 rounded-xl p-6 mb-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold">{t('page.nextHours', { count: hourlyRange })}</h2>
        <div className="flex rounded-lg overflow-hidden border border-gray-600 text-xs">
          {RANGES.map((range) => (
            <button
              key={range}
              onClick={() => setHourlyRange(range)}
              className={`px-3 py-1 transition-colors ${
                hourlyRange === range
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
              }`}
              aria-label={t('page.showNextHours', { count: range })}
              aria-pressed={hourlyRange === range}
            >
              {range}h
            </button>
          ))}
        </div>
      </div>
      {timeseries.length === 0 && (
        <p className="text-sm text-gray-400">{t('page.noUpcomingHours')}</p>
      )}
      <div className="flex gap-4 overflow-x-auto pb-2">
        {timeseries.slice(0, hourlyRange).map((entry, index) => {
          const dt = new Date(entry.time)
          const hour = formatTime(dt, { hour: 'numeric', hour12: false })
          const sym =
            entry.data.next_1_hours?.summary.symbol_code ||
            entry.data.next_6_hours?.summary.symbol_code ||
            'cloudy'
          // Show date separator when crossing midnight (hour 0) after the first entry
          const showDateSep = index > 0 && dt.getHours() === 0
          const dateLabel = formatDate(dt, { weekday: 'short', month: 'short', day: 'numeric' })
          return (
            <div key={entry.time} className="flex items-start gap-4">
              {showDateSep && (
                <div className="flex flex-col items-center self-stretch">
                  <div className="w-px bg-gray-600 flex-1" />
                  <span className="text-xs text-gray-400 whitespace-nowrap rotate-0 py-1 px-1 bg-gray-700 rounded text-center leading-tight">
                    {dateLabel}
                  </span>
                  <div className="w-px bg-gray-600 flex-1" />
                </div>
              )}
              <div className="flex flex-col items-center gap-1 min-w-[3.5rem]">
                <span className="text-xs text-gray-400">{hour}</span>
                <span className="text-blue-400">{getWeatherIcon(sym, 20)}</span>
                <span className="text-sm font-medium">
                  {Math.round(entry.data.instant.details.air_temperature)}°
                </span>
                {entry.data.next_1_hours?.details.precipitation_amount ? (
                  <span className="text-xs text-blue-400">
                    {entry.data.next_1_hours.details.precipitation_amount} mm
                  </span>
                ) : null}
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}
