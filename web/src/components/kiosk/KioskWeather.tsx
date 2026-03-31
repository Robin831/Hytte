import { useMemo, useState, useEffect } from 'react'
// No i18n in kiosk — hardcoded strings for old-browser compatibility
import { Droplets, Wind } from 'lucide-react'
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
  forecast?: ForecastData | null
}

export default function KioskWeather({ outdoor, forecast }: Props) {
  // Kiosk uses hardcoded strings to avoid old-browser i18n failures

  // Keep `now` up-to-date every minute so the forecast strip rolls forward even
  // when the forecast data itself hasn't changed (e.g. mock/no-token mode).
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

  const currentEntry = forecast?.properties?.timeseries?.[0]
  const windSpeed = currentEntry?.data?.instant?.details?.wind_speed

  return (
    <div className="px-4 py-3">
      {/* Current conditions */}
      <div className="flex items-center gap-6 mb-4">
        {outdoor != null && (
          <>
            <div className="text-5xl font-bold text-white">
              {outdoor.Temperature.toFixed(1)}°
            </div>
            <div className="flex flex-col gap-1 text-gray-300">
              <div className="flex items-center gap-1 text-lg">
                <Droplets size={18} className="text-blue-400" />
                <span>{outdoor.Humidity}%</span>
              </div>
              {windSpeed != null && (
                <div className="flex items-center gap-1 text-lg">
                  <Wind size={18} className="text-gray-400" />
                  <span>{windSpeed.toFixed(1)} m/s</span>
                </div>
              )}
            </div>
          </>
        )}
        {outdoor == null && (
          <div className="text-gray-400 text-lg">Ingen værdata</div>
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
