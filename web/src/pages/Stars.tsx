import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Star } from 'lucide-react'

interface Balance {
  current_balance: number
  total_earned: number
  total_spent: number
  level: number
  xp: number
  title: string
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
  weekly_workouts: number
}

const reasonEmoji: Record<string, string> = {
  showed_up: '🏃',
  duration_bonus: '⏱️',
  effort_bonus: '💪',
  first_kilometer: '🎉',
  '5k_finisher': '🥈',
  '10k_hero': '🥇',
  half_marathon_legend: '🏆',
  century_club: '💯',
  explorer_500k: '🗺️',
  titan_1000k: '🌍',
  pr_longest_run: '📏',
  pr_calorie_burn: '🔥',
  pr_elevation: '⛰️',
  pr_fastest_5k: '⚡',
  pr_fastest_pace: '💨',
  zone_commander: '🎯',
  zone_explorer: '🌈',
  easy_day_hero: '😌',
  threshold_trainer: '🧠',
}

export default function Stars() {
  const { t } = useTranslation('common')
  const [balance, setBalance] = useState<Balance | null>(null)
  const [txnData, setTxnData] = useState<TransactionsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [balRes, txnRes] = await Promise.all([
          fetch('/api/stars/balance', { credentials: 'include' }),
          fetch('/api/stars/transactions?limit=20', { credentials: 'include' }),
        ])
        if (!balRes.ok || !txnRes.ok) {
          throw new Error('fetch failed')
        }
        const [bal, txn] = await Promise.all([balRes.json(), txnRes.json()])
        setBalance(bal)
        setTxnData(txn)
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
        <div className="text-gray-400">{t('status.loading')}...</div>
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

  return (
    <div className="p-6 max-w-2xl mx-auto space-y-6">
      <div className="flex items-center gap-3">
        <Star size={24} className="text-yellow-400" />
        <h1 className="text-2xl font-semibold text-white">{t('stars.title')}</h1>
      </div>

      {/* Star Balance Card */}
      <div className="rounded-xl bg-gradient-to-br from-yellow-500/20 to-orange-500/20 border border-yellow-500/30 p-8 text-center">
        <div className="relative inline-block">
          <span className="text-7xl font-bold text-yellow-400 star-pulse">
            {balance?.current_balance ?? 0}
          </span>
        </div>
        <div className="mt-2 flex justify-center gap-1">
          {[...Array(5)].map((_, i) => (
            <Star
              key={i}
              size={20}
              className={i < Math.min(5, Math.floor((balance?.current_balance ?? 0) / 10)) ? 'text-yellow-400 fill-yellow-400' : 'text-gray-600'}
            />
          ))}
        </div>
        <p className="mt-2 text-yellow-300/80 text-sm">{t('stars.balance')}</p>
        {balance && (
          <p className="mt-1 text-gray-400 text-xs">
            {t('stars.totalEarned')}: {balance.total_earned}
          </p>
        )}
        {balance && (
          <div className="mt-4 inline-block bg-yellow-500/10 border border-yellow-500/20 rounded-full px-4 py-1">
            <span className="text-yellow-300 text-sm font-medium">
              {t('stars.level', { level: balance.level })} · {balance.title}
            </span>
          </div>
        )}
      </div>

      {/* This Week Stats */}
      {txnData && (
        <div className="grid grid-cols-2 gap-4">
          <div className="bg-gray-800/60 rounded-lg border border-gray-700 p-4 text-center">
            <p className="text-2xl font-bold text-yellow-400">{txnData.weekly_stars}</p>
            <p className="text-gray-400 text-sm mt-1">{t('stars.weeklyStars')}</p>
          </div>
          <div className="bg-gray-800/60 rounded-lg border border-gray-700 p-4 text-center">
            <p className="text-2xl font-bold text-orange-400">{txnData.weekly_workouts}</p>
            <p className="text-gray-400 text-sm mt-1">{t('stars.weeklyWorkouts')}</p>
          </div>
        </div>
      )}

      {/* Recent Activity */}
      <div>
        <h2 className="text-lg font-semibold text-white mb-3">{t('stars.recentActivity')}</h2>
        {transactions.length === 0 ? (
          <div className="p-8 text-center bg-gray-800/50 rounded-lg border border-gray-700">
            <Star size={40} className="text-yellow-400/40 mx-auto mb-3" />
            <p className="text-gray-300">{t('stars.noActivity')}</p>
            <p className="text-gray-500 text-sm mt-1">{t('stars.noActivityHint')}</p>
          </div>
        ) : (
          <div className="space-y-2">
            {transactions.map(tx => (
              <div
                key={tx.id}
                className="flex items-center justify-between bg-gray-800/60 rounded-lg border border-gray-700/50 px-4 py-3"
              >
                <div className="flex items-center gap-3">
                  <span className="text-xl" role="img" aria-hidden>
                    {reasonEmoji[tx.reason] ?? '⭐'}
                  </span>
                  <div>
                    <p className="text-white text-sm font-medium">
                      {t(`stars.reasons.${tx.reason}`, { defaultValue: tx.reason })}
                    </p>
                    {tx.description && (
                      <p className="text-gray-400 text-xs">{tx.description}</p>
                    )}
                  </div>
                </div>
                <span className="text-yellow-400 font-bold text-sm">+{tx.amount}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      <style>{`
        @keyframes star-pulse {
          0%, 100% { transform: scale(1); filter: drop-shadow(0 0 8px rgba(250,204,21,0.5)); }
          50% { transform: scale(1.05); filter: drop-shadow(0 0 16px rgba(250,204,21,0.8)); }
        }
        .star-pulse {
          display: inline-block;
          animation: star-pulse 2s ease-in-out infinite;
        }
      `}</style>
    </div>
  )
}
