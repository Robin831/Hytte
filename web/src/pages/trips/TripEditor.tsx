import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2 } from 'lucide-react'
import { type I18n, type Lang, type Trip, type TripDoc, type TripInput, TRIP_KINDS, editI18n, tripToInput, txt, updateTrip } from './tripsApi'
import type tripsEn from '../../../public/locales/en/trips.json'
import { DateFields, FamilyPicker } from './Family'
import { useFamily } from './useFamily'

type FieldKey = keyof typeof tripsEn.field
type OptionKey = keyof typeof tripsEn.option

export type SectionKey = 'core' | 'days' | 'flights' | 'stays' | 'transport' | 'contacts' | 'documents' | 'notes' | 'followups'

type FieldType = 'i18n' | 'i18nLong' | 'text' | 'local' | 'tz' | 'date' | 'time' | 'bool' | 'select'
interface Field { key: FieldKey; type: FieldType; options?: OptionKey[]; tzKey?: string }

const SCHEMA: Record<Exclude<SectionKey, 'core' | 'days'>, Field[]> = {
  flights: [
    { key: 'airline', type: 'text' }, { key: 'flight_no', type: 'text' }, { key: 'phase', type: 'text' },
    { key: 'from', type: 'text' }, { key: 'from_name', type: 'text' }, { key: 'dep_local', type: 'local', tzKey: 'dep_tz' },
    { key: 'to', type: 'text' }, { key: 'to_name', type: 'text' }, { key: 'arr_local', type: 'local', tzKey: 'arr_tz' },
    { key: 'booking_ref', type: 'text' }, { key: 'seat', type: 'text' }, { key: 'note', type: 'i18nLong' },
  ],
  stays: [
    { key: 'name', type: 'text' }, { key: 'address', type: 'text' }, { key: 'phone', type: 'text' }, { key: 'phase', type: 'text' },
    { key: 'check_in', type: 'local', tzKey: 'tz' }, { key: 'check_out', type: 'local', tzKey: 'tz' },
    { key: 'booking_ref', type: 'text' }, { key: 'room', type: 'text' }, { key: 'price', type: 'text' }, { key: 'note', type: 'i18nLong' },
  ],
  transport: [
    { key: 'title', type: 'i18n' }, { key: 'when_local', type: 'local', tzKey: 'tz' }, { key: 'phase', type: 'text' },
    { key: 'ref', type: 'text' }, { key: 'phone', type: 'text' }, { key: 'detail', type: 'i18nLong' },
  ],
  contacts: [
    { key: 'label', type: 'i18n' }, { key: 'value', type: 'text' },
    { key: 'kind', type: 'select', options: ['phone', 'ref', 'email', 'url', 'text'] }, { key: 'urgent', type: 'bool' }, { key: 'note', type: 'i18n' },
  ],
  documents: [{ key: 'title', type: 'i18n' }, { key: 'status', type: 'select', options: ['todo', 'done'] }, { key: 'detail', type: 'i18n' }],
  notes: [{ key: 'title', type: 'i18n' }, { key: 'phase', type: 'text' }, { key: 'body', type: 'i18nLong' }],
  followups: [{ key: 'title', type: 'i18n' }, { key: 'detail', type: 'i18n' }, { key: 'done', type: 'bool' }],
}

const STEP_FIELDS: Field[] = [{ key: 'time', type: 'time' }, { key: 'label', type: 'i18n' }, { key: 'text', type: 'i18n' }, { key: 'key', type: 'bool' }]

const BLANK: Record<string, () => Record<string, unknown>> = {
  flights: () => ({ phase: '', airline: '', flight_no: '', from: '', from_name: '', to: '', to_name: '', dep_local: '', dep_tz: '', arr_local: '', arr_tz: '', booking_ref: '', seat: '', note: {} }),
  stays: () => ({ phase: '', name: '', address: '', phone: '', check_in: '', check_out: '', tz: '', booking_ref: '', room: '', price: '', note: {} }),
  transport: () => ({ phase: '', title: {}, when_local: '', tz: '', detail: {}, ref: '', phone: '' }),
  contacts: () => ({ label: {}, value: '', kind: 'phone', urgent: false, note: {} }),
  documents: () => ({ title: {}, status: 'todo', detail: {} }),
  notes: () => ({ phase: '', title: {}, body: {} }),
  followups: () => ({ title: {}, detail: {}, done: false }),
  steps: () => ({ time: '', label: {}, text: {}, key: false }),
  days: () => ({ date: '', title: {}, summary: {}, highlight: false, steps: [] }),
}

const input = 'w-full rounded border border-gray-700 bg-gray-800 px-2 py-1.5 text-sm'

