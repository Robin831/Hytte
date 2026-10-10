import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Check, Clock, Copy, Pencil, Plane, Plus, Trash2, Trophy } from 'lucide-react'
import {
  type Checklist, type I18n, type Lang, type Trip,
  addChecklist, addItem, countdown, dateIn, deleteChecklist, deleteItem, deleteTrip, departureInstant, fetchTrip,
  formatClock, formatDate, formatLocal, intlLocale, setItemDone, toLang, tripState, txt, zonedInstant,
} from './tripsApi'
import { SectionEditor, type SectionKey } from './TripEditor'

function useMinuteClock() {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 30_000)
    return () => clearInterval(id)
  }, [])
  return now
}

function CopyButton({ value }: { value: string }) {
  const { t } = useTranslation('trips')
  const [copied, setCopied] = useState(false)
  return (
    <button type="button" aria-label={t('copy', { value })} className="rounded p-1 text-gray-400 hover:text-gray-100 cursor-pointer"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value)
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        } catch { /* clipboard unavailable: the value is visible to select */ }
      }}>
      {copied ? <Check size={13} className="text-green-400" /> : <Copy size={13} />}
    </button>
  )
}

function Section({ id, title, onEdit, children }: { id: string; title: string; onEdit?: () => void; children: React.ReactNode }) {
  const { t } = useTranslation('trips')
  return (
    <section id={id} className="scroll-mt-28 border-t border-gray-800 pt-6" aria-labelledby={`${id}-h`}>
      <div className="mb-3 flex items-center justify-between gap-2">
        <h2 id={`${id}-h`} className="text-xl font-bold">{title}</h2>
        {onEdit && (
          <button type="button" onClick={onEdit} className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-100 cursor-pointer">
            <Pencil size={13} /> {t('edit')}
          </button>
        )}
      </div>
      {children}
    </section>
  )
}

function NowBox({ trip, now, lang }: { trip: Trip; now: Date; lang: Lang }) {
  const { t } = useTranslation('trips')
  const state = tripState(trip, now)
  const d = trip.doc
  if (state === 'before') {
    const c = countdown(departureInstant(trip).getTime() - now.getTime())
    return (
      <div className="rounded-lg border border-amber-700 bg-amber-900/20 p-4">
        <p className="text-xs font-semibold uppercase tracking-wide text-amber-300">{t('now')}</p>
        <p className="mt-1 text-lg font-semibold">{t('countdown', c)}</p>
      </div>
    )
  }
  if (state === 'after') {
    return (
      <div className="rounded-lg border border-gray-700 bg-gray-800/40 p-4">
        <p className="text-lg font-semibold">{t('tripDone')}</p>
        {d.race?.result && (
          <p className="mt-1 inline-flex items-center gap-1.5 text-green-300"><Trophy size={14} aria-hidden="true" /> {d.race.name}: {d.race.result}</p>
        )}
      </div>
    )
  }
  const today = dateIn(trip.dest_tz, now)
  const phase = d.phases.find(p => p.start <= today && today <= p.end)
  const flightsToday = d.flights.filter(f => f.dep_local && dateIn(f.dep_tz, zonedInstant(f.dep_local, f.dep_tz)) === dateIn(f.dep_tz, now))
  const stay = d.stays.find(s => s.check_in && s.check_out && zonedInstant(s.check_in, s.tz) <= now && now < zonedInstant(s.check_out, s.tz))
  const plan = d.days.find(day => day.date === today)
  return (
    <div className="rounded-lg border border-blue-700 bg-blue-900/20 p-4">
      <p className="text-xs font-semibold uppercase tracking-wide text-blue-300">{t('now')}{phase && ` · ${txt(phase.title, lang)}`}</p>
      {flightsToday.map(f => (
        <p key={f.flight_no + f.dep_local} className="mt-1 font-semibold">
          <Plane size={14} className="mr-1 inline" aria-hidden="true" />
          {f.flight_no} {f.from} {f.dep_local.slice(11)} → {f.to} {f.arr_local.slice(11)}
        </p>
      ))}
      {stay && <p className="mt-1">{t('stayingAt', { name: stay.name })}</p>}
      {plan && <p className="mt-1 text-sm text-gray-300">{txt(plan.title, lang)}{plan.summary && txt(plan.summary, lang) ? ` — ${txt(plan.summary, lang)}` : ''}</p>}
      {!flightsToday.length && !stay && !plan && <p className="mt-1 text-sm text-gray-300">{t('dayOf', { day: formatDate(today, lang) })}</p>}
    </div>
  )
}

