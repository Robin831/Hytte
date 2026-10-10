import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router'
import { Check, ExternalLink, Medal, Pencil, Plus, Search, Trash2, X } from 'lucide-react'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import {
  type HallSummary, type PersonalBest, type RaceResult, type ResultInput,
  confirmResults, createResult, deleteResult, fetchResults, flag, formatDuration, intlLocale, lookupMissingTimes,
  lookupTime, parseDuration, resultToInput, updateResult,
} from './racesApi'
import { useRaceFormat } from './useRaceFormat'

const DISTANCES: { key: PersonalBest['distance']; meters: number; tol: number }[] = [
  { key: '3k', meters: 3000, tol: 60 }, { key: '5k', meters: 5000, tol: 100 }, { key: '10k', meters: 10000, tol: 200 },
  { key: 'half', meters: 21097, tol: 300 }, { key: 'marathon', meters: 42195, tol: 400 },
]
const distanceKey = (m: number) => DISTANCES.find(d => Math.abs(d.meters - m) <= d.tol)?.key
const LINE_COLOR = '#3b82f6' // validated against the dark surface
const tooltipStyle = { backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: 8, color: '#f3f4f6', fontSize: 12 }
const hm = (s: number) => {
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return h ? `${h}:${String(m).padStart(2, '0')}` : `${m}:${String(Math.round(s % 60)).padStart(2, '0')}`
}

