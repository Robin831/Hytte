import {
  ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, Legend,
} from 'recharts'
import type { Stage } from '../../types/lactate'

interface Props {
  stages: Stage[]
}

export default function DualAxisChart({ stages }: Props) {
  const data = stages
    .slice()
    .sort((a, b) => a.speed_kmh - b.speed_kmh)
    .filter((s) => s.heart_rate_bpm > 0)
    .map((s) => ({
      speed: s.speed_kmh,
      lactate: s.lactate_mmol,
      hr: s.heart_rate_bpm,
    }))

  if (data.length < 2) return null

  return (
    <div className="w-full h-72" role="img" aria-label="Dual-axis lactate and heart rate chart">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 10, right: 20, left: 0, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
          <XAxis
            dataKey="speed"
            label={{ value: 'Speed (km/h)', position: 'insideBottom', offset: -3, fill: '#9ca3af', fontSize: 12 }}
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            stroke="#4b5563"
          />
          <YAxis
            yAxisId="lactate"
            label={{ value: 'Lactate (mmol/L)', angle: -90, position: 'insideLeft', offset: 10, fill: '#3b82f6', fontSize: 12 }}
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            stroke="#3b82f6"
          />
          <YAxis
            yAxisId="hr"
            orientation="right"
            label={{ value: 'Heart Rate (bpm)', angle: 90, position: 'insideRight', offset: 10, fill: '#ef4444', fontSize: 12 }}
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            stroke="#ef4444"
          />
          <Tooltip
            contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px' }}
            labelStyle={{ color: '#9ca3af' }}
            formatter={(value: number, name: string) => {
              if (name === 'Lactate') return [`${value.toFixed(1)} mmol/L`, name]
              return [`${value} bpm`, name]
            }}
            labelFormatter={(label: number) => `${label.toFixed(1)} km/h`}
          />
          <Legend wrapperStyle={{ color: '#9ca3af', fontSize: 12 }} />
          <Line
            yAxisId="lactate"
            type="monotone"
            dataKey="lactate"
            stroke="#3b82f6"
            strokeWidth={2}
            dot={{ fill: '#3b82f6', r: 4 }}
            activeDot={{ r: 6 }}
            name="Lactate"
          />
          <Line
            yAxisId="hr"
            type="monotone"
            dataKey="hr"
            stroke="#ef4444"
            strokeWidth={2}
            dot={{ fill: '#ef4444', r: 4 }}
            activeDot={{ r: 6 }}
            name="Heart Rate"
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
