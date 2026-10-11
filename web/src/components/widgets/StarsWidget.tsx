import { Flame, Star } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { getJSON, useWidgetData } from './useWidgetData'

interface Stars {
  balance: number
  title: string
  streak: number
}

async function loadStars(signal: AbortSignal): Promise<Stars> {
  const [balance, streaks] = await Promise.all([
    getJSON<{ current_balance: number; title: string }>('/api/stars/balance', signal),
    // The streak is a bonus line: the balance still shows without it.
    getJSON<{ daily_workout?: { current_count: number } }>('/api/stars/streaks', signal).catch(() => null),
  ])
  return { balance: balance.current_balance, title: balance.title, streak: streaks?.daily_workout?.current_count ?? 0 }
}

/** A child's star balance, level title and workout streak. */
export default function StarsWidget() {
  const { t } = useTranslation('dashboard')
  const { data, loaded } = useWidgetData(loadStars)
  if (!loaded || !data) return null

  return (
    <Widget title={t('widgets.stars.title')}>
      <p className="flex items-center gap-2 text-3xl font-bold tabular-nums">
        <Star size={24} className="shrink-0 fill-yellow-400 text-yellow-400" aria-hidden="true" />
        {data.balance}
        <span className="text-sm font-normal text-gray-400">{t('widgets.stars.stars', { count: data.balance })}</span>
      </p>
      {data.title && <p className="mt-1 text-sm text-gray-300">{data.title}</p>}
      {data.streak > 0 && (
        <p className="mt-2 flex items-center gap-1.5 text-sm text-gray-200">
          <Flame size={16} className="shrink-0 text-orange-400" aria-hidden="true" />
          {t('widgets.stars.streak', { count: data.streak })}
        </p>
      )}
      <WidgetLink to="/stars">{t('widgets.stars.open')}</WidgetLink>
    </Widget>
  )
}
