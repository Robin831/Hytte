import { useState, useMemo } from 'react'
import {
  ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, Legend, ReferenceLine,
} from 'recharts'
import type { LactateTest } from '../../types/lactate'

interface Props {
  tests: LactateTest[]
}

function interpolateLactate(stages: LactateTest['stages'], targetSpeed: number): number | null {
  const sorted = stages.slice().sort((a, b) => a.speed_kmh - b.speed_kmh)
  if (sorted.length === 0) return null

  // Exact match
  const exact = sorted.find((s) => Math.abs(s.speed_kmh - targetSpeed) < 0.01)
  if (exact) return exact.lactate_mmol

  // Out of range
  if (targetSpeed < sorted[0].speed_kmh || targetSpeed > sorted[sorted.length - 1].speed_kmh) return null

  // Linear interpolation between bracketing stages
  for (let i = 0; i < sorted.length - 1; i++) {
    if (sorted[i].speed_kmh <= targetSpeed && sorted[i + 1].speed_kmh >= targetSpeed) {
      const ratio = (targetSpeed - sorted[i].speed_kmh) / (sorted[i + 1].speed_kmh - sorted[i].speed_kmh)
      return sorted[i].lactate_mmol + ratio * (sorted[i + 1].lactate_mmol - sorted[i].lactate_mmol)
    }
  }
  return null
}

export default function FixedSpeedChart({ tests }: Props) {
  // Collect all unique speeds across tests
  const availableSpeeds = useMemo(() => {
    const speeds = new Set<number>()
    tests.forEach((t) => t.stages.forEach((s) => speeds.add(s.speed_kmh)))
    return Array.from(speeds).sort((a, b) => a - b)
  }, [tests])

  const [selectedSpeed, setSelectedSpeed] = useState<number>(() =>
    availableSpeeds.length > 0 ? availableSpeeds[Math.floor(availableSpeeds.length / 2)] : 10
  )

  // Derive effective speed during render to avoid setState-in-effect
  const effectiveSpeed = useMemo(() => {
    if (availableSpeeds.includes(selectedSpeed)) return selectedSpeed
    return availableSpeeds.length > 0 ? availableSpeeds[Math.floor(availableSpeeds.length / 2)] : selectedSpeed
  }, [availableSpeeds, selectedSpeed])

  const data = useMemo(() => {
    return tests
      .slice()
      .sort((a, b) => a.date.localeCompare(b.date))
      .map((t) => {
        const lactate = interpolateLactate(t.stages, effectiveSpeed)
        if (lactate === null) return null
        const [y, m, d] = t.date.split('-').map(Number)
        return {
          date: t.date,
          label: new Date(y, m - 1, d).toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
          lactate: Math.round(lactate * 100) / 100,
          comment: t.comment,
        }
      })
      .filter((d): d is NonNullable<typeof d> => d !== null)
  }, [tests, effectiveSpeed])

  if (availableSpeeds.length === 0) {
    return <p className="text-gray-500 text-sm text-center py-8">No test data available.</p>
  }

  return (
    <div>
      <div className="flex items-center gap-3 mb-4">
        <label htmlFor="fixed-speed-select" className="text-sm text-gray-400">Track lactate at:</label>
        <select
          id="fixed-speed-select"
          value={effectiveSpeed}
          onChange={(e) => setSelectedSpeed(Number(e.target.value))}
          className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-1.5 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          {availableSpeeds.map((s) => (
            <option key={s} value={s}>{s.toFixed(1)} km/h</option>
          ))}
        </select>
      </div>

      {data.length < 2 ? (
        <p className="text-gray-500 text-sm text-center py-8">
          Not enough tests with data at {effectiveSpeed.toFixed(1)} km/h to show a trend.
        </p>
      ) : (
        <div className="w-full h-64" role="img" aria-label={`Lactate at ${effectiveSpeed.toFixed(1)} km/h over time chart`}>
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={data} margin={{ top: 10, right: 20, left: 0, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
              <XAxis
                dataKey="label"
                tick={{ fill: '#9ca3af', fontSize: 11 }}
                stroke="#4b5563"
              />
              <YAxis
                tick={{ fill: '#9ca3af', fontSize: 11 }}
                stroke="#4b5563"
                label={{ value: 'Lactate (mmol/L)', angle: -90, position: 'insideLeft', offset: 10, fill: '#9ca3af', fontSize: 12 }}
              />
              <Tooltip
                contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px' }}
                labelStyle={{ color: '#9ca3af' }}
                formatter={(value: number) => [`${value.toFixed(2)} mmol/L`, 'Lactate']}
              />
              <Legend wrapperStyle={{ color: '#9ca3af', fontSize: 12 }} />
              <ReferenceLine
                y={4.0}
                stroke="#6b7280"
                strokeDasharray="6 3"
                label={{ value: '4.0', position: 'right', fill: '#6b7280', fontSize: 10 }}
              />
              <Line
                type="monotone"
                dataKey="lactate"
                stroke="#f59e0b"
                strokeWidth={2}
                dot={{ fill: '#f59e0b', r: 5 }}
                activeDot={{ r: 7 }}
                name={`Lactate @ ${effectiveSpeed.toFixed(1)} km/h`}
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  )
}
