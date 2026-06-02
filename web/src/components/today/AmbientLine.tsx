import { useEffect, useReducer } from 'react'
import { useTranslation } from 'react-i18next'
import { usePreferredLocation } from '../../usePreferredLocation'
import { getWeatherDescription } from '../../weatherUtils'

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

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'start': return { loading: true, error: false, data: state.data }
    case 'done': return { loading: false, error: false, data: action.data }
    case 'error': return { loading: false, error: true, data: state.data }
  }
}

/** Maps the local hour to a time-of-day greeting key. */
function greetingPeriod(hour: number): 'morning' | 'afternoon' | 'evening' | 'night' {
  if (hour >= 5 && hour <= 11) return 'morning'
  if (hour >= 12 && hour <= 17) return 'afternoon'
  if (hour >= 18 && hour <= 21) return 'evening'
  return 'night'
}

/**
 * A single muted line beneath the date combining a time-of-day greeting (with the
 * user's first name when authenticated) and a compact ambient weather summary.
 * The greeting renders immediately and is never gated on the weather fetch; while
 * the forecast loads a placeholder is shown, and on failure the ambient clause is
 * silently omitted.
 */
export default function AmbientLine({ firstName }: { firstName?: string }) {
  const { t, i18n } = useTranslation('today')
  const { t: tWeather } = useTranslation('weather')
  const location = usePreferredLocation()
  const [{ loading, error, data }, dispatch] = useReducer(reducer, {
    loading: true,
    error: false,
    data: null,
  })

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

  const period = greetingPeriod(new Date().getHours())
  const greeting = firstName
    ? t(`greeting.${period}`, { name: firstName })
    : t(`greeting.${period}Guest`)

  const current = data?.properties?.timeseries?.[0]
  const temp = current?.data.instant.details.air_temperature
  const symbol =
    current?.data.next_1_hours?.summary.symbol_code ??
    current?.data.next_6_hours?.summary.symbol_code

  let ambient: string | null = null
  if (temp !== undefined && symbol) {
    const condition = getWeatherDescription(symbol, tWeather)
    const tempStr = new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 0 }).format(temp)
    ambient = t('ambient.summary', { condition, temp: tempStr })
  } else if (loading && !error) {
    ambient = t('ambient.loading')
  }

  return (
    <p className="text-xs text-gray-500 mt-1">
      {greeting}
      {ambient && <span> · {ambient}</span>}
    </p>
  )
}
