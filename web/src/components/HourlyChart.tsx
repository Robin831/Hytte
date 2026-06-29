import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown } from 'lucide-react'
import type { TimeseriesEntry } from '../lib/weatherForecast'
import { getWeatherIcon, getWeatherDescription } from '../weatherUtils'
import { formatTime } from '../utils/formatDate'

interface HourPoint {
  time: string
  temp: number
  precip: number
  symbol: string
}

function toHourPoints(timeseries: TimeseriesEntry[]): HourPoint[] {
  return timeseries.map((entry) => ({
    time: entry.time,
    temp: entry.data.instant.details.air_temperature,
    precip: entry.data.next_1_hours?.details?.precipitation_amount ?? 0,
    symbol:
      entry.data.next_1_hours?.summary.symbol_code ||
      entry.data.next_6_hours?.summary.symbol_code ||
      'cloudy',
  }))
}

// SVG viewBox dimensions — the chart scales to its container via width: 100%.
const VIEW_W = 720
const VIEW_H = 180
const PAD_X = 24
const PAD_TOP = 24
const PAD_BOTTOM = 28

export default function HourlyChart({ timeseries }: { timeseries: TimeseriesEntry[] }) {
  const { t } = useTranslation('weather')
  const [expanded, setExpanded] = useState(false)
  const tableId = useId()

  const points = toHourPoints(timeseries)
  if (points.length === 0) return null

  const temps = points.map((p) => p.temp)
  const minTemp = Math.min(...temps)
  const maxTemp = Math.max(...temps)
  const tempRange = maxTemp - minTemp || 1
  const maxPrecip = Math.max(...points.map((p) => p.precip), 0)

  const plotW = VIEW_W - PAD_X * 2
  const plotH = VIEW_H - PAD_TOP - PAD_BOTTOM
  // Distribute points evenly; guard the single-point case to avoid divide-by-zero.
  const stepX = points.length > 1 ? plotW / (points.length - 1) : 0

  const xAt = (i: number) =>
    points.length > 1 ? PAD_X + stepX * i : PAD_X + plotW / 2
  const yAtTemp = (temp: number) =>
    PAD_TOP + (1 - (temp - minTemp) / tempRange) * plotH
  // Bar width: leave a small gap between bars, fall back to a fixed width for one point.
  const barW = points.length > 1 ? Math.max(2, stepX * 0.55) : Math.min(40, plotW * 0.5)

  const linePoints = points
    .map((p, i) => `${xAt(i).toFixed(1)},${yAtTemp(p.temp).toFixed(1)}`)
    .join(' ')

  const chartLabel = t('chart.ariaChart', {
    hours: points.length,
    min: Math.round(minTemp),
    max: Math.round(maxTemp),
  })

  return (
    <section className="bg-gray-800 rounded-xl p-6 mb-6">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        aria-controls={tableId}
        aria-label={expanded ? t('chart.collapse') : t('chart.expand')}
        className="w-full text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 rounded-lg"
      >
        <div className="flex items-center justify-between mb-4">
          <div>
            <h2 className="text-lg font-semibold">{t('chart.title')}</h2>
            <p className="text-xs text-gray-400">
              {expanded ? t('chart.collapseHint') : t('chart.expandHint')}
            </p>
          </div>
          <ChevronDown
            size={20}
            className={`text-gray-400 shrink-0 transition-transform ${expanded ? 'rotate-180' : ''}`}
          />
        </div>

        <svg
          viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
          role="img"
          aria-label={chartLabel}
          className="w-full h-auto"
        >
          {/* Precipitation bars (drawn first so the temperature line sits on top). */}
          {maxPrecip > 0 &&
            points.map((p, i) => {
              if (p.precip <= 0) return null
              const barH = (p.precip / maxPrecip) * plotH
              const x = xAt(i) - barW / 2
              const y = VIEW_H - PAD_BOTTOM - barH
              return (
                <rect
                  key={`bar-${p.time}`}
                  x={x.toFixed(1)}
                  y={y.toFixed(1)}
                  width={barW.toFixed(1)}
                  height={barH.toFixed(1)}
                  rx={1}
                  className="fill-blue-500/40"
                />
              )
            })}

          {/* Baseline along the x-axis. */}
          <line
            x1={PAD_X}
            y1={VIEW_H - PAD_BOTTOM}
            x2={VIEW_W - PAD_X}
            y2={VIEW_H - PAD_BOTTOM}
            className="stroke-gray-700"
            strokeWidth={1}
          />

          {/* Temperature line. */}
          {points.length > 1 && (
            <polyline
              points={linePoints}
              fill="none"
              className="stroke-orange-400"
              strokeWidth={2}
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          )}

          {/* Temperature points with a label at each end of the window. */}
          {points.map((p, i) => {
            const x = xAt(i)
            const y = yAtTemp(p.temp)
            const showLabel =
              points.length === 1 || i === 0 || i === points.length - 1
            return (
              <g key={`pt-${p.time}`}>
                <circle cx={x.toFixed(1)} cy={y.toFixed(1)} r={2.5} className="fill-orange-400" />
                {showLabel && (
                  <text
                    x={x.toFixed(1)}
                    y={(y - 8).toFixed(1)}
                    textAnchor={i === points.length - 1 && points.length > 1 ? 'end' : 'middle'}
                    className="fill-gray-200 text-[11px]"
                  >
                    {Math.round(p.temp)}°
                  </text>
                )}
              </g>
            )
          })}

          {/* Hour labels along the x-axis (every few hours to avoid crowding). */}
          {points.map((p, i) => {
            const everyN = points.length > 8 ? Math.ceil(points.length / 6) : 1
            if (i % everyN !== 0 && i !== points.length - 1) return null
            return (
              <text
                key={`hr-${p.time}`}
                x={xAt(i).toFixed(1)}
                y={VIEW_H - 8}
                textAnchor="middle"
                className="fill-gray-500 text-[10px]"
              >
                {formatTime(p.time, { hour: 'numeric', hour12: false })}
              </text>
            )
          })}
        </svg>

        <div className="mt-3 flex items-center gap-4 text-xs text-gray-400">
          <span className="flex items-center gap-1.5">
            <span className="inline-block w-3 h-0.5 bg-orange-400 rounded" />
            {t('chart.tempLegend')}
          </span>
          <span className="flex items-center gap-1.5">
            <span className="inline-block w-3 h-3 bg-blue-500/40 rounded-sm" />
            {t('chart.precipLegend')}
          </span>
        </div>
      </button>

      {expanded && (
        <div id={tableId} className="mt-4 overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-gray-400 border-b border-gray-700">
                <th scope="col" className="py-2 pr-3 font-medium">{t('chart.table.hour')}</th>
                <th scope="col" className="py-2 pr-3 font-medium">{t('chart.table.temp')}</th>
                <th scope="col" className="py-2 pr-3 font-medium">{t('chart.table.precip')}</th>
                <th scope="col" className="py-2 font-medium">{t('chart.table.symbol')}</th>
              </tr>
            </thead>
            <tbody>
              {points.map((p) => (
                <tr key={`row-${p.time}`} className="border-b border-gray-700/50 last:border-0">
                  <td className="py-2 pr-3 whitespace-nowrap text-gray-300">
                    {formatTime(p.time, { hour: '2-digit', minute: '2-digit' })}
                  </td>
                  <td className="py-2 pr-3 whitespace-nowrap font-medium">
                    {Math.round(p.temp)}°
                  </td>
                  <td className="py-2 pr-3 whitespace-nowrap text-blue-400">
                    {p.precip > 0 ? t('chart.table.precipValue', { mm: p.precip }) : '—'}
                  </td>
                  <td className="py-2">
                    <span className="flex items-center gap-2 text-gray-300">
                      <span className="text-blue-400">{getWeatherIcon(p.symbol, 20, getWeatherDescription(p.symbol, t))}</span>
                      <span className="hidden sm:inline" aria-hidden="true">{getWeatherDescription(p.symbol, t)}</span>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
