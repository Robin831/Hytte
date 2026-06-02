import { useState, useEffect } from 'react'
import { Cloud } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTime } from '../../utils/formatDate'
import { usePreferredLocation } from '../../usePreferredLocation'
import { getWeatherIcon } from '../../weatherUtils'
import { useForecast } from '../../hooks/useForecast'

export default function ClockWeatherWidget() {
  const { t } = useTranslation('today')
  const location = usePreferredLocation()
  const [now, setNow] = useState(() => new Date())
  const { error, data } = useForecast()

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(id)
  }, [])

  const current = data?.properties?.timeseries?.[0]
  const temp = current?.data.instant.details.air_temperature
  const symbol =
    current?.data.next_1_hours?.summary.symbol_code ??
    current?.data.next_6_hours?.summary.symbol_code ??
    'cloudy'
  const weatherIcon = getWeatherIcon(symbol, 16)
  const timeStr = formatTime(now, { hour: '2-digit', minute: '2-digit' })

  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="tabular-nums font-medium">{timeStr}</span>
      <span className="text-gray-500">·</span>
      {temp !== undefined ? (
        <>
          {weatherIcon}
          <span className="text-gray-300">{Math.round(temp)}°</span>
          <span className="text-gray-500 truncate">{location.name}</span>
        </>
      ) : error && !data ? (
        <>
          <Cloud size={16} className="text-gray-500 shrink-0" />
          <span className="text-gray-500">{t('unavailable')}</span>
        </>
      ) : (
        <>
          <Cloud size={16} className="text-gray-500 shrink-0" />
          <span className="text-gray-500">{t('clockWeather.loading')}</span>
        </>
      )}
    </div>
  )
}
