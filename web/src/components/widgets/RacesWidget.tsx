import { Link } from 'react-router'
import { Flag } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { useWidgetData } from './useWidgetData'
import { type Deadline, type RaceEvent, type WatchState, daysUntil, fetchRaces } from '../../pages/races/racesApi'
import { useRaceFormat } from '../../pages/races/useRaceFormat'

const loadRaces = (signal: AbortSignal) => fetchRaces(signal)

/** Watch states that mean the race is still ahead of you. */
const ACTIVE: WatchState[] = ['registered', 'got_place', 'lottery_entered', 'planning', 'watching']
const RUNNING: WatchState[] = ['registered', 'got_place']

interface Watched {
  event: RaceEvent
  state: WatchState
}

export default function RacesWidget() {
  const { t } = useTranslation('dashboard')
  const { t: tRaces, fuzzy, daysLeft } = useRaceFormat()
  const { data, loaded } = useWidgetData(loadRaces)
  if (!loaded || !data) return null

  const stateOf = new Map((data.watches ?? []).map((w) => [w.event_id, w.state]))
  const watched: Watched[] = (data.events ?? [])
    .filter((e) => ACTIVE.includes(stateOf.get(e.id) as WatchState) && daysUntil(e.race_date) >= 0)
    .map((e) => ({ event: e, state: stateOf.get(e.id) as WatchState }))
    .sort((a, b) => a.event.race_date.localeCompare(b.event.race_date))

  // The race you are actually running outranks one you are only watching.
  const next = watched.find((w) => RUNNING.includes(w.state)) ?? watched[0]
  const deadlines: { event: RaceEvent; deadline: Deadline }[] = watched
    .flatMap((w) => (w.event.deadlines ?? []).map((deadline) => ({ event: w.event, deadline })))
    .filter((d) => daysUntil(d.deadline.due_date) >= 0)
    .sort((a, b) => a.deadline.due_date.localeCompare(b.deadline.due_date))
    .slice(0, 2)

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
