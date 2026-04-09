import { useEffect, useReducer } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { getWeatherIcon, getWeatherDescription } from '../../weatherUtils'
import { usePreferredLocation } from '../../usePreferredLocation'

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

type FetchState = {
  loading: boolean
  forecast: ForecastResponse | null
  error: boolean
}

type FetchAction =
  | { type: 'start' }
  | { type: 'success'; data: ForecastResponse }
  | { type: 'error' }

function fetchReducer(state: FetchState, action: FetchAction): FetchState {
  switch (action.type) {
    case 'start': return { loading: true, forecast: state.forecast, error: false }
    case 'success': return { loading: false, forecast: action.data, error: false }
    case 'error': return { loading: false, forecast: state.forecast, error: true }
    default: return state
  }
}

function getTodayHighLow(timeseries: TimeseriesEntry[]): { high: number; low: number } | null {
  const now = new Date()
  const todayKey = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`

  const todayTemps: number[] = []
  for (const entry of timeseries) {
    const dt = new Date(entry.time)
    const key = `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, '0')}-${String(dt.getDate()).padStart(2, '0')}`
    if (key === todayKey) {
      todayTemps.push(entry.data.instant.details.air_temperature)
    }
  }

  if (todayTemps.length === 0) return null
  return {
    high: Math.round(Math.max(...todayTemps)),
    low: Math.round(Math.min(...todayTemps)),
  }
}

export default function WeatherCard() {
  const { t: tToday } = useTranslation('today')
  const { t: tWeather } = useTranslation('weather')
  const location = usePreferredLocation()
  const [{ loading, forecast, error }, dispatch] = useReducer(fetchReducer, {
    loading: true,
    forecast: null,
    error: false,
  })

  useEffect(() => {
    let cancelled = false
    dispatch({ type: 'start' })
    fetch(`/api/weather/forecast?lat=${location.lat}&lon=${location.lon}&location=${encodeURIComponent(location.name)}`)
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch forecast')
        return r.json() as Promise<ForecastResponse>
      })
      .then((data) => {
        if (!cancelled) dispatch({ type: 'success', data })
      })
      .catch(() => {
        if (!cancelled) dispatch({ type: 'error' })
      })
    return () => { cancelled = true }
  }, [location.lat, location.lon, location.name])

  const current = forecast?.properties?.timeseries?.[0]
  const symbolCode =
    current?.data.next_1_hours?.summary.symbol_code ||
    current?.data.next_6_hours?.summary.symbol_code ||
    'cloudy'
  const highLow = forecast ? getTodayHighLow(forecast.properties.timeseries) : null

  return (
    <div className="bg-gray-800 rounded-xl p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xs uppercase tracking-wide text-gray-500">
          {tToday('widgets.weather')}
        </h2>
        <Link to="/weather" className="text-xs text-gray-500 hover:text-gray-400">
          →
        </Link>
      </div>

      {loading && !forecast && (
        <div className="flex items-center gap-3" role="status" aria-live="polite">
          <span className="sr-only">{tToday('clockWeather.loading')}</span>
          <div className="h-8 w-8 bg-gray-700 rounded animate-pulse" />
          <div className="h-5 bg-gray-700 rounded animate-pulse w-40" />
        </div>
      )}

      {error && !forecast && (
        <p className="text-red-400 text-sm">{tToday('unavailable')}</p>
      )}

      {current && (
        <div className="flex items-center gap-3">
          <div className="shrink-0 text-blue-400">
            {getWeatherIcon(symbolCode, 32)}
          </div>
          <div className="flex items-baseline gap-2 min-w-0">
            <span className="text-2xl font-bold tabular-nums">
              {Math.round(current.data.instant.details.air_temperature)}°
            </span>
            <span className="text-sm text-gray-400 truncate">
              {getWeatherDescription(symbolCode, tWeather)}
            </span>
            {highLow && (
              <span className="text-xs text-gray-500 tabular-nums shrink-0">
                {highLow.high}° / {highLow.low}°
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
