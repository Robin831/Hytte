import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { getJSON, source, useWidgetData } from './useWidgetData'
import { usePreferredLocation } from '../../usePreferredLocation'
import { formatTime } from '../../utils/formatDate'

interface Moon {
  phase: string
  illumination: number
  moonrise: string | null
  moonset: string | null
}

/** The phases the server names, with the symbol and translation for each. */
const PHASES = {
  'New Moon': { symbol: '🌑', key: 'widgets.moon.phase.new_moon' },
  'Waxing Crescent': { symbol: '🌒', key: 'widgets.moon.phase.waxing_crescent' },
  'First Quarter': { symbol: '🌓', key: 'widgets.moon.phase.first_quarter' },
  'Waxing Gibbous': { symbol: '🌔', key: 'widgets.moon.phase.waxing_gibbous' },
  'Full Moon': { symbol: '🌕', key: 'widgets.moon.phase.full_moon' },
  'Waning Gibbous': { symbol: '🌖', key: 'widgets.moon.phase.waning_gibbous' },
  'Last Quarter': { symbol: '🌗', key: 'widgets.moon.phase.last_quarter' },
  'Waning Crescent': { symbol: '🌘', key: 'widgets.moon.phase.waning_crescent' },
} as const

/** Tonight's moon where you are: phase, how much is lit, and when it rises and sets. */
export default function MoonWidget() {
  const { t } = useTranslation('dashboard')
  const { lat, lon } = usePreferredLocation()
  const moonSource = useMemo(
    () => source(`moon:${lat},${lon}`, () => getJSON<{ moon: Moon }>(`/api/skywatch/now?lat=${lat}&lon=${lon}`).then((d) => d.moon)),
    [lat, lon],
  )
  const { data: moon, loaded } = useWidgetData(moonSource)
  if (!loaded || !moon) return null

  const known = PHASES[moon.phase as keyof typeof PHASES]
  const name = known ? t(known.key) : moon.phase
  const time = (iso: string) => formatTime(iso, { hour: '2-digit', minute: '2-digit' })

  return (
    <Widget title={t('widgets.moon.title')}>
      <div className="flex items-center gap-4">
        <span className="text-5xl leading-none" role="img" aria-label={name}>
          {known?.symbol ?? '🌙'}
        </span>
        <div className="min-w-0">
          <p className="text-base font-semibold">{name}</p>
          <p className="text-sm text-gray-400">{t('widgets.moon.lit', { percent: Math.round(moon.illumination * 100) })}</p>
        </div>
      </div>
      {(moon.moonrise || moon.moonset) && (
        <p className="mt-3 flex flex-wrap gap-x-4 text-xs tabular-nums text-gray-400">
          {moon.moonrise && <span>{t('widgets.moon.rises', { time: time(moon.moonrise) })}</span>}
          {moon.moonset && <span>{t('widgets.moon.sets', { time: time(moon.moonset) })}</span>}
        </p>
      )}
      <WidgetLink to="/skywatch">{t('widgets.moon.open')}</WidgetLink>
    </Widget>
  )
}
