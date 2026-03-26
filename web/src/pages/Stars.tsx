import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Activity, Clock, MapPin, Star } from 'lucide-react'
import { xpForLevel, xpProgressPercent, getFlameVariant } from '../utils/stars'
import LevelBadge from '../components/LevelBadge'
import Confetti from '../components/Confetti'
import '../stars.css'

const LAST_SEEN_LEVEL_KEY = 'hytte_last_seen_level'

interface Balance {
  current_balance: number
  total_earned: number
  total_spent: number
  level: number
  xp: number
  title: string
  emoji?: string
}

interface Transaction {
  id: number
  amount: number
  reason: string
  description: string
  created_at: string
}

interface TransactionsResponse {
  transactions: Transaction[]
  weekly_stars: number
  weekly_starred_workouts: number
}

interface StreakEntry {
  current_count: number
  longest_count: number
  last_activity: string
  shield_active: boolean
}

interface StreaksResponse {
  daily_workout: StreakEntry
  weekly_workout: StreakEntry
}

interface WeeklyBonusItem {
  reason: string
  description: string
  amount: number
}

interface WeeklyBonusSummaryResponse {
  week_key: string
  bonuses: WeeklyBonusItem[]
  total_stars: number
  perfect_week: boolean
}

const NAV_CARDS = [
  { to: '/stars/badges', emoji: '🏅', key: 'nav.badges' },
  { to: '/stars/rewards', emoji: '🎁', key: 'nav.rewards' },
  { to: '/stars', emoji: '🎯', key: 'nav.challenges' },
  { to: '/stars', emoji: '🏆', key: 'nav.leaderboard' },
] as const

const REASON_EMOJI: Record<string, string> = {
  showed_up: '💪',
  duration_bonus: '⏱️',
  effort_bonus: '❤️',
  distance_milestone: '🏃',
  first_kilometer: '🏃',
  '5k_finisher': '🏃',
  '10k_hero': '🏃',
  half_marathon_legend: '🏃',
  century_club: '🏃',
  explorer_500k: '🏃',
  titan_1000k: '🏃',
  streak: '🔥',
  weekly_bonus: '📅',
  personal_record: '🏆',
  pr_longest_run: '🏆',
  pr_calorie_burn: '🏆',
  pr_elevation: '🏆',
  pr_fastest_5k: '🏆',
  pr_fastest_pace: '🏆',
  badge: '🏅',
  zone_commander: '🏅',
  zone_explorer: '🏅',
  easy_day_hero: '🏅',
  threshold_trainer: '🏅',
}

function formatRelativeTime(dateStr: string, locale: string): string {
  const date = new Date(dateStr)
  const now = Date.now()
  const diffMs = now - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  if (diffMins < 60) return rtf.format(-diffMins, 'minute')
  if (diffHours < 24) return rtf.format(-diffHours, 'hour')
  return rtf.format(-diffDays, 'day')
}

function WeeklyBonusSummary({ data }: { data: WeeklyBonusSummaryResponse | null }) {
  const { t } = useTranslation('common')

  if (!data) return null

  if (data.bonuses.length === 0) {
    return (
      <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5 text-center">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">
          {t('stars.weeklyBonus.title')}
        </h2>
        <p className="text-gray-500 text-sm">{t('stars.weeklyBonus.checkBackMonday')}</p>
      </div>
    )
  }

  return (
    <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5">
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">
          {t('stars.weeklyBonus.title')}
        </h2>
        {data.perfect_week && (
          <span className="text-xs bg-yellow-500/20 text-yellow-300 border border-yellow-500/30 rounded-full px-2 py-0.5">
            ⭐ {t('stars.weeklyBonus.perfectWeek')}
          </span>
        )}
      </div>
      <ul className="space-y-1.5">
        {data.bonuses.map((item, i) => (
          <li key={i} className="flex items-center justify-between text-sm">
            <span className="text-gray-300 truncate mr-2">{item.description}</span>
            <span className="text-yellow-400 font-medium flex-shrink-0">+{item.amount} ⭐</span>
          </li>
        ))}
      </ul>
      {data.total_stars > 0 && (
        <div className="mt-3 pt-3 border-t border-gray-700 flex items-center justify-between">
          <span className="text-gray-400 text-sm">{t('stars.weeklyBonus.total')}</span>
          <span className="text-yellow-300 font-bold text-sm">+{data.total_stars} ⭐</span>
        </div>
      )}
    </div>
  )
}

