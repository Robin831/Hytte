import { useMemo, useState, useEffect } from 'react'
import { Droplets, Wind, Thermometer, Volume2 } from 'lucide-react'
import { getWeatherIcon } from '../../weatherUtils'

// Kiosk-local time formatter — avoids importing utils/formatDate which
// depends on i18n (fails on Android 5 / old Firefox).
function kioskFormatTime(dateStr: string): string {
  const d = new Date(dateStr)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

interface OutdoorReadings {
  Temperature: number
  Humidity: number
}

interface IndoorReadings {
  Temperature: number
  Humidity: number
  CO2: number
  Noise: number
  Pressure: number
}

interface WindReadings {
  Speed: number
  Gust: number
  Direction: number
}

export interface TimeseriesEntry {
  time: string
  data: {
    instant: {
      details: {
        air_temperature: number
        wind_speed: number
        relative_humidity?: number
        wind_from_direction?: number
      }
    }
    next_1_hours?: {
      summary: { symbol_code: string }
    }
    next_6_hours?: {
      summary: { symbol_code: string }
    }
  }
}

export interface ForecastData {
  properties: {
    timeseries: TimeseriesEntry[]
  }
}

interface Props {
  outdoor?: OutdoorReadings | null
  indoor?: IndoorReadings | null
  wind?: WindReadings | null
  forecast?: ForecastData | null
}

function co2Color(co2: number): string {
  if (co2 < 1000) return 'text-green-400'
  if (co2 < 1500) return 'text-yellow-400'
  return 'text-red-400'
}

export default function KioskWeather({ outdoor, indoor, wind, forecast }: Props) {
  // Keep `now` up-to-date every minute so the forecast strip rolls forward
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const updateNow = () => setNow(Date.now())
    updateNow()
    const id = window.setInterval(updateNow, 60_000)
    return () => window.clearInterval(id)
  }, [])

  // Extract next 6 hourly forecast entries from now
  const hourlyForecast = useMemo(() => {
    const result: { time: string; hour: string; symbolCode: string; temp: number }[] = []
    if (!forecast?.properties?.timeseries) return result
    const upcoming = forecast.properties.timeseries
      .filter((e) => new Date(e.time).getTime() >= now - 30 * 60000)
      .slice(0, 6)
    for (const entry of upcoming) {
      const symbolCode =
        entry.data.next_1_hours?.summary?.symbol_code ??
        entry.data.next_6_hours?.summary?.symbol_code ??
        'cloudy'
      result.push({
        time: entry.time,
        hour: kioskFormatTime(entry.time),
        symbolCode,
        temp: Math.round(entry.data.instant.details.air_temperature),
      })
    }
    return result
  }, [forecast, now])

  return (
    <div className="px-4 py-3">
      {/* Netatmo readings — outdoor + indoor side by side */}
      <div className="flex gap-6 mb-3">
        {/* Outdoor */}
        {outdoor != null ? (
          <div className="flex items-center gap-3">
            <div className="text-4xl font-bold text-white">
              {outdoor.Temperature.toFixed(1)}°
            </div>
            <div className="flex flex-col gap-0.5 text-sm text-gray-300">
              <div className="flex items-center gap-1">
                <Droplets size={14} className="text-blue-400" />
                <span>{outdoor.Humidity}%</span>
              </div>
              <span className="text-xs text-gray-500">ute</span>
            </div>
          </div>
        ) : (
          <div className="text-gray-400">Ingen værdata</div>
        )}

        {/* Indoor */}
        {indoor != null && (
          <div className="flex items-center gap-3">
            <div className="text-4xl font-bold text-gray-300">
              {indoor.Temperature.toFixed(1)}°
            </div>
            <div className="flex flex-col gap-0.5 text-sm text-gray-300">
              <div className={`flex items-center gap-1 ${co2Color(indoor.CO2)}`}>
                <span>CO₂ {indoor.CO2}</span>
              </div>
              <span className="text-xs text-gray-500">inne</span>
            </div>
          </div>
        )}

        {/* Wind */}
        {wind != null && (
          <div className="flex items-center gap-2 text-sm text-gray-300">
            <Wind size={16} className="text-gray-400" />
            <span>{wind.Speed.toFixed(1)} m/s</span>
          </div>
        )}
      </div>

      {/* 6-hour forecast strip */}
      {hourlyForecast.length > 0 && (
        <div className="flex gap-2 overflow-x-auto">
          {hourlyForecast.map((h) => (
            <div
              key={h.time}
              className="flex flex-col items-center bg-gray-800 rounded-lg px-3 py-2 min-w-[64px]"
            >
              <span className="text-xs text-gray-400 mb-1">{h.hour}</span>
              {getWeatherIcon(h.symbolCode, 22)}
              <span className="text-sm text-white mt-1">{h.temp}°</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
