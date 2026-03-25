import React, { useState, useEffect, useMemo } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Star, Flame, Trophy, ChevronLeft, ChevronRight } from 'lucide-react'
import {
  BarChart, Bar, LineChart, Line,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
} from 'recharts'
import { formatDate, formatNumber } from '../utils/formatDate'

interface ChildStats {
  current_balance: number
  total_earned: number
  total_spent: number
  level: number
  xp: number
  title: string
  current_streak: number
  longest_streak: number
  this_week_stars: number
  this_week_starred_workouts: number
  last_week_stars: number
  last_week_starred_workouts: number
  recent_transactions: StarTransaction[]
  active_challenges: ActiveChallenge[]
}

interface StarTransaction {
  id: number
  amount: number
  reason: string
  description: string
  created_at: string
}

interface ActiveChallenge {
  id: number
  title: string
  description: string
  progress: number
  goal: number
}

interface ChildWorkout {
  id: number
  started_at: string
  sport: string
  duration_seconds: number
  distance_meters: number
  avg_heart_rate: number
  calories: number
  ascent_meters: number
  stars: number
}

interface ChildInfo {
  nickname: string
  avatar_emoji: string
}

function xpForLevel(n: number): number {
  if (n <= 0) return 0
  return Math.round(50 * Math.pow(n, 1.6))
}

function xpProgressPercent(level: number, xp: number): number {
  const currentThreshold = xpForLevel(level - 1)
  const nextThreshold = xpForLevel(level)
  if (nextThreshold <= currentThreshold) return 100
  return Math.min(100, Math.max(0, ((xp - currentThreshold) / (nextThreshold - currentThreshold)) * 100))
}

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

const PAGE_SIZE = 20

