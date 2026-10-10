import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2, Users } from 'lucide-react'
import { useAuth } from '../../auth'
import {
  type Flex, type Person, type Traveller, addPerson, ageOn, deletePerson, toLang, updatePerson, weekdayNames,
} from './tripsApi'

/** Pick travellers from the roster; anyone else can be typed in. */
export function FamilyPicker({ people, value, onChange, date }: {
  people: Person[]
  value: Traveller[]
  onChange: (travellers: Traveller[]) => void
  date: string
}) {
  const { t } = useTranslation('trips')
  const picked = new Set(value.filter(v => v.person_id).map(v => v.person_id))
  const others = value.filter(v => !v.person_id)
  const [othersText, setOthersText] = useState(() => others.map(o => o.name).join(', '))

  const toggle = (p: Person, on: boolean) => {
    const rest = value.filter(v => v.person_id !== p.id)
    const age = ageOn(p.birth_year, date)
    onChange(on ? [...rest, { name: p.name, person_id: p.id, user_id: p.user_id, child: age !== null && age < 18 }] : rest)
  }
  const setOthers = (text: string) => {
    setOthersText(text)
    const names = text.split(',').map(s => s.trim()).filter(Boolean)
    onChange([...value.filter(v => v.person_id), ...names.map(name => ({ name, child: others.find(o => o.name === name)?.child ?? false }))])
  }

  return (
    <fieldset>
      <legend className="mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-400">{t('field.travellers')}</legend>
      <div className="flex flex-wrap gap-2">
        {people.map(p => {
          const age = ageOn(p.birth_year, date)
          return (
            <label key={p.id} className={`inline-flex cursor-pointer items-center gap-2 rounded-full border px-3 py-1.5 text-sm ${
              picked.has(p.id) ? 'border-blue-600 bg-blue-600/20' : 'border-gray-700 bg-gray-800'}`}>
              <input type="checkbox" checked={picked.has(p.id)} onChange={e => toggle(p, e.target.checked)} className="accent-blue-500" />
              {p.name}
              {age !== null && age < 18 && <span className="text-xs text-gray-400">{t('ageYears', { age })}</span>}
            </label>
          )
        })}
      </div>
      <input className="mt-2 w-full rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm" value={othersText}
        placeholder={t('field.othersHint')} aria-label={t('field.others')} onChange={e => setOthers(e.target.value)} />
    </fieldset>
  )
}

/** Fixed dates or a tentative window ("2 nights, leaving a Friday, some time in November"). */
export function DateFields({ start, end, flex, onChange }: {
  start: string
  end: string
  flex: Flex | null | undefined
  onChange: (v: { start: string; end: string; flex: Flex | null }) => void
}) {
  const { t, i18n } = useTranslation('trips')
  const days = weekdayNames(toLang(i18n.language))
  const input = 'w-full rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm'
  const label = 'mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-400'
  const tentative = !!flex && !flex.chosen
  const setFlex = (f: Partial<Flex>) => {
    const next = { ...(flex ?? { from: start, to: end, nights: 2, depart_days: [5], chosen: false }), ...f }
    onChange({ start: next.from, end: next.to, flex: next })
  }

  return (
    <div className="space-y-3">
      <div className="inline-flex rounded-lg border border-gray-700 bg-gray-800 p-0.5" role="group" aria-label={t('dates.mode')}>
        {(['fixed', 'flexible'] as const).map(m => (
          <button key={m} type="button" aria-pressed={(m === 'flexible') === tentative}
            onClick={() => m === 'fixed' ? onChange({ start, end, flex: flex ? { ...flex, chosen: true } : null }) : setFlex({ chosen: false })}
            className={`rounded-md px-3 py-1 text-sm cursor-pointer ${(m === 'flexible') === tentative ? 'bg-blue-600 text-white' : 'text-gray-300'}`}>
            {t(`dates.${m}`)}
          </button>
        ))}
      </div>
      {!tentative ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <div>
            <label className={label} htmlFor="dt-start">{t('field.start')}</label>
            <input id="dt-start" type="date" className={input} value={start} required
              onChange={e => onChange({ start: e.target.value, end: end && end >= e.target.value ? end : e.target.value, flex: flex ?? null })} />
          </div>
          <div>
            <label className={label} htmlFor="dt-end">{t('field.end')}</label>
            <input id="dt-end" type="date" className={input} value={end} min={start} onChange={e => onChange({ start, end: e.target.value, flex: flex ?? null })} />
          </div>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-3">
          <div>
            <label className={label} htmlFor="dt-from">{t('dates.from')}</label>
            <input id="dt-from" type="date" className={input} value={flex!.from} required onChange={e => setFlex({ from: e.target.value })} />
          </div>
          <div>
            <label className={label} htmlFor="dt-to">{t('dates.to')}</label>
            <input id="dt-to" type="date" className={input} value={flex!.to} min={flex!.from} required onChange={e => setFlex({ to: e.target.value })} />
          </div>
          <div>
            <label className={label} htmlFor="dt-nights">{t('dates.nights')}</label>
            <input id="dt-nights" type="number" min={1} max={30} className={input} value={flex!.nights}
              onChange={e => setFlex({ nights: Math.max(1, Number(e.target.value) || 1) })} />
          </div>
          <div className="sm:col-span-3">
            <span className={label}>{t('dates.departDays')}</span>
            <div className="flex flex-wrap gap-1.5" role="group" aria-label={t('dates.departDays')}>
              {[1, 2, 3, 4, 5, 6, 0].map(d => {
                const on = flex!.depart_days.includes(d)
                return (
                  <button key={d} type="button" aria-pressed={on}
                    onClick={() => setFlex({ depart_days: on ? flex!.depart_days.filter(x => x !== d) : [...flex!.depart_days, d] })}
                    className={`rounded-full border px-3 py-1 text-sm cursor-pointer ${on ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-700 bg-gray-800 text-gray-300'}`}>
                    {days[d]}
                  </button>
                )
              })}
            </div>
            <p className="mt-1 text-xs text-gray-500">{t('dates.departHint')}</p>
          </div>
        </div>
      )}
    </div>
  )
}

