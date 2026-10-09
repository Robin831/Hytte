import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  type Deadline, type DeadlineInput, type DeadlineKind, type DatePrecision, type EntryType, type EventInput,
  type EventText, type Lang, type RaceEvent, type RaceStatus, type Series, type Travel,
  DEADLINE_KINDS, ENTRY_TYPES, HALF_M, LANGS, MARATHON_M, PRECISIONS, SERIES, STATUSES, TRAVELS,
  createDeadline, createRace, emptyText, eventToInput, updateDeadline, updateRace,
} from './racesApi'

const input = 'w-full rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500'
const label = 'block text-xs font-semibold uppercase tracking-wide text-gray-400 mb-1'

const TEXT_FIELDS: (keyof EventText)[] = ['place', 'participants', 'course', 'travel', 'how', 'price']

function blankInput(): EventInput {
  return {
    name: '', edition_year: 0, race_date: '', date_precision: 'day', country: '', distance_m: MARATHON_M,
    status: 'later', entry_type: 'unknown', travel: '', url: '', series: [],
    texts: Object.fromEntries(LANGS.map(l => [l, emptyText()])),
  }
}

/** Create or edit a catalog race (admin). Edits send every language, so nothing is lost. */
export default function RaceEditor({ event, onCancel, onSaved }: {
  event?: RaceEvent
  onCancel: () => void
  onSaved: (event: RaceEvent) => void
}) {
  const { t } = useTranslation('races')
  const [form, setForm] = useState<EventInput>(() => (event ? eventToInput(event) : blankInput()))
  const [lang, setLang] = useState<Lang>('nb')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const set = <K extends keyof EventInput>(key: K, value: EventInput[K]) => setForm(f => ({ ...f, [key]: value }))
  const setText = (field: keyof EventText, value: string) =>
    setForm(f => ({ ...f, texts: { ...f.texts, [lang]: { ...emptyText(), ...f.texts[lang], [field]: value } } }))

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError('')
    try {
      const res = event ? await updateRace(event.id, form) : await createRace(form)
      onSaved(res.event)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.save'))
    } finally {
      setSaving(false)
    }
  }

  const distancePreset = form.distance_m === HALF_M ? 'half' : form.distance_m === MARATHON_M ? 'marathon' : 'other'

  return (
    <form onSubmit={save} className="mb-6 space-y-4 rounded-lg border border-gray-700 bg-gray-800/50 p-4" aria-label={event ? t('admin.edit') : t('admin.newRace')}>
      <h2 className="text-lg font-semibold">{event ? t('admin.edit') : t('admin.newRace')}</h2>

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <label className={label} htmlFor="race-name">{t('admin.name')}</label>
          <input id="race-name" className={input} value={form.name} onChange={e => set('name', e.target.value)} required maxLength={200} />
        </div>
        <div>
          <label className={label} htmlFor="race-date">{t('admin.date')}</label>
          <input id="race-date" type="date" className={input} value={form.race_date} onChange={e => set('race_date', e.target.value)} required />
        </div>
        <div>
          <label className={label} htmlFor="race-precision">{t('admin.precision')}</label>
          <select id="race-precision" className={input} value={form.date_precision} onChange={e => set('date_precision', e.target.value as DatePrecision)}>
            {PRECISIONS.map(p => <option key={p} value={p}>{t(`precisionName.${p}`)}</option>)}
          </select>
        </div>
        <div>
          <label className={label} htmlFor="race-distance">{t('admin.distance')}</label>
          <div className="flex gap-2">
            <select
              id="race-distance"
              className={input}
              value={distancePreset}
              onChange={e => set('distance_m', e.target.value === 'half' ? HALF_M : e.target.value === 'marathon' ? MARATHON_M : 10000)}
            >
              <option value="half">{t('distance.half')}</option>
              <option value="marathon">{t('distance.marathon')}</option>
              <option value="other">{t('distance.other')}</option>
            </select>
            {distancePreset === 'other' && (
              <input type="number" min={1} aria-label={t('admin.meters')} className={input} value={form.distance_m} onChange={e => set('distance_m', Number(e.target.value))} />
            )}
          </div>
        </div>
        <div>
          <label className={label} htmlFor="race-country">{t('admin.country')}</label>
          <input id="race-country" className={input} value={form.country} onChange={e => set('country', e.target.value.toUpperCase())} maxLength={2} placeholder="NO" />
        </div>
        <div>
          <label className={label} htmlFor="race-status">{t('admin.status')}</label>
          <select id="race-status" className={input} value={form.status} onChange={e => set('status', e.target.value as RaceStatus)}>
            {STATUSES.map(s => <option key={s} value={s}>{t(`status.${s}`)}</option>)}
          </select>
        </div>
        <div>
          <label className={label} htmlFor="race-entry">{t('admin.entryType')}</label>
          <select id="race-entry" className={input} value={form.entry_type} onChange={e => set('entry_type', e.target.value as EntryType)}>
            {ENTRY_TYPES.map(s => <option key={s} value={s}>{t(`entryType.${s}`)}</option>)}
          </select>
        </div>
        <div>
          <label className={label} htmlFor="race-travel">{t('facts.travel')}</label>
          <select id="race-travel" className={input} value={form.travel} onChange={e => set('travel', e.target.value as Travel)}>
            {TRAVELS.map(s => <option key={s || 'unset'} value={s}>{s ? t(`travel.${s}`) : '—'}</option>)}
          </select>
        </div>
        <div>
          <label className={label} htmlFor="race-url">{t('admin.url')}</label>
          <input id="race-url" type="url" className={input} value={form.url} onChange={e => set('url', e.target.value)} placeholder="https://" />
        </div>
        <fieldset className="sm:col-span-2">
          <legend className={label}>{t('admin.series')}</legend>
          <div className="flex flex-wrap gap-4">
            {SERIES.map(s => (
              <label key={s} className="inline-flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={form.series.includes(s)}
                  onChange={e => set('series', e.target.checked ? [...form.series, s] : form.series.filter(x => x !== s) as Series[])}
                />
                {t(`series.${s}`)}
              </label>
            ))}
          </div>
        </fieldset>
      </div>

      <div>
        <div role="tablist" aria-label={t('admin.texts')} className="mb-3 flex gap-1 border-b border-gray-700">
          {LANGS.map(l => (
            <button
              key={l}
              type="button"
              role="tab"
              aria-selected={lang === l}
              onClick={() => setLang(l)}
              className={`border-b-2 px-3 py-2 text-sm font-medium cursor-pointer ${lang === l ? 'border-blue-500 text-white' : 'border-transparent text-gray-400'}`}
            >
              {t(`lang.${l}`)}
            </button>
          ))}
        </div>
        <div className="grid gap-3">
          {TEXT_FIELDS.map(f => (
            <div key={f}>
              <label className={label} htmlFor={`race-text-${f}`}>{t(`textField.${f}`)}</label>
              <textarea
                id={`race-text-${f}`}
                rows={f === 'how' ? 3 : 1}
                className={input}
                value={form.texts[lang]?.[f] ?? ''}
                onChange={e => setText(f, e.target.value)}
              />
            </div>
          ))}
        </div>
      </div>

      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
      <div className="flex gap-2">
        <button type="submit" disabled={saving} className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer">
          {t('admin.save')}
        </button>
        <button type="button" onClick={onCancel} className="rounded-lg bg-gray-700 px-4 py-2 text-sm hover:bg-gray-600 cursor-pointer">
          {t('admin.cancel')}
        </button>
      </div>
    </form>
  )
}

