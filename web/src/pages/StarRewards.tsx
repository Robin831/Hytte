import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, ShoppingBag, Star, Clock, CheckCircle, XCircle } from 'lucide-react'
import { formatDate } from '../utils/formatDate'
import '../stars.css'

interface KidReward {
  id: number
  title: string
  description: string
  star_cost: number
  icon_emoji: string
  can_afford: boolean
  times_claimed: number
  max_claims: number | null
}

interface KidClaim {
  id: number
  reward_id: number
  reward_title: string
  reward_icon: string
  status: 'pending' | 'approved' | 'denied'
  stars_spent: number
  note?: string
  resolved_at?: string | null
  created_at: string
}

interface BalanceData {
  current_balance: number
}

function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation('common')

  if (status === 'pending') {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-yellow-500/20 text-yellow-300 font-medium border border-yellow-500/30">
        <Clock size={10} />
        {t('stars.rewards.pending')}
      </span>
    )
  }
  if (status === 'approved') {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-green-500/20 text-green-300 font-medium border border-green-500/30">
        <CheckCircle size={10} />
        {t('stars.rewards.approved')}
      </span>
    )
  }
  if (status === 'denied') {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-red-500/20 text-red-300 font-medium border border-red-500/30">
        <XCircle size={10} />
        {t('stars.rewards.denied')}
      </span>
    )
  }
  return null
}

interface RewardCardProps {
  reward: KidReward
  currentBalance: number
  latestClaim: KidClaim | undefined
  onClaim: (rewardId: number) => Promise<void>
}

function RewardCard({ reward, currentBalance, latestClaim, onClaim }: RewardCardProps) {
  const { t } = useTranslation('common')
  const [claiming, setClaiming] = useState(false)

  const handleClaim = async () => {
    setClaiming(true)
    try {
      await onClaim(reward.id)
    } finally {
      setClaiming(false)
    }
  }

  const shortfall = reward.star_cost - currentBalance

  return (
    <div className="relative flex flex-col gap-3 rounded-2xl border border-gray-700 bg-gray-800/70 p-5 transition-all duration-200 hover:scale-[1.02] hover:border-yellow-400/50 hover:shadow-lg hover:shadow-yellow-400/10">
      {reward.times_claimed > 0 && (
        <div className="absolute top-3 right-3">
          <span className="text-xs px-2 py-0.5 rounded-full bg-purple-500/20 text-purple-300 border border-purple-500/30 font-medium">
            {t('stars.rewards.timesClaimedBadge', { count: reward.times_claimed })}
          </span>
        </div>
      )}

      <div className="text-5xl text-center" role="img" aria-hidden="true">
        {reward.icon_emoji}
      </div>

      <div className="text-center space-y-1">
        <h3 className="text-white font-bold text-lg leading-tight">{reward.title}</h3>
        {reward.description && (
          <p className="text-gray-400 text-sm leading-snug">{reward.description}</p>
        )}
      </div>

      <div className="flex items-center justify-center gap-1.5 text-yellow-400 font-bold text-xl">
        <Star size={18} className="fill-yellow-400 text-yellow-400" />
        {reward.star_cost}
      </div>

      {latestClaim && (
        <div className="flex justify-center">
          <StatusBadge status={latestClaim.status} />
        </div>
      )}

      {reward.can_afford ? (
        <button
          type="button"
          onClick={handleClaim}
          disabled={claiming}
          className="mt-auto w-full rounded-xl bg-gradient-to-r from-yellow-400 to-orange-400 py-2.5 text-sm font-bold text-gray-900 transition-all duration-200 hover:from-yellow-300 hover:to-orange-300 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
        >
          {claiming ? '...' : t('stars.rewards.claimButton', { cost: reward.star_cost })}
        </button>
      ) : (
        <div className="mt-auto w-full rounded-xl bg-gray-700/50 py-2.5 text-center text-sm font-medium text-gray-500 border border-gray-700">
          {t('stars.rewards.needMore', { count: shortfall })}
        </div>
      )}
    </div>
  )
}

