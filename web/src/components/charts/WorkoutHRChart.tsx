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

export default function WorkoutHRChart({ samples, height = 250 }: Props) {
  const data = samples
    .filter((s) => s.hr && s.hr > 0)
    .map((s) => ({
      time: Math.round(s.t / 60000),
      hr: s.hr,
    }))

  if (data.length === 0) {
    return <p className="text-gray-500 text-sm">No heart rate data available</p>
  }

  return (
    <div className="w-full" style={{ height }} role="img" aria-label="Heart rate over time">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
          <XAxis
            dataKey="time"
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            label={{ value: 'Minutes', position: 'insideBottom', offset: -3, fill: '#9ca3af', fontSize: 11 }}
          />
          <YAxis
            domain={['dataMin - 10', 'dataMax + 10']}
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            label={{ value: 'BPM', angle: -90, position: 'insideLeft', fill: '#9ca3af', fontSize: 11 }}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: '#1f2937',
              border: '1px solid #374151',
              borderRadius: '8px',
              color: '#e5e7eb',
            }}
            formatter={(value: number) => [`${value} bpm`, 'Heart Rate']}
            labelFormatter={(label: number) => `${label} min`}
          />
          <Line
            type="monotone"
            dataKey="hr"
            stroke="#ef4444"
            strokeWidth={1.5}
            dot={false}
            name="Heart Rate"
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
