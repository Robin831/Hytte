import { useEffect, useReducer, useState } from 'react'
import { Thermometer, Droplets, Wind, ChevronDown, ChevronUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
} from 'recharts'
import Widget from '../Widget'

// --- Types matching Go ModuleReadings JSON (no json tags → PascalCase) ---

interface IndoorReadings {
  Temperature: number
  Humidity: number
  CO2: number
  Noise: number
  Pressure: number
}

interface OutdoorReadings {
  Temperature: number
  Humidity: number
}

interface WindReadings {
  Speed: number
  Gust: number
  Direction: number
}

interface ModuleReadings {
  Indoor: IndoorReadings | null
  Outdoor: OutdoorReadings | null
  Wind: WindReadings | null
  FetchedAt: string
}

// History API: { readings: [...] }
interface HistoryReading {
  Timestamp: string
  ModuleType: string
  Metric: string
  Value: number
}

// --- Fetch state ---

type FetchState<T> = {
  loading: boolean
  data: T | null
  error: boolean
}

type FetchAction<T> =
  | { type: 'start' }
  | { type: 'success'; data: T }
  | { type: 'error' }

function fetchReducer<T>(state: FetchState<T>, action: FetchAction<T>): FetchState<T> {
  switch (action.type) {
    case 'start':
      return { loading: true, data: state.data, error: false }
    case 'success':
      return { loading: false, data: action.data, error: false }
    case 'error':
      return { loading: false, data: state.data, error: true }
    default:
      return state
  }
}

// --- Helpers ---

function minutesAgo(isoTimestamp: string): number {
  const ts = new Date(isoTimestamp).getTime()
  if (isNaN(ts)) return 0
  return Math.max(0, Math.floor((Date.now() - ts) / 60000))
}

function co2Color(co2: number): string {
  if (co2 < 1000) return 'text-green-400'
  if (co2 <= 1500) return 'text-yellow-400'
  return 'text-red-400'
}

function outdoorTempColor(temp: number): string {
  if (temp < 5) return 'text-blue-400'
  if (temp > 25) return 'text-orange-400'
  return 'text-green-400'
}

function buildSparklineData(
  readings: HistoryReading[],
  moduleType: string,
  metric: string,
): { time: number; value: number }[] {
  return readings
    .filter((r) => r.ModuleType === moduleType && r.Metric === metric)
    .map((r) => ({
      time: new Date(r.Timestamp).getTime(),
      value: Math.round(r.Value * 10) / 10,
    }))
}

// --- Component ---

