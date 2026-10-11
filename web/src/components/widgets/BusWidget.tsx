import { useEffect } from 'react'
import { Bus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { invalidate, useWidgetData } from './useWidgetData'
import { departuresSource } from './sources'
import { useCurrentTime } from '../../hooks/useCurrentTime'
import { formatTime } from '../../utils/formatDate'

/** Departures move on by themselves, so the list is fetched again this often. */
const REFRESH_MS = 120_000
const MAX_DEPARTURES = 4
/** Beyond this the clock time says enough; nearer, the minutes left matter more. */
const COUNTDOWN_MINUTES = 30

/** The next departures from your saved stops. */
export default function BusWidget() {
  const { t } = useTranslation('dashboard')
  const now = useCurrentTime()
  const { data: stops, loaded } = useWidgetData(departuresSource)

  useEffect(() => {
    const id = setInterval(() => invalidate(departuresSource.key), REFRESH_MS)
    return () => clearInterval(id)
  }, [])

  // No saved stops means there is nothing to say; no departures left today is worth saying.
  if (!loaded || !stops || stops.length === 0) return null

  const next = stops
    .flatMap((stop) => stop.departures.map((d) => ({ ...d, stop: stop.stop_name, at: new Date(d.departure_time).getTime() })))
    .filter((d) => d.at > now.getTime())
    .sort((a, b) => a.at - b.at)
    .slice(0, MAX_DEPARTURES)

  return (
    <Widget title={t('widgets.bus.title')}>
      {next.length === 0 ? (
        <p className="text-sm text-gray-400">{t('widgets.bus.none')}</p>
      ) : (
        <ul className="space-y-2">
          {next.map((d) => {
            const minutes = Math.max(0, Math.round((d.at - now.getTime()) / 60_000))
            return (
              <li key={`${d.stop}-${d.line}-${d.departure_time}`} className="flex items-center gap-2 text-sm">
                <Bus size={16} className="shrink-0 text-gray-400" aria-hidden="true" />
                <span className="shrink-0 font-semibold text-blue-400">{d.line}</span>
                <span className="min-w-0">
                  <span className="block truncate text-gray-200">{d.destination}</span>
                  {stops.length > 1 && <span className="block truncate text-xs text-gray-500">{d.stop}</span>}
                </span>
                <span className="ml-auto shrink-0 text-right tabular-nums">
                  <span className={minutes <= COUNTDOWN_MINUTES ? 'font-semibold text-gray-100' : 'text-gray-300'}>
                    {minutes <= COUNTDOWN_MINUTES
                      ? t('widgets.bus.minutes', { count: minutes })
                      : formatTime(d.departure_time, { hour: '2-digit', minute: '2-digit' })}
                  </span>
                  {d.delay_minutes > 0 && (
                    <span className="block text-xs text-amber-300">{t('widgets.bus.delayed', { count: d.delay_minutes })}</span>
                  )}
                </span>
              </li>
            )
          })}
        </ul>
      )}
      <WidgetLink to="/transit">{t('widgets.bus.open')}</WidgetLink>
    </Widget>
  )
}
