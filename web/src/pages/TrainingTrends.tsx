import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, TrendingUp, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { isAutoTag, displayTag, AUTO_TAG_TOOLTIP } from '../tags'
import { formatDate } from '../utils/formatDate'
import { AcrGauge } from '../components/AcrGauge'
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  BarChart,
  Bar,
  ComposedChart,
  Legend,
} from 'recharts'
import { useAuth } from '../auth'
import type { WeeklySummary, ProgressionGroup, TrainingLoadResponse } from '../types/training'

interface WeeklyLoadChartProps {
  data: TrainingLoadResponse
}

function WeeklyLoadChart({ data }: WeeklyLoadChartProps) {
  const { t } = useTranslation('training')

  const chronological = data.weeks.slice().reverse()
  const chartData = chronological.map((w, i) => {
    const windowStart = Math.max(0, i - 3)
    const window = chronological.slice(windowStart, i + 1)
    const chronicLoad = window.reduce((sum, wk) => sum + wk.total_load, 0) / window.length
    return {
      week: formatDate(w.week_start + 'T00:00:00', { month: 'short', day: 'numeric' }),
      easy_load: Number(w.easy_load.toFixed(1)),
      hard_load: Number(w.hard_load.toFixed(1)),
      chronic_load: Number(chronicLoad.toFixed(1)),
    }
  })

  const statusLabel = t(`trends.weeklyLoad.statusLabels.${data.status}`, { defaultValue: data.status })

  return (
    <div className="bg-gray-800 rounded-xl p-6 mb-6">
      <div className="flex items-center justify-between mb-1">
        <div>
          <h2 className="text-lg font-semibold">{t('trends.weeklyLoad.title')}</h2>
          <p className="text-xs text-gray-400 mt-0.5">{t('trends.weeklyLoad.status')} {statusLabel}</p>
        </div>
        {data.acr != null && (
          <div
            className="w-28 h-16 shrink-0"
            aria-label={t('trends.acrGauge.ariaLabel', { value: data.acr.toFixed(2) })}
          >
            <AcrGauge
              acr={data.acr}
              ariaLabel={t('trends.acrGauge.ariaLabel', { value: data.acr.toFixed(2) })}
            />
          </div>
        )}
      </div>
      <div className="w-full h-64 mt-4" role="img" aria-label={t('trends.weeklyLoad.ariaLabel')}>
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart data={chartData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
            <XAxis dataKey="week" tick={{ fill: '#9ca3af', fontSize: 10 }} />
            <YAxis tick={{ fill: '#9ca3af', fontSize: 11 }} />
            <Tooltip
              contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px', color: '#e5e7eb' }}
            />
            <Legend wrapperStyle={{ fontSize: 12, color: '#9ca3af' }} />
            <Bar dataKey="easy_load" stackId="load" fill="#22c55e" radius={[0, 0, 0, 0]} name={t('trends.weeklyLoad.easyLoad')} />
            <Bar dataKey="hard_load" stackId="load" fill="#f97316" radius={[4, 4, 0, 0]} name={t('trends.weeklyLoad.hardLoad')} />
            <Line
              type="monotone"
              dataKey="chronic_load"
              stroke="#a78bfa"
              strokeWidth={2}
              dot={false}
              name={t('trends.weeklyLoad.chronicLoad')}
            />
          </ComposedChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}

function formatPace(secPerKm: number): string {
  if (secPerKm <= 0) return '--:--'
  let mins = Math.floor(secPerKm / 60)
  let secs = Math.round(secPerKm % 60)
  if (secs === 60) { mins++; secs = 0 }
  return `${mins}:${secs.toString().padStart(2, '0')}`
}

export default function TrainingTrends() {
  const { user } = useAuth()
  const { t } = useTranslation(['training', 'common'])
  const [summaries, setSummaries] = useState<WeeklySummary[]>([])
  const [groups, setGroups] = useState<ProgressionGroup[]>([])
  const [loadData, setLoadData] = useState<TrainingLoadResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedGroup, setSelectedGroup] = useState<string>('')

  function formatDuration(seconds: number): string {
    const h = (seconds / 3600).toFixed(1)
    return `${h}${t('units.h')}`
  }

  useEffect(() => {
    if (!user) return
    const load = async () => {
      try {
        const [sRes, pRes, lRes] = await Promise.all([
          fetch('/api/training/summary', { credentials: 'include' }),
          fetch('/api/training/progression', { credentials: 'include' }),
          fetch('/api/training/load?weeks=12', { credentials: 'include' }),
        ])
        if (sRes.ok) {
          const sData = await sRes.json()
          setSummaries(sData.summaries || [])
        } else {
          setError(t('errors.failedToLoadSummaries'))
        }
        if (pRes.ok) {
          const pData = await pRes.json()
          setGroups(pData.groups || [])
          if (pData.groups?.length > 0) {
            setSelectedGroup(`${pData.groups[0].tag}:${pData.groups[0].sport}:${pData.groups[0].lap_count}`)
          }
        } else {
          setError(t('errors.failedToLoadProgressionData'))
        }
        if (lRes.ok) {
          const lData = await lRes.json()
          setLoadData(lData)
        } else {
          setError(t('errors.failedToLoadTrainingLoad'))
        }
      } catch {
        setError(t('errors.failedToLoadTrendData'))
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [user, t])

  const groupKey = (g: ProgressionGroup) => `${g.tag}:${g.sport}:${g.lap_count}`
  const tagCounts = groups.reduce((acc, g) => { acc[g.tag] = (acc[g.tag] || 0) + 1; return acc }, {} as Record<string, number>)
  const activeGroup = groups.find((g) => groupKey(g) === selectedGroup)

  // Prepare weekly volume chart data (most recent first, reverse for chart).
  const volumeData = summaries
    .slice()
    .reverse()
    .map((s) => ({
      week: formatDate(s.week_start + 'T00:00:00', { month: 'short', day: 'numeric' }),
      hours: Number((s.total_duration_seconds / 3600).toFixed(1)),
      km: Number((s.total_distance_meters / 1000).toFixed(1)),
      count: s.workout_count,
    }))

  // Prepare progression chart data.
  const progressionData = activeGroup?.workouts.map((w) => ({
    date: formatDate(w.date, { month: 'short', day: 'numeric' }),
    avgHR: w.avg_hr,
    avgPace: w.avg_pace_sec_per_km,
  })) || []

  if (loading) {
    return (
      <div className="max-w-4xl mx-auto px-4 py-8">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-gray-800 rounded w-48" />
          <div className="h-64 bg-gray-800 rounded" />
        </div>
      </div>
    )
  }

  return (
    <>
      {error && (
        <div className="max-w-4xl mx-auto px-4 pt-4">
          <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">{error}</div>
        </div>
      )}
    <div className="max-w-4xl mx-auto px-4 py-8">
      <Link to="/training" className="flex items-center gap-2 text-gray-400 hover:text-white mb-4 text-sm">
        <ArrowLeft size={16} /> {t('backToTraining')}
      </Link>

      <div className="flex items-center gap-3 mb-6">
        <TrendingUp size={24} className="text-green-400" />
        <h1 className="text-2xl font-bold">{t('trends.title')}</h1>
      </div>

      {/* Weekly load */}
      {loadData && loadData.weeks.length > 0 && (
        <WeeklyLoadChart data={loadData} />
      )}

      {/* Weekly volume */}
      {volumeData.length > 0 && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          <h2 className="text-lg font-semibold mb-4">{t('trends.weeklyVolume.title')}</h2>
          <div className="w-full h-64" role="img" aria-label={t('trends.weeklyVolume.ariaLabel')}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={volumeData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="week" tick={{ fill: '#9ca3af', fontSize: 10 }} />
                <YAxis tick={{ fill: '#9ca3af', fontSize: 11 }} label={{ value: t('trends.weeklyVolume.hours'), angle: -90, position: 'insideLeft', fill: '#9ca3af', fontSize: 11 }} />
                <Tooltip
                  contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px', color: '#e5e7eb' }}
                  formatter={(value, name) => {
                    if (name === 'hours') return [formatDuration(Number(value) * 3600), t('trends.weeklyVolume.duration')]
                    if (name === 'km') return [`${value} ${t('units.km')}`, t('trends.weeklyVolume.distance')]
                    return [value, name]
                  }}
                />
                <Bar dataKey="hours" fill="#3b82f6" radius={[4, 4, 0, 0]} name="hours" />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Progression by tag */}
      {groups.length > 0 && (
        <div className="bg-gray-800 rounded-xl p-6">
          <h2 className="text-lg font-semibold mb-4">{t('trends.progression.title')}</h2>

          <div className="flex gap-2 mb-4 flex-wrap">
            {groups.map((g) => {
              const key = groupKey(g)
              const isAuto = isAutoTag(g.tag)
              const tagName = displayTag(g.tag)
              const label = tagCounts[g.tag] > 1 ? t('trends.progression.groupLabel', { tag: tagName, sport: g.sport, lapCount: g.lap_count }) : tagName
              return (
              <button
                key={key}
                onClick={() => setSelectedGroup(key)}
                aria-pressed={selectedGroup === key}
                title={isAuto ? AUTO_TAG_TOOLTIP : undefined}
                className={`inline-flex items-center gap-1 px-3 py-1.5 text-xs rounded-lg transition-colors ${
                  selectedGroup === key
                    ? 'bg-blue-500/20 text-blue-400 border border-blue-500/40'
                    : 'bg-gray-700 text-gray-400 hover:text-white border border-gray-600'
                }`}
              >
                {isAuto && <Sparkles size={10} />}
                {label} ({g.workouts.length})
              </button>
              )
            })}
          </div>

          {progressionData.length > 1 && (
            <div className="space-y-6">
              {/* HR trend */}
              <div>
                <h3 className="text-sm text-gray-400 mb-2">{t('trends.hrTrend.title')}</h3>
                <div className="w-full h-48" role="img" aria-label={t('trends.hrTrend.ariaLabel')}>
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={progressionData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                      <XAxis dataKey="date" tick={{ fill: '#9ca3af', fontSize: 10 }} />
                      <YAxis domain={['dataMin - 5', 'dataMax + 5']} tick={{ fill: '#9ca3af', fontSize: 11 }} />
                      <Tooltip
                        contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px', color: '#e5e7eb' }}
                        formatter={(value) => [`${Number(value)} ${t('units.bpm')}`, t('trends.hrTrend.avgHR')]}
                      />
                      <Line type="monotone" dataKey="avgHR" stroke="#ef4444" strokeWidth={2} dot={{ r: 3, fill: '#ef4444' }} name={t('trends.hrTrend.avgHR')} />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
                <p className="text-xs text-gray-500 mt-1">
                  {t('trends.hrTrend.hint')}
                </p>
              </div>

              {/* Pace trend */}
              <div>
                <h3 className="text-sm text-gray-400 mb-2">{t('trends.paceTrend.title')}</h3>
                <div className="w-full h-48" role="img" aria-label={t('trends.paceTrend.ariaLabel')}>
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={progressionData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                      <XAxis dataKey="date" tick={{ fill: '#9ca3af', fontSize: 10 }} />
                      <YAxis
                        reversed
                        domain={['dataMin - 5', 'dataMax + 5']}
                        tick={{ fill: '#9ca3af', fontSize: 11 }}
                        tickFormatter={(v: number) => formatPace(v)}
                      />
                      <Tooltip
                        contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px', color: '#e5e7eb' }}
                        formatter={(value) => [formatPace(Number(value)), t('trends.paceTrend.avgPace')]}
                      />
                      <Line type="monotone" dataKey="avgPace" stroke="#3b82f6" strokeWidth={2} dot={{ r: 3, fill: '#3b82f6' }} name={t('trends.paceTrend.avgPace')} />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
                <p className="text-xs text-gray-500 mt-1">
                  {t('trends.paceTrend.hint')}
                </p>
              </div>
            </div>
          )}

          {progressionData.length <= 1 && activeGroup && (
            <p className="text-gray-500 text-sm">
              {t('trends.needMoreWorkouts', { tag: displayTag(activeGroup.tag) })}
            </p>
          )}
        </div>
      )}

      {groups.length === 0 && summaries.length === 0 && (!loadData || loadData.weeks.length === 0) && (
        <div className="bg-gray-800 rounded-xl p-12 text-center">
          <TrendingUp size={48} className="mx-auto mb-4 text-gray-600" />
          <h2 className="text-xl font-semibold mb-2">{t('trends.emptyTitle')}</h2>
          <p className="text-gray-400">{t('trends.emptyDescription')}</p>
        </div>
      )}
    </div>
    </>
  )
}
