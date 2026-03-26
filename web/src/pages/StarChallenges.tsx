import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Target, ArrowLeft, ChevronDown, ChevronUp, CheckCircle } from 'lucide-react'
import '../stars.css'

interface ChallengeWithProgress {
  id: number
  creator_id: number
  title: string
  description: string
  challenge_type: string
  target_value: number
  star_reward: number
  start_date: string
  end_date: string
  is_active: boolean
  created_at: string
  updated_at: string
  current_value: number
  completed: boolean
}

function daysRemaining(endDate: string): number | null {
  if (!endDate) return null
  const parts = endDate.split('-')
  if (parts.length !== 3) return null
  const [y, m, d] = parts.map(Number)
  const end = new Date(y, m - 1, d).getTime()
  if (isNaN(end)) return null
  const now = Date.now()
  return Math.max(0, Math.ceil((end - now) / (1000 * 60 * 60 * 24)))
}

function progressPercent(current: number, target: number): number {
  if (target <= 0) return 0
  return Math.min(100, Math.round((current / target) * 100))
}

interface ChallengeCardProps {
  challenge: ChallengeWithProgress
  t: ReturnType<typeof useTranslation<'common'>>['t']
}

function ChallengeCard({ challenge, t }: ChallengeCardProps) {
  const percent = progressPercent(challenge.current_value, challenge.target_value)
  const days = daysRemaining(challenge.end_date)

  return (
    <div className="rounded-xl bg-gray-800/60 border border-gray-700 p-4 flex flex-col gap-3">
      <div className="flex items-start justify-between gap-2">
        <div className="flex-1 min-w-0">
          <h3 className="text-white font-semibold text-sm leading-tight">{challenge.title}</h3>
          {challenge.description && (
            <p className="text-gray-400 text-xs mt-1 leading-snug">{challenge.description}</p>
          )}
        </div>
        <span className="flex-shrink-0 rounded-full bg-yellow-400/10 border border-yellow-400/30 px-2 py-0.5 text-yellow-400 text-xs font-semibold whitespace-nowrap">
          {t('stars.challenges.starReward', { count: challenge.star_reward })}
        </span>
      </div>

      {challenge.completed ? (
        <div className="flex items-center gap-2 text-green-400 text-sm font-semibold">
          <CheckCircle size={16} />
          <span>{t('stars.challenges.completedBadge')}</span>
        </div>
      ) : (
        <>
          <div>
            <div className="flex justify-between text-xs text-gray-400 mb-1">
              <span>{percent}%</span>
              <span>
                {challenge.current_value} / {challenge.target_value}
              </span>
            </div>
            <div
              className="h-2 w-full rounded-full bg-gray-700 overflow-hidden"
              role="progressbar"
              aria-valuenow={percent}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-label={`${challenge.title}: ${percent}% complete`}
            >
              <div
                className="h-full rounded-full bg-yellow-400 transition-all duration-300"
                style={{ width: `${percent}%` }}
                aria-hidden="true"
              />
            </div>
          </div>
          {days !== null && (
            <p className="text-xs text-gray-500">
              {days === 0
                ? t('stars.challenges.expiresToday')
                : t('stars.challenges.daysRemaining', { count: days })}
            </p>
          )}
        </>
      )}
    </div>
  )
}

interface HistoryCardProps {
  challenge: ChallengeWithProgress
  t: ReturnType<typeof useTranslation<'common'>>['t']
}

function HistoryCard({ challenge, t }: HistoryCardProps) {
  const percent = progressPercent(challenge.current_value, challenge.target_value)

  return (
    <div className="rounded-xl bg-gray-800/30 border border-gray-700/50 p-4 flex flex-col gap-2 opacity-60">
      <div className="flex items-start justify-between gap-2">
        <h3 className="text-gray-300 font-medium text-sm leading-tight">{challenge.title}</h3>
        <span className="flex-shrink-0 rounded-full bg-gray-700 px-2 py-0.5 text-gray-400 text-xs font-semibold whitespace-nowrap">
          {t('stars.challenges.starReward', { count: challenge.star_reward })}
        </span>
      </div>
      <div className="h-1.5 w-full rounded-full bg-gray-700 overflow-hidden">
        <div
          className="h-full rounded-full bg-gray-500"
          style={{ width: `${percent}%` }}
        />
      </div>
      <div className="flex justify-between text-xs text-gray-500">
        <span>{percent}%</span>
        <span>{challenge.current_value} / {challenge.target_value}</span>
      </div>
    </div>
  )
}

