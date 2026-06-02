import { useTranslation } from 'react-i18next'
import type { DaySummary } from '../types'
import { formatMins } from '../dateUtils'

export default function FlexTrendChart({ summaries }: { summaries: DaySummary[] }) {
  const { t } = useTranslation('workhours')

  // Use only work days (Mon-Fri), sorted chronologically
  const dataPoints = summaries
    .filter(s => {
      const dow = new Date(s.date + 'T12:00:00').getDay()
      return dow !== 0 && dow !== 6
    })
    .sort((a, b) => a.date.localeCompare(b.date))

  if (dataPoints.length === 0) return null

  // Cumulative remainder
  const points = dataPoints.reduce<number[]>((acc, s) => {
    acc.push((acc.length > 0 ? acc[acc.length - 1] : 0) + s.remainder_minutes)
    return acc
  }, [])

  const W = 400
  const H = 72
  const PX = 4
  const PY = 8
  const chartW = W - 2 * PX
  const chartH = H - 2 * PY

  const minVal = Math.min(0, ...points)
  const maxVal = Math.max(0, ...points)
  const range = maxVal - minVal || 1

  const n = points.length
  const toX = (i: number) => PX + (n > 1 ? (i / (n - 1)) * chartW : chartW / 2)
  const toY = (v: number) => PY + chartH - ((v - minVal) / range) * chartH

  const polyPts = points.map((v, i) => `${toX(i).toFixed(1)},${toY(v).toFixed(1)}`).join(' ')
  const zeroY = toY(0).toFixed(1)
  const lastVal = points[points.length - 1]
  const lineColor = lastVal >= 0 ? '#4ade80' : '#f87171'

  return (
    <section className="space-y-2">
      <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
        {t('flexTrend')}
      </h2>
      <div className="bg-gray-800 rounded-lg px-3 py-3">
        <svg viewBox={`0 0 ${W} ${H}`} className="w-full" aria-hidden="true">
          {/* Zero reference line */}
          <line
            x1={PX}
            y1={zeroY}
            x2={W - PX}
            y2={zeroY}
            stroke="#374151"
            strokeWidth="1"
            strokeDasharray="4 3"
          />
          {/* Trend line */}
          {n > 1 && (
            <polyline points={polyPts} fill="none" stroke={lineColor} strokeWidth="2" strokeLinejoin="round" />
          )}
          {/* Data points */}
          {points.map((v, i) => (
            <circle key={dataPoints[i].date} cx={toX(i).toFixed(1)} cy={toY(v).toFixed(1)} r="3" fill={lineColor} />
          ))}
        </svg>
        <div className="flex justify-between text-xs text-gray-500 mt-1 px-1">
          <span>{dataPoints[0]?.date.slice(8)}</span>
          <span
            className={`font-mono font-medium ${lastVal >= 0 ? 'text-green-400' : 'text-red-400'}`}
          >
            {lastVal > 0 ? '+' : ''}
            {formatMins(lastVal)}
          </span>
          <span>{dataPoints[dataPoints.length - 1]?.date.slice(8)}</span>
        </div>
      </div>
    </section>
  )
}
