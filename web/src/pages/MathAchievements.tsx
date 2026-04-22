import { useEffect, useReducer } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { ParseKeys } from 'i18next'
import { ArrowLeft, Award, CheckCircle2, Lock, Timer, Trophy, Zap } from 'lucide-react'
import { formatDate } from '../utils/formatDate'

type Tier = 'marathon' | 'blitz' | 'rivalry'

interface UserStats {
  has_marathon: boolean
  best_marathon_ms: number
  best_marathon_wrong: number
  has_blitz: boolean
  best_blitz_streak: number
  on_top_any_board: boolean
}

interface EarnedRow {
  code: string
  title: string
  description: string
  tier: Tier
  unlocked_at: string
  session_id?: number
}

interface LockedRow {
  code: string
  title: string
  description: string
  tier: Tier
}

interface AchievementsResponse {
  user_stats: UserStats
  earned: EarnedRow[]
  locked: LockedRow[]
}

type FetchState = { loading: boolean; data: AchievementsResponse | null; error: string }
type FetchAction =
  | { type: 'start' }
  | { type: 'success'; data: AchievementsResponse }
  | { type: 'error'; message: string }

function fetchReducer(_state: FetchState, action: FetchAction): FetchState {
  switch (action.type) {
    case 'start': return { loading: true, error: '', data: null }
    case 'success': return { loading: false, error: '', data: action.data }
    case 'error': return { loading: false, error: action.message, data: null }
  }
}

const TIER_ORDER: Tier[] = ['marathon', 'blitz', 'rivalry']

function tierIcon(tier: Tier): React.ReactNode {
  switch (tier) {
    case 'marathon': return <Timer size={18} className="text-blue-300" />
    case 'blitz': return <Zap size={18} className="text-yellow-300" />
    case 'rivalry': return <Trophy size={18} className="text-pink-300" />
  }
}

// formatMarathonDuration formats milliseconds as M:SS for use in progress
// hints. The Sub-N achievements all live in the 3:00–10:00 window so the
// minute value is single-digit.
function formatMarathonDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000))
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds - minutes * 60
  return `${minutes}:${String(seconds).padStart(2, '0')}`
}

// formatDeltaSeconds formats a positive number of milliseconds as e.g. "42s"
// or "1:05" — used for the "X to go" portion of progress hints.
function formatDeltaSeconds(deltaMs: number): string {
  const totalSeconds = Math.max(0, Math.ceil(deltaMs / 1000))
  if (totalSeconds < 60) return `${totalSeconds}s`
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds - minutes * 60
  return `${minutes}:${String(seconds).padStart(2, '0')}`
}

// MARATHON_THRESHOLDS_MS maps each marathon Sub-N code to its duration cap
// in ms. Lives on the client because the threshold is what powers the
// progress hint — the server-authoritative check uses the same numbers.
const MARATHON_THRESHOLDS_MS: Record<string, number> = {
  marathon_sub_10: 10 * 60_000,
  marathon_sub_7: 7 * 60_000,
  marathon_sub_5: 5 * 60_000,
  marathon_sub_4: 4 * 60_000,
  marathon_sub_3: 3 * 60_000,
}

const STREAK_THRESHOLDS: Record<string, number> = {
  streak_25: 25,
  streak_50: 50,
  streak_100: 100,
}

interface ProgressHintProps {
  code: string
  stats: UserStats
}

