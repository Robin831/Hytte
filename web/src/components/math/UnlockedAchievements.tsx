import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { ParseKeys } from 'i18next'
import { Award, Sparkles } from 'lucide-react'

export interface UnlockedAchievement {
  code: string
  title: string
  description: string
  tier: string
  unlocked_at: string
}

// UnlockedAchievementsBanner is the in-page notification we show on the
// session result screen when one or more achievements were unlocked by
// the run that just finished. It is intentionally lightweight — the full
// celebration animation is deferred to the polish bead.
export function UnlockedAchievementsBanner({ items }: { items: UnlockedAchievement[] }) {
  const { t, i18n } = useTranslation('regnemester')
  if (!items || items.length === 0) return null

  return (
    <div className="mb-6 rounded-lg border border-pink-500/40 bg-pink-500/10 px-4 py-3">
      <div className="flex items-center gap-2 mb-2">
        <Sparkles size={20} className="text-pink-300" />
        <h2 className="font-semibold text-pink-200">
          {t('achievements.unlockedBannerTitle', { count: items.length })}
        </h2>
      </div>
      <ul className="space-y-1.5">
        {items.map(item => {
          const titleKey = `achievements.codes.${item.code}.title` as ParseKeys<'regnemester'>
          const title = i18n.exists(`regnemester:${titleKey}`) ? t(titleKey) : item.title
          return (
            <li key={item.code} className="flex items-start gap-2 text-sm text-white">
              <Award size={16} className="text-yellow-300 shrink-0 mt-0.5" />
              <span className="font-medium">{title}</span>
            </li>
          )
        })}
      </ul>
      <Link
        to="/math/achievements"
        className="mt-3 inline-block text-sm font-medium text-pink-200 hover:text-white"
      >
        {t('achievements.viewLink')}
      </Link>
    </div>
  )
}
