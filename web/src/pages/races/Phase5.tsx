import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router'
import { Check, Plus, Trash2, Users } from 'lucide-react'
import {
  type FamilyWatch, type Ledger, type SeriesProgress,
  deleteFinish, fetchLedger, fetchSeries, formatDuration, intlLocale, parseDuration, saveFinish,
} from './racesApi'
import { useRaceFormat } from './useRaceFormat'
import { useFamily } from './prices'

const COMMITTED = new Set(['registered', 'got_place', 'completed'])

/** Compact initials for family members on a race; names and states in the tooltip. */
export function FamilyChips({ family }: { family: FamilyWatch[] }) {
  const { t } = useRaceFormat()
  if (family.length === 0) return null
  const title = family.map(f => `${f.name}: ${t(`watchState.${f.state}`)}`).join('\n')
  return (
    <span className="inline-flex items-center gap-1" title={title} aria-label={t('family.label', { names: title.replace(/\n/g, ', ') })}>
      <Users size={12} className="text-gray-500" aria-hidden="true" />
      {family.slice(0, 3).map(f => (
        <span
          key={f.user_id}
          aria-hidden="true"
          className={`inline-flex h-5 w-5 items-center justify-center rounded-full text-[10px] font-semibold ${
            COMMITTED.has(f.state) ? 'bg-green-800 text-green-100' : 'bg-gray-700 text-gray-200'
          }`}
        >
          {f.name.slice(0, 1).toUpperCase()}
        </span>
      ))}
      {family.length > 3 && <span className="text-xs text-gray-500" aria-hidden="true">+{family.length - 3}</span>}
    </span>
  )
}

/** Chips for one race, read from FamilyContext. */
export function RaceFamilyChips({ eventId }: { eventId: number }) {
  return <FamilyChips family={useFamily(eventId)} />
}

/** Full list for the race page. */
export function FamilyList({ family }: { family: FamilyWatch[] }) {
  const { t } = useRaceFormat()
  if (family.length === 0) return null
  return (
    <section className="mt-6" aria-labelledby="race-family">
      <h2 id="race-family" className="text-sm font-semibold uppercase tracking-wide text-gray-400">{t('family.title')}</h2>
      <ul className="mt-2 flex flex-wrap gap-2">
        {family.map(f => (
          <li key={f.user_id} className="inline-flex items-center gap-2 rounded-full border border-gray-700 bg-gray-800/50 py-1 pl-1 pr-3 text-sm">
            {f.picture ? (
              <img src={f.picture} alt="" className="h-6 w-6 rounded-full" referrerPolicy="no-referrer" />
            ) : (
              <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-gray-700 text-xs font-semibold">{f.name.slice(0, 1)}</span>
            )}
            <span className="font-medium">{f.name}</span>
            <span className="text-gray-400">{t(`watchState.${f.state}`)}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}

function AddFinish({ raceKey, onSaved }: { raceKey: string; onSaved: () => void }) {
  const { t } = useRaceFormat()
  const [open, setOpen] = useState(false)
  const [year, setYear] = useState('')
  const [time, setTime] = useState('')
  const [error, setError] = useState('')
  const seconds = parseDuration(time)
  const timeInvalid = time.trim() !== '' && seconds === null
  const yearNum = Number(year)
  const yearInvalid = !(Number.isInteger(yearNum) && yearNum >= 1897 && yearNum <= new Date().getFullYear())

  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)} className="inline-flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300 cursor-pointer">
        <Plus size={12} aria-hidden="true" /> {t('series.addFinish')}
      </button>
    )
  }
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (yearInvalid || timeInvalid) return
    try {
      await saveFinish(raceKey, yearNum, seconds)
      setOpen(false)
      setYear('')
      setTime('')
      onSaved()
    } catch {
      setError(t('errors.save'))
    }
  }
  return (
    <form onSubmit={save} className="mt-1 flex flex-wrap items-end gap-2 text-sm" aria-label={t('series.addFinish')}>
      <label className="text-xs text-gray-400">
        {t('series.year')}
        <input value={year} onChange={e => setYear(e.target.value.replace(/\D/g, '').slice(0, 4))} inputMode="numeric" placeholder="2019"
          className="mt-0.5 block w-20 rounded border border-gray-700 bg-gray-800 px-2 py-1 tabular-nums" />
      </label>
      <label className="text-xs text-gray-400">
        {t('series.time')}
        <input value={time} onChange={e => setTime(e.target.value)} placeholder="3:29:59" aria-invalid={timeInvalid}
          className={`mt-0.5 block w-24 rounded border bg-gray-800 px-2 py-1 tabular-nums ${timeInvalid ? 'border-red-500' : 'border-gray-700'}`} />
      </label>
      <button type="submit" disabled={yearInvalid || timeInvalid} className="rounded bg-blue-600 px-2 py-1 text-xs font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer">
        {t('admin.save')}
      </button>
      <button type="button" onClick={() => setOpen(false)} className="rounded bg-gray-700 px-2 py-1 text-xs hover:bg-gray-600 cursor-pointer">{t('admin.cancel')}</button>
      {error && <p role="alert" className="basis-full text-xs text-red-300">{error}</p>}
    </form>
  )
}