function ChecklistBlock({ list, trip, lang, onChange }: { list: Checklist; trip: Trip; lang: Lang; onChange: () => void }) {
  const { t } = useTranslation('trips')
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const done = list.items.filter(i => i.done).length
  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try { await fn(); onChange() } finally { setBusy(false) }
  }
  return (
    <div className="rounded-lg border border-gray-800 p-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="font-semibold">{txt(list.title, lang)}</h3>
        <span className="text-xs tabular-nums text-gray-400">{done === list.items.length && done > 0 ? t('allDone') : t('progress', { done, total: list.items.length })}</span>
      </div>
      <ul className="mt-2 space-y-1">
        {list.items.map(item => (
          <li key={item.id} className="flex items-start gap-2">
            <input type="checkbox" checked={item.done} disabled={!trip.can_edit || busy} aria-label={txt(item.title, lang)}
              onChange={e => act(() => setItemDone(item.id, e.target.checked))} className="mt-1 h-4 w-4 accent-blue-500" />
            <div className="min-w-0 flex-1 text-sm">
              <span className={item.done ? 'text-gray-500 line-through' : item.urgent ? 'font-semibold text-amber-200' : ''}>{txt(item.title, lang)}</span>
              {txt(item.detail, lang) && <span className="block text-xs text-gray-500">{txt(item.detail, lang)}</span>}
              {item.done && item.done_by_name && <span className="block text-xs text-gray-500">{t('doneBy', { name: item.done_by_name })}</span>}
            </div>
            {trip.can_edit && (
              <button type="button" onClick={() => act(() => deleteItem(item.id))} aria-label={t('removeItem')} className="text-gray-600 hover:text-red-300 cursor-pointer">
                <Trash2 size={12} />
              </button>
            )}
          </li>
        ))}
      </ul>
      {trip.can_edit && (
        <form className="mt-2 flex gap-2" onSubmit={e => { e.preventDefault(); if (text.trim()) act(async () => { await addItem(list.id, { [lang]: text.trim() }); setText('') }) }}>
          <input value={text} onChange={e => setText(e.target.value)} placeholder={t('addItem')} aria-label={t('addItem')}
            className="flex-1 rounded border border-gray-700 bg-gray-800 px-2 py-1 text-sm" />
          <button type="submit" disabled={busy} className="rounded bg-gray-700 px-2 text-sm hover:bg-gray-600 cursor-pointer" aria-label={t('addItem')}><Plus size={14} /></button>
        </form>
      )}
    </div>
  )
}