function deadlineToInput(d?: Deadline): DeadlineInput {
  return {
    kind: d?.kind ?? 'entry_closes',
    due_date: d?.due_date ?? '',
    date_precision: d?.date_precision ?? 'day',
    due_time: d?.due_time ?? '',
    tz: d?.tz ?? '',
    expected: d?.expected ?? false,
    texts: Object.fromEntries(LANGS.map(l => [l, { what: d?.texts[l]?.what ?? '' }])),
  }
}

/** Create or edit one deadline (admin). */
export function DeadlineEditor({ eventId, deadline, onCancel, onSaved }: {
  eventId: number
  deadline?: Deadline
  onCancel: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation('races')
  const [form, setForm] = useState<DeadlineInput>(() => deadlineToInput(deadline))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const set = <K extends keyof DeadlineInput>(key: K, value: DeadlineInput[K]) => setForm(f => ({ ...f, [key]: value }))

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError('')
    try {
      if (deadline) await updateDeadline(deadline.id, form)
      else await createDeadline(eventId, form)
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.save'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={save} className="space-y-3 rounded-lg border border-gray-700 bg-gray-800/50 p-3" aria-label={t('admin.deadline')}>
      <div className="grid gap-3 sm:grid-cols-3">
        <div>
          <label className={label} htmlFor="dl-kind">{t('admin.kind')}</label>
          <select id="dl-kind" className={input} value={form.kind} onChange={e => set('kind', e.target.value as DeadlineKind)}>
            {DEADLINE_KINDS.map(k => <option key={k} value={k}>{t(`kind.${k}`)}</option>)}
          </select>
        </div>
        <div>
          <label className={label} htmlFor="dl-date">{t('admin.date')}</label>
          <input id="dl-date" type="date" className={input} value={form.due_date} onChange={e => set('due_date', e.target.value)} required />
        </div>
        <div>
          <label className={label} htmlFor="dl-precision">{t('admin.precision')}</label>
          <select id="dl-precision" className={input} value={form.date_precision} onChange={e => set('date_precision', e.target.value as DatePrecision)}>
            {PRECISIONS.map(p => <option key={p} value={p}>{t(`precisionName.${p}`)}</option>)}
          </select>
        </div>
        <div>
          <label className={label} htmlFor="dl-time">{t('admin.time')}</label>
          <input id="dl-time" type="time" className={input} value={form.due_time} onChange={e => set('due_time', e.target.value)} />
        </div>
        <div>
          <label className={label} htmlFor="dl-tz">{t('admin.tz')}</label>
          <input id="dl-tz" className={input} value={form.tz} onChange={e => set('tz', e.target.value)} placeholder="Europe/Oslo" />
        </div>
        <label className="mt-6 inline-flex items-center gap-2 text-sm">
          <input type="checkbox" checked={form.expected} onChange={e => set('expected', e.target.checked)} />
          {t('admin.expected')}
        </label>
      </div>
      {LANGS.map(l => (
        <div key={l}>
          <label className={label} htmlFor={`dl-what-${l}`}>{t('admin.what')} ({t(`lang.${l}`)})</label>
          <input
            id={`dl-what-${l}`}
            className={input}
            value={form.texts[l]?.what ?? ''}
            onChange={e => set('texts', { ...form.texts, [l]: { what: e.target.value } })}
          />
        </div>
      ))}
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
      <div className="flex gap-2">
        <button type="submit" disabled={saving} className="rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer">
          {t('admin.save')}
        </button>
        <button type="button" onClick={onCancel} className="rounded-lg bg-gray-700 px-3 py-1.5 text-sm hover:bg-gray-600 cursor-pointer">
          {t('admin.cancel')}
        </button>
      </div>
    </form>
  )
}