function ProgressHint({ code, stats }: ProgressHintProps) {
  const { t } = useTranslation('regnemester')

  const marathonThreshold = MARATHON_THRESHOLDS_MS[code]
  if (marathonThreshold !== undefined) {
    if (!stats.has_marathon) {
      return <span className="text-xs text-gray-500">{t('achievements.progress.noMarathon')}</span>
    }
    const delta = stats.best_marathon_ms - marathonThreshold
    if (delta <= 0) {
      // The user already meets the time but the achievement is somehow still
      // locked (e.g. their best run predates the achievement system). Show a
      // hint that finishing one more run will unlock it.
      return (
        <span className="text-xs text-blue-300">
          {t('achievements.progress.marathonReady', { best: formatMarathonDuration(stats.best_marathon_ms) })}
        </span>
      )
    }
    return (
      <span className="text-xs text-gray-400">
        {t('achievements.progress.marathonGap', {
          best: formatMarathonDuration(stats.best_marathon_ms),
          gap: formatDeltaSeconds(delta),
        })}
      </span>
    )
  }

  if (code === 'marathon_perfect_100') {
    if (!stats.has_marathon) {
      return <span className="text-xs text-gray-500">{t('achievements.progress.noMarathon')}</span>
    }
    return (
      <span className="text-xs text-gray-400">
        {t('achievements.progress.marathonPerfect', { wrong: stats.best_marathon_wrong })}
      </span>
    )
  }

  const streakThreshold = STREAK_THRESHOLDS[code]
  if (streakThreshold !== undefined) {
    if (!stats.has_blitz) {
      return <span className="text-xs text-gray-500">{t('achievements.progress.noBlitz')}</span>
    }
    if (stats.best_blitz_streak >= streakThreshold) {
      return (
        <span className="text-xs text-blue-300">
          {t('achievements.progress.streakReady', { streak: stats.best_blitz_streak })}
        </span>
      )
    }
    return (
      <span className="text-xs text-gray-400">
        {t('achievements.progress.streakGap', {
          best: stats.best_blitz_streak,
          gap: streakThreshold - stats.best_blitz_streak,
        })}
      </span>
    )
  }

  if (code === 'first_blood') {
    if (stats.on_top_any_board) {
      return <span className="text-xs text-blue-300">{t('achievements.progress.firstBloodReady')}</span>
    }
    return <span className="text-xs text-gray-400">{t('achievements.progress.firstBloodHint')}</span>
  }

  return null
}

interface AchievementCardProps {
  code: string
  tier: Tier
  fallbackTitle: string
  fallbackDescription: string
  earnedAt?: string
  stats: UserStats
}

function AchievementCard({ code, tier, fallbackTitle, fallbackDescription, earnedAt, stats }: AchievementCardProps) {
  const { t, i18n } = useTranslation('regnemester')
  const titleKey = `achievements.codes.${code}.title` as ParseKeys<'regnemester'>
  const descKey = `achievements.codes.${code}.description` as ParseKeys<'regnemester'>
  // Fall back to backend-supplied English strings if a translation is
  // missing — keeps the page useful while a new code awaits translation.
  const hasTitle = i18n.exists(`regnemester:${titleKey}`)
  const hasDesc = i18n.exists(`regnemester:${descKey}`)
  const title = hasTitle ? t(titleKey) : fallbackTitle
  const description = hasDesc ? t(descKey) : fallbackDescription
  const isEarned = !!earnedAt

  return (
    <li
      className={`rounded-lg border p-4 transition-colors ${
        isEarned
          ? 'border-yellow-500/40 bg-yellow-500/10'
          : 'border-gray-700 bg-gray-800/60 opacity-80'
      }`}
    >
      <div className="flex items-start gap-3">
        <div
          className={`p-2 rounded-md shrink-0 ${
            isEarned ? 'bg-yellow-500/20 text-yellow-300' : 'bg-gray-800 text-gray-500'
          }`}
        >
          {isEarned ? <CheckCircle2 size={20} /> : <Lock size={20} />}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 mb-1 flex-wrap">
            {tierIcon(tier)}
            <h3 className={`text-base font-semibold ${isEarned ? 'text-yellow-200' : 'text-white'}`}>
              {title}
            </h3>
          </div>
          <p className={`text-sm ${isEarned ? 'text-yellow-100/80' : 'text-gray-400'}`}>
            {description}
          </p>
          <div className="mt-2">
            {isEarned && earnedAt ? (
              <span className="text-xs text-yellow-300/80">
                {t('achievements.unlockedOn', {
                  date: formatDate(earnedAt, { year: 'numeric', month: 'short', day: 'numeric' }),
                })}
              </span>
            ) : (
              <ProgressHint code={code} stats={stats} />
            )}
          </div>
        </div>
      </div>
    </li>
  )
}

