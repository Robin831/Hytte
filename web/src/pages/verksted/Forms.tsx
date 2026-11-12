import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Star } from 'lucide-react'
import { Dialog, DialogBody, DialogFooter, DialogHeader } from '../../components/ui/dialog'
import { statusLabel } from './labels'
import { STATUSES, type Gift, type GiftInput, type GiftStatus, type Person, type PersonInput, type Wish, type WishInput } from './api'

const input = 'w-full bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm text-white'
const label = 'block text-xs text-gray-400 mb-1'

function num(v: string): number {
  const n = Number(v.replace(',', '.').replace(/\s/g, ''))
  return Number.isFinite(n) && n >= 0 ? n : 0
}

function Footer({ onCancel, onDelete, saving, canSave }: { onCancel: () => void; onDelete?: () => void; saving: boolean; canSave: boolean }) {
  const { t } = useTranslation('verksted')
  return (
    <DialogFooter className="justify-between">
      {onDelete ? (
        <button type="button" onClick={onDelete} className="text-sm text-red-300 hover:text-red-200 cursor-pointer">{t('form.delete')}</button>
      ) : <span />}
      <div className="flex gap-2">
        <button type="button" onClick={onCancel} className="px-3 py-1.5 text-sm text-gray-300 hover:text-white cursor-pointer">{t('form.cancel')}</button>
        <button type="submit" disabled={saving || !canSave} className="px-3 py-1.5 text-sm bg-emerald-600 hover:bg-emerald-500 rounded disabled:opacity-50 cursor-pointer">
          {t('form.save')}
        </button>
      </div>
    </DialogFooter>
  )
}

// --- gift ------------------------------------------------------------------

