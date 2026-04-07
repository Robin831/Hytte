import { useState, useEffect, useReducer } from 'react'
import { Cloud } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTime } from '../../utils/formatDate'
import { usePreferredLocation } from '../../usePreferredLocation'
import { getWeatherIcon } from '../../weatherUtils'

interface TimeseriesEntry {
  time: string
  data: {
    instant: {
      details: {
        air_temperature: number
      }
    }
    next_1_hours?: { summary: { symbol_code: string } }
    next_6_hours?: { summary: { symbol_code: string } }
  }
}

interface ForecastResponse {
  properties: { timeseries: TimeseriesEntry[] }
}

type State = { loading: boolean; error: boolean; data: ForecastResponse | null }
type Action = { type: 'start' } | { type: 'done'; data: ForecastResponse } | { type: 'error' }

function reducer(_state: State, action: Action): State {
  switch (action.type) {
    case 'start': return { loading: true, error: false, data: _state.data }
    case 'done': return { loading: false, error: false, data: action.data }
    case 'error': return { loading: false, error: true, data: _state.data }
  }
}

export default function ClockWeatherWidget() {
  const { t } = useTranslation('today')
  const location = usePreferredLocation()
  const [now, setNow] = useState(() => new Date())
  const [{ data }, dispatch] = useReducer(reducer, { loading: true, error: false, data: null })

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(id)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'start' })
    fetch(
      `/api/weather/forecast?lat=${location.lat}&lon=${location.lon}&location=${encodeURIComponent(location.name)}`,
      { signal: controller.signal },
    )
      .then((r) => (r.ok ? (r.json() as Promise<ForecastResponse>) : Promise.reject()))
      .then((d) => dispatch({ type: 'done', data: d }))
      .catch(() => { if (!controller.signal.aborted) dispatch({ type: 'error' }) })
    return () => controller.abort()
  }, [location.lat, location.lon, location.name])

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
      ) : (
        <>
          <Cloud size={16} className="text-gray-500 shrink-0" />
          <span className="text-gray-500">{t('clockWeather.loading')}</span>
        </>
      )}
    </div>
  )
}