export default function FamilyChildDetail() {
  const { id } = useParams<{ id: string }>()
  const childId = parseInt(id ?? '0', 10)
  const { t } = useTranslation('common')

  const [stats, setStats] = useState<ChildStats | null>(null)
  const [childInfo, setChildInfo] = useState<ChildInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const [chartWorkouts, setChartWorkouts] = useState<ChildWorkout[]>([])
  const [tableWorkouts, setTableWorkouts] = useState<ChildWorkout[]>([])
  const [workoutsTotal, setWorkoutsTotal] = useState(0)
  const [workoutsPage, setWorkoutsPage] = useState(0)
  const [workoutsLoading, setWorkoutsLoading] = useState(false)

  useEffect(() => {
    if (!childId) return
    loadInitialData()
  }, [childId])

  async function loadInitialData() {
    try {
      setLoading(true)
      setError('')
      const [statsRes, childrenRes, workoutsRes] = await Promise.all([
        fetch(`/api/family/children/${childId}/stats`, { credentials: 'include' }),
        fetch('/api/family/children', { credentials: 'include' }),
        fetch(`/api/family/children/${childId}/workouts?limit=100&offset=0`, { credentials: 'include' }),
      ])
      if (!statsRes.ok) throw new Error('failed')

      const statsData: ChildStats = await statsRes.json()
      setStats(statsData)

      if (childrenRes.ok) {
        const childrenData = await childrenRes.json()
        const child = (childrenData.children ?? []).find(
          (c: { child_id: number; nickname: string; avatar_emoji: string }) => c.child_id === childId
        )
        if (child) {
          setChildInfo({ nickname: child.nickname, avatar_emoji: child.avatar_emoji })
        }
      }

      if (workoutsRes.ok) {
        const workoutsData = await workoutsRes.json()
        const all: ChildWorkout[] = workoutsData.workouts ?? []
        setChartWorkouts(all)
        setTableWorkouts(all.slice(0, PAGE_SIZE))
        setWorkoutsTotal(workoutsData.total ?? 0)
      }
    } catch {
      setError(t('family.detail.errors.failedToLoad'))
    } finally {
      setLoading(false)
    }
  }

  async function goToPage(newPage: number) {
    setWorkoutsPage(newPage)
    const offset = newPage * PAGE_SIZE

    // Use cached chart workouts if the page is within what we have loaded
    if (offset < chartWorkouts.length) {
      setTableWorkouts(chartWorkouts.slice(offset, offset + PAGE_SIZE))
      return
    }

    try {
      setWorkoutsLoading(true)
      const res = await fetch(
        `/api/family/children/${childId}/workouts?limit=${PAGE_SIZE}&offset=${offset}`,
        { credentials: 'include' }
      )
      if (!res.ok) throw new Error('failed')
      const data = await res.json()
      setTableWorkouts(data.workouts ?? [])
    } catch {
      // silently retain previous page on error
    } finally {
      setWorkoutsLoading(false)
    }
  }

  // Compute weekly aggregates for the last 8 weeks (Monday-based)
  const weeklyChartData = useMemo(() => {
    const now = new Date()
    const daysToMonday = (now.getDay() + 6) % 7
    const thisMonday = new Date(now)
    thisMonday.setHours(0, 0, 0, 0)
    thisMonday.setDate(now.getDate() - daysToMonday)

    return Array.from({ length: 8 }, (_, i) => {
      const weekStart = new Date(thisMonday)
      weekStart.setDate(thisMonday.getDate() - (7 - i) * 7)
      const weekEnd = new Date(weekStart)
      weekEnd.setDate(weekStart.getDate() + 7)

      const wos = chartWorkouts.filter(w => {
        const d = new Date(w.started_at)
        return d >= weekStart && d < weekEnd
      })

      return {
        label: formatDate(weekStart, { month: 'short', day: 'numeric' }),
        distance: Math.round(wos.reduce((s, w) => s + w.distance_meters, 0) / 100) / 10,
        workouts: wos.length,
        stars: wos.reduce((s, w) => s + w.stars, 0),
      }
    })
  }, [chartWorkouts])

  const totalPages = Math.ceil(workoutsTotal / PAGE_SIZE)

  if (loading) {
    return <div className="p-6 text-gray-400">{t('status.loading')}...</div>
  }

  if (error) {
    return (
      <div className="p-6">
        <Link
          to="/family"
          className="flex items-center gap-2 text-gray-400 hover:text-white mb-4 text-sm transition-colors"
        >
          <ArrowLeft size={16} />
          {t('actions.back')}
        </Link>
        <div className="p-3 bg-red-900/40 border border-red-700 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      </div>
    )
  }

  if (!stats) return null

  const progressPct = xpProgressPercent(stats.level, stats.xp)
  const nickname = childInfo?.nickname || t('family.unknownChild', { id: childId })
  const avatar = childInfo?.avatar_emoji || '⭐'

  return (
    <div className="p-6 max-w-4xl mx-auto">
      {/* Back navigation */}
      <Link
        to="/family"
        className="flex items-center gap-2 text-gray-400 hover:text-white mb-6 text-sm transition-colors"
      >
        <ArrowLeft size={16} />
        {t('actions.back')}
      </Link>

      {/* Header */}
      <div className="flex items-start gap-4 mb-6 p-4 rounded-xl bg-gradient-to-br from-blue-500/10 to-indigo-500/10 border border-blue-500/20">
        <div
          className="flex-shrink-0 w-16 h-16 flex items-center justify-center rounded-full bg-gray-700/60 text-3xl"
          aria-hidden="true"
        >
          {avatar}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap mb-1">
            <h1 className="text-xl font-bold text-white truncate">{nickname}</h1>
            <span className="text-xs bg-yellow-500/20 text-yellow-300 border border-yellow-500/30 rounded-full px-2 py-0.5 flex-shrink-0">
              {t('stars.level', { level: stats.level })} · {stats.title}
            </span>
          </div>
          <div className="flex items-center gap-1.5 mb-2">
            <Star size={14} className="text-yellow-400" aria-hidden="true" />
            <span className="text-white font-semibold">{formatNumber(stats.current_balance)}</span>
            <span className="text-gray-400 text-sm">{t('stars.balance')}</span>
          </div>
          <div
            className="w-full h-2 bg-gray-700 rounded-full overflow-hidden"
            role="progressbar"
            aria-valuenow={progressPct}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={t('family.xpProgress')}
          >
            <div
              className="h-full bg-gradient-to-r from-yellow-400 to-orange-400 rounded-full transition-all duration-300"
              style={{ width: `${progressPct}%` }}
            />
          </div>
          <p className="text-xs text-gray-500 mt-0.5">
            {t('family.xpValue', { xp: formatNumber(stats.xp) })}
          </p>
        </div>
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-6">
        <StatCard
          icon={<Flame size={16} className={stats.current_streak > 0 ? 'text-orange-400' : 'text-gray-500'} />}
          value={formatNumber(stats.current_streak)}
          label={t('family.detail.currentStreak')}
        />
        <StatCard
          icon={<Trophy size={16} className="text-yellow-400" />}
          value={formatNumber(stats.longest_streak)}
          label={t('family.detail.longestStreak')}
        />
        <StatCard
          icon={<Star size={16} className="text-green-400" />}
          value={formatNumber(stats.total_earned)}
          label={t('family.detail.totalEarned')}
        />
        <StatCard
          icon={<Star size={16} className="text-red-400" />}
          value={formatNumber(stats.total_spent)}
          label={t('family.detail.totalSpent')}
        />
      </div>

      {/* Charts */}
      <div className="space-y-4 mb-6">
        {/* Weekly Distance */}
        <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-4">
          <h2 className="text-sm font-medium text-gray-300 mb-4">{t('family.detail.weeklyDistance')}</h2>
          <ResponsiveContainer width="100%" height={160}>
            <BarChart data={weeklyChartData} margin={{ top: 4, right: 4, left: -20, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" vertical={false} />
              <XAxis dataKey="label" tick={{ fill: '#9CA3AF', fontSize: 11 }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fill: '#9CA3AF', fontSize: 11 }} axisLine={false} tickLine={false} />
              <Tooltip
                contentStyle={{ backgroundColor: '#1F2937', border: '1px solid #374151', borderRadius: '8px' }}
                labelStyle={{ color: '#E5E7EB' }}
                itemStyle={{ color: '#60A5FA' }}
                formatter={(v: number) => [`${v} km`, t('family.detail.distance')]}
              />
              <Bar dataKey="distance" fill="#3B82F6" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </div>

        {/* Weekly Workout Count */}
        <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-4">
          <h2 className="text-sm font-medium text-gray-300 mb-4">{t('family.detail.weeklyWorkouts')}</h2>
          <ResponsiveContainer width="100%" height={160}>
            <BarChart data={weeklyChartData} margin={{ top: 4, right: 4, left: -20, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" vertical={false} />
              <XAxis dataKey="label" tick={{ fill: '#9CA3AF', fontSize: 11 }} axisLine={false} tickLine={false} />
              <YAxis
                allowDecimals={false}
                tick={{ fill: '#9CA3AF', fontSize: 11 }}
                axisLine={false}
                tickLine={false}
              />
              <Tooltip
                contentStyle={{ backgroundColor: '#1F2937', border: '1px solid #374151', borderRadius: '8px' }}
                labelStyle={{ color: '#E5E7EB' }}
                itemStyle={{ color: '#34D399' }}
                formatter={(v: number) => [v, t('family.detail.workoutsLabel')]}
              />
              <Bar dataKey="workouts" fill="#10B981" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </div>

        {/* Stars over time */}
        <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-4">
          <h2 className="text-sm font-medium text-gray-300 mb-4">{t('family.detail.starsOverTime')}</h2>
          <ResponsiveContainer width="100%" height={160}>
            <LineChart data={weeklyChartData} margin={{ top: 4, right: 4, left: -20, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" vertical={false} />
              <XAxis dataKey="label" tick={{ fill: '#9CA3AF', fontSize: 11 }} axisLine={false} tickLine={false} />
              <YAxis
                allowDecimals={false}
                tick={{ fill: '#9CA3AF', fontSize: 11 }}
                axisLine={false}
                tickLine={false}
              />
              <Tooltip
                contentStyle={{ backgroundColor: '#1F2937', border: '1px solid #374151', borderRadius: '8px' }}
                labelStyle={{ color: '#E5E7EB' }}
                itemStyle={{ color: '#FBBF24' }}
                formatter={(v: number) => [v, t('family.detail.stars')]}
              />
              <Line
                dataKey="stars"
                stroke="#F59E0B"
                strokeWidth={2}
                dot={{ fill: '#F59E0B', strokeWidth: 0, r: 3 }}
                activeDot={{ r: 5 }}
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* Recent Workouts Table */}
      <section className="mb-6">
        <h2 className="text-lg font-medium text-white mb-3">{t('family.detail.recentWorkouts')}</h2>
        {workoutsTotal === 0 ? (
          <p className="text-gray-400 text-sm">{t('family.detail.noWorkouts')}</p>
        ) : (
          <>
            <div className="overflow-x-auto -mx-2 px-2">
              <table className="w-full text-sm min-w-[560px]">
                <thead>
                  <tr className="border-b border-gray-700">
                    <th className="text-left text-gray-400 font-medium pb-2 pr-3">{t('family.detail.date')}</th>
                    <th className="text-left text-gray-400 font-medium pb-2 pr-3">{t('family.detail.sport')}</th>
                    <th className="text-right text-gray-400 font-medium pb-2 pr-3">{t('family.detail.duration')}</th>
                    <th className="text-right text-gray-400 font-medium pb-2 pr-3">{t('family.detail.distance')}</th>
                    <th className="text-right text-gray-400 font-medium pb-2 pr-3">{t('family.detail.avgHR')}</th>
                    <th className="text-right text-gray-400 font-medium pb-2 pr-3">{t('family.detail.calories')}</th>
                    <th className="text-right text-gray-400 font-medium pb-2">{t('family.detail.stars')}</th>
                  </tr>
                </thead>
                <tbody>
                  {workoutsLoading ? (
                    <tr>
                      <td colSpan={7} className="py-4 text-center text-gray-400">
                        {t('status.loading')}...
                      </td>
                    </tr>
                  ) : (
                    tableWorkouts.map(wo => (
                      <tr
                        key={wo.id}
                        className="border-b border-gray-800 hover:bg-gray-800/40 transition-colors"
                      >
                        <td className="py-2 pr-3 text-gray-300">
                          {formatDate(wo.started_at, { month: 'short', day: 'numeric' })}
                        </td>
                        <td className="py-2 pr-3 text-white capitalize">{wo.sport}</td>
                        <td className="py-2 pr-3 text-right text-gray-300">
                          {formatDuration(wo.duration_seconds)}
                        </td>
                        <td className="py-2 pr-3 text-right text-gray-300">
                          {wo.distance_meters > 0
                            ? `${(wo.distance_meters / 1000).toFixed(1)} km`
                            : '—'}
                        </td>
                        <td className="py-2 pr-3 text-right text-gray-300">
                          {wo.avg_heart_rate > 0 ? wo.avg_heart_rate : '—'}
                        </td>
                        <td className="py-2 pr-3 text-right text-gray-300">
                          {wo.calories > 0 ? formatNumber(wo.calories) : '—'}
                        </td>
                        <td className="py-2 text-right">
                          {wo.stars > 0 ? (
                            <span className="inline-flex items-center justify-end gap-1 text-yellow-400">
                              <Star size={12} aria-hidden="true" />
                              {wo.stars}
                            </span>
                          ) : (
                            '—'
                          )}
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            {/* Pagination */}
            {totalPages > 1 && (
              <div className="flex items-center justify-between mt-3">
                <span className="text-xs text-gray-400">
                  {formatNumber(workoutsPage * PAGE_SIZE + 1)}–
                  {formatNumber(Math.min((workoutsPage + 1) * PAGE_SIZE, workoutsTotal))} /{' '}
                  {formatNumber(workoutsTotal)}
                </span>
                <div className="flex items-center gap-1">
                  <button
                    onClick={() => goToPage(workoutsPage - 1)}
                    disabled={workoutsPage === 0}
                    className="p-1.5 text-gray-400 hover:text-white disabled:opacity-40 disabled:cursor-not-allowed transition-colors cursor-pointer"
                    aria-label={t('family.detail.prevPage')}
                  >
                    <ChevronLeft size={16} aria-hidden="true" />
                  </button>
                  <span className="text-xs text-gray-400 px-1">
                    {workoutsPage + 1} / {totalPages}
                  </span>
                  <button
                    onClick={() => goToPage(workoutsPage + 1)}
                    disabled={workoutsPage >= totalPages - 1}
                    className="p-1.5 text-gray-400 hover:text-white disabled:opacity-40 disabled:cursor-not-allowed transition-colors cursor-pointer"
                    aria-label={t('family.detail.nextPage')}
                  >
                    <ChevronRight size={16} aria-hidden="true" />
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </section>

      {/* Recent Star Transactions */}
      <section className="mb-6">
        <h2 className="text-lg font-medium text-white mb-3">{t('family.detail.recentTransactions')}</h2>
        {stats.recent_transactions.length === 0 ? (
          <p className="text-gray-400 text-sm">{t('family.detail.noTransactions')}</p>
        ) : (
          <div className="max-h-72 overflow-y-auto space-y-2 pr-1">
            {stats.recent_transactions.map(tx => (
              <div
                key={tx.id}
                className="flex items-center justify-between gap-3 p-3 rounded-lg bg-gray-800/60 border border-gray-700"
              >
                <div className="flex items-center gap-2 min-w-0">
                  <Star
                    size={14}
                    className={tx.amount > 0 ? 'text-yellow-400 flex-shrink-0' : 'text-red-400 flex-shrink-0'}
                    aria-hidden="true"
                  />
                  <div className="min-w-0">
                    <p className="text-white text-sm truncate">
                      {tx.description ||
                        t(`stars.reasons.${tx.reason}`, { defaultValue: tx.reason })}
                    </p>
                    <p className="text-gray-500 text-xs">
                      {formatDate(tx.created_at, { month: 'short', day: 'numeric' })}
                    </p>
                  </div>
                </div>
                <span
                  className={`flex-shrink-0 font-semibold text-sm ${
                    tx.amount > 0 ? 'text-green-400' : 'text-red-400'
                  }`}
                >
                  {tx.amount > 0 ? '+' : ''}
                  {tx.amount}
                </span>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Active Challenges */}
      {stats.active_challenges && stats.active_challenges.length > 0 && (
        <section className="mb-6">
          <h2 className="text-lg font-medium text-white mb-3">{t('family.detail.activeChallenges')}</h2>
          <div className="space-y-3">
            {stats.active_challenges.map(challenge => {
              const pct = challenge.goal > 0 ? Math.min(100, (challenge.progress / challenge.goal) * 100) : 0
              return (
                <div key={challenge.id} className="p-3 rounded-lg bg-gray-800/60 border border-gray-700">
                  <div className="flex items-center justify-between mb-1">
                    <p className="text-white text-sm font-medium">{challenge.title}</p>
                    <span className="text-gray-400 text-xs">
                      {challenge.progress}/{challenge.goal}
                    </span>
                  </div>
                  {challenge.description && (
                    <p className="text-gray-500 text-xs mb-2">{challenge.description}</p>
                  )}
                  <div
                    className="w-full h-2 bg-gray-700 rounded-full overflow-hidden"
                    role="progressbar"
                    aria-valuenow={pct}
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-label={challenge.title}
                  >
                    <div
                      className="h-full bg-gradient-to-r from-blue-500 to-purple-500 rounded-full transition-all duration-300"
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                </div>
              )
            })}
          </div>
        </section>
      )}
    </div>
  )
}

interface StatCardProps {
  icon: React.ReactNode
  value: string
  label: string
}

function StatCard({ icon, value, label }: StatCardProps) {
  return (
    <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-3">
      <div className="mb-1">{icon}</div>
      <p className="text-white font-bold text-lg leading-tight">{value}</p>
      <p className="text-gray-400 text-xs mt-0.5">{label}</p>
    </div>
  )
}
