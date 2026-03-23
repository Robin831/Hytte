import { useEffect, useReducer } from 'react'
import { Link } from 'react-router-dom'
import { Droplets, Wind } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import { getWeatherIcon, getWeatherDescription } from '../../weatherUtils'
import { usePreferredLocation } from '../../usePreferredLocation'

interface TimeseriesEntry {
  time: string
  data: {
    instant: {
      details: {
        air_temperature: number
        wind_speed: number
        relative_humidity: number
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
  error: string | null
}

type FetchAction =
  | { type: 'start' }
  | { type: 'success'; data: ForecastResponse }
  | { type: 'error'; error: string }

function fetchReducer(state: FetchState, action: FetchAction): FetchState {
  switch (action.type) {
    case 'start': return { loading: true, forecast: state.forecast, error: null }
    case 'success': return { loading: false, forecast: action.data, error: null }
    case 'error': return { loading: false, forecast: state.forecast, error: action.error }
    default: return state
  }
}

export default function WeatherWidget() {
  const { t: tDash } = useTranslation('dashboard')
  const { t: tWeather } = useTranslation('weather')
  const location = usePreferredLocation()
  const [{ loading, forecast, error }, dispatch] = useReducer(fetchReducer, {
    loading: true,
    forecast: null,
    error: null,
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
      .catch((err: unknown) => {
        if (!cancelled) dispatch({ type: 'error', error: err instanceof Error ? err.message : 'Unknown error' })
      })
    return () => { cancelled = true }
  }, [location.lat, location.lon, location.name])

  const current = forecast?.properties?.timeseries?.[0]
  const symbolCode =
    current?.data.next_1_hours?.summary.symbol_code ||
    current?.data.next_6_hours?.summary.symbol_code ||
    'cloudy'

  return (
    <Widget title={tDash('widgets.weather.title')}>
      {loading && !forecast && (
        <p className="text-gray-400 text-sm">{tDash('widgets.weather.loading')}</p>
      )}
      {error && !forecast && (
        <p className="text-red-400 text-sm">{tDash('widgets.weather.error')}</p>
      )}
      {current && (
        <div>
          <div className="flex items-center justify-between mb-3">
            <div>
              <p className="text-xs text-gray-400 mb-1">{location.name}</p>
              <div className="flex items-end gap-2">
                <span className="text-4xl font-bold tabular-nums">
                  {Math.round(current.data.instant.details.air_temperature)}°
                </span>
                <span className="text-sm text-gray-300 mb-1">
                  {getWeatherDescription(symbolCode, tWeather)}
                </span>
              </div>
            </div>
            <div className="text-blue-400">{getWeatherIcon(symbolCode, 40)}</div>
          </div>

          <div className="flex items-center gap-4 text-xs text-gray-400 border-t border-gray-700 pt-3">
            <div className="flex items-center gap-1">
              <Wind size={12} />
              <span>{current.data.instant.details.wind_speed} m/s</span>
            </div>
            <div className="flex items-center gap-1">
              <Droplets size={12} />
              <span>{Math.round(current.data.instant.details.relative_humidity)}%</span>
            </div>
          </div>

          <Link
            to="/weather"
            className="inline-block mt-3 text-xs text-blue-400 hover:text-blue-300"
          >
            {tDash('widgets.weather.fullForecast')}
          </Link>
        </div>
      )}
    </Widget>
  )
}