export default function Stars() {
  const { t, i18n } = useTranslation('common')
  const [balance, setBalance] = useState<Balance | null>(null)
  const [txnData, setTxnData] = useState<TransactionsResponse | null>(null)
  const [streaks, setStreaks] = useState<StreaksResponse | null>(null)
  const [weeklySummary, setWeeklySummary] = useState<WeeklyBonusSummaryResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showConfetti, setShowConfetti] = useState(false)
  const handleConfettiDone = useCallback(() => setShowConfetti(false), [])

  useEffect(() => {
    const fetchData = async () => {
      setError(null)
      setLoading(true)
      try {
        const [balRes, txnRes, streakRes, summaryRes] = await Promise.all([
          fetch('/api/stars/balance', { credentials: 'include' }),
          fetch('/api/stars/transactions?limit=20', { credentials: 'include' }),
          fetch('/api/stars/streaks', { credentials: 'include' }),
          fetch('/api/stars/weekly-bonus-summary', { credentials: 'include' }),
        ])
        if (!balRes.ok || !txnRes.ok || !streakRes.ok || !summaryRes.ok) {
          throw new Error('fetch failed')
        }
        const [bal, txn, streak, summary] = await Promise.all([
          balRes.json(),
          txnRes.json(),
          streakRes.json(),
          summaryRes.json(),
        ])
        try {
          const stored = localStorage.getItem(LAST_SEEN_LEVEL_KEY)
          if (stored === null) {
            localStorage.setItem(LAST_SEEN_LEVEL_KEY, String(bal.level))
          } else {
            const parsed = parseInt(stored, 10)
            const lastLevel = Number.isNaN(parsed) ? bal.level : parsed
            if (bal.level > lastLevel) {
              setShowConfetti(true)
            }
            localStorage.setItem(LAST_SEEN_LEVEL_KEY, String(bal.level))
          }
        } catch (storageError) {
          console.warn('Failed to access localStorage for stars level tracking:', storageError)
        }
        setBalance(bal)
        setTxnData(txn)
        setStreaks(streak)
        setWeeklySummary(summary)
      } catch {
        setError(t('stars.errors.failedToLoad'))
      } finally {
        setLoading(false)
      }
    }
    fetchData()
  }, [t])

  if (loading) {
    return (
      <div className="p-6 max-w-2xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Star size={24} className="text-yellow-400" />
          <h1 className="text-2xl font-semibold text-white">{t('stars.title')}</h1>
        </div>
        <div className="space-y-4">
          <div className="h-48 rounded-xl bg-gray-800 animate-pulse" />
          <div className="h-24 rounded-xl bg-gray-800 animate-pulse" />
          <div className="h-32 rounded-xl bg-gray-800 animate-pulse" />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6 max-w-2xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Star size={24} className="text-yellow-400" />
          <h1 className="text-2xl font-semibold text-white">{t('stars.title')}</h1>
        </div>
        <div className="text-red-400">{error}</div>
      </div>
    )
  }

  const transactions = txnData?.transactions ?? []
  const dailyStreak = streaks?.daily_workout ?? { current_count: 0, longest_count: 0, last_activity: '', shield_active: false }
  const weeklyStreak = streaks?.weekly_workout ?? { current_count: 0, longest_count: 0, last_activity: '', shield_active: false }
  const xpPercent = balance ? xpProgressPercent(balance.level, balance.xp) : 0

  return (
    <div className="p-6 max-w-2xl mx-auto space-y-6">
      <Confetti active={showConfetti} onDone={handleConfettiDone} />
      <div className="flex items-center gap-3">
        <Star size={24} className="text-yellow-400" />
        <h1 className="text-2xl font-semibold text-white">{t('stars.title')}</h1>
      </div>

      {/* Star Balance Card */}
      <div className="rounded-xl bg-gradient-to-br from-yellow-500/20 to-orange-500/20 border border-yellow-500/30 p-8 text-center">
        <div className="relative inline-block">
          <span className="text-8xl font-black star-sparkle">
            {balance?.current_balance ?? 0}
          </span>
        </div>
        <p className="mt-3 text-yellow-300/80 text-sm">{t('stars.balance')}</p>
        {balance && (
          <p className="mt-1 text-gray-400 text-xs">
            {t('stars.totalEarned')}: {balance.total_earned}
          </p>
        )}

        {/* Level badge */}
        {balance && (
          <div className="mt-5">
            <div className="mb-4">
              <LevelBadge level={balance.level} emoji={balance.emoji} title={balance.title} />
            </div>

            {/* XP Progress bar */}
            <div className="px-2">
              <div className="flex justify-between text-xs text-gray-400 mb-1">
                <span>{t('stars.xp.progress')}</span>
                <span>{t('stars.xp.currentOfNext', { current: balance.xp, total: xpForLevel(balance.level) })}</span>
              </div>
              <div className="h-3 rounded-full bg-gray-700/60 overflow-hidden">
                <div
                  className="h-full rounded-full transition-all duration-700"
                  style={{
                    width: `${xpPercent}%`,
                    background: 'linear-gradient(90deg, #7c3aed, #2563eb)',
                  }}
                />
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Streaks */}
      <div className="grid grid-cols-2 gap-4">
        {/* Daily streak */}
        <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5 flex flex-col items-center gap-1.5 min-h-[130px]">
          <p className="text-gray-400 text-xs font-medium uppercase tracking-wide">
            {t('stars.streak.daily')}
          </p>
          <span
            className={getFlameVariant(dailyStreak.current_count)}
            aria-hidden="true"
          >
            🔥
          </span>
          <div className="flex items-center gap-1.5">
            <p className="text-white text-2xl font-bold leading-none">
              {dailyStreak.current_count}
            </p>
            {dailyStreak.shield_active && (
              <span
                className="text-base leading-none"
                role="img"
                aria-label={t('stars.streak.shielded')}
              >
                🛡️
              </span>
            )}
          </div>
          <p className="text-orange-300/80 text-xs font-medium">{t('stars.streak.days')}</p>
          <p className="text-gray-500 text-xs">
            {t('stars.streak.best', { count: dailyStreak.longest_count })}
          </p>
        </div>

        {/* Weekly streak */}
        <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5 flex flex-col items-center gap-1.5 min-h-[130px]">
          <p className="text-gray-400 text-xs font-medium uppercase tracking-wide">
            {t('stars.streak.weekly')}
          </p>
          <span
            className={getFlameVariant(weeklyStreak.current_count)}
            aria-hidden="true"
          >
            🔥
          </span>
          <p className="text-white text-2xl font-bold leading-none">
            {weeklyStreak.current_count}
          </p>
          <p className="text-orange-300/80 text-xs font-medium">{t('stars.streak.weeks')}</p>
          <p className="text-gray-500 text-xs">
            {t('stars.streak.best', { count: weeklyStreak.longest_count })}
          </p>
        </div>
      </div>

      {/* Weekly Bonus Summary */}
      <WeeklyBonusSummary data={weeklySummary} />

      {/* This Week Stats */}
      {txnData && (
        <div className="grid grid-cols-2 gap-4">
          <div className="bg-gray-800/60 rounded-lg border border-gray-700 p-4 text-center min-h-[72px]">
            <p className="text-2xl font-bold text-yellow-400">{txnData.weekly_stars}</p>
            <p className="text-gray-400 text-sm mt-1">{t('stars.weeklyStars')}</p>
          </div>
          <div className="bg-gray-800/60 rounded-lg border border-gray-700 p-4 text-center min-h-[72px]">
            <p className="text-2xl font-bold text-orange-400">{txnData.weekly_starred_workouts}</p>
            <p className="text-gray-400 text-sm mt-1">{t('stars.weeklyWorkouts')}</p>
          </div>
        </div>
      )}

      {/* Quick Stats Card */}
      <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-4">
          {t('stars.quickStats.title')}
        </h2>
        <div className="grid grid-cols-3 gap-4">
          <div className="flex flex-col items-center gap-2">
            <Activity size={20} className="text-blue-400" />
            <p className="text-white text-xl font-bold leading-none">
              {txnData?.weekly_starred_workouts ?? 0}
            </p>
            <p className="text-gray-400 text-xs text-center">{t('stars.quickStats.workouts')}</p>
          </div>
          <div className="flex flex-col items-center gap-2">
            <MapPin size={20} className="text-green-400" />
            <p className="text-gray-500 text-sm leading-none italic">{t('stars.quickStats.unavailable')}</p>
            <p className="text-gray-400 text-xs text-center">{t('stars.quickStats.distance')}</p>
          </div>
          <div className="flex flex-col items-center gap-2">
            <Clock size={20} className="text-purple-400" />
            <p className="text-gray-500 text-sm leading-none italic">{t('stars.quickStats.unavailable')}</p>
            <p className="text-gray-400 text-xs text-center">{t('stars.quickStats.time')}</p>
          </div>
        </div>
      </div>

      {/* Recent Activity Feed */}
      <div>
        <h2 className="text-lg font-semibold text-white mb-3">{t('stars.recentActivity')}</h2>
        {transactions.length === 0 ? (
          <div className="p-8 text-center bg-gray-800/50 rounded-lg border border-gray-700">
            <Star size={40} className="text-yellow-400/40 mx-auto mb-3" />
            <p className="text-gray-300">{t('stars.noActivity')}</p>
            <p className="text-gray-500 text-sm mt-1">{t('stars.noActivityHint')}</p>
          </div>
        ) : (
          <ul
            className="space-y-2 max-h-[420px] overflow-y-auto pr-1"
            style={{ scrollbarWidth: 'thin', scrollbarColor: '#374151 transparent' }}
          >
            {transactions.map(tx => (
              <li
                key={tx.id}
                className="flex items-center justify-between bg-gray-800/60 rounded-lg border border-gray-700/50 px-4 py-3 min-h-[60px]"
              >
                <div className="flex items-center gap-3">
                  <span className="text-xl flex-shrink-0" role="img" aria-hidden>
                    {REASON_EMOJI[tx.reason] ?? '⭐'}
                  </span>
                  <div>
                    <p className="text-white text-sm font-medium">
                      {t(`stars.reasons.${tx.reason}`, { defaultValue: tx.reason })}
                    </p>
                    {tx.description && (
                      <p className="text-gray-400 text-xs">{tx.description}</p>
                    )}
                    <p className="text-gray-500 text-xs">
                      {formatRelativeTime(tx.created_at, i18n.language)}
                    </p>
                  </div>
                </div>
                <span
                  className={`font-bold text-sm flex-shrink-0 ml-3 ${
                    tx.amount > 0 ? 'text-yellow-400' : tx.amount < 0 ? 'text-red-400' : 'text-gray-400'
                  }`}
                >
                  {tx.amount > 0 ? '+' : ''}
                  {tx.amount}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* Navigation Cards */}
      <div className="grid grid-cols-2 gap-4">
        {NAV_CARDS.map(({ to, emoji, key }) => (
          <Link
            key={to}
            to={to}
            className="bg-gray-800/60 rounded-xl border border-gray-700 p-5 flex flex-col gap-2 hover:bg-gray-700/60 hover:border-gray-600 transition-colors min-h-[96px]"
          >
            <span className="text-2xl" role="img" aria-hidden="true">{emoji}</span>
            <p className="text-white font-semibold text-sm">{t(`stars.${key}.title`)}</p>
            <p className="text-gray-400 text-xs">{t(`stars.${key}.description`)}</p>
          </Link>
        ))}
      </div>
    </div>
  )
}