function FieldInput({ field, value, row, lang, tz, onChange, onTZ }: {
  field: Field; value: unknown; row: Record<string, unknown>; lang: Lang; tz: string
  onChange: (v: unknown) => void; onTZ: (tz: string) => void
}) {
  const { t } = useTranslation('trips')
  const label = <span className="mb-0.5 block text-xs text-gray-400">{t(`field.${field.key}`)}</span>
  switch (field.type) {
    case 'i18n':
    case 'i18nLong': {
      const v = value as I18n | undefined
      const cur = v?.[lang] ?? ''
      const fallback = cur ? '' : txt(v, lang)
      const props = { value: cur, placeholder: fallback, className: input, onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => onChange(editI18n(v, lang, e.target.value)) }
      return <label className={field.type === 'i18nLong' ? 'sm:col-span-2' : ''}>{label}{field.type === 'i18nLong' ? <textarea rows={3} {...props} /> : <input {...props} />}</label>
    }
    case 'bool':
      return <label className="inline-flex items-center gap-2 pt-5 text-sm"><input type="checkbox" checked={!!value} onChange={e => onChange(e.target.checked)} /> {t(`field.${field.key}`)}</label>
    case 'select':
      return <label>{label}<select className={input} value={String(value ?? '')} onChange={e => onChange(e.target.value)}>
        {field.options!.map(o => <option key={o} value={o}>{t(`option.${o}`)}</option>)}</select></label>
    case 'local':
      return (
        <label>
          {label}
          <span className="flex gap-1">
            <input type="datetime-local" className={input} value={String(value ?? '')} onChange={e => onChange(e.target.value)} />
            <input className={`${input} w-36`} value={String(row[field.tzKey!] ?? tz)} placeholder="Europe/Oslo" aria-label={t('field.tz')}
              onChange={e => onTZ(e.target.value)} />
          </span>
        </label>
      )
    case 'date':
      return <label>{label}<input type="date" className={input} value={String(value ?? '')} onChange={e => onChange(e.target.value)} /></label>
    case 'time':
      return <label>{label}<input className={input} value={String(value ?? '')} placeholder="07:00" onChange={e => onChange(e.target.value)} /></label>
    default:
      return <label>{label}<input className={input} value={String(value ?? '')} onChange={e => onChange(e.target.value)} /></label>
  }
}

function RowFields({ fields, row, lang, tz, onRow }: { fields: Field[]; row: Record<string, unknown>; lang: Lang; tz: string; onRow: (r: Record<string, unknown>) => void }) {
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {fields.map(f => (
        <FieldInput key={f.key} field={f} value={row[f.key]} row={row} lang={lang} tz={tz}
          onChange={v => onRow({ ...row, [f.key]: v, ...(f.tzKey && !row[f.tzKey] ? { [f.tzKey]: tz } : {}) })}
          onTZ={z => onRow({ ...row, [f.tzKey!]: z })} />
      ))}
    </div>
  )
}

