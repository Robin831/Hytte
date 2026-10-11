import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import { CalendarCheck, CalendarX, Flag, Plane } from 'lucide-react'
import { type Candidate, type Trip, fetchCandidates, formatDate, toLang, txt } from './tripsApi'

/** A tentative trip's possible dates, each with what's already on the calendar. */
export function DateCandidates({ trip }: { trip: Trip }) {
  const { t, i18n } = useTranslation('trips')
  const lang = toLang(i18n.language)
  const [cands, setCands] = useState<Candidate[] | null>(null)
  const [error, setError] = useState('')
  const [onlyFree, setOnlyFree] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    fetchCandidates(trip.id, controller.signal)
      .then(d => setCands(d.candidates))
      .catch(() => { if (!controller.signal.aborted) setError(t('errors.load')) })
    return () => controller.abort()
  }, [trip.id, trip.updated_at, t])

  const shown = (cands ?? []).filter(c => !onlyFree || c.clashes.length === 0)
  const free = (cands ?? []).filter(c => c.clashes.length === 0).length
  const icon = { calendar: CalendarX, race: Flag, trip: Plane }

  return (
    <section className="mt-6 rounded-lg border border-amber-800 bg-amber-950/20 p-4" aria-labelledby="choose-dates-h">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="choose-dates-h" className="text-xl font-bold">{t('dates.choose')}</h2>
        {cands && cands.length > 0 && (
          <label className="inline-flex items-center gap-2 text-sm text-gray-300">
            <input type="checkbox" checked={onlyFree} onChange={e => setOnlyFree(e.target.checked)} /> {t('dates.onlyFree', { n: free })}
          </label>
        )}
      </div>
      <p className="mt-1 text-sm text-gray-400">{t('dates.chooseAbout')} {trip.can_edit && t('dates.askToPick')}</p>
      {error && <p role="alert" className="mt-2 text-sm text-red-300">{error}</p>}
      {cands && cands.length === 0 && <p className="mt-3 text-sm text-gray-400">{t('dates.none')}</p>}
      <ul className="mt-3 space-y-2">
        {shown.map(c => (
          <li key={c.start} className={`flex flex-wrap items-start justify-between gap-3 rounded-lg border p-3 ${c.clashes.length ? 'border-gray-800' : 'border-green-900 bg-green-950/20'}`}>
            <div className="min-w-0">
              <p className="font-semibold">{formatDate(c.start, lang)} – {formatDate(c.end, lang)}</p>
              {c.clashes.length === 0 ? (
                <p className="mt-0.5 inline-flex items-center gap-1 text-sm text-green-300"><CalendarCheck size={14} aria-hidden="true" /> {t('dates.free')}</p>
              ) : (
                <ul className="mt-1 space-y-0.5 text-sm text-amber-200">
                  {c.clashes.map((x, i) => {
                    const Icon = icon[x.kind]
                    const title = x.kind === 'trip' ? txt(x.title_i18n, lang) : x.title
                    return (
                      <li key={i} className="flex items-center gap-1.5">
                        <Icon size={13} aria-hidden="true" className="shrink-0" />
                        <span className="sr-only">{t(`dates.clash.${x.kind}`)}: </span>
                        {x.kind === 'race' && x.id ? <Link to={`/races/${x.id}`} className="hover:underline">{title}</Link>
                          : x.kind === 'trip' && x.id ? <Link to={`/trips/${x.id}`} className="hover:underline">{title}</Link>
                          : title}
                        {x.who && <span className="text-xs text-gray-400">· {x.who}</span>}
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}

/** Shown on a trip whose dates were picked from a window. */
export function ReopenDates({ trip }: { trip: Trip }) {
  const { t, i18n } = useTranslation('trips')
  const lang = toLang(i18n.language)
  const f = trip.doc.flex
  if (!f || !f.chosen) return null
  return <p className="mt-2 text-xs text-gray-400">{t('dates.pickedFrom', { from: formatDate(f.from, lang), to: formatDate(f.to, lang) })}</p>
}
