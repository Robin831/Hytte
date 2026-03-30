import { useState, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import KioskClock from '../components/kiosk/KioskClock'
import KioskBusDepartures from '../components/kiosk/KioskBusDepartures'
import KioskWeather from '../components/kiosk/KioskWeather'
import KioskSunrise from '../components/kiosk/KioskSunrise'
import mockData from '../mocks/kioskData.json'

interface Departure {
  line: string
  destination: string
  departure_time: string
  is_realtime: boolean
  delay_minutes: number
  platform?: string
}

interface StopDepartures {
  stop_id: string
  stop_name: string
  departures: Departure[]
}

interface OutdoorReadings {
  Temperature: number
  Humidity: number
}

interface SunTimes {
  kind: string
  sunrise?: string
  sunset?: string
}

interface ForecastData {
  properties: {
    timeseries: unknown[]
  }
}

interface KioskData {
  transit: StopDepartures[]
  outdoor?: OutdoorReadings | null
  forecast?: ForecastData | null
  sun?: SunTimes | null
  fetched_at: string
}

const POLL_INTERVAL_MS = 30_000

export default function KioskPage() {
  const [searchParams] = useSearchParams()
  const token = searchParams.get('token')

  const [data, setData] = useState<KioskData>(() => mockData as KioskData)

  useEffect(() => {
    if (!token) return

    const controller = new AbortController()

    async function fetchData() {
      try {
        const res = await fetch(`/api/kiosk/data?token=${encodeURIComponent(token!)}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) return
        const json: KioskData = await res.json()
        setData(json)
      } catch {
        // Keep displaying last known data on error
      }
    }

    fetchData()
    const intervalId = setInterval(fetchData, POLL_INTERVAL_MS)

    return () => {
      controller.abort()
      clearInterval(intervalId)
    }
  }, [token])

  return (
    <div className="min-h-screen bg-gray-950 text-white flex flex-col overflow-hidden">
      {/* Clock & Date */}
      <KioskClock />

      {/* Divider */}
      <div className="h-px bg-gray-800 mx-4" />

      {/* Bus Departures */}
      <div className="flex-1 overflow-y-auto py-2">
        <KioskBusDepartures stops={data.transit} />
      </div>

      {/* Divider */}
      <div className="h-px bg-gray-800 mx-4" />

      {/* Weather strip */}
      <KioskWeather
        outdoor={data.outdoor ?? null}
        forecast={data.forecast as ForecastData | null}
      />

      {/* Divider */}
      <div className="h-px bg-gray-800 mx-4" />

      {/* Sunrise / Sunset */}
      <KioskSunrise sun={data.sun ?? null} />
    </div>
  )
}