/** One distance's times over the years; faster is up (reversed axis). */
function ProgressionChart({ label, points }: { label: string; points: { date: string; seconds: number; race: string }[] }) {
  const { t, lang } = useRaceFormat()
  const fmt = new Intl.DateTimeFormat(intlLocale(lang), { month: 'short', year: 'numeric', timeZone: 'UTC' })
  const data = points.map(p => ({ ...p, x: Date.parse(p.date + 'T12:00:00Z') }))
  return (
    <figure className="rounded-lg border border-gray-800 p-3">
      <figcaption className="text-sm font-semibold">{label}</figcaption>
      <div className="mt-2 h-40" role="img" aria-label={t('hall.progressionOf', { distance: label })}>
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={data} margin={{ top: 8, right: 12, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#374151" vertical={false} />
            <XAxis dataKey="x" type="number" scale="time" domain={['dataMin', 'dataMax']} tickFormatter={(v: number) => fmt.format(new Date(v))}
              tick={{ fill: '#6b7280', fontSize: 10 }} tickLine={false} axisLine={{ stroke: '#374151' }} minTickGap={30} />
            <YAxis reversed dataKey="seconds" tickFormatter={hm} tick={{ fill: '#6b7280', fontSize: 10 }} width={44} tickLine={false} axisLine={false}
              domain={['dataMin - 60', 'dataMax + 60']} />
            <Tooltip
              contentStyle={tooltipStyle}
              labelFormatter={(v) => fmt.format(new Date(Number(v)))}
              formatter={(value, _n, item) => [formatDuration(Number(value)), (item?.payload as { race?: string })?.race ?? '']}
            />
            <Line type="monotone" dataKey="seconds" stroke={LINE_COLOR} strokeWidth={2} dot={{ r: 4, fill: LINE_COLOR, stroke: '#111827', strokeWidth: 2 }} activeDot={{ r: 6 }} />
          </LineChart>
        </ResponsiveContainer>
      </div>
      <p className="mt-1 text-xs text-gray-500">{t('hall.fasterUp')}</p>
    </figure>
  )
}

const emptyInput = (): ResultInput => ({
  person_name: '', race_name: '', race_date: '', distance_m: 21097, city: '', country: '', finish_seconds: null, bib: '', notes: '',
})

function ResultForm({ initial, onSaved, onCancel }: { initial?: RaceResult; onSaved: () => void; onCancel: () => void }) {
  const { t } = useRaceFormat()
  const [form, setForm] = useState<ResultInput>(() => (initial ? resultToInput(initial) : emptyInput()))
  const [time, setTime] = useState(initial?.finish_seconds ? formatDuration(initial.finish_seconds) : '')
  const [error, setError] = useState('')
  const set = <K extends keyof ResultInput>(k: K, v: ResultInput[K]) => setForm(f => ({ ...f, [k]: v }))
  const timeInvalid = time.trim() !== '' && parseDuration(time) === null
  const input = 'w-full rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm'
  const label = 'mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-400'
  const preset = distanceKey(form.distance_m) ?? 'other'

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (timeInvalid) return
    const seconds = time.trim() ? parseDuration(time) : null
    const body: ResultInput = {
      ...form,
      finish_seconds: seconds,
      time_source: seconds !== initial?.finish_seconds ? (seconds ? 'manual' : '') : form.time_source,
    }
    try {
      if (initial) await updateResult(initial.id, body)
      else await createResult({ ...body, status: 'confirmed' })
      onSaved()
    } catch (err) {
      setError(err instanceof Error && err.message.includes('already') ? t('hall.duplicate') : t('errors.save'))
    }
  }

  return (
    <form onSubmit={save} className="space-y-3 rounded-lg border border-gray-700 bg-gray-800/50 p-4" aria-label={initial ? t('hall.edit') : t('hall.add')}>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <label className={label} htmlFor="res-name">{t('hall.race')}</label>
          <input id="res-name" className={input} value={form.race_name} required onChange={e => set('race_name', e.target.value)} />
        </div>
        <div>
          <label className={label} htmlFor="res-date">{t('admin.date')}</label>
          <input id="res-date" type="date" className={input} value={form.race_date} required onChange={e => set('race_date', e.target.value)} />
        </div>
        <div>
          <label className={label} htmlFor="res-distance">{t('admin.distance')}</label>
          <div className="flex gap-2">
            <select id="res-distance" className={input} value={preset}
              onChange={e => { const d = DISTANCES.find(x => x.key === e.target.value); if (d) set('distance_m', d.meters) }}>
              {DISTANCES.map(d => <option key={d.key} value={d.key}>{t(`hall.distance.${d.key}`)}</option>)}
              <option value="other">{t('distance.other')}</option>
            </select>
            {preset === 'other' && (
              <input type="number" min={1} aria-label={t('admin.meters')} className={input} value={form.distance_m} onChange={e => set('distance_m', Number(e.target.value))} />
            )}
          </div>
        </div>
        <div>
          <label className={label} htmlFor="res-time">{t('hall.time')}</label>
          <input id="res-time" className={`${input} tabular-nums ${timeInvalid ? 'border-red-500' : ''}`} value={time} placeholder="1:45:00"
            aria-invalid={timeInvalid} onChange={e => setTime(e.target.value)} />
        </div>
        <div>
          <label className={label} htmlFor="res-person">{t('hall.runner')}</label>
          <input id="res-person" className={input} value={form.person_name} placeholder={t('hall.me')} onChange={e => set('person_name', e.target.value)} />
        </div>
        <div>
          <label className={label} htmlFor="res-city">{t('hall.city')}</label>
          <input id="res-city" className={input} value={form.city} onChange={e => set('city', e.target.value)} />
        </div>
        <div>
          <label className={label} htmlFor="res-country">{t('admin.country')}</label>
          <input id="res-country" className={input} value={form.country} maxLength={2} onChange={e => set('country', e.target.value.toUpperCase())} />
        </div>
        <div className="sm:col-span-2">
          <label className={label} htmlFor="res-notes">{t('watch.notes')}</label>
          <input id="res-notes" className={input} value={form.notes} onChange={e => set('notes', e.target.value)} />
        </div>
      </div>
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
      <div className="flex gap-2">
        <button type="submit" disabled={timeInvalid} className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer">{t('admin.save')}</button>
        <button type="button" onClick={onCancel} className="rounded-lg bg-gray-700 px-4 py-2 text-sm hover:bg-gray-600 cursor-pointer">{t('admin.cancel')}</button>
      </div>
    </form>
  )
}

const CONFIDENCE_CLASS = { high: 'bg-green-900/50 text-green-300', medium: 'bg-amber-900/40 text-amber-300', low: 'bg-gray-700 text-gray-300', '': 'bg-gray-700 text-gray-300' }

/** The hall of fame: review queue for imports, PBs, progression and every race run. */
export function HallOfFame() {
  const { t, lang } = useRaceFormat()
  const [results, setResults] = useState<RaceResult[]>([])
  const [summary, setSummary] = useState<HallSummary | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [editing, setEditing] = useState<RaceResult | 'new' | null>(null)
  const [person, setPerson] = useState('')

  const load = useCallback(async (signal?: AbortSignal) => {
    const data = await fetchResults(signal)
    if (signal?.aborted) return
    setResults(data.results)
    setSummary(data.summary)
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

  const reload = useCallback(() => { load().catch(() => setError(t('errors.load'))) }, [load, t])
  const act = async (fn: () => Promise<unknown>) => {
    setError('')
    try {
      await fn()
      reload()
    } catch {
      setError(t('errors.save'))
    }
  }

  const pending = results.filter(r => r.status === 'pending')
  const confirmed = results.filter(r => r.status === 'confirmed')
  const people = useMemo(() => [...new Set(confirmed.map(r => r.person_name).filter(Boolean))].sort(), [confirmed])
  const shown = confirmed.filter(r => r.person_name === person)
  const pbIds = new Set((summary?.pbs ?? []).map(p => p.result_id))
  const dateFmt = new Intl.DateTimeFormat(intlLocale(lang), { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' })
  const fmtDate = (r: RaceResult) => r.date_exact ? dateFmt.format(new Date(r.race_date + 'T12:00:00Z')) : r.race_date.slice(0, 4)

  const progression = useMemo(() => DISTANCES.map(d => ({
    key: d.key,
    points: confirmed
      .filter(r => r.person_name === '' && r.finish_seconds && distanceKey(r.distance_m) === d.key)
      .sort((a, b) => a.race_date.localeCompare(b.race_date))
      .map(r => ({ date: r.race_date, seconds: r.finish_seconds!, race: r.race_name })),
  })).filter(p => p.points.length >= 2), [confirmed])

  const byYear = useMemo(() => {
    const groups: { year: string; items: RaceResult[] }[] = []
    for (const r of shown) {
      const y = r.race_date.slice(0, 4)
      if (!groups.length || groups[groups.length - 1].year !== y) groups.push({ year: y, items: [] })
      groups[groups.length - 1].items.push(r)
    }
    return groups
  }, [shown])

  const findMissing = async () => {
    setError('')
    try {
      const res = await lookupMissingTimes()
      setNotice(t('hall.lookupQueued', { count: res.queued }))
    } catch (err) {
      setError(err instanceof Error && err.message.includes('budget') ? t('research.budget') : t('errors.save'))
    }
  }

  const missingTimes = confirmed.filter(r => !r.finish_seconds).length

  return (
    <div className="space-y-8">
      {error && <p role="alert" className="rounded-lg border border-red-700 bg-red-900/50 p-3 text-sm text-red-200">{error}</p>}
      {notice && <p role="status" className="rounded-lg border border-blue-800 bg-blue-900/30 p-3 text-sm text-blue-200">{notice}</p>}

      {pending.length > 0 && (
        <section aria-labelledby="hall-review" className="rounded-lg border border-amber-800 bg-amber-900/10 p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 id="hall-review" className="text-lg font-bold">{t('hall.reviewTitle', { count: pending.length })}</h2>
            {pending.some(r => r.confidence === 'high') && (
              <button type="button" onClick={() => act(() => confirmResults({ all_high: true }))}
                className="inline-flex items-center gap-1.5 rounded-lg bg-green-700 px-3 py-1.5 text-sm font-medium hover:bg-green-600 cursor-pointer">
                <Check size={14} /> {t('hall.confirmHigh')}
              </button>
            )}
          </div>
          <p className="mt-1 text-sm text-gray-400">{t('hall.reviewIntro')}</p>
          <ul className="mt-3">
            {pending.map(r => (
              <li key={r.id} className="flex flex-wrap items-start justify-between gap-3 border-t border-gray-800 py-3">
                <div className="min-w-0 text-sm">
                  <p className="font-semibold">
                    {r.race_name} <span className="font-normal text-gray-400">· {fmtDate(r)} · {t(`hall.distance.${distanceKey(r.distance_m) ?? 'other'}`, { km: (r.distance_m / 1000).toFixed(1) })}</span>
                    {r.person_name && <span className="ml-2 rounded-full bg-gray-700 px-2 py-0.5 text-xs">{r.person_name}</span>}
                  </p>
                  <p className="mt-0.5 text-gray-400">
                    <span className={`mr-2 rounded-full px-2 py-0.5 text-xs font-semibold ${CONFIDENCE_CLASS[r.confidence]}`}>{t(`hall.confidence.${r.confidence || 'low'}`)}</span>
                    {r.finish_seconds ? <span className="tabular-nums">{formatDuration(r.finish_seconds)}</span> : t('hall.noTime')}
                    {r.evidence.length > 0 && <> · {r.evidence.map(e => e.what).filter(Boolean).slice(0, 2).join('; ')}</>}
                  </p>
                  {r.notes && <p className="mt-0.5 text-xs text-gray-500">{r.notes}</p>}
                </div>
                <div className="flex gap-1">
                  <button type="button" onClick={() => act(() => confirmResults({ ids: [r.id] }))} aria-label={t('hall.confirm', { race: r.race_name })}
                    className="rounded-lg bg-green-800 px-2.5 py-1.5 text-green-100 hover:bg-green-700 cursor-pointer"><Check size={14} /></button>
                  <button type="button" onClick={() => setEditing(r)} aria-label={t('hall.edit')}
                    className="rounded-lg bg-gray-700 px-2.5 py-1.5 hover:bg-gray-600 cursor-pointer"><Pencil size={14} /></button>
                  <button type="button" onClick={() => act(() => deleteResult(r.id))} aria-label={t('hall.reject', { race: r.race_name })}
                    className="rounded-lg bg-gray-700 px-2.5 py-1.5 text-red-300 hover:bg-gray-600 cursor-pointer"><X size={14} /></button>
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}

      {editing && (
        <ResultForm initial={editing === 'new' ? undefined : editing} onCancel={() => setEditing(null)} onSaved={() => { setEditing(null); reload() }} />
      )}

      <section aria-labelledby="hall-title">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 id="hall-title" className="text-xl font-bold">{t('hall.title')}</h2>
            {summary && summary.races > 0 && (
              <p className="mt-1 text-sm text-gray-400">
                {t('hall.stats', { races: summary.races, since: summary.first_year, countries: summary.countries.length })}{' '}
                <span aria-hidden="true">{summary.countries.map(flag).join(' ')}</span>
              </p>
            )}
          </div>
          <div className="flex flex-wrap gap-2">
            {missingTimes > 0 && (
              <button type="button" onClick={findMissing} className="inline-flex items-center gap-1.5 rounded-lg bg-gray-800 px-3 py-2 text-sm hover:bg-gray-700 cursor-pointer">
                <Search size={14} /> {t('hall.findMissing', { count: missingTimes })}
              </button>
            )}
            <button type="button" onClick={() => setEditing('new')} className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-2 text-sm font-medium hover:bg-blue-500 cursor-pointer">
              <Plus size={14} /> {t('hall.add')}
            </button>
          </div>
        </div>

        {summary && summary.pbs.length > 0 && (
          <div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-5">
            {summary.pbs.map(pb => (
              <div key={pb.distance} className="rounded-lg border border-gray-800 bg-gray-800/40 p-3">
                <p className="text-xs font-semibold uppercase tracking-wide text-gray-500">{t(`hall.distance.${pb.distance}`)}</p>
                <p className="mt-1 text-xl font-bold tabular-nums">{pb.finish_seconds ? formatDuration(pb.finish_seconds) : '—'}</p>
                <p className="mt-0.5 truncate text-xs text-gray-400" title={pb.race_name}>
                  {pb.finish_seconds ? `${pb.race_name} · ${pb.race_date.slice(0, 4)}` : ''}
                </p>
                <p className="text-xs text-gray-500">{t('hall.raceCount', { count: pb.count })}</p>
              </div>
            ))}
          </div>
        )}
      </section>

      {progression.length > 0 && (
        <section aria-labelledby="hall-progression">
          <h2 id="hall-progression" className="text-lg font-bold">{t('hall.progression')}</h2>
          <div className="mt-3 grid gap-3 md:grid-cols-2">
            {progression.map(p => <ProgressionChart key={p.key} label={t(`hall.distance.${p.key}`)} points={p.points} />)}
          </div>
        </section>
      )}

      <section aria-labelledby="hall-all">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 id="hall-all" className="text-lg font-bold">{t('hall.allRaces')}</h2>
          {people.length > 0 && (
            <div className="flex flex-wrap gap-1" role="group" aria-label={t('hall.runner')}>
              {['', ...people].map(p => (
                <button key={p || 'me'} type="button" aria-pressed={person === p} onClick={() => setPerson(p)}
                  className={`rounded-full border px-3 py-1 text-sm cursor-pointer ${person === p ? 'border-blue-600 bg-blue-600' : 'border-gray-700 bg-gray-800 hover:bg-gray-700'}`}>
                  {p || t('hall.me')}
                </button>
              ))}
            </div>
          )}
        </div>
        {shown.length === 0 ? (
          <p className="mt-3 text-sm text-gray-400">{t('hall.empty')}</p>
        ) : byYear.map(g => (
          <div key={g.year} className="mt-4">
            <h3 className="text-sm font-semibold uppercase tracking-wide text-gray-400">{g.year} <span className="text-gray-600">{g.items.length}</span></h3>
            <ul>
              {g.items.map(r => (
                <li key={r.id} className="grid grid-cols-[1fr_auto] items-start gap-2 border-t border-gray-800 py-2 text-sm">
                  <div className="min-w-0">
                    <span className="font-medium">
                      {r.event_id ? <Link to={`/races/${r.event_id}`} className="hover:text-blue-300">{r.race_name}</Link> : r.race_name}
                    </span>
                    {pbIds.has(r.id) && (
                      <span className="ml-2 inline-flex items-center gap-1 rounded-full bg-amber-900/50 px-2 py-0.5 text-xs font-semibold text-amber-200">
                        <Medal size={11} aria-hidden="true" /> {t('hall.pb')}
                      </span>
                    )}
                    {r.series_key && <span className="ml-2 rounded-full bg-gray-800 px-2 py-0.5 text-xs text-gray-300">{t(`hall.seriesBadge.${r.series_key.endsWith('_half') ? 'superhalfs' : 'major'}`)}</span>}
                    <span className="block text-xs text-gray-500">
                      {fmtDate(r)} · {flag(r.country)} {r.city} · {t(`hall.distance.${distanceKey(r.distance_m) ?? 'other'}`, { km: (r.distance_m / 1000).toFixed(1) })}
                    </span>
                  </div>
                  <div className="flex items-center gap-2">
                    {r.finish_seconds ? (
                      <span className="tabular-nums font-semibold" title={t(`hall.source.${r.time_source || 'manual'}`)}>
                        {formatDuration(r.finish_seconds)}
                        {r.time_url && <a href={r.time_url} target="_blank" rel="noopener noreferrer" className="ml-1 text-blue-400" aria-label={t('hall.resultsPage')}><ExternalLink size={11} className="inline" /></a>}
                      </span>
                    ) : (
                      <button type="button" onClick={() => act(async () => { await lookupTime(r.id); setNotice(t('hall.lookupQueued', { count: 1 })) })}
                        className="inline-flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300 cursor-pointer">
                        <Search size={11} /> {t('hall.findTime')}
                      </button>
                    )}
                    <button type="button" onClick={() => setEditing(r)} aria-label={t('hall.edit')} className="text-gray-500 hover:text-gray-200 cursor-pointer"><Pencil size={13} /></button>
                    <button type="button" onClick={() => { if (window.confirm(t('hall.confirmDelete', { race: r.race_name }))) act(() => deleteResult(r.id)) }}
                      aria-label={t('hall.delete')} className="text-gray-500 hover:text-red-300 cursor-pointer"><Trash2 size={13} /></button>
                  </div>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </section>
    </div>
  )
}