export default function TripPage() {
  const { id } = useParams()
  const tripId = Number(id)
  const navigate = useNavigate()
  const { t, i18n } = useTranslation('trips')
  const lang = toLang(i18n.language)
  const now = useMinuteClock()
  const [trip, setTrip] = useState<Trip | null>(null)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<SectionKey | null>(null)
  const [newList, setNewList] = useState('')

  const load = useCallback(async (signal?: AbortSignal) => {
    const data = await fetchTrip(tripId, signal)
    if (!signal?.aborted) setTrip(data.trip)
  }, [tripId])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try { await load(controller.signal) } catch { if (!controller.signal.aborted) setError(t('errors.load')) }
    })()
    return () => controller.abort()
  }, [load, t])

  const reload = useCallback(() => { load().catch(() => setError(t('errors.load'))) }, [load, t])

  const sections = useMemo(() => {
    if (!trip) return []
    const d = trip.doc
    return ([
      ['checklists', trip.checklists.length > 0 || trip.can_edit],
      ['days', d.days.length > 0 || trip.can_edit],
      ['flights', d.flights.length > 0 || trip.can_edit],
      ['stays', d.stays.length > 0 || trip.can_edit],
      ['transport', d.transport.length > 0],
      ['contacts', d.contacts.length > 0 || trip.can_edit],
      ['documents', d.documents.length > 0],
      ['notes', d.notes.length > 0 || trip.can_edit],
      ['followups', d.followups.length > 0],
    ] as [SectionKey | 'checklists', boolean][]).filter(([, show]) => show).map(([k]) => k)
  }, [trip])

  if (!trip) {
    return (
      <div className="mx-auto max-w-3xl p-4 md:p-8">
        <Link to="/trips" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-200"><ArrowLeft size={16} /> {t('back')}</Link>
        {error ? <p role="alert" className="mt-6 text-red-300">{error}</p> : <div className="mt-10 h-8 w-8 animate-spin rounded-full border-2 border-gray-600 border-t-blue-500" aria-label={t('loading')} />}
      </div>
    )
  }

  const d = trip.doc
  const phaseTitle = (key: string) => txt(d.phases.find(p => p.key === key)?.title, lang)
  const edit = (k: SectionKey) => (trip.can_edit ? () => setEditing(k) : undefined)
  const showDest = trip.dest_tz !== trip.home_tz
  const tzCity = (tz: string) => tz.split('/').pop()?.replace('_', ' ')

  return (
    <div className="mx-auto max-w-3xl p-4 md:p-8">
      <Link to="/trips" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-200"><ArrowLeft size={16} /> {t('back')}</Link>

      <header className="mt-3">
        <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">{formatDate(trip.start_date, lang)} – {formatDate(trip.end_date, lang)} · {t(`kind.${trip.kind}`)}</p>
        <div className="flex items-start justify-between gap-3">
          <h1 className="mt-1 text-3xl font-bold leading-tight">{txt(d.title, lang)}</h1>
          {trip.can_edit && (
            <button type="button" onClick={() => setEditing('core')} className="mt-2 inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-100 cursor-pointer"><Pencil size={13} /> {t('edit')}</button>
          )}
        </div>
        {d.route.length > 0 && (
          <p className="mt-2 flex flex-wrap items-center gap-1 font-mono text-sm">
            {d.route.map((stop, i) => (
              <span key={i} className="inline-flex items-center gap-1">
                {i > 0 && <span className="text-gray-600" aria-hidden="true">→</span>}
                <span className="rounded bg-gray-800 px-2 py-0.5">{stop}</span>
              </span>
            ))}
          </p>
        )}
        {d.travellers.length > 0 && (
          <p className="mt-2 text-sm text-gray-400">{d.travellers.map(tr => tr.child ? `${tr.name} (${t('child')})` : tr.name).join(', ')}</p>
        )}
        {txt(d.summary, lang) && <p className="mt-3 text-gray-300">{txt(d.summary, lang)}</p>}
        <div className="mt-4 grid gap-3 sm:grid-cols-[1fr_auto]">
          <NowBox trip={trip} now={now} lang={lang} />
          <div className="flex gap-3 rounded-lg border border-gray-800 p-3 text-sm" aria-label={t('clocks')}>
            <div><p className="text-xs text-gray-500">{tzCity(trip.home_tz)}</p><p className="text-lg font-semibold tabular-nums">{formatClock(trip.home_tz, now, lang)}</p></div>
            {showDest && <div><p className="text-xs text-gray-500">{tzCity(trip.dest_tz)}</p><p className="text-lg font-semibold tabular-nums">{formatClock(trip.dest_tz, now, lang)}</p></div>}
          </div>
        </div>
        {trip.result_id && (
          <Link to="/races?tab=hall" className="mt-3 inline-flex items-center gap-1.5 text-sm text-blue-400 hover:text-blue-300"><Trophy size={14} /> {t('inHallOfFame')}</Link>
        )}
      </header>

      {editing && (
        <div className="mt-6">
          <SectionEditor section={editing} trip={trip} lang={lang} onCancel={() => setEditing(null)} onSaved={() => { setEditing(null); reload() }} />
        </div>
      )}

      <nav className="sticky top-14 md:top-0 z-20 -mx-4 mt-6 flex gap-1 overflow-x-auto border-b border-gray-800 bg-gray-900/95 px-4 py-2 backdrop-blur-sm md:-mx-8 md:px-8" aria-label={t('sections')}>
        {sections.map(s => (
          <a key={s} href={`#trip-${s}`} className="shrink-0 rounded-full bg-gray-800 px-3 py-1 text-sm hover:bg-gray-700">{t(`section.${s}`)}</a>
        ))}
      </nav>

      <div className="mt-6 space-y-8">
        {sections.includes('checklists') && (
          <Section id="trip-checklists" title={t('section.checklists')}>
            <div className="grid gap-3 sm:grid-cols-2">
              {trip.checklists.map(list => (
                <div key={list.id}>
                  {list.phase && <p className="mb-1 text-xs uppercase tracking-wide text-gray-500">{phaseTitle(list.phase)}</p>}
                  <ChecklistBlock list={list} trip={trip} lang={lang} onChange={reload} />
                  {trip.can_edit && list.items.length === 0 && (
                    <button type="button" className="mt-1 text-xs text-gray-500 hover:text-red-300 cursor-pointer" onClick={async () => { await deleteChecklist(list.id); reload() }}>{t('removeList')}</button>
                  )}
                </div>
              ))}
            </div>
            {trip.can_edit && (
              <form className="mt-3 flex gap-2" onSubmit={async e => { e.preventDefault(); if (newList.trim()) { await addChecklist(trip.id, { [lang]: newList.trim() } as I18n); setNewList(''); reload() } }}>
                <input value={newList} onChange={e => setNewList(e.target.value)} placeholder={t('newList')} aria-label={t('newList')}
                  className="flex-1 rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm" />
                <button type="submit" className="rounded-lg bg-gray-700 px-3 text-sm hover:bg-gray-600 cursor-pointer">{t('add')}</button>
              </form>
            )}
          </Section>
        )}

        {sections.includes('days') && (
          <Section id="trip-days" title={t('section.days')} onEdit={edit('days')}>
            <div className="space-y-3">
              {d.days.map(day => (
                <div key={day.date} className={`rounded-lg border p-3 ${day.highlight ? 'border-red-800 bg-red-950/30' : 'border-gray-800'}`}>
                  <h3 className="font-semibold">{formatDate(day.date, lang)}{txt(day.title, lang) && ` · ${txt(day.title, lang)}`}</h3>
                  {txt(day.summary, lang) && <p className="text-sm text-gray-400">{txt(day.summary, lang)}</p>}
                  <ol className="mt-2 space-y-1.5">
                    {day.steps.map((s, i) => (
                      <li key={i} className="grid grid-cols-[4.5rem_minmax(0,1fr)] gap-2 text-sm">
                        <span className="text-xs font-semibold tabular-nums text-gray-400">{s.time || txt(s.label, lang)}</span>
                        <span className={s.key ? 'font-semibold text-amber-200' : ''}>{txt(s.text, lang)}</span>
                      </li>
                    ))}
                  </ol>
                </div>
              ))}
              {d.days.length === 0 && <p className="text-sm text-gray-500">{t('emptySection')}</p>}
            </div>
          </Section>
        )}

        {sections.includes('flights') && (
          <Section id="trip-flights" title={t('section.flights')} onEdit={edit('flights')}>
            <ol className="space-y-2">
              {d.flights.map((f, i) => (
                <li key={i} className="rounded-lg border border-gray-800 p-3 text-sm">
                  <div className="flex flex-wrap items-baseline justify-between gap-2">
                    <span className="font-semibold">{f.airline} {f.flight_no}{f.phase && <span className="ml-2 text-xs font-normal text-gray-500">{phaseTitle(f.phase)}</span>}</span>
                    {f.booking_ref && <span className="font-mono text-xs text-gray-400">{f.booking_ref} <CopyButton value={f.booking_ref} /></span>}
                  </div>
                  <p className="mt-1 tabular-nums">
                    <span className="font-semibold">{f.from}</span> {formatLocal(f.dep_local, lang)} → <span className="font-semibold">{f.to}</span> {formatLocal(f.arr_local, lang)}
                  </p>
                  <p className="text-xs text-gray-500">{[f.from_name, f.to_name].filter(Boolean).join(' → ')} · {t('localTimes')}</p>
                  {txt(f.note, lang) && <p className="mt-1 text-gray-300">{txt(f.note, lang)}</p>}
                </li>
              ))}
              {d.flights.length === 0 && <p className="text-sm text-gray-500">{t('emptySection')}</p>}
            </ol>
          </Section>
        )}

        {sections.includes('stays') && (
          <Section id="trip-stays" title={t('section.stays')} onEdit={edit('stays')}>
            {d.stays.map((s, i) => (
              <div key={i} className="mb-2 rounded-lg border border-gray-800 p-3 text-sm">
                <p className="font-semibold">{s.name}</p>
                {s.address && <p className="text-gray-400">{s.address}</p>}
                <dl className="mt-2 grid grid-cols-[7rem_minmax(0,1fr)] gap-x-3 gap-y-1">
                  {s.check_in && <><dt className="text-gray-500">{t('checkIn')}</dt><dd>{formatLocal(s.check_in, lang)}</dd></>}
                  {s.check_out && <><dt className="text-gray-500">{t('checkOut')}</dt><dd>{formatLocal(s.check_out, lang)}</dd></>}
                  {s.room && <><dt className="text-gray-500">{t('room')}</dt><dd>{s.room}</dd></>}
                  {s.price && <><dt className="text-gray-500">{t('price')}</dt><dd>{s.price}</dd></>}
                  {s.booking_ref && <><dt className="text-gray-500">{t('bookingRef')}</dt><dd className="font-mono">{s.booking_ref} <CopyButton value={s.booking_ref} /></dd></>}
                  {s.phone && <><dt className="text-gray-500">{t('phone')}</dt><dd><a href={`tel:${s.phone}`} className="text-blue-400">{s.phone}</a></dd></>}
                </dl>
                {txt(s.note, lang) && <p className="mt-2 text-gray-300">{txt(s.note, lang)}</p>}
              </div>
            ))}
            {d.stays.length === 0 && <p className="text-sm text-gray-500">{t('emptySection')}</p>}
          </Section>
        )}

        {sections.includes('transport') && (
          <Section id="trip-transport" title={t('section.transport')} onEdit={edit('transport')}>
            {d.transport.map((tr, i) => (
              <div key={i} className="mb-2 rounded-lg border border-gray-800 p-3 text-sm">
                <p className="font-semibold">{txt(tr.title, lang)}{tr.when_local && <span className="ml-2 font-normal text-gray-400"><Clock size={12} className="mr-1 inline" />{formatLocal(tr.when_local, lang)}</span>}</p>
                {txt(tr.detail, lang) && <p className="mt-1 text-gray-300">{txt(tr.detail, lang)}</p>}
                <p className="mt-1 text-xs text-gray-400">
                  {tr.ref && <span className="font-mono">{tr.ref} <CopyButton value={tr.ref} /></span>}
                  {tr.phone && <a href={`tel:${tr.phone}`} className="ml-2 text-blue-400">{tr.phone}</a>}
                </p>
              </div>
            ))}
          </Section>
        )}

        {sections.includes('contacts') && (
          <Section id="trip-contacts" title={t('section.contacts')} onEdit={edit('contacts')}>
            <ul className="divide-y divide-gray-800 rounded-lg border border-gray-800">
              {d.contacts.map((c, i) => (
                <li key={i} className={`flex items-start justify-between gap-3 p-3 text-sm ${c.urgent ? 'bg-red-950/30' : ''}`}>
                  <div className="min-w-0">
                    <p className={c.urgent ? 'font-semibold text-red-200' : 'font-medium'}>{txt(c.label, lang)}</p>
                    {txt(c.note, lang) && <p className="text-xs text-gray-500">{txt(c.note, lang)}</p>}
                  </div>
                  <span className="flex shrink-0 items-center gap-1 font-mono">
                    {c.kind === 'phone' ? <a href={`tel:${c.value}`} className="text-blue-400">{c.value}</a>
                      : c.kind === 'email' ? <a href={`mailto:${c.value}`} className="text-blue-400">{c.value}</a>
                      : c.kind === 'url' ? <a href={c.value} target="_blank" rel="noopener noreferrer" className="text-blue-400">{c.value}</a>
                      : c.value}
                    <CopyButton value={c.value} />
                  </span>
                </li>
              ))}
            </ul>
            {d.contacts.length === 0 && <p className="text-sm text-gray-500">{t('emptySection')}</p>}
          </Section>
        )}

        {sections.includes('documents') && (
          <Section id="trip-documents" title={t('section.documents')} onEdit={edit('documents')}>
            <ul className="space-y-1 text-sm">
              {d.documents.map((doc, i) => (
                <li key={i}><span className={doc.status === 'done' ? 'text-green-400' : 'text-amber-300'}>{doc.status === 'done' ? '✓' : '•'}</span> {txt(doc.title, lang)}
                  {txt(doc.detail, lang) && <span className="block pl-4 text-xs text-gray-500">{txt(doc.detail, lang)}</span>}</li>
              ))}
            </ul>
          </Section>
        )}

        {sections.includes('notes') && (
          <Section id="trip-notes" title={t('section.notes')} onEdit={edit('notes')}>
            {d.notes.map((n, i) => (
              <div key={i} className="mb-3">
                <h3 className="font-semibold">{txt(n.title, lang)}{n.phase && <span className="ml-2 text-xs font-normal text-gray-500">{phaseTitle(n.phase)}</span>}</h3>
                <p className="mt-1 whitespace-pre-line text-sm text-gray-300">{txt(n.body, lang)}</p>
              </div>
            ))}
            {d.notes.length === 0 && <p className="text-sm text-gray-500">{t('emptySection')}</p>}
          </Section>
        )}

        {sections.includes('followups') && (
          <Section id="trip-followups" title={t('section.followups')} onEdit={edit('followups')}>
            <ul className="space-y-2 text-sm">
              {d.followups.map((f, i) => (
                <li key={i}>
                  <span className={f.done ? 'text-gray-500 line-through' : 'font-medium'}>{f.done ? '✓ ' : '• '}{txt(f.title, lang)}</span>
                  {txt(f.detail, lang) && <span className="block pl-4 text-xs text-gray-500">{txt(f.detail, lang)}</span>}
                </li>
              ))}
            </ul>
          </Section>
        )}
      </div>

      {trip.can_edit && (
        <button type="button" className="mt-10 text-sm text-gray-500 hover:text-red-300 cursor-pointer"
          onClick={async () => { if (window.confirm(t('confirmDelete'))) { await deleteTrip(trip.id); navigate('/trips') } }}>
          {t('deleteTrip')}
        </button>
      )}
      <p className="mt-6 text-xs text-gray-500">{t('translationNote')} · {new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: 'medium' }).format(new Date(trip.updated_at))}</p>
    </div>
  )
}
