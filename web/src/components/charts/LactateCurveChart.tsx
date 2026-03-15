import {
  ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, ReferenceLine, Legend,
} from 'recharts'
import type { Stage, ThresholdResult } from '../../types/lactate'

interface Props {
  stages: Stage[]
  thresholds?: ThresholdResult[]
  selectedMethod?: string
}

const thresholdColors: Record<string, string> = {
  OBLA: '#f59e0b',
  Dmax: '#10b981',
  ModDmax: '#8b5cf6',
  'Log-log': '#ec4899',
  ExpDmax: '#06b6d4',
}

export default function LactateCurveChart({ stages, thresholds, selectedMethod }: Props) {
  const data = stages
    .slice()
    .sort((a, b) => a.speed_kmh - b.speed_kmh)
    .map((s) => ({
      speed: s.speed_kmh,
      lactate: s.lactate_mmol,
    }))

  const validThresholds = thresholds?.filter((t) => t.valid) ?? []
  const displayThresholds = selectedMethod
    ? validThresholds.filter((t) => t.method === selectedMethod)
    : validThresholds

  return (
    <div className="w-full h-72">
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
            label={{ value: 'Lactate (mmol/L)', angle: -90, position: 'insideLeft', offset: 10, fill: '#9ca3af', fontSize: 12 }}
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            stroke="#4b5563"
          />
          <Tooltip
            contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px' }}
            labelStyle={{ color: '#9ca3af' }}
            itemStyle={{ color: '#e5e7eb' }}
            formatter={(value: number) => [`${value.toFixed(1)} mmol/L`, 'Lactate']}
            labelFormatter={(label: number) => `${label.toFixed(1)} km/h`}
          />
          <Legend wrapperStyle={{ color: '#9ca3af', fontSize: 12 }} />
          <Line
            type="monotone"
            dataKey="lactate"
            stroke="#3b82f6"
            strokeWidth={2}
            dot={{ fill: '#3b82f6', r: 4 }}
            activeDot={{ r: 6 }}
            name="Lactate"
          />
          {/* OBLA 4.0 reference line */}
          <ReferenceLine
            y={4.0}
            stroke="#6b7280"
            strokeDasharray="6 3"
            label={{ value: '4.0 mmol/L', position: 'right', fill: '#6b7280', fontSize: 10 }}
          />
          {/* Threshold markers as vertical lines */}
          {displayThresholds.map((t) => (
            <ReferenceLine
              key={t.method}
              x={t.speed_kmh}
              stroke={thresholdColors[t.method] || '#f59e0b'}
              strokeDasharray="4 4"
              strokeWidth={2}
              label={{
                value: `${t.method} ${t.speed_kmh.toFixed(1)}`,
                position: 'top',
                fill: thresholdColors[t.method] || '#f59e0b',
                fontSize: 10,
              }}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