export default function StarChallenges() {
  const { t } = useTranslation('common')
  const [challenges, setChallenges] = useState<ChallengeWithProgress[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)

  useEffect(() => {
    const controller = new AbortController()

    const fetchChallenges = async () => {
      setError(null)
      setLoading(true)
      try {
        const res = await fetch('/api/stars/challenges', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error('fetch failed')
        const data: { challenges: ChallengeWithProgress[] } = await res.json()
        setChallenges(data.challenges ?? [])
      } catch (err: unknown) {
        if (controller.signal.aborted) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(t('stars.challenges.errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }

    fetchChallenges()
    return () => controller.abort()
  }, [t])

  const now = new Date()
  const isExpired = (c: ChallengeWithProgress) => {
    const parts = c.end_date.split('-')
    if (parts.length !== 3) return false
    const [y, m, d] = parts.map(Number)
    const end = new Date(y, m - 1, d)
    return !isNaN(end.getTime()) && end < now
  }
  const active = challenges.filter(c => !c.completed && !isExpired(c))
  const completed = challenges.filter(c => c.completed)
  const history = challenges.filter(c => !c.completed && isExpired(c))

  if (loading) {
    return (
      <div className="p-6 max-w-2xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Target size={24} className="text-yellow-400" />
          <h1 className="text-2xl font-semibold text-white">{t('stars.challenges.title')}</h1>
        </div>
        <div className="space-y-3">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-28 rounded-xl bg-gray-800 animate-pulse" />
          ))}
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6 max-w-2xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Target size={24} className="text-yellow-400" />
          <h1 className="text-2xl font-semibold text-white">{t('stars.challenges.title')}</h1>
        </div>
        <div className="text-red-400">{error}</div>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-2xl mx-auto space-y-6">
      <div className="flex items-center gap-3">
        <Link
          to="/stars"
          className="text-gray-400 hover:text-white transition-colors"
          aria-label={t('stars.challenges.back')}
        >
          <ArrowLeft size={20} />
        </Link>
        <Target size={24} className="text-yellow-400" />
        <h1 className="text-2xl font-semibold text-white">{t('stars.challenges.title')}</h1>
      </div>

      {/* Active challenges */}
      {active.length === 0 && completed.length === 0 ? (
        <div className="rounded-xl bg-gray-800/50 border border-gray-700 p-8 text-center">
          <p className="text-gray-400">{t('stars.challenges.noChallenges')}</p>
          <p className="text-gray-500 text-sm mt-1">{t('stars.challenges.noChallengesHint')}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {active.map(c => (
            <ChallengeCard key={c.id} challenge={c} t={t} />
          ))}
        </div>
      )}

      {/* Completed challenges */}
      {completed.length > 0 && (
        <div className="space-y-3">
          <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wide">
            {t('stars.challenges.completed')}
          </h2>
          {completed.map(c => (
            <ChallengeCard key={c.id} challenge={c} t={t} />
          ))}
        </div>
      )}

      {/* History section (collapsible) */}
      {history.length > 0 && (
        <div>
          <button
            type="button"
            onClick={() => setHistoryOpen(v => !v)}
            className="flex items-center gap-2 text-sm font-semibold text-gray-400 uppercase tracking-wide cursor-pointer hover:text-gray-300 transition-colors"
            aria-expanded={historyOpen}
          >
            {historyOpen ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
            {t('stars.challenges.history')}
            <span className="text-xs normal-case font-normal text-gray-500">({history.length})</span>
          </button>
          {historyOpen && (
            <div className="mt-3 space-y-2">
              {history.map(c => (
                <HistoryCard key={c.id} challenge={c} t={t} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