export function GiftDialog({ gift, initial, people, ownIds, onSave, onDelete, onClose }: {
  gift: Gift | null
  initial?: GiftInput
  people: Person[]
  ownIds: Set<number>
  onSave: (in_: GiftInput) => Promise<void>
  onDelete?: () => void
  onClose: () => void
}) {
  const { t } = useTranslation('verksted')
  const titleId = useId()
  const start = gift ?? initial ?? {}
  const [title, setTitle] = useState(start.title ?? '')
  const [recipients, setRecipients] = useState<number[]>(start.recipient_ids ?? [])
  const [giver, setGiver] = useState(start.giver ?? '')
  const [price, setPrice] = useState(start.price_nok ? String(start.price_nok) : '')
  const [status, setStatus] = useState<GiftStatus>(start.status ?? 'idea')
  const [shop, setShop] = useState(start.shop ?? '')
  const [url, setUrl] = useState(start.url ?? '')
  const [notes, setNotes] = useState(start.notes ?? '')
  const [saving, setSaving] = useState(false)
  const choosable = people.filter(p => !p.archived && !ownIds.has(p.id))
  const suggestions = t('gift.fromSuggestions').split('|')

  const toggle = (id: number) => setRecipients(r => (r.includes(id) ? r.filter(x => x !== id) : [...r, id]))
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      await onSave({ title, recipient_ids: recipients, giver, price_nok: num(price), status, shop, url, notes, wish_id: start.wish_id ?? undefined })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onClose={onClose} aria-labelledby={titleId} maxWidth="max-w-lg">
      <form onSubmit={submit} className="flex flex-col min-h-0">
        <DialogHeader id={titleId} title={gift ? t('gift.edit') : t('gift.add')} onClose={onClose} />
        <DialogBody className="space-y-3">
          <div>
            <label htmlFor="g-title" className={label}>{t('gift.titleLabel')}</label>
            <input id="g-title" value={title} onChange={e => setTitle(e.target.value)} placeholder={t('gift.titlePlaceholder')} className={input} required />
          </div>
          <fieldset>
            <legend className={label}>{t('gift.for')}</legend>
            <div className="flex flex-wrap gap-1.5">
              {choosable.map(p => (
                <button
                  key={p.id}
                  type="button"
                  aria-pressed={recipients.includes(p.id)}
                  onClick={() => toggle(p.id)}
                  className={`px-2.5 py-1 rounded-full text-xs border cursor-pointer ${recipients.includes(p.id) ? 'bg-emerald-700 border-emerald-500 text-white' : 'bg-gray-800 border-gray-700 text-gray-300'}`}
                >
                  {p.name}
                </button>
              ))}
            </div>
            {recipients.length === 0 && <p className="mt-1 text-xs text-amber-300">{t('form.pickRecipient')}</p>}
          </fieldset>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label htmlFor="g-giver" className={label}>{t('gift.from')}</label>
              <input id="g-giver" value={giver} onChange={e => setGiver(e.target.value)} list="g-giver-list" className={input} />
              <datalist id="g-giver-list">{suggestions.map(s => <option key={s} value={s} />)}</datalist>
            </div>
            <div>
              <label htmlFor="g-price" className={label}>{t('gift.price')}</label>
              <input id="g-price" value={price} onChange={e => setPrice(e.target.value)} inputMode="decimal" className={input} />
            </div>
            <div>
              <label htmlFor="g-status" className={label}>{t('gift.status')}</label>
              <select id="g-status" value={status} onChange={e => setStatus(e.target.value as GiftStatus)} className={input}>
                {STATUSES.map(s => <option key={s} value={s}>{statusLabel(t, s)}</option>)}
              </select>
            </div>
            <div>
              <label htmlFor="g-shop" className={label}>{t('gift.shop')}</label>
              <input id="g-shop" value={shop} onChange={e => setShop(e.target.value)} className={input} />
            </div>
          </div>
          <div>
            <label htmlFor="g-url" className={label}>{t('gift.link')}</label>
            <input id="g-url" value={url} onChange={e => setUrl(e.target.value)} inputMode="url" className={input} />
          </div>
          <div>
            <label htmlFor="g-notes" className={label}>{t('gift.notes')}</label>
            <textarea id="g-notes" value={notes} onChange={e => setNotes(e.target.value)} rows={2} className={input} />
          </div>
        </DialogBody>
        <Footer onCancel={onClose} onDelete={onDelete} saving={saving} canSave={!!title.trim() && recipients.length > 0} />
      </form>
    </Dialog>
  )
}

// --- wish ------------------------------------------------------------------

export function WishDialog({ wish, personId, onSave, onDelete, onClose }: {
  wish: Wish | null
  personId: number
  onSave: (in_: WishInput) => Promise<void>
  onDelete?: () => void
  onClose: () => void
}) {
  const { t } = useTranslation('verksted')
  const titleId = useId()
  const [title, setTitle] = useState(wish?.title ?? '')
  const [url, setUrl] = useState(wish?.url ?? '')
  const [price, setPrice] = useState(wish?.price_nok ? String(wish.price_nok) : '')
  const [priority, setPriority] = useState(wish?.priority ?? 2)
  const [notes, setNotes] = useState(wish?.notes ?? '')
  const [saving, setSaving] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      await onSave({ person_id: personId, title, url, price_nok: num(price), priority, notes })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onClose={onClose} aria-labelledby={titleId}>
      <form onSubmit={submit} className="flex flex-col min-h-0">
        <DialogHeader id={titleId} title={wish ? t('wish.edit') : t('wish.add')} onClose={onClose} />
        <DialogBody className="space-y-3">
          <div>
            <label htmlFor="w-title" className={label}>{t('gift.titleLabel')}</label>
            <input id="w-title" value={title} onChange={e => setTitle(e.target.value)} className={input} required />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label htmlFor="w-price" className={label}>{t('gift.price')}</label>
              <input id="w-price" value={price} onChange={e => setPrice(e.target.value)} inputMode="decimal" className={input} />
            </div>
            <fieldset>
              <legend className={label}>{t('wish.priority')}</legend>
              <div className="flex gap-1">
                {[1, 2, 3].map(n => (
                  <button
                    key={n}
                    type="button"
                    onClick={() => setPriority(n)}
                    aria-pressed={priority === n}
                    aria-label={t('wish.priorityLevel', { count: n })}
                    className="p-0.5 cursor-pointer"
                  >
                    <Star size={20} className={n <= priority ? 'fill-amber-400 text-amber-400' : 'text-gray-600'} />
                  </button>
                ))}
              </div>
            </fieldset>
          </div>
          <div>
            <label htmlFor="w-url" className={label}>{t('gift.link')}</label>
            <input id="w-url" value={url} onChange={e => setUrl(e.target.value)} inputMode="url" className={input} />
          </div>
          <div>
            <label htmlFor="w-notes" className={label}>{t('gift.notes')}</label>
            <textarea id="w-notes" value={notes} onChange={e => setNotes(e.target.value)} rows={2} className={input} />
          </div>
        </DialogBody>
        <Footer onCancel={onClose} onDelete={onDelete} saving={saving} canSave={!!title.trim()} />
      </form>
    </Dialog>
  )
}