export default function MathAchievements() {
  const { t } = useTranslation('regnemester')
  const [{ loading, data, error }, dispatch] = useReducer(fetchReducer, {
    loading: true,
    data: null,
    error: '',
  })

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'start' })
    fetch('/api/math/achievements', { credentials: 'include', signal: controller.signal })
      .then(res => {
        if (!res.ok) throw new Error('fetch failed')
        return res.json() as Promise<AchievementsResponse>
      })
      .then(json => {
        if (!controller.signal.aborted) dispatch({ type: 'success', data: json })
      })
      .catch(err => {
        if (controller.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
        dispatch({ type: 'error', message: t('achievements.errorLoad') })
      })
    return () => { controller.abort() }
  }, [t])

  return (
    <div className="max-w-3xl mx-auto p-4 sm:p-6 space-y-5">
      <div className="flex items-center gap-3">
        <Link
          to="/math"
          aria-label={t('back')}
          className="text-gray-400 hover:text-white transition-colors"
        >
          <ArrowLeft size={20} />
        </Link>
        <Award size={24} className="text-yellow-400 shrink-0" />
        <h1 className="text-2xl sm:text-3xl font-bold text-white">{t('achievements.title')}</h1>
      </div>

      <p className="text-gray-400 text-sm">{t('achievements.intro')}</p>

      {loading && (
        <div className="space-y-2" aria-live="polite">
          {[0, 1, 2].map(i => (
            <div key={i} className="h-20 rounded-lg bg-gray-800 animate-pulse" />
          ))}
        </div>
      )}

      {!loading && error && (
        <div className="rounded border border-red-500/50 bg-red-500/10 px-3 py-2 text-sm text-red-300">
          {error}
        </div>
      )}

      {!loading && !error && data && (
        <div className="space-y-6">
          {(() => {
            // Build per-tier groups from earned + locked collections, then
            // render each tier section. Doing the partition once here keeps
            // the JSX tidy and avoids re-iterating the registry per tier.
            const byTier = new Map<Tier, { earned: EarnedRow[]; locked: LockedRow[] }>()
            for (const tier of TIER_ORDER) {
              byTier.set(tier, { earned: [], locked: [] })
            }
            for (const row of data.earned) {
              byTier.get(row.tier)?.earned.push(row)
            }
            for (const row of data.locked) {
              byTier.get(row.tier)?.locked.push(row)
            }

            return TIER_ORDER.map(tier => {
              const group = byTier.get(tier)
              if (!group || (group.earned.length === 0 && group.locked.length === 0)) {
                return null
              }
              const tierLabelKey = `achievements.tiers.${tier}` as ParseKeys<'regnemester'>
              return (
                <section key={tier} aria-labelledby={`tier-${tier}-heading`}>
                  <h2
                    id={`tier-${tier}-heading`}
                    className="text-sm font-semibold uppercase tracking-wider text-gray-400 mb-3 flex items-center gap-2"
                  >
                    {tierIcon(tier)}
                    {t(tierLabelKey)}
                  </h2>
                  <ul className="space-y-2">
                    {group.earned.map(row => (
                      <AchievementCard
                        key={row.code}
                        code={row.code}
                        tier={row.tier}
                        fallbackTitle={row.title}
                        fallbackDescription={row.description}
                        earnedAt={row.unlocked_at}
                        stats={data.user_stats}
                      />
                    ))}
                    {group.locked.map(row => (
                      <AchievementCard
                        key={row.code}
                        code={row.code}
                        tier={row.tier}
                        fallbackTitle={row.title}
                        fallbackDescription={row.description}
                        stats={data.user_stats}
                      />
                    ))}
                  </ul>
                </section>
              )
            })
          })()}

          {data.earned.length === 0 && (
            <div className="rounded-lg border border-gray-700 bg-gray-800/60 p-4 text-sm text-gray-400">
              {t('achievements.emptyState')}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
