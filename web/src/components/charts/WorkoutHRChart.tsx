import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ReferenceLine,
} from 'recharts'
import type { Sample } from '../../types/training'
import { rollingAvg } from './chartUtils'

interface Props {
  samples: Sample[]
  avgHeartRate?: number
  height?: number
}

const SMOOTHING_WINDOW = 12

export default function WorkoutHRChart({ samples, avgHeartRate, height = 250 }: Props) {
  const rawData = samples
    .filter((s) => s.hr && s.hr > 0)
    .map((s) => ({
      time: s.t / 60000,
      hr: s.hr as number,
    }))

  if (rawData.length === 0) {
    return <p className="text-gray-500 text-sm">No heart rate data available</p>
  }

  const smoothedValues = rollingAvg(
    rawData.map((d) => d.hr),
    SMOOTHING_WINDOW,
  )
  const data = rawData.map((d, i) => ({ ...d, hr: smoothedValues[i] }))

  // Use the workout-level avg HR to match what's shown in summary stats.
  const avgHR = avgHeartRate && avgHeartRate > 0 ? avgHeartRate : 0

  return (
    <div className="w-full" style={{ height }} role="img" aria-label="Heart rate over time">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
          <XAxis
            dataKey="time"
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            tickFormatter={(v: number) => Math.round(v).toString()}
            label={{ value: 'Minutes', position: 'insideBottom', offset: -3, fill: '#9ca3af', fontSize: 11 }}
          />
          <YAxis
            domain={[(min: number) => Math.floor(min) - 10, (max: number) => Math.ceil(max) + 10]}
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            tickFormatter={(v: number) => Math.round(v).toString()}
            label={{ value: 'BPM', angle: -90, position: 'insideLeft', fill: '#9ca3af', fontSize: 11 }}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: '#1f2937',
              border: '1px solid #374151',
              borderRadius: '8px',
              color: '#e5e7eb',
            }}
            formatter={(value) => [`${Math.round(Number(value))} bpm`, 'Heart Rate']}
            labelFormatter={(label) => `${Math.round(Number(label))} min`}
          />
          {avgHR > 0 && (
            <ReferenceLine
              y={avgHR}
              stroke="#9ca3af"
              strokeDasharray="5 5"
              label={{ value: `Avg ${Math.round(avgHR)} bpm`, position: 'right', fill: '#9ca3af', fontSize: 11 }}
            />
          )}
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
