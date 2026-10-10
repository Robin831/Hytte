import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Bookmark, BookmarkCheck, ExternalLink, Plane } from 'lucide-react'
import { type RaceEvent, type RaceStatus, type Travel, type Watch, dateBlock, flag, isNewRace, pickText } from './racesApi'
import { useRaceFormat } from './useRaceFormat'
import { usePriceText, useHome } from './prices'
import { RaceFamilyChips } from './Phase5'

const STATUS_CLASS: Record<RaceStatus, string> = {
  open: 'bg-green-900/50 text-green-300 border-green-800',
  later: 'bg-amber-900/40 text-amber-300 border-amber-800',
  closed: 'bg-red-900/40 text-red-300 border-red-800',
}

export function StatusPill({ status }: { status: RaceStatus }) {
  const { t } = useTranslation('races')
  return (
    <span className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-0.5 text-xs font-medium ${STATUS_CLASS[status]}`}>
      <span className="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" />
      {t(`status.${status}`)}
    </span>
  )
}

const TRAVEL_CLASS: Record<Exclude<Travel, ''>, string> = {
  direct: 'bg-green-900/40 text-green-300',
  nearby: 'bg-amber-900/40 text-amber-300',
  none: 'bg-red-900/40 text-red-300',
}

export function TravelBadge({ travel }: { travel: Travel }) {
  const { t } = useTranslation('races')
  if (!travel) return null
  return (
    <span className={`mr-1.5 inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${TRAVEL_CLASS[travel]}`}>
      <Plane size={11} aria-hidden="true" />
      {t(`travel.${travel}`)}
    </span>
  )
}

export function DateBlock({ event }: { event: RaceEvent }) {
  const { t, lang } = useRaceFormat()
  const { month, day } = dateBlock(event, lang)
  const fuzzyWord = event.date_precision === 'day' ? null : t(`precisionShort.${event.date_precision}`)
  return (
    <div className="w-12 shrink-0 text-center leading-none" aria-hidden="true">
      <span className="block text-xs font-semibold uppercase tracking-wider text-gray-400">{month}</span>
      {day ? (
        <span className="mt-1 block text-2xl font-bold tabular-nums">{day}</span>
      ) : (
        <span className="mt-2 block text-sm font-semibold text-gray-300">{fuzzyWord}</span>
      )}
    </div>
  )
}

interface RaceRowProps {
  event: RaceEvent
  watch?: Watch
  onToggleWatch?: (event: RaceEvent) => void
  busy?: boolean
}

/** One race in a list: date block, name/place, status, facts, how to get in. */
export function RaceRow({ event, watch, onToggleWatch, busy }: RaceRowProps) {
  const { t, lang, fuzzy } = useRaceFormat()
  const text = pickText(event.texts, lang)
  const priced = usePriceText(lang)
  const home = useHome()
  const tracked = !!watch

  return (
    <article className="flex gap-3 border-t border-gray-800 py-4" aria-labelledby={`race-${event.id}`}>
      <DateBlock event={event} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <h3 id={`race-${event.id}`} className="min-w-0 text-lg font-semibold leading-tight">
            <Link to={`/races/${event.id}`} className="hover:text-blue-300">{event.name}</Link>
            <span className="mt-0.5 block text-sm font-normal text-gray-400">
              {flag(event.country)} {text?.place} · <span className="sr-only">{t('detail.date')}: </span>
              {fuzzy(event.race_date, event.date_precision)}
            </span>
          </h3>
          <div className="flex items-center gap-2">
            <RaceFamilyChips eventId={event.id} />
            {isNewRace(event) && <span className="rounded-full bg-purple-900/50 px-2 py-0.5 text-xs font-semibold text-purple-200">{t('newBadge')}</span>}
            {watch && <span className="rounded-full bg-blue-900/50 px-2 py-0.5 text-xs text-blue-200">{t(`watchState.${watch.state}`)}</span>}
            <StatusPill status={event.status} />
            {onToggleWatch && (
              <button
                type="button"
                onClick={() => onToggleWatch(event)}
                disabled={busy}
                aria-pressed={tracked}
                title={tracked ? t('watch.untrack') : t('watch.track')}
                aria-label={`${tracked ? t('watch.untrack') : t('watch.track')}: ${event.name}`}
                className={`rounded-full p-1.5 transition-colors cursor-pointer disabled:opacity-50 ${
                  tracked ? 'text-blue-300 hover:text-blue-200' : 'text-gray-500 hover:text-gray-200'
                }`}
              >
                {tracked ? <BookmarkCheck size={18} /> : <Bookmark size={18} />}
              </button>
            )}
          </div>
        </div>

        <dl className="mt-2 grid grid-cols-1 gap-x-3 gap-y-1 text-sm sm:grid-cols-[7rem_minmax(0,1fr)]">
          {text?.participants && <>
            <dt className="text-xs font-semibold uppercase tracking-wide text-gray-500 sm:pt-0.5">{t('facts.participants')}</dt>
            <dd className="mb-1 sm:mb-0">{text.participants}</dd>
          </>}
          {text?.course && <>
            <dt className="text-xs font-semibold uppercase tracking-wide text-gray-500 sm:pt-0.5">{t('facts.course')}</dt>
            <dd className="mb-1 sm:mb-0">{text.course}</dd>
          </>}
          {(event.travel || text?.travel) && <>
            <dt className="text-xs font-semibold uppercase tracking-wide text-gray-500 sm:pt-0.5">{t('facts.travel', { city: home.city })}</dt>
            <dd className="mb-1 sm:mb-0"><TravelBadge travel={event.travel} />{text?.travel}</dd>
          </>}
        </dl>

        {text?.how && <p className="mt-2 text-sm text-gray-300">{priced(text.how, event)}</p>}
        {text?.price && <p className="mt-1 text-sm tabular-nums"><span className="font-semibold">{t('facts.price')}:</span> {priced(text.price, event)}</p>}
        {event.url && (
          <a href={event.url} target="_blank" rel="noopener noreferrer" className="mt-2 inline-flex items-center gap-1 text-sm font-medium text-blue-400 hover:text-blue-300">
            {t('facts.organizer')} <ExternalLink size={12} aria-hidden="true" />
          </a>
        )}
      </div>
    </article>
  )
}
