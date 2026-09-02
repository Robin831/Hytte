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
  // Night mode: render the reduced-contrast palette so a wall-mounted screen
  // does not light up a dark room. Defaults to the normal daytime palette.
  dimmed?: boolean
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

// Countdown colours. Colour is the whole signal for an imminent departure, so
// the dimmed variants stop at the -700 shades: dark enough not to light up the
// room, but still far enough apart in luminance and hue that red/yellow/green
// stay tellable apart on a dark panel across the room.
function countdownColor(mins: number, dimmed: boolean): string {
  if (mins <= 1) return dimmed ? 'text-red-700' : 'text-red-400'
  if (mins <= 5) return dimmed ? 'text-yellow-700' : 'text-yellow-400'
  return dimmed ? 'text-green-700' : 'text-green-400'
}

export default function KioskBusDepartures({ stops, dimmed = false }: Props) {
  // Kiosk uses hardcoded strings (no i18n) to avoid old-browser failures
  // Toggle visibility to retrigger the fade-in animation whenever stops data refreshes
  const [visible, setVisible] = useState(true)
  const prevStopsRef = useRef(stops)
  // Tick every second so the countdown stays accurate and minutes don't visibly jump
  const [, setTick] = useState(0)

  useEffect(() => {
    const id = window.setInterval(() => setTick((n) => n + 1), 1_000)
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
      <div className={`px-6 py-4 text-xl ${dimmed ? 'text-gray-600' : 'text-gray-400'}`}>
        Ingen avganger
      </div>
    )
  }

  return (
    <div
      className="px-4 transition-opacity duration-300"
      style={{ opacity: visible ? 1 : 0 }}
      data-dimmed={dimmed ? 'true' : 'false'}
    >
      {stops.map((stop) => (
        <div key={stop.stop_id} className="mb-4">
          <div
            className={`text-sm font-semibold uppercase tracking-widest mb-2 px-2 ${
              dimmed ? 'text-gray-600' : 'text-gray-400'
            }`}
          >
            {stop.stop_name}
          </div>
          <div className="space-y-1">
            {[...stop.departures]
              .filter((dep) => {
                const t = new Date(dep.departure_time).getTime()
                return !isNaN(t) && t > Date.now()
              })
              .sort(
                (a, b) =>
                  new Date(a.departure_time).getTime() -
                  new Date(b.departure_time).getTime(),
              )
              .slice(0, 6)
              .map((dep) => {
              const mins = minutesUntil(dep.departure_time)
              return (
                <div
                  key={`${dep.line}-${dep.departure_time}`}
                  className={`flex items-center gap-3 rounded-lg px-3 py-2 ${
                    dimmed ? 'bg-gray-900' : 'bg-gray-800'
                  }`}
                >
                  <span
                    className={`${lineBadgeColor(dep.line)} text-sm font-bold w-10 h-8 flex items-center justify-center rounded ${
                      dimmed ? 'text-gray-400 opacity-40' : 'text-white'
                    }`}
                  >
                    {dep.line}
                  </span>
                  <span
                    className={`flex-1 text-lg truncate ${dimmed ? 'text-gray-500' : 'text-white'}`}
                  >
                    {dep.destination}
                  </span>
                  <span
                    className={`text-lg font-mono font-semibold tabular-nums ${countdownColor(mins, dimmed)}`}
                  >
                    {mins === 0 ? 'nå' : `${mins} min`}
                  </span>
                  {dep.delay_minutes > 0 && (
                    <span className={`text-xs ${dimmed ? 'text-red-700' : 'text-red-400'}`}>
                      +{dep.delay_minutes}
                    </span>
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
