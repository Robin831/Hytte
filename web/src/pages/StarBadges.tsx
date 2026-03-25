import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Medal, ArrowLeft } from 'lucide-react'
import '../stars.css'

interface AvailableBadge {
  key: string
  name: string
  description: string
  icon_emoji: string
  category: string
  tier: string
  xp_reward: number
  earned: boolean
  awarded_at?: string
}

const CATEGORIES = ['distance', 'consistency', 'speed', 'variety', 'heart', 'fun', 'secret'] as const
type Category = typeof CATEGORIES[number]

function tierBorderClass(tier: string): string {
  switch (tier) {
    case 'bronze': return 'border-amber-600'
    case 'silver': return 'border-gray-400'
    case 'gold': return 'border-yellow-400 badge-gold-glow'
    case 'platinum':
    case 'diamond': return 'border-cyan-400 badge-diamond-shimmer'
    default: return 'border-gray-600'
  }
}

function tierLabelClass(tier: string): string {
  switch (tier) {
    case 'bronze': return 'text-amber-500'
    case 'silver': return 'text-gray-300'
    case 'gold': return 'text-yellow-400'
    case 'platinum':
    case 'diamond': return 'text-cyan-400'
    default: return 'text-gray-500'
  }
}

interface BadgeCardProps {
  badge: AvailableBadge
  locale: string
  t: ReturnType<typeof useTranslation<'common'>>['t']
}

function BadgeCard({ badge, locale, t }: BadgeCardProps) {
  const [hovered, setHovered] = useState(false)

  const borderClass = tierBorderClass(badge.tier)
  const tierClass = tierLabelClass(badge.tier)

  const formattedDate = badge.awarded_at
    ? new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(new Date(badge.awarded_at))
    : null

  return (
    <div
      className={`relative rounded-xl border-2 ${borderClass} p-4 flex flex-col items-center gap-2 text-center transition-all duration-200 min-h-[120px] bg-gray-800/60`}
      style={badge.earned ? {} : { filter: 'grayscale(1)', opacity: 0.4 }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <span className="text-3xl" role="img" aria-hidden="true">{badge.icon_emoji}</span>
      <p className="text-white text-xs font-semibold leading-tight">{badge.name}</p>
      <p className={`text-xs font-medium uppercase tracking-wide ${tierClass}`}>{badge.tier}</p>

      {hovered && (
        <div className="absolute inset-0 rounded-xl bg-gray-900/92 flex flex-col items-center justify-center p-3 gap-1 z-10">
          <span className="text-2xl" role="img" aria-hidden="true">{badge.icon_emoji}</span>
          <p className="text-white text-xs font-semibold leading-tight">{badge.name}</p>
          <p className="text-gray-300 text-xs text-center leading-tight">{badge.description}</p>
          {badge.earned && formattedDate && (
            <p className="text-yellow-400 text-xs mt-1">
              {t('stars.badges.awardedOn', { date: formattedDate })}
            </p>
          )}
          {badge.earned && (
            <p className={`text-xs font-medium ${tierClass}`}>+{badge.xp_reward} XP</p>
          )}
        </div>
      )}
    </div>
  )
}

export default function StarBadges() {
  const { t, i18n } = useTranslation('common')
  const [badges, setBadges] = useState<AvailableBadge[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [activeCategory, setActiveCategory] = useState<Category>('distance')

  useEffect(() => {
    const fetchBadges = async () => {
      setError(null)
      setLoading(true)
      try {
        const res = await fetch('/api/stars/badges/available', { credentials: 'include' })
        if (!res.ok) throw new Error('fetch failed')
        const data: AvailableBadge[] = await res.json()
        setBadges(data)
      } catch {
        setError(t('stars.badges.errors.failedToLoad'))
      } finally {
        setLoading(false)
      }
    }
    fetchBadges()
  }, [t])

  const byCategory = CATEGORIES.reduce<Record<string, AvailableBadge[]>>((acc, cat) => {
    acc[cat] = badges.filter(b => b.category === cat)
    return acc
  }, {})

  const visibleCategories = CATEGORIES.filter(cat => byCategory[cat].length > 0)
  const currentCategory = visibleCategories.includes(activeCategory) ? activeCategory : (visibleCategories[0] ?? 'distance')
  const currentBadges = byCategory[currentCategory] ?? []
  const earnedCount = badges.filter(b => b.earned).length

  if (loading) {
    return (
      <div className="p-6 max-w-2xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Medal size={24} className="text-yellow-400" />
          <h1 className="text-2xl font-semibold text-white">{t('stars.badges.title')}</h1>
        </div>
        <div className="space-y-4">
          <div className="h-10 rounded-lg bg-gray-800 animate-pulse" />
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            {[...Array(6)].map((_, i) => (
              <div key={i} className="h-32 rounded-xl bg-gray-800 animate-pulse" />
            ))}
          </div>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6 max-w-2xl mx-auto">
        <div className="flex items-center gap-3 mb-6">
          <Medal size={24} className="text-yellow-400" />
          <h1 className="text-2xl font-semibold text-white">{t('stars.badges.title')}</h1>
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
          aria-label={t('stars.badges.back')}
        >
          <ArrowLeft size={20} />
        </Link>
        <Medal size={24} className="text-yellow-400" />
        <h1 className="text-2xl font-semibold text-white">{t('stars.badges.title')}</h1>
        <span className="ml-auto text-sm text-gray-400">
          {t('stars.badges.earnedCount', { count: earnedCount, total: badges.length })}
        </span>
      </div>

      {/* Category tabs */}
      <div
        className="flex gap-2 overflow-x-auto pb-1"
        style={{ scrollbarWidth: 'none' }}
      >
        {visibleCategories.map(cat => {
          const catBadges = byCategory[cat]
          const catEarned = catBadges.filter(b => b.earned).length
          return (
            <button
              key={cat}
              onClick={() => setActiveCategory(cat)}
              className={`flex-shrink-0 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors cursor-pointer ${
                currentCategory === cat
                  ? 'bg-gray-700 text-white'
                  : 'text-gray-400 hover:text-white hover:bg-gray-800/50'
              }`}
            >
              {t(`stars.badges.categories.${cat}`)}
              <span className="ml-1.5 text-xs text-gray-500">{catEarned}/{catBadges.length}</span>
            </button>
          )
        })}
      </div>

      {/* Badge grid */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {currentBadges.length > 0 ? (
          currentBadges.map(badge => (
            <BadgeCard key={badge.key} badge={badge} locale={i18n.language} t={t} />
          ))
        ) : (
          <div className="col-span-2 sm:col-span-3 p-8 text-center bg-gray-800/50 rounded-lg border border-gray-700">
            <p className="text-gray-400">{t('stars.badges.empty')}</p>
          </div>
        )}
      </div>
    </div>
  )
}