/** Manage the roster (admin): birth years for everyone, people without an account. */
export function FamilyPanel({ people, onChange }: { people: Person[]; onChange: () => void }) {
  const { t } = useTranslation('trips')
  const { user } = useAuth()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [year, setYear] = useState('')
  const [error, setError] = useState('')
  if (!user?.is_admin) return null
  const input = 'rounded-lg border border-gray-700 bg-gray-800 px-2 py-1.5 text-sm'
  const act = async (fn: () => Promise<unknown>) => {
    setError('')
    try { await fn(); onChange() } catch (err) { setError(err instanceof Error ? err.message : t('errors.save')) }
  }

  return (
    <section className="mb-6" aria-labelledby="family-h">
      <button type="button" onClick={() => setOpen(o => !o)} aria-expanded={open}
        className="inline-flex items-center gap-1.5 text-sm text-gray-300 hover:text-white cursor-pointer">
        <Users size={16} aria-hidden="true" /> <span id="family-h">{t('family.title')}</span>
        <span className="text-xs text-gray-500">({people.length})</span>
      </button>
      {open && (
        <div className="mt-3 rounded-lg border border-gray-800 p-3">
          <p className="mb-2 text-xs text-gray-500">{t('family.about')}</p>
          <ul className="divide-y divide-gray-800">
            {people.map(p => (
              <li key={p.id} className="flex flex-wrap items-center gap-2 py-2 text-sm">
                <span className="min-w-24 flex-1 font-medium">{p.name}{p.user_id && <span className="ml-2 text-xs text-gray-500">{t('family.account')}</span>}</span>
                <label className="inline-flex items-center gap-1 text-xs text-gray-400">
                  {t('family.birthYear')}
                  <input type="number" min={1900} max={new Date().getFullYear()} className={`${input} w-24`} defaultValue={p.birth_year ?? ''}
                    aria-label={`${t('family.birthYear')}: ${p.name}`}
                    onBlur={e => {
                      const v = e.target.value ? Number(e.target.value) : null
                      if (v !== p.birth_year) act(() => updatePerson(p.id, p.user_id ? '' : p.name, v))
                    }} />
                </label>
                {!p.user_id && (
                  <button type="button" aria-label={`${t('remove')}: ${p.name}`} className="text-gray-500 hover:text-red-300 cursor-pointer"
                    onClick={() => { if (window.confirm(t('family.confirmRemove', { name: p.name }))) act(() => deletePerson(p.id)) }}>
                    <Trash2 size={14} />
                  </button>
                )}
              </li>
            ))}
          </ul>
          <form className="mt-2 flex flex-wrap gap-2" onSubmit={e => {
            e.preventDefault()
            if (!name.trim()) return
            act(async () => { await addPerson(name.trim(), year ? Number(year) : null); setName(''); setYear('') })
          }}>
            <input className={`${input} flex-1`} value={name} onChange={e => setName(e.target.value)} placeholder={t('family.name')} aria-label={t('family.name')} />
            <input type="number" className={`${input} w-28`} value={year} onChange={e => setYear(e.target.value)} placeholder={t('family.birthYear')} aria-label={t('family.birthYear')} />
            <button type="submit" className="inline-flex items-center gap-1 rounded-lg bg-gray-700 px-3 text-sm hover:bg-gray-600 cursor-pointer"><Plus size={14} /> {t('family.add')}</button>
          </form>
          {error && <p role="alert" className="mt-2 text-sm text-red-300">{error}</p>}
        </div>
      )}
    </section>
  )
}