export default function StarRewards() {
  const { t } = useTranslation('common')
  const [rewards, setRewards] = useState<KidReward[]>([])
  const [claims, setClaims] = useState<KidClaim[]>([])
  const [balance, setBalance] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [notification, setNotification] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [refresh, setRefresh] = useState(0)

  useEffect(() => {
    const controller = new AbortController()

    const fetchAll = async () => {
      setError(null)
      setLoading(true)
      try {
        const [balanceRes, rewardsRes, claimsRes] = await Promise.all([
          fetch('/api/stars/balance', { credentials: 'include', signal: controller.signal }),
          fetch('/api/stars/rewards', { credentials: 'include', signal: controller.signal }),
          fetch('/api/stars/claims', { credentials: 'include', signal: controller.signal }),
        ])

        if (!balanceRes.ok || !rewardsRes.ok || !claimsRes.ok) {
          throw new Error('fetch failed')
        }

        const [balanceData, rewardsData, claimsData] = await Promise.all([
          balanceRes.json() as Promise<BalanceData>,
          rewardsRes.json() as Promise<{ rewards: KidReward[] }>,
          claimsRes.json() as Promise<{ claims: KidClaim[] }>,
        ])

        setBalance(balanceData.current_balance)
        setRewards(rewardsData.rewards ?? [])
        setClaims(claimsData.claims ?? [])
      } catch (err: unknown) {
        if (controller.signal.aborted) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(t('stars.rewards.errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
        }
      }
    }

    fetchAll()

    return () => {
      controller.abort()
    }
  }, [t, refresh])

  const handleClaim = async (rewardId: number) => {
    try {
      const res = await fetch(`/api/stars/rewards/${rewardId}/claim`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        setNotification({ message: body.error ?? t('stars.rewards.errors.failedToClaim'), type: 'error' })
      } else {
        setNotification({ message: t('stars.rewards.claimSuccess'), type: 'success' })
        setRefresh(r => r + 1)
      }
    } catch {
      setNotification({ message: t('stars.rewards.errors.failedToClaim'), type: 'error' })
    }

    setTimeout(() => setNotification(null), 4000)
  }

  const latestClaimByReward = claims.reduce<Record<number, KidClaim>>((acc, claim) => {
    const existing = acc[claim.reward_id]
    if (!existing || claim.created_at > existing.created_at) {
      acc[claim.reward_id] = claim
    }
    return acc
  }, {})

  const sortedClaims = [...claims].sort((a, b) => b.created_at.localeCompare(a.created_at))

  if (loading) {
    return (
      <div className="p-6 max-w-3xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <ShoppingBag size={24} className="text-yellow-400" />
          <h1 className="text-2xl font-semibold text-white">{t('stars.rewards.title')}</h1>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-56 rounded-2xl bg-gray-800 animate-pulse" />
          ))}
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6 max-w-3xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <ShoppingBag size={24} className="text-yellow-400" />
          <h1 className="text-2xl font-semibold text-white">{t('stars.rewards.title')}</h1>
        </div>
        <div className="text-red-400">{error}</div>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-3xl mx-auto space-y-8">
      <div className="flex items-center gap-3">
        <Link
          to="/stars"
          className="text-gray-400 hover:text-white transition-colors"
          aria-label={t('stars.rewards.back')}
        >
          <ArrowLeft size={20} />
        </Link>
        <ShoppingBag size={24} className="text-yellow-400" />
        <h1 className="text-2xl font-semibold text-white">{t('stars.rewards.title')}</h1>
      </div>

      <div className="rounded-2xl bg-gradient-to-r from-yellow-500/20 via-orange-500/20 to-pink-500/20 border border-yellow-400/30 p-6 text-center">
        <p className="text-gray-300 text-sm mb-2">{t('stars.rewards.currentBalance')}</p>
        <div className="flex items-center justify-center gap-3">
          <Star size={28} className="fill-yellow-400 text-yellow-400 star-sparkle" />
          <span className="text-5xl font-extrabold text-yellow-400 star-sparkle">{balance}</span>
          <Star size={28} className="fill-yellow-400 text-yellow-400 star-sparkle" />
        </div>
      </div>

      {notification && (
        <div
          className={`rounded-xl px-4 py-3 text-sm text-center border ${
            notification.type === 'success'
              ? 'bg-green-500/20 border-green-400/30 text-green-300'
              : 'bg-red-500/20 border-red-400/30 text-red-300'
          }`}
        >
          {notification.message}
        </div>
      )}

      {rewards.length === 0 ? (
        <div className="rounded-xl bg-gray-800/50 border border-gray-700 p-10 text-center space-y-2">
          <p className="text-4xl" role="img" aria-hidden="true">🎁</p>
          <p className="text-gray-300 font-medium">{t('stars.rewards.noRewards')}</p>
          <p className="text-gray-500 text-sm">{t('stars.rewards.noRewardsHint')}</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {rewards.map(reward => (
            <RewardCard
              key={reward.id}
              reward={reward}
              currentBalance={balance}
              latestClaim={latestClaimByReward[reward.id]}
              onClaim={handleClaim}
            />
          ))}
        </div>
      )}

      <div className="space-y-4">
        <h2 className="text-lg font-semibold text-white">{t('stars.rewards.claimHistory')}</h2>
        {sortedClaims.length === 0 ? (
          <div className="rounded-xl bg-gray-800/50 border border-gray-700 p-6 text-center text-gray-400 text-sm">
            {t('stars.rewards.noClaims')}
          </div>
        ) : (
          <div className="space-y-2">
            {sortedClaims.map(claim => (
              <div
                key={claim.id}
                className="flex items-center gap-3 rounded-xl bg-gray-800/50 border border-gray-700 px-4 py-3"
              >
                <span className="text-2xl" role="img" aria-hidden="true">{claim.reward_icon}</span>
                <div className="flex-1 min-w-0">
                  <p className="text-white text-sm font-medium truncate">{claim.reward_title}</p>
                  <p className="text-gray-500 text-xs">{formatDate(claim.created_at, { dateStyle: 'medium' })}</p>
                </div>
                <div className="flex flex-col items-end gap-1 shrink-0">
                  <div className="flex items-center gap-1 text-yellow-400 text-sm font-bold">
                    <Star size={12} className="fill-yellow-400" />
                    {claim.stars_spent}
                  </div>
                  <StatusBadge status={claim.status} />
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
