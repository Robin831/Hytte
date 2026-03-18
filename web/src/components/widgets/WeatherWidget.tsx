import { useState, useEffect } from 'react'
import { Link } from 'react-router'
import {
  Cloud,
  CloudDrizzle,
  CloudFog,
  CloudLightning,
  CloudRain,
  CloudSnow,
  CloudSun,
  Droplets,
  Sun,
  Wind,
} from 'lucide-react'
import Widget from '../Widget'
import { resolveLocation } from '../../recentLocations'
import type { RecentLocation } from '../../recentLocations'

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

function getWeatherIcon(symbolCode: string, size = 24) {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  const props = { size, className: 'shrink-0' }
  if (code.includes('thunder')) return <CloudLightning {...props} />
  if (code.includes('snow') || code.includes('sleet')) return <CloudSnow {...props} />
  if (code.includes('drizzle') || code.includes('lightrain')) return <CloudDrizzle {...props} />
  if (code.includes('heavyrain') || code.includes('rain')) return <CloudRain {...props} />
  if (code.includes('fog')) return <CloudFog {...props} />
  if (code === 'clearsky') return <Sun {...props} />
  if (code === 'fair' || code.includes('partlycloudy')) return <CloudSun {...props} />
  return <Cloud {...props} />
}

function getWeatherDescription(symbolCode: string): string {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  const descriptions: Record<string, string> = {
    clearsky: 'Clear sky',
    fair: 'Fair',
    partlycloudy: 'Partly cloudy',
    cloudy: 'Cloudy',
    lightrainshowers: 'Light rain showers',
    rainshowers: 'Rain showers',
    heavyrainshowers: 'Heavy rain showers',
    lightrain: 'Light rain',
    rain: 'Rain',
    heavyrain: 'Heavy rain',
    lightsnow: 'Light snow',
    snow: 'Snow',
    heavysnow: 'Heavy snow',
    fog: 'Fog',
  }
  return descriptions[code] ?? code.replace(/_/g, ' ')
}

export default function WeatherWidget() {
  const [location] = useState<RecentLocation>(resolveLocation)
  const [forecast, setForecast] = useState<ForecastResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    fetch(`/api/weather/forecast?lat=${location.lat}&lon=${location.lon}&location=${encodeURIComponent(location.name)}`)
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch forecast')
        return r.json() as Promise<ForecastResponse>
      })
      .then((data) => {
        if (!cancelled) setForecast(data)
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Unknown error')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [location.lat, location.lon, location.name])

  const current = forecast?.properties?.timeseries?.[0]
  const symbolCode =
    current?.data.next_1_hours?.summary.symbol_code ||
    current?.data.next_6_hours?.summary.symbol_code ||
    'cloudy'

  return (
    <Widget title="Weather">
      {loading && !forecast && (
        <p className="text-gray-400 text-sm">Loading…</p>
      )}
      {error && !forecast && (
        <p className="text-red-400 text-sm">Could not load weather</p>
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
                  {getWeatherDescription(symbolCode)}
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
            Full forecast →
          </Link>
        </div>
      )}
    </Widget>
  )
}