export default function NetatmoWidget() {
  const { t } = useTranslation('dashboard')

  const [currentState, dispatchCurrent] = useReducer(
    fetchReducer as (s: FetchState<ModuleReadings>, a: FetchAction<ModuleReadings>) => FetchState<ModuleReadings>,
    { loading: true, data: null, error: false },
  )

  const [historyState, dispatchHistory] = useReducer(
    fetchReducer as (s: FetchState<HistoryReading[]>, a: FetchAction<HistoryReading[]>) => FetchState<HistoryReading[]>,
    { loading: false, data: null, error: false },
  )

  const [expanded, setExpanded] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    dispatchCurrent({ type: 'start' })
    fetch('/api/netatmo/current', { credentials: 'include', signal: controller.signal })
      .then((r) => {
        if (!r.ok) throw new Error('fetch failed')
        return r.json() as Promise<ModuleReadings>
      })
      .then((data) => {
        dispatchCurrent({ type: 'success', data })
      })
      .catch((err) => {
        if (err.name !== 'AbortError') dispatchCurrent({ type: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [])

  useEffect(() => {
    if (!expanded) return
    const controller = new AbortController()
    dispatchHistory({ type: 'start' })
    fetch('/api/netatmo/history?hours=24', { credentials: 'include', signal: controller.signal })
      .then((r) => {
        if (!r.ok) throw new Error('fetch failed')
        return r.json() as Promise<{ readings: HistoryReading[] }>
      })
      .then(({ readings }) => {
        dispatchHistory({ type: 'success', data: readings })
      })
      .catch((err) => {
        if (err.name !== 'AbortError') dispatchHistory({ type: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [expanded])

  const { loading, data, error } = currentState
  const readings = data

  const outdoorSparkline =
    historyState.data ? buildSparklineData(historyState.data, 'outdoor', 'temperature') : []
  const indoorSparkline =
    historyState.data ? buildSparklineData(historyState.data, 'indoor', 'temperature') : []

  return (
    <Widget title={t('widgets.netatmo.title')}>
      {loading && !readings && (
        <p className="text-gray-400 text-sm">{t('widgets.netatmo.loading')}</p>
      )}
      {error && !readings && (
        <p className="text-red-400 text-sm">{t('widgets.netatmo.error')}</p>
      )}

      {readings && (
        <div className="space-y-3">
          {/* Indoor */}
          {readings.Indoor && (
            <div className="pb-2 border-b border-gray-700">
              <p className="text-xs text-gray-500 uppercase tracking-wide mb-1">
                {t('widgets.netatmo.indoor')}
              </p>
              <div className="flex items-center gap-4 flex-wrap">
                <div className="flex items-center gap-1">
                  <Thermometer size={14} className="text-gray-400" />
                  <span className="text-lg font-semibold tabular-nums">
                    {readings.Indoor.Temperature.toFixed(1)}°
                  </span>
                </div>
                <div className="flex items-center gap-1 text-sm text-gray-300">
                  <Droplets size={14} className="text-gray-400" />
                  <span>{readings.Indoor.Humidity}%</span>
                </div>
                {readings.Indoor.CO2 > 0 && (
                  <div className={`flex items-center gap-1 text-sm ${co2Color(readings.Indoor.CO2)}`}>
                    <span className="text-xs font-medium">CO₂</span>
                    <span>{readings.Indoor.CO2} ppm</span>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Outdoor */}
          {readings.Outdoor && (
            <div className="pb-2 border-b border-gray-700">
              <p className="text-xs text-gray-500 uppercase tracking-wide mb-1">
                {t('widgets.netatmo.outdoor')}
              </p>
              <div className="flex items-center gap-4 flex-wrap">
                <div className="flex items-center gap-1">
                  <Thermometer size={14} className="text-gray-400" />
                  <span className={`text-lg font-semibold tabular-nums ${outdoorTempColor(readings.Outdoor.Temperature)}`}>
                    {readings.Outdoor.Temperature.toFixed(1)}°
                  </span>
                </div>
                <div className="flex items-center gap-1 text-sm text-gray-300">
                  <Droplets size={14} className="text-gray-400" />
                  <span>{readings.Outdoor.Humidity}%</span>
                </div>
              </div>
            </div>
          )}

          {/* Wind */}
          {readings.Wind && (
            <div className="pb-2 border-b border-gray-700">
              <p className="text-xs text-gray-500 uppercase tracking-wide mb-1">
                {t('widgets.netatmo.wind')}
              </p>
              <div className="flex items-center gap-4 flex-wrap">
                <div className="flex items-center gap-1 text-sm text-gray-300">
                  <Wind size={14} className="text-gray-400" />
                  <span>
                    {t('widgets.netatmo.windSpeed', { speed: readings.Wind.Speed.toFixed(1) })}
                  </span>
                </div>
                {readings.Wind.Gust > 0 && (
                  <div className="text-sm text-gray-400">
                    {t('widgets.netatmo.windGust', { gust: readings.Wind.Gust.toFixed(1) })}
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Timestamp + expand toggle */}
          <div className="flex items-center justify-between pt-1">
            <p className="text-xs text-gray-500">
              {t('widgets.netatmo.updatedAgo', { count: minutesAgo(readings.FetchedAt) })}
            </p>
            <button
              type="button"
              onClick={() => setExpanded((e) => !e)}
              className="flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300"
              aria-label={expanded ? t('widgets.netatmo.collapse') : t('widgets.netatmo.expand')}
            >
              {expanded ? (
                <>
                  {t('widgets.netatmo.collapse')}
                  <ChevronUp size={14} />
                </>
              ) : (
                <>
                  {t('widgets.netatmo.expand')}
                  <ChevronDown size={14} />
                </>
              )}
            </button>
          </div>

          {/* Expanded: 24h sparklines */}
          {expanded && (
            <div className="pt-2 space-y-4">
              {outdoorSparkline.length > 1 && (
                <div>
                  <p className="text-xs text-gray-500 mb-1">
                    {t('widgets.netatmo.outdoorTrend')}
                  </p>
                  <ResponsiveContainer width="100%" height={60}>
                    <LineChart data={outdoorSparkline} role="img" aria-label={t('widgets.netatmo.outdoorTrend')}>
                      <XAxis dataKey="time" hide />
                      <YAxis hide domain={['auto', 'auto']} />
                      <Tooltip
                        contentStyle={{ backgroundColor: '#1f2937', border: 'none', borderRadius: '6px', fontSize: '11px' }}
                        labelFormatter={(v: number) =>
                          new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(v)
                        }
                        formatter={(v: number) => [`${v}°`, '']}
                      />
                      <Line
                        type="monotone"
                        dataKey="value"
                        stroke="#60a5fa"
                        dot={false}
                        strokeWidth={1.5}
                        isAnimationActive={false}
                      />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
              )}

              {indoorSparkline.length > 1 && (
                <div>
                  <p className="text-xs text-gray-500 mb-1">
                    {t('widgets.netatmo.indoorTrend')}
                  </p>
                  <ResponsiveContainer width="100%" height={60}>
                    <LineChart data={indoorSparkline} role="img" aria-label={t('widgets.netatmo.indoorTrend')}>
                      <XAxis dataKey="time" hide />
                      <YAxis hide domain={['auto', 'auto']} />
                      <Tooltip
                        contentStyle={{ backgroundColor: '#1f2937', border: 'none', borderRadius: '6px', fontSize: '11px' }}
                        labelFormatter={(v: number) =>
                          new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(v)
                        }
                        formatter={(v: number) => [`${v}°`, '']}
                      />
                      <Line
                        type="monotone"
                        dataKey="value"
                        stroke="#f97316"
                        dot={false}
                        strokeWidth={1.5}
                        isAnimationActive={false}
                      />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
              )}

              {historyState.loading && (
                <p className="text-xs text-gray-400">{t('widgets.netatmo.loadingHistory')}</p>
              )}
              {historyState.error && (
                <p className="text-xs text-red-400">{t('widgets.netatmo.errorHistory')}</p>
              )}
              {historyState.data &&
                outdoorSparkline.length <= 1 &&
                indoorSparkline.length <= 1 && (
                  <p className="text-xs text-gray-500">{t('widgets.netatmo.noHistory')}</p>
                )}
            </div>
          )}
        </div>
      )}
    </Widget>
  )
}