/** Progress through the World Marathon Majors and European Marathon Classics. */
export function SeriesSection() {
  const { t } = useRaceFormat()
  const [series, setSeries] = useState<SeriesProgress[] | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(async (signal?: AbortSignal) => {
    const data = await fetchSeries(signal)
    if (!signal?.aborted) setSeries(data.series)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        await load(controller.signal)
      } catch {
        if (!controller.signal.aborted) setError(t('errors.load'))
      }
    })()
    return () => controller.abort()
  }, [load, t])

  const reload = () => { load().catch(() => setError(t('errors.load'))) }
  const remove = async (raceKey: string, year: number) => {
    if (!window.confirm(t('series.confirmRemove', { year }))) return
    try {
      await deleteFinish(raceKey, year)
      reload()
    } catch {
      setError(t('errors.save'))
    }
  }

  return (
    <section aria-labelledby="season-series">
      <h2 id="season-series" className="text-xl font-bold">{t('series.title')}</h2>
      <p className="mt-1 text-sm text-gray-400">{t('series.intro')}</p>
      {error && <p role="alert" className="mt-2 text-sm text-red-300">{error}</p>}
      <div className="mt-4 grid gap-4 md:grid-cols-2">
        {series?.map(s => {
          const pct = Math.min(100, (s.done / s.required) * 100)
          return (
            <div key={s.key} className="rounded-lg border border-gray-800 p-4">
              <div className="flex items-baseline justify-between gap-2">
                <h3 className="font-semibold">{t(`series.${s.key}`)}</h3>
                <span className="text-sm tabular-nums text-gray-300">{t('series.progress', { done: s.done, required: s.required })}</span>
              </div>
              <div
                className="mt-2 h-2 rounded-full bg-gray-700"
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={s.required}
                aria-valuenow={s.done}
                aria-label={t(`series.${s.key}`)}
              >
                <div className={`h-2 rounded-full ${s.done >= s.required ? 'bg-green-500' : 'bg-blue-500'}`} style={{ width: `${pct}%` }} />
              </div>
              {s.done >= s.required && <p className="mt-2 text-sm font-medium text-green-400">{t(`series.complete.${s.key}`)}</p>}
              <ul className="mt-3 space-y-2 text-sm">
                {s.races.map(r => {
                  const done = r.finishes.length > 0
                  return (
                    <li key={r.key}>
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <span className="inline-flex items-center gap-2">
                          <span className={`inline-flex h-5 w-5 items-center justify-center rounded-full ${done ? 'bg-green-600' : 'border border-gray-600'}`}>
                            {done && <Check size={12} aria-hidden="true" />}
                          </span>
                          <span className={done ? 'font-medium' : 'text-gray-300'}>{r.name}</span>
                          {done && <span className="sr-only">{t('series.done')}</span>}
                        </span>
                        {r.next_event_id && (
                          <Link to={`/races/${r.next_event_id}`} className="text-xs text-blue-400 hover:text-blue-300">{t('series.nextEdition')}</Link>
                        )}
                      </div>
                      {r.finishes.length > 0 && (
                        <ul className="ml-7 mt-0.5 text-xs text-gray-400">
                          {r.finishes.map(f => (
                            <li key={f.year} className="flex items-center gap-2">
                              <span className="tabular-nums">{f.year}{f.finish_seconds ? ` · ${formatDuration(f.finish_seconds)}` : ''}</span>
                              {f.source === 'auto' && <span className="text-gray-500">({t('series.fromCatalog')})</span>}
                              <button type="button" onClick={() => remove(r.key, f.year)} aria-label={t('series.remove', { year: f.year })}
                                className="text-gray-500 hover:text-red-300 cursor-pointer">
                                <Trash2 size={11} />
                              </button>
                            </li>
                          ))}
                        </ul>
                      )}
                      <div className="ml-7"><AddFinish raceKey={r.key} onSaved={reload} /></div>
                    </li>
                  )
                })}
              </ul>
            </div>
          )
        })}
      </div>
    </section>
  )
}

