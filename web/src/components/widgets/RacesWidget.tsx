import { Link } from 'react-router'
import { Flag } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { useWidgetData } from './useWidgetData'
import { racesSource, watchedRaces } from './sources'
import { useRaceFormat } from '../../pages/races/useRaceFormat'

export default function RacesWidget() {
  const { t } = useTranslation('dashboard')
  const { t: tRaces, fuzzy, daysLeft } = useRaceFormat()
  const { data, loaded } = useWidgetData(racesSource)
  if (!loaded || !data) return null

  const { races, deadlines: allDeadlines } = watchedRaces(data)
  // The race you are actually running outranks one you are only watching.
  const next = races.find((r) => r.running) ?? races[0]
  const deadlines = allDeadlines.slice(0, 2)

  return (
    <Widget title={t('widgets.races.title')}>
      {next ? (
        <Link to={`/races/${next.event.id}`} className="block hover:text-blue-300">
          <p className="flex items-center gap-2 text-base font-semibold">
            <Flag size={16} className="shrink-0 text-blue-400" aria-hidden="true" />
            <span className="truncate">{next.event.name}</span>
          </p>
          <p className="mt-1 text-sm text-blue-300">{daysLeft(next.event.race_date)}</p>
          <p className="mt-0.5 text-xs text-gray-500">
            {fuzzy(next.event.race_date, next.event.date_precision)} · {tRaces(`watchState.${next.state}`)}
          </p>
        </Link>
      ) : (
        <p className="text-sm text-gray-400">{t('widgets.races.none')}</p>
      )}

      {deadlines.length > 0 && (
        <ul className="mt-3 space-y-1 border-t border-gray-700 pt-3">
          {deadlines.map(({ event, deadline }) => (
            <li key={deadline.id} className="flex items-baseline justify-between gap-3 text-sm">
              <Link to={`/races/${event.id}`} className="min-w-0 truncate text-gray-200 hover:text-blue-300">
                {tRaces(`kind.${deadline.kind}`)} · {event.name}
              </Link>
              <span className="shrink-0 text-xs text-amber-300">{daysLeft(deadline.due_date)}</span>
            </li>
          ))}
        </ul>
      )}

      <WidgetLink to="/races">{t('widgets.races.all')}</WidgetLink>
    </Widget>
  )
}
