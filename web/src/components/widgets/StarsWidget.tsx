import { Flame, Star } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { useWidgetData } from './useWidgetData'
import { starsSource } from './sources'

/** A child's star balance, level title and workout streak. */
export default function StarsWidget() {
  const { t } = useTranslation('dashboard')
  const { data, loaded } = useWidgetData(starsSource)
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