/** Edit one section of a trip; saves the whole trip. */
export function SectionEditor({ section, trip, lang, onCancel, onSaved }: {
  section: SectionKey; trip: Trip; lang: Lang; onCancel: () => void; onSaved: () => void
}) {
  const { t } = useTranslation('trips')
  const [form, setForm] = useState<TripInput>(() => tripToInput(trip))
  const family = useFamily()
  const [error, setError] = useState('')
  const doc = form.doc
  const setDoc = (d: Partial<TripDoc>) => setForm(f => ({ ...f, doc: { ...f.doc, ...d } }))

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      await updateTrip(trip.id, form)
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.save'))
    }
  }

  let body: React.ReactNode
  if (section === 'core') {
    body = (
      <div className="grid gap-2 sm:grid-cols-2">
        <FieldInput field={{ key: 'title', type: 'i18n' }} value={doc.title} row={{}} lang={lang} tz="" onChange={v => setDoc({ title: v as I18n })} onTZ={() => {}} />
        <label><span className="mb-0.5 block text-xs text-gray-400">{t('field.kind')}</span>
          <select className={input} value={form.kind} onChange={e => setForm({ ...form, kind: e.target.value as TripInput['kind'] })}>
            {TRIP_KINDS.map(k => <option key={k} value={k}>{t(`kind.${k}`)}</option>)}</select></label>
        <div className="sm:col-span-2">
          <DateFields start={form.start_date} end={form.end_date} flex={doc.flex}
            onChange={v => setForm(f => ({ ...f, start_date: v.start, end_date: v.end, doc: { ...f.doc, flex: v.flex } }))} />
        </div>
        <FieldInput field={{ key: 'homeTZ', type: 'text' }} value={form.home_tz} row={{}} lang={lang} tz="" onChange={v => setForm({ ...form, home_tz: String(v) })} onTZ={() => {}} />
        <FieldInput field={{ key: 'destTZ', type: 'text' }} value={form.dest_tz} row={{}} lang={lang} tz="" onChange={v => setForm({ ...form, dest_tz: String(v) })} onTZ={() => {}} />
        <label className="sm:col-span-2"><span className="mb-0.5 block text-xs text-gray-400">{t('field.route')}</span>
          <input className={input} value={doc.route.join(', ')} placeholder="BGO, AMS, CWL" onChange={e => setDoc({ route: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} /></label>
        <div className="sm:col-span-2">
          <FamilyPicker people={family.people} value={doc.travellers} onChange={travellers => setDoc({ travellers })} date={form.start_date} />
        </div>
        <FieldInput field={{ key: 'summary', type: 'i18nLong' }} value={doc.summary} row={{}} lang={lang} tz="" onChange={v => setDoc({ summary: v as I18n })} onTZ={() => {}} />
        <label className="inline-flex items-center gap-2 text-sm"><input type="checkbox" checked={form.share_family} onChange={e => setForm({ ...form, share_family: e.target.checked })} /> {t('field.shareFamily')}</label>
      </div>
    )
  } else if (section === 'days') {
    const days = doc.days as unknown as Record<string, unknown>[]
    const setDays = (d: Record<string, unknown>[]) => setDoc({ days: d as unknown as TripDoc['days'] })
    body = (
      <div className="space-y-4">
        {days.map((day, i) => (
          <fieldset key={i} className="rounded border border-gray-700 p-3">
            <RowFields fields={[{ key: 'date', type: 'date' }, { key: 'highlight', type: 'bool' }, { key: 'title', type: 'i18n' }, { key: 'summary', type: 'i18n' }]}
              row={day} lang={lang} tz={trip.dest_tz} onRow={r => setDays(days.map((x, j) => (j === i ? r : x)))} />
            <p className="mt-3 text-xs font-semibold uppercase text-gray-500">{t('field.steps')}</p>
            {(day.steps as Record<string, unknown>[]).map((step, k) => (
              <div key={k} className="mt-2 flex items-start gap-2">
                <div className="flex-1"><RowFields fields={STEP_FIELDS} row={step} lang={lang} tz={trip.dest_tz}
                  onRow={r => setDays(days.map((x, j) => (j === i ? { ...x, steps: (x.steps as Record<string, unknown>[]).map((s, m) => (m === k ? r : s)) } : x)))} /></div>
                <button type="button" aria-label={t('remove')} className="mt-5 text-gray-500 hover:text-red-300 cursor-pointer"
                  onClick={() => setDays(days.map((x, j) => (j === i ? { ...x, steps: (x.steps as unknown[]).filter((_, m) => m !== k) } : x)))}><Trash2 size={14} /></button>
              </div>
            ))}
            <div className="mt-2 flex gap-3">
              <button type="button" className="text-xs text-blue-400 cursor-pointer" onClick={() => setDays(days.map((x, j) => (j === i ? { ...x, steps: [...(x.steps as unknown[]), BLANK.steps()] } : x)))}>+ {t('addStep')}</button>
              <button type="button" className="text-xs text-gray-500 hover:text-red-300 cursor-pointer" onClick={() => setDays(days.filter((_, j) => j !== i))}>{t('removeDay')}</button>
            </div>
          </fieldset>
        ))}
        <button type="button" className="inline-flex items-center gap-1 text-sm text-blue-400 cursor-pointer" onClick={() => setDays([...days, BLANK.days()])}><Plus size={14} /> {t('addDay')}</button>
      </div>
    )
  } else {
    const rows = (doc[section] as unknown as Record<string, unknown>[]) ?? []
    const setRows = (r: Record<string, unknown>[]) => setDoc({ [section]: r } as Partial<TripDoc>)
    body = (
      <div className="space-y-3">
        {rows.map((row, i) => (
          <fieldset key={i} className="relative rounded border border-gray-700 p-3 pr-9">
            <RowFields fields={SCHEMA[section]} row={row} lang={lang} tz={trip.dest_tz} onRow={r => setRows(rows.map((x, j) => (j === i ? r : x)))} />
            <button type="button" aria-label={t('remove')} className="absolute right-2 top-2 text-gray-500 hover:text-red-300 cursor-pointer" onClick={() => setRows(rows.filter((_, j) => j !== i))}><Trash2 size={14} /></button>
          </fieldset>
        ))}
        <button type="button" className="inline-flex items-center gap-1 text-sm text-blue-400 cursor-pointer" onClick={() => setRows([...rows, BLANK[section]()])}><Plus size={14} /> {t('addRow')}</button>
      </div>
    )
  }

  return (
    <form onSubmit={save} className="space-y-3 rounded-lg border border-gray-700 bg-gray-800/50 p-4" aria-label={t('editSection', { section: t(`section.${section}`) })}>
      <h2 className="text-lg font-semibold">{t('editSection', { section: t(`section.${section}`) })}</h2>
      <p className="text-xs text-gray-500">{t('editLangHint')}</p>
      {body}
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
      <div className="flex gap-2">
        <button type="submit" className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium hover:bg-blue-500 cursor-pointer">{t('save')}</button>
        <button type="button" onClick={onCancel} className="rounded-lg bg-gray-700 px-4 py-2 text-sm hover:bg-gray-600 cursor-pointer">{t('cancel')}</button>
      </div>
    </form>
  )
}
