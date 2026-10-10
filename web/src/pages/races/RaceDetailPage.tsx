import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { ArrowLeft, ExternalLink, Pencil, Plus, Trash2 } from 'lucide-react'
import { useAuth } from '../../auth'
import {
  type Change, type Deadline, type ResearchRun, type DeadlineKind, type Lang, type RaceEvent, type RaceStatus, type Watch, type WatchState,
  DEADLINE_KINDS, LANGS, STATUSES, WATCH_STATES, deadlineMoment, deleteDeadline, deleteRace, deleteWatch, fetchRace, flag, intlLocale,
  pickText, setWatch,
} from './racesApi'
import { StatusPill, TravelBadge } from './RaceParts'
import { useRaceFormat } from './useRaceFormat'
import RaceEditor, { DeadlineEditor } from './RaceEditor'
import { ResearchStatus } from './Research'
import { StrideLink } from './StrideLink'
import { type Rates, RatesContext, usePriceText } from './prices'

export default function RaceDetailPage() {
  const { id } = useParams()
  const raceId = Number(id)
  const navigate = useNavigate()
  const { user, hasFeature } = useAuth()
  const { t, lang, fuzzy } = useRaceFormat()

  const [event, setEvent] = useState<RaceEvent | null>(null)
  const [changes, setChanges] = useState<Change[]>([])
  const [watch, setWatchState] = useState<Watch | null>(null)
  const [rates, setRates] = useState<Rates>({})
  const [research, setResearch] = useState<ResearchRun | null>(null)
  const [notes, setNotes] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [savedNote, setSavedNote] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editingDeadline, setEditingDeadline] = useState<Deadline | 'new' | null>(null)

  const validId = Number.isInteger(raceId) && raceId > 0
  // Captured once per visit; used to split upcoming from past deadlines.
  const [now] = useState(() => Date.now())

  const load = useCallback(async (signal?: AbortSignal) => {
    const data = await fetchRace(raceId, signal)
    if (signal?.aborted) return
    setEvent(data.event)
    setChanges(data.changes)
    setWatchState(data.watch)
    setNotes(data.watch?.notes ?? '')
    setRates(data.rates ?? {})
    setResearch(data.research ?? null)
    setError('')
  }, [raceId])

  useEffect(() => {
    if (!validId) return
    const controller = new AbortController()
    ;(async () => {
      try {
        await load(controller.signal)
      } catch {
        if (!controller.signal.aborted) setError(t('errors.load'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [load, validId, t])

  const reload = useCallback(() => {
    load().catch(() => setError(t('errors.load')))
  }, [load, t])

  const saveWatch = async (state: WatchState | null, nextNotes = notes) => {
    setSaving(true)
    setSavedNote(false)
    try {
      if (state === null) {
        await deleteWatch(raceId)
        setWatchState(null)
        setNotes('')
      } else {
        const res = await setWatch(raceId, state, nextNotes)
        setWatchState(res.watch)
        if (state === watch?.state) setSavedNote(true)
      }
    } catch {
      setError(t('errors.save'))
    } finally {
      setSaving(false)
    }
  }

  const removeRace = async () => {
    if (!event || !window.confirm(t('admin.confirmDelete', { name: event.name }))) return
    try {
      await deleteRace(event.id)
      navigate('/races?tab=discover')
    } catch {
      setError(t('errors.save'))
    }
  }

  const removeDeadline = async (d: Deadline) => {
    if (!window.confirm(t('admin.confirmDeleteDeadline'))) return
    try {
      await deleteDeadline(d.id)
      reload()
    } catch {
      setError(t('errors.save'))
    }
  }

  if (validId && loading) {
    return (
      <div className="flex h-48 items-center justify-center" aria-label={t('loading')}>
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-600 border-t-blue-500" />
      </div>
    )
  }

  if (!event) {
    return (
      <div className="mx-auto max-w-3xl p-4 md:p-8">
        <Link to="/races" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-200"><ArrowLeft size={16} /> {t('detail.back')}</Link>
        <p role="alert" className="mt-6 text-red-300">{error || t('errors.notFound')}</p>
      </div>
    )
  }

  const text = pickText(event.texts, lang)
  const upcoming = event.deadlines.filter(d => deadlineMoment(d) >= now)
  const past = event.deadlines.filter(d => deadlineMoment(d) < now)
  const checked = event.checked_at
    ? new Intl.DateTimeFormat(intlLocale(lang), { day: 'numeric', month: 'short', year: 'numeric' }).format(new Date(event.checked_at))
    : ''

  return (
    <RatesContext.Provider value={rates}>
    <div className="mx-auto max-w-3xl p-4 md:p-8">
      <Link to="/races" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-200">
        <ArrowLeft size={16} /> {t('detail.back')}
      </Link>

      <header className="mt-3 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-2xl font-bold leading-tight">{event.name}</h1>
          <p className="mt-1 text-gray-400">
            {flag(event.country)} {text?.place} · {fuzzy(event.race_date, event.date_precision)}
          </p>
          <div className="mt-2 flex flex-wrap items-center gap-2 text-xs">
            <StatusPill status={event.status} />
            <span className="rounded-full bg-gray-800 px-2.5 py-0.5 text-gray-300">{t(`entryType.${event.entry_type}`)}</span>
            {event.series.map(s => <span key={s} className="rounded-full bg-gray-800 px-2.5 py-0.5 text-gray-300">{t(`series.${s}`)}</span>)}
          </div>
        </div>
        {user?.is_admin && !editing && (
          <div className="flex gap-2">
            <button type="button" onClick={() => setEditing(true)} className="inline-flex items-center gap-1.5 rounded-lg bg-gray-800 px-3 py-1.5 text-sm hover:bg-gray-700 cursor-pointer">
              <Pencil size={14} /> {t('admin.edit')}
            </button>
            <button type="button" onClick={removeRace} aria-label={t('admin.delete')} className="rounded-lg bg-gray-800 px-2.5 py-1.5 text-sm text-red-300 hover:bg-gray-700 cursor-pointer">
              <Trash2 size={14} />
            </button>
          </div>
        )}
      </header>

      {error && <div role="alert" className="mt-4 rounded-lg border border-red-700 bg-red-900/50 p-3 text-sm text-red-200">{error}</div>}

      {editing && (
        <div className="mt-4">
          <RaceEditor event={event} onCancel={() => setEditing(false)} onSaved={() => { setEditing(false); reload() }} />
        </div>
      )}

      {/* My status */}
      <section className="mt-6 rounded-lg border border-gray-800 bg-gray-800/40 p-4" aria-labelledby="my-status">
        <h2 id="my-status" className="text-sm font-semibold uppercase tracking-wide text-gray-400">{t('watch.title')}</h2>
        {watch ? (
          <div className="mt-2 space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              <label htmlFor="watch-state" className="sr-only">{t('watch.state')}</label>
              <select
                id="watch-state"
                value={watch.state}
                disabled={saving}
                onChange={e => saveWatch(e.target.value as WatchState)}
                className="rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm"
              >
                {WATCH_STATES.map(s => <option key={s} value={s}>{t(`watchState.${s}`)}</option>)}
              </select>
              <button type="button" onClick={() => saveWatch(null)} disabled={saving} className="text-sm text-gray-400 hover:text-red-300 cursor-pointer">
                {t('watch.untrack')}
              </button>
            </div>
            <div>
              <label htmlFor="watch-notes" className="mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-500">{t('watch.notes')}</label>
              <textarea
                id="watch-notes"
                rows={3}
                value={notes}
                maxLength={4000}
                onChange={e => { setNotes(e.target.value); setSavedNote(false) }}
                placeholder={t('watch.notesPlaceholder')}
                className="w-full rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <div className="mt-1 flex items-center gap-3">
                <button
                  type="button"
                  onClick={() => saveWatch(watch.state, notes)}
                  disabled={saving || notes === watch.notes}
                  className="rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer"
                >
                  {t('watch.saveNotes')}
                </button>
                {savedNote && <span className="text-sm text-green-400" role="status">{t('watch.saved')}</span>}
              </div>
            </div>
            <p className="text-xs text-gray-500">
              {t('watch.remindersHint')}{' '}
              <Link to="/settings#races" className="text-blue-400 hover:text-blue-300">{t('watch.remindersSettings')}</Link>
            </p>
          </div>
        ) : (
          <div className="mt-2">
            <p className="text-sm text-gray-400">{t('watch.notTracking')}</p>
            <button type="button" onClick={() => saveWatch('watching', '')} disabled={saving} className="mt-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer">
              {t('watch.track')}
            </button>
          </div>
        )}
        {hasFeature('stride') && (
          <StrideLink eventId={event.id} watch={watch} onChange={w => { setWatchState(w); setNotes(w.notes) }} />
        )}
      </section>

      {/* Facts */}
      <section className="mt-6" aria-labelledby="facts">
        <h2 id="facts" className="sr-only">{t('detail.facts')}</h2>
        <dl className="grid grid-cols-1 gap-x-4 gap-y-2 text-sm sm:grid-cols-[8rem_minmax(0,1fr)]">
          {text?.participants && <><dt className="text-xs font-semibold uppercase tracking-wide text-gray-500 sm:pt-0.5">{t('facts.participants')}</dt><dd>{text.participants}</dd></>}
          {text?.course && <><dt className="text-xs font-semibold uppercase tracking-wide text-gray-500 sm:pt-0.5">{t('facts.course')}</dt><dd>{text.course}</dd></>}
          {(event.travel || text?.travel) && <><dt className="text-xs font-semibold uppercase tracking-wide text-gray-500 sm:pt-0.5">{t('facts.travel')}</dt><dd><TravelBadge travel={event.travel} />{text?.travel}</dd></>}
          {text?.price && <><dt className="text-xs font-semibold uppercase tracking-wide text-gray-500 sm:pt-0.5">{t('facts.price')}</dt><dd className="tabular-nums"><Priced text={text.price} country={event.country} /></dd></>}
        </dl>
        {text?.how && (
          <>
            <h3 className="mt-4 text-xs font-semibold uppercase tracking-wide text-gray-500">{t('detail.howToGetIn')}</h3>
            <p className="mt-1 text-gray-200"><Priced text={text.how} country={event.country} /></p>
          </>
        )}
        <div className="mt-3 flex flex-wrap items-center gap-4 text-sm">
          {event.url && (
            <a href={event.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 font-medium text-blue-400 hover:text-blue-300">
              {t('facts.organizer')} <ExternalLink size={12} aria-hidden="true" />
            </a>
          )}
          {checked && <span className="text-gray-500">{t('detail.lastChecked', { date: checked })}</span>}
        </div>
        <ResearchStatus key={research?.id ?? 0} eventId={event.id} run={research} onFinished={reload} />
      </section>

      {/* Deadlines */}
      <section className="mt-8" aria-labelledby="deadlines">
        <div className="flex items-center justify-between">
          <h2 id="deadlines" className="text-xl font-bold">{t('detail.deadlines')}</h2>
          {user?.is_admin && editingDeadline === null && (
            <button type="button" onClick={() => setEditingDeadline('new')} className="inline-flex items-center gap-1 text-sm text-blue-400 hover:text-blue-300 cursor-pointer">
              <Plus size={14} /> {t('admin.addDeadline')}
            </button>
          )}
        </div>
        {editingDeadline === 'new' && (
          <div className="mt-3">
            <DeadlineEditor eventId={event.id} onCancel={() => setEditingDeadline(null)} onSaved={() => { setEditingDeadline(null); reload() }} />
          </div>
        )}
        {event.deadlines.length === 0 ? (
          <p className="mt-2 text-sm text-gray-500">{t('detail.noDeadlines')}</p>
        ) : (
          <ul className="mt-2">
            {[...upcoming, ...past].map(d => (
              <li key={d.id}>
                {editingDeadline !== null && editingDeadline !== 'new' && editingDeadline.id === d.id ? (
                  <div className="py-2">
                    <DeadlineEditor eventId={event.id} deadline={d} onCancel={() => setEditingDeadline(null)} onSaved={() => { setEditingDeadline(null); reload() }} />
                  </div>
                ) : (
                  <DeadlineItem
                    deadline={d}
                    past={deadlineMoment(d) < now}
                    admin={!!user?.is_admin}
                    onEdit={() => setEditingDeadline(d)}
                    onDelete={() => removeDeadline(d)}
                  />
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      {/* Change history */}
      <section className="mt-8" aria-labelledby="history">
        <h2 id="history" className="text-xl font-bold">{t('detail.history')}</h2>
        {changes.length === 0 ? (
          <p className="mt-2 text-sm text-gray-500">{t('detail.noHistory')}</p>
        ) : (
          <ul className="mt-2 text-sm">
            {changes.map(c => <ChangeItem key={c.id} change={c} />)}
          </ul>
        )}
      </section>
    </div>
    </RatesContext.Provider>
  )
}

function DeadlineItem({ deadline: d, past, admin, onEdit, onDelete }: {
  deadline: Deadline
  past: boolean
  admin: boolean
  onEdit: () => void
  onDelete: () => void
}) {
  const { t, lang, deadlineWhen, daysLeft } = useRaceFormat()
  const priced = usePriceText(lang)
  const what = pickText(d.texts, lang)?.what
  return (
    <div className={`grid grid-cols-1 gap-1 border-t border-gray-800 py-3 sm:grid-cols-[11rem_minmax(0,1fr)_auto] sm:gap-4 ${past ? 'opacity-50' : ''}`}>
      <div className="text-sm">
        <span className="font-semibold tabular-nums">{deadlineWhen(d)}</span>
        <span className="block text-xs text-gray-500">
          {daysLeft(d.due_date)}
          {d.expected && <span className="ml-2 font-semibold uppercase tracking-wide text-amber-400">{t('expected')}</span>}
        </span>
      </div>
      <div className="min-w-0 text-sm">
        <span className="font-medium text-amber-300">{t(`kind.${d.kind}`)}</span>
        {what && <p className="mt-0.5 text-gray-300">{priced(what)}</p>}
      </div>
      {admin && (
        <div className="flex gap-1">
          <button type="button" onClick={onEdit} aria-label={t('admin.editDeadline')} className="rounded p-1.5 text-gray-400 hover:text-gray-200 cursor-pointer"><Pencil size={14} /></button>
          <button type="button" onClick={onDelete} aria-label={t('admin.deleteDeadline')} className="rounded p-1.5 text-gray-400 hover:text-red-300 cursor-pointer"><Trash2 size={14} /></button>
        </div>
      )}
    </div>
  )
}

const KNOWN_FIELDS = ['created', 'name', 'race_date', 'date_precision', 'status', 'entry_type', 'travel', 'url',
  'series', 'country', 'distance_m', 'edition_year'] as const
type KnownField = typeof KNOWN_FIELDS[number]
const isKnownField = (f: string): f is KnownField => (KNOWN_FIELDS as readonly string[]).includes(f)
const isKind = (k: string): k is DeadlineKind => (DEADLINE_KINDS as string[]).includes(k)
const isLang = (l: string): l is Lang => (LANGS as string[]).includes(l)
const TEXT_FIELDS = ['place', 'participants', 'course', 'travel', 'how', 'price'] as const
const isTextField = (f: string): f is typeof TEXT_FIELDS[number] => (TEXT_FIELDS as readonly string[]).includes(f)
const isStatus = (s: string): s is RaceStatus => (STATUSES as string[]).includes(s)
const SOURCES = ['manual', 'research', 'seed'] as const
const isSource = (s: string): s is typeof SOURCES[number] => (SOURCES as readonly string[]).includes(s)

/** Human label for a change-log field: "status", "texts.en.how", "deadline.lottery_closes". */
function useFieldLabel() {
  const { t } = useRaceFormat()
  return (field: string): string => {
    if (field.startsWith('deadline.')) {
      const kind = field.slice(9)
      return `${t('detail.deadline')}: ${isKind(kind) ? t(`kind.${kind}`) : kind}`
    }
    if (field.startsWith('texts.')) {
      const [, l, f] = field.split('.')
      const name = isTextField(f) ? t(`textField.${f}`) : f
      return `${name} (${isLang(l) ? t(`lang.${l}`) : l})`
    }
    return isKnownField(field) ? t(`field.${field}`) : field
  }
}

function ChangeItem({ change: c }: { change: Change }) {
  const { t, lang } = useRaceFormat()
  const label = useFieldLabel()
  const when = new Intl.DateTimeFormat(intlLocale(lang), { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })
    .format(new Date(c.created_at))
  const show = (v: string): string => (c.field === 'status' && isStatus(v) ? t(`status.${v}`) : v)
  return (
    <li className="border-t border-gray-800 py-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="font-medium">{label(c.field)}</span>
        <span className="text-xs text-gray-500">{when} · {isSource(c.source) ? t(`source.${c.source}`) : c.source}</span>
      </div>
      {c.field !== 'created' && (
        <p className="mt-0.5 break-words text-gray-400">
          {c.old_value && <del className="text-gray-500">{show(c.old_value)}</del>}
          {c.old_value && c.new_value && ' → '}
          {c.new_value && <ins className="no-underline text-gray-200">{show(c.new_value)}</ins>}
        </p>
      )}
    </li>
  )
}

/** A text with kroner amounts added after foreign prices. Must render inside RatesContext. */
function Priced({ text, country }: { text: string; country: string }) {
  const { lang } = useRaceFormat()
  const priced = usePriceText(lang)
  return <>{priced(text, { country })}</>
}
