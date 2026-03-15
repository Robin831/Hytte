import { useState, useMemo } from 'react'
import {
  ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, Legend,
} from 'recharts'
import type { LactateTest } from '../../types/lactate'

interface Props {
  tests: LactateTest[]
}

const lineColors = ['#3b82f6', '#ef4444', '#10b981', '#f59e0b', '#8b5cf6', '#ec4899', '#06b6d4', '#f97316']

export default function ComparisonChart({ tests }: Props) {
  const [selectedIds, setSelectedIds] = useState<Set<number>>(() => {
    const recent = tests.slice(0, 3)
    return new Set(recent.map((t) => t.id))
  })

  // Derive effective selection during render to avoid stale IDs without setState-in-effect
  const validSelectedIds = useMemo(() => {
    const validIds = new Set(tests.map((t) => t.id))
    const cleaned = new Set([...selectedIds].filter((id) => validIds.has(id)))
    if (cleaned.size === 0) {
      return new Set(tests.slice(0, 3).map((t) => t.id))
    }
    return cleaned
  }, [selectedIds, tests])

  const toggleTest = (id: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const selectedTests = useMemo(
    () => tests.filter((t) => validSelectedIds.has(t.id)),
    [tests, validSelectedIds]
  )

  // Build unified data array with all speeds as x-axis
  const { data, testLabels } = useMemo(() => {
    const allSpeeds = new Set<number>()
    selectedTests.forEach((t) => t.stages.forEach((s) => allSpeeds.add(s.speed_kmh)))
    const speeds = Array.from(allSpeeds).sort((a, b) => a - b)

    const labels = selectedTests.map((t) => {
      const [y, m, d] = t.date.split('-').map(Number)
      const dateStr = new Date(y, m - 1, d).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: '2-digit' })
      return t.comment ? `${dateStr} - ${t.comment}` : dateStr
    })

    const points = speeds.map((speed) => {
      const point: Record<string, number> = { speed }
      selectedTests.forEach((t, idx) => {
        const stage = t.stages.find((s) => Math.abs(s.speed_kmh - speed) < 0.01)
        if (stage) {
          point[`test_${idx}`] = stage.lactate_mmol
        }
      })
      return point
    })

    return { data: points, testLabels: labels }
  }, [selectedTests])

  const formatDate = (t: LactateTest) => {
    const [y, m, d] = t.date.split('-').map(Number)
    return new Date(y, m - 1, d).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  }

  return (
    <div>
      {/* Test selector */}
      <div className="flex flex-wrap gap-2 mb-4" role="group" aria-label="Select tests to compare">
        {tests.map((t) => (
          <button
            key={t.id}
            onClick={() => toggleTest(t.id)}
            aria-pressed={validSelectedIds.has(t.id)}
            className={`px-3 py-1.5 text-xs rounded-lg transition-colors cursor-pointer ${
              validSelectedIds.has(t.id)
                ? 'bg-blue-500/20 text-blue-400 border border-blue-500/40'
                : 'bg-gray-700 text-gray-400 border border-gray-600 hover:text-white'
            }`}
          >
            {formatDate(t)}{t.comment ? ` - ${t.comment}` : ''}
          </button>
        ))}
      </div>

      {selectedTests.length === 0 ? (
        <p className="text-gray-500 text-sm text-center py-8">Select tests to compare.</p>
      ) : (
        <div className="w-full h-72" role="img" aria-label="Lactate curve comparison chart">
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
                labelFormatter={(label: unknown) => `${(label as number).toFixed(1)} km/h`}
                formatter={(value: unknown, name: string | number | undefined) => {
                  const n = String(name ?? '')
                  const idx = parseInt(n.replace('test_', ''))
                  return [`${(value as number).toFixed(1)} mmol/L`, testLabels[idx] || n]
                }}
              />
              <Legend
                wrapperStyle={{ color: '#9ca3af', fontSize: 12 }}
                formatter={(_value, entry) => {
                  const key = String(entry.dataKey ?? '')
                  const idx = parseInt(key.replace('test_', ''))
                  return testLabels[idx] || key
                }}
              />
              {selectedTests.map((_t, idx) => (
                <Line
                  key={idx}
                  type="monotone"
                  dataKey={`test_${idx}`}
                  stroke={lineColors[idx % lineColors.length]}
                  strokeWidth={2}
                  dot={{ fill: lineColors[idx % lineColors.length], r: 4 }}
                  activeDot={{ r: 6 }}
                  connectNulls={false}
                  name={`test_${idx}`}
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  )
}
