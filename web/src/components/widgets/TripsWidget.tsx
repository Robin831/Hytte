import { Link } from 'react-router'
import { Plane } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { daysBetween, useWidgetData } from './useWidgetData'
import { type TripSummary, dateIn, fetchTrips, toLang, tripState, txt } from '../../pages/trips/tripsApi'

const loadTrips = (signal: AbortSignal) => fetchTrips(signal).then((d) => d.trips ?? [])

/** Ongoing trips first, then upcoming ones by start date; past trips are dropped. */
function upcomingTrips(trips: TripSummary[], now: Date): TripSummary[] {
  return trips
    .filter((t) => tripState(t, now) !== 'after')
    .sort((a, b) => a.start_date.localeCompare(b.start_date))
}

export default function TripsWidget() {
  const { t, i18n } = useTranslation('dashboard')
  const { data, loaded } = useWidgetData(loadTrips)
  if (!loaded || !data) return null

  const now = new Date()
  const trips = upcomingTrips(data, now)
  if (trips.length === 0) return null

  const lang = toLang(i18n.language)
  const dateFmt = new Intl.DateTimeFormat(i18n.language, { day: 'numeric', month: 'short', timeZone: 'UTC' })
  const fmt = (d: string) => dateFmt.format(new Date(d + 'T12:00:00Z'))

  const when = (trip: TripSummary) => {
    if (trip.tentative) return t('widgets.trips.tentative')
    const today = dateIn(trip.dest_tz, now)
    if (tripState(trip, now) === 'during') {
      return t('widgets.trips.dayOf', {
        day: daysBetween(trip.start_date, today) + 1,
        total: daysBetween(trip.start_date, trip.end_date) + 1,
      })
    }
    const days = daysBetween(today, trip.start_date)
    return days === 1 ? t('widgets.trips.tomorrow') : t('widgets.trips.inDays', { count: days })
  }

  const [next, ...later] = trips
  return (
    <Widget title={t('widgets.trips.title')}>
      <Link to={`/trips/${next.id}`} className="block hover:text-blue-300">
        <p className="flex items-center gap-2 text-base font-semibold">
          <Plane size={16} className="shrink-0 text-blue-400" aria-hidden="true" />
          <span className="truncate">{txt(next.title, lang) || next.route.join(' → ')}</span>
        </p>
        <p className="mt-1 text-sm text-blue-300">{when(next)}</p>
        <p className="mt-0.5 text-xs text-gray-500">
          {fmt(next.start_date)} – {fmt(next.end_date)}
          {next.route.length > 0 && ` · ${next.route.join(' → ')}`}
        </p>
        {next.total > 0 && (
          <p className="mt-0.5 text-xs text-gray-500">
            {next.open > 0 ? t('widgets.trips.checklistOpen', { count: next.open }) : t('widgets.trips.checklistDone')}
          </p>
        )}
      </Link>

      {later.length > 0 && (
        <ul className="mt-3 space-y-1 border-t border-gray-700 pt-3">
          {later.slice(0, 2).map((trip) => (
            <li key={trip.id} className="flex items-baseline justify-between gap-3 text-sm">
              <Link to={`/trips/${trip.id}`} className="truncate text-gray-200 hover:text-blue-300">
                {txt(trip.title, lang) || trip.route.join(' → ')}
              </Link>
              <span className="shrink-0 text-xs text-gray-500">{when(trip)}</span>
            </li>
          ))}
        </ul>
      )}

      <WidgetLink to="/trips">{t('widgets.trips.all')}</WidgetLink>
    </Widget>
  )
}