/** The user's lottery record: entries, outcomes, losing streaks. */
export function LedgerSection() {
  const { t, lang } = useRaceFormat()
  const [ledger, setLedger] = useState<Ledger | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    fetchLedger(controller.signal).then(setLedger).catch(() => {})
    return () => controller.abort()
  }, [])

  if (!ledger) return null
  const dateFmt = new Intl.DateTimeFormat(intlLocale(lang), { day: 'numeric', month: 'short', year: 'numeric' })
  const decided = ledger.won + ledger.lost
  return (
    <section aria-labelledby="season-ledger">
      <h2 id="season-ledger" className="text-xl font-bold">{t('ledger.title')}</h2>
      {ledger.entered === 0 ? (
        <p className="mt-2 text-sm text-gray-400">{t('ledger.empty')}</p>
      ) : (
        <>
          <p className="mt-1 text-sm text-gray-400">
            {t('ledger.summary', { entered: ledger.entered, won: ledger.won, lost: ledger.lost, pending: ledger.pending })}
            {decided > 0 && <> {t('ledger.rate', { rate: Math.round((ledger.won / decided) * 100) })}</>}
          </p>
          {ledger.streaks.map(s => (
            <p key={s.race} className="mt-1 text-sm text-amber-300">{t('ledger.streak', { race: s.race, count: s.losses })}</p>
          ))}
          <table className="mt-3 w-full text-left text-sm">
            <thead>
              <tr className="text-xs uppercase tracking-wide text-gray-500">
                <th className="py-1 font-semibold">{t('ledger.race')}</th>
                <th className="py-1 font-semibold">{t('ledger.entered')}</th>
                <th className="py-1 font-semibold">{t('ledger.result')}</th>
              </tr>
            </thead>
            <tbody>
              {ledger.entries.map(e => (
                <tr key={e.event_id} className="border-t border-gray-800">
                  <td className="py-2"><Link to={`/races/${e.event_id}`} className="hover:text-blue-300">{e.name}</Link> <span className="text-gray-500">{e.edition_year}</span></td>
                  <td className="py-2 tabular-nums text-gray-400">{e.entered_at ? dateFmt.format(new Date(e.entered_at)) : '—'}</td>
                  <td className={`py-2 font-medium ${e.outcome === 'won' ? 'text-green-400' : e.outcome === 'lost' ? 'text-gray-400' : 'text-amber-300'}`}>
                    {t(`ledger.outcome.${e.outcome}`)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </section>
  )
}
