import { useState } from 'react'
import {
  ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, Legend,
} from 'recharts'

interface TrendPoint {
  date: string
  label: string
  speed: number
  lactate: number
  hr: number
}

interface Props {
  data: TrendPoint[]
}

type Metric = 'speed' | 'lactate' | 'hr'

const metricConfig: Record<Metric, { label: string; unit: string; color: string }> = {
  speed: { label: 'Threshold Speed', unit: 'km/h', color: '#3b82f6' },
  lactate: { label: 'Threshold Lactate', unit: 'mmol/L', color: '#f59e0b' },
  hr: { label: 'Threshold HR', unit: 'bpm', color: '#ef4444' },
}

export default function ThresholdTrendsChart({ data }: Props) {
  const [metric, setMetric] = useState<Metric>('speed')

  if (data.length < 2) {
    return (
      <p className="text-gray-500 text-sm text-center py-8">
        At least 2 tests are needed to show trends.
      </p>
    )
  }

  const config = metricConfig[metric]
  const filtered = metric === 'hr' ? data.filter((d) => d.hr > 0) : data

  return (
    <div>
      <div className="flex gap-2 mb-4">
        {(Object.keys(metricConfig) as Metric[]).map((m) => (
          <button
            key={m}
            onClick={() => setMetric(m)}
            className={`px-3 py-1.5 text-xs rounded-lg transition-colors cursor-pointer ${
              metric === m
                ? 'bg-blue-500/20 text-blue-400 border border-blue-500/40'
                : 'bg-gray-700 text-gray-400 border border-gray-600 hover:text-white'
            }`}
          >
            {metricConfig[m].label}
          </button>
        ))}
      </div>
      <div className="w-full h-64">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={filtered} margin={{ top: 10, right: 20, left: 0, bottom: 5 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
            <XAxis
              dataKey="label"
              tick={{ fill: '#9ca3af', fontSize: 11 }}
              stroke="#4b5563"
            />
            <YAxis
              tick={{ fill: '#9ca3af', fontSize: 11 }}
              stroke="#4b5563"
              label={{ value: `${config.label} (${config.unit})`, angle: -90, position: 'insideLeft', offset: 10, fill: '#9ca3af', fontSize: 12 }}
            />
            <Tooltip
              contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px' }}
              labelStyle={{ color: '#9ca3af' }}
              formatter={(value: number) => [`${metric === 'hr' ? value : value.toFixed(2)} ${config.unit}`, config.label]}
            />
            <Legend wrapperStyle={{ color: '#9ca3af', fontSize: 12 }} />
            <Line
              type="monotone"
              dataKey={metric}
              stroke={config.color}
              strokeWidth={2}
              dot={{ fill: config.color, r: 5 }}
              activeDot={{ r: 7 }}
              name={config.label}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}
