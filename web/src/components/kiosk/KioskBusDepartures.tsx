import { useState, useEffect, useRef } from 'react'

interface Departure {
  line: string
  destination: string
  departure_time: string
  is_realtime: boolean
  delay_minutes: number
}

interface StopDepartures {
  stop_id: string
  stop_name: string
  departures: Departure[]
}

interface Props {
  stops: StopDepartures[]
}

function minutesUntil(departureTime: string): number {
  const diff = new Date(departureTime).getTime() - Date.now()
  return Math.max(0, Math.round(diff / 60000))
}

// Stable set of line badge colors keyed by line string
const LINE_COLORS: Record<string, string> = {
  '1': 'bg-red-600',
  '2': 'bg-blue-600',
  '3': 'bg-green-700',
  '4': 'bg-orange-600',
  '5': 'bg-purple-600',
  T1: 'bg-red-700',
  T2: 'bg-blue-700',
  T3: 'bg-indigo-600',
  T4: 'bg-cyan-700',
  T5: 'bg-teal-700',
}

function lineBadgeColor(line: string): string {
  if (LINE_COLORS[line]) return LINE_COLORS[line]
  // Deterministic fallback based on first character code
  const colors = [
    'bg-pink-700',
    'bg-yellow-700',
    'bg-lime-700',
    'bg-emerald-700',
    'bg-sky-700',
    'bg-violet-700',
  ]
  return colors[line.charCodeAt(0) % colors.length]
}

export default function KioskBusDepartures({ stops }: Props) {
  // Kiosk uses hardcoded strings (no i18n) to avoid old-browser failures
  // Toggle visibility to retrigger the fade-in animation whenever stops data refreshes
  const [visible, setVisible] = useState(true)
  const prevStopsRef = useRef(stops)
  // Tick every 30s so minutesUntil() stays accurate between data refreshes
  const [, setTick] = useState(0)

  useEffect(() => {
    const id = window.setInterval(() => setTick((n) => n + 1), 30_000)
    return () => window.clearInterval(id)
  }, [])

  useEffect(() => {
    if (stops !== prevStopsRef.current) {
      prevStopsRef.current = stops
      setVisible(false)
      const id = setTimeout(() => setVisible(true), 150)
      return () => clearTimeout(id)
    }
  }, [stops])

  if (stops.length === 0) {
    return (
      <div className="px-6 py-4 text-gray-400 text-xl">Ingen avganger</div>
    )
  }

  return (
    <div
      className="px-4 transition-opacity duration-300"
      style={{ opacity: visible ? 1 : 0 }}
    >
      {stops.map((stop) => (
        <div key={stop.stop_id} className="mb-4">
          <div className="text-sm font-semibold uppercase tracking-widest text-gray-400 mb-2 px-2">
            {stop.stop_name}
          </div>
          <div className="space-y-1">
            {stop.departures.slice(0, 6).map((dep) => {
              const mins = minutesUntil(dep.departure_time)
              return (
                <div
                  key={`${dep.line}-${dep.departure_time}`}
                  className="flex items-center gap-3 bg-gray-800 rounded-lg px-3 py-2"
                >
                  <span
                    className={`${lineBadgeColor(dep.line)} text-white text-sm font-bold w-10 h-8 flex items-center justify-center rounded`}
                  >
                    {dep.line}
                  </span>
                  <span className="flex-1 text-lg text-white truncate">
                    {dep.destination}
                  </span>
                  <span
                    className={`text-lg font-mono font-semibold tabular-nums ${
                      mins <= 1 ? 'text-red-400' : mins <= 5 ? 'text-yellow-400' : 'text-green-400'
                    }`}
                  >
                    {mins === 0 ? 'nå' : `${mins} min`}
                  </span>
                  {dep.delay_minutes > 0 && (
                    <span className="text-xs text-red-400">+{dep.delay_minutes}</span>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}
