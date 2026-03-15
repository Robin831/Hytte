import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
} from 'recharts'
import type { Sample } from '../../types/training'

interface Props {
  samples: Sample[]
  height?: number
}

function speedToPace(speedMPerS: number): number {
  if (speedMPerS <= 0) return 0
  return 1000 / speedMPerS / 60 // min/km
}

function formatPace(paceMinPerKm: number): string {
  if (paceMinPerKm <= 0 || paceMinPerKm > 30) return '--:--'
  let mins = Math.floor(paceMinPerKm)
  let secs = Math.round((paceMinPerKm - mins) * 60)
  if (secs === 60) { mins++; secs = 0 }
  return `${mins}:${secs.toString().padStart(2, '0')}`
}

export default function WorkoutPaceChart({ samples, height = 250 }: Props) {
  // Downsample for performance and smooth the pace data.
  const step = Math.max(1, Math.floor(samples.length / 300))
  const data: { time: number; pace: number }[] = []

  for (let i = 0; i < samples.length; i += step) {
    const s = samples[i]
    if (s.spd && s.spd > 0.5) {
      data.push({
        time: Math.round(s.t / 60000),
        pace: speedToPace(s.spd),
      })
    }
  }

  if (data.length === 0) {
    return <p className="text-gray-500 text-sm">No pace data available</p>
  }

  return (
    <div className="w-full" style={{ height }} role="img" aria-label="Pace over time">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
          <XAxis
            dataKey="time"
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            label={{ value: 'Minutes', position: 'insideBottom', offset: -3, fill: '#9ca3af', fontSize: 11 }}
          />
          <YAxis
            reversed
            domain={['dataMin - 0.5', 'dataMax + 0.5']}
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            tickFormatter={(v: number) => formatPace(v)}
            label={{ value: 'min/km', angle: -90, position: 'insideLeft', fill: '#9ca3af', fontSize: 11 }}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: '#1f2937',
              border: '1px solid #374151',
              borderRadius: '8px',
              color: '#e5e7eb',
            }}
            formatter={(value) => [formatPace(Number(value)), 'Pace']}
            labelFormatter={(label) => `${String(label)} min`}
          />
          <Line
            type="monotone"
            dataKey="pace"
            stroke="#3b82f6"
            strokeWidth={1.5}
            dot={false}
            name="Pace"
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