// --- person ----------------------------------------------------------------

export function PersonDialog({ person, groups, onSave, onDelete, onClose }: {
  person: Person | null
  groups: string[]
  onSave: (in_: PersonInput) => Promise<void>
  onDelete?: () => void
  onClose: () => void
}) {
  const { t } = useTranslation('verksted')
  const titleId = useId()
  const [name, setName] = useState(person?.name ?? '')
  const [group, setGroup] = useState(person?.group ?? '')
  const [birthYear, setBirthYear] = useState(person?.birth_year ? String(person.birth_year) : '')
  const [sizes, setSizes] = useState(person?.sizes ?? '')
  const [notes, setNotes] = useState(person?.notes ?? '')
  const [archived, setArchived] = useState(person?.archived ?? false)
  const [saving, setSaving] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      await onSave({ name, group, birth_year: birthYear ? Number(birthYear) : 0, sizes, notes, archived })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onClose={onClose} aria-labelledby={titleId}>
      <form onSubmit={submit} className="flex flex-col min-h-0">
        <DialogHeader id={titleId} title={person ? t('person.edit') : t('person.add')} onClose={onClose} />
        <DialogBody className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label htmlFor="p-name" className={label}>{t('form.name')}</label>
              <input id="p-name" value={name} onChange={e => setName(e.target.value)} className={input} required />
            </div>
            <div>
              <label htmlFor="p-group" className={label}>{t('form.group')}</label>
              <input id="p-group" value={group} onChange={e => setGroup(e.target.value)} list="p-group-list" className={input} />
              <datalist id="p-group-list">{groups.map(g => <option key={g} value={g} />)}</datalist>
            </div>
            <div>
              <label htmlFor="p-born" className={label}>{t('form.birthYear')}</label>
              <input id="p-born" value={birthYear} onChange={e => setBirthYear(e.target.value.replace(/\D/g, '').slice(0, 4))} inputMode="numeric" className={input} />
            </div>
          </div>
          <div>
            <label htmlFor="p-sizes" className={label}>{t('form.sizes')}</label>
            <input id="p-sizes" value={sizes} onChange={e => setSizes(e.target.value)} className={input} />
          </div>
          <div>
            <label htmlFor="p-notes" className={label}>{t('form.notes')}</label>
            <textarea id="p-notes" value={notes} onChange={e => setNotes(e.target.value)} rows={3} className={input} />
          </div>
          {person && (
            <label className="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
              <input type="checkbox" checked={archived} onChange={e => setArchived(e.target.checked)} className="accent-emerald-500" />
              {t('form.archived')}
            </label>
          )}
        </DialogBody>
        <Footer onCancel={onClose} onDelete={onDelete} saving={saving} canSave={!!name.trim()} />
      </form>
    </Dialog>
  )
}
