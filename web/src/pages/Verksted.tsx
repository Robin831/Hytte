import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, ExternalLink, Gift as GiftIcon, Pencil, Plus, Star, UserPlus, Wrench } from 'lucide-react'
import { useAuth } from '../auth'
import { ConfirmDialog } from '../components/ui/dialog'
import { formatNumber } from '../utils/formatDate'
import {
  STATUSES, ageIn, api, daysToChristmasEve, isBought, shareOf,
  type Gift, type GiftInput, type GiftStatus, type Overview, type Person, type Wish,
} from './verksted/api'
import { GiftDialog, PersonDialog, WishDialog } from './verksted/Forms'
import { STATUS_STYLE, statusLabel } from './verksted/labels'

type Tab = 'people' | 'gifts' | 'shopping' | 'kids'

function kr(n: number): string {
  return formatNumber(Math.round(n), { style: 'currency', currency: 'NOK', maximumFractionDigits: 0 })
}

type DialogState =
  | { kind: 'gift'; gift: Gift | null; initial?: GiftInput }
  | { kind: 'wish'; wish: Wish | null; personId: number }
  | { kind: 'person'; person: Person | null }
  | { kind: 'confirm'; message: string; action: () => Promise<void> }
  | null

export default function Verksted() {
  const { t } = useTranslation('verksted')
  const { user } = useAuth()
  const [season, setSeason] = useState<number | null>(null)
  const [data, setData] = useState<Overview | null>(null)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Tab>('people')
  const [dialog, setDialog] = useState<DialogState>(null)
  const [open, setOpen] = useState<Set<number>>(new Set())
  const [showArchived, setShowArchived] = useState(false)
  const [statusFilter, setStatusFilter] = useState<GiftStatus | ''>('')

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const o = await api.overview(season, signal)
      setData(o)
      setError('')
    } catch (e) {
      if (!signal?.aborted) setError(e instanceof Error ? e.message : t('loadError'))
    }
  }, [season, t])

  useEffect(() => {
    const c = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates
    load(c.signal)
    return () => c.abort()
  }, [load])

  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      setDialog(null)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('saveError'))
    }
  }

  const ownIds = useMemo(() => new Set((data?.people ?? []).filter(p => p.user_id === user?.id).map(p => p.id)), [data, user])
  const people = useMemo(() => (data?.people ?? []).filter(p => showArchived || !p.archived), [data, showArchived])
  const groups = useMemo(() => [...new Set((data?.people ?? []).map(p => p.group).filter(Boolean))], [data])
  const giftsFor = useCallback((pid: number) => (data?.gifts ?? []).filter(g => g.recipient_ids.includes(pid)), [data])
  const wishesFor = useCallback((pid: number) => (data?.wishes ?? []).filter(w => w.person_id === pid), [data])
  const nameOf = useMemo(() => new Map((data?.people ?? []).map(p => [p.id, p.name])), [data])

  if (!data) {
    return (
      <div className="min-h-screen bg-gray-900 text-white p-4">
        {error ? (
          <div role="alert" className="max-w-xl mx-auto p-3 bg-red-900/40 border border-red-800 text-red-300 text-sm rounded flex justify-between">
            <span>{error}</span>
            <button type="button" onClick={() => load()} className="underline cursor-pointer">{t('retry')}</button>
          </div>
        ) : null}
      </div>
    )
  }

  const gifts = data.gifts
  const spent = gifts.reduce((a, g) => a + g.price_nok, 0)
  const bought = gifts.filter(g => isBought(g.status)).length
  const wrapped = gifts.filter(g => g.status === 'wrapped' || g.status === 'given').length
  const days = daysToChristmasEve(data.season)

  const saveGift = (g: Gift | null) => (in_: GiftInput) =>
    run(() => (g ? api.updateGift(g.id, in_) : api.addGift({ ...in_, season: data.season })))

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <div className="max-w-5xl mx-auto px-4 py-6 space-y-5 pb-24">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <Wrench size={28} className="text-emerald-400 shrink-0" aria-hidden="true" />
            <div>
              <h1 className="text-2xl font-semibold">{t('title')}</h1>
              {days >= 0 && (
                <p className="text-sm text-gray-400">{days === 0 ? t('countdownToday') : t('countdown', { count: days })}</p>
              )}
            </div>
          </div>
          <label className="text-xs text-gray-400 flex items-center gap-2">
            {t('seasonLabel')}
            <select
              value={data.season}
              onChange={e => setSeason(Number(e.target.value))}
              className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white"
            >
              {data.seasons.map(s => <option key={s} value={s}>{s}</option>)}
            </select>
          </label>
        </header>

        {error && (
          <div role="alert" className="p-3 bg-red-900/40 border border-red-800 text-red-300 text-sm rounded flex justify-between gap-2">
            <span>{error}</span>
            <button type="button" onClick={() => setError('')} className="underline cursor-pointer">×</button>
          </div>
        )}

        <Summary
          spent={spent}
          budget={data.total_budget_nok}
          giftCount={gifts.length}
          bought={bought}
          wrapped={wrapped}
          onBudget={b => run(() => api.setTotalBudget(data.season, b))}
        />

        <div role="tablist" aria-label={t('tabs.label')} className="flex gap-1 overflow-x-auto border-b border-gray-800">
          {(['people', 'gifts', 'shopping', 'kids'] as Tab[]).map(k => (
            <button
              key={k}
              role="tab"
              type="button"
              aria-selected={tab === k}
              onClick={() => setTab(k)}
              className={`px-3 py-2 text-sm whitespace-nowrap border-b-2 cursor-pointer ${tab === k ? 'border-emerald-400 text-white' : 'border-transparent text-gray-400 hover:text-white'}`}
            >
              {k === 'people' ? t('tabs.people') : k === 'gifts' ? t('tabs.gifts') : k === 'shopping' ? t('tabs.shopping') : t('tabs.kids')}
            </button>
          ))}
        </div>

        {tab === 'people' && (
          <section className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <label className="text-xs text-gray-400 flex items-center gap-2 cursor-pointer">
                <input type="checkbox" checked={showArchived} onChange={e => setShowArchived(e.target.checked)} className="accent-emerald-500" />
                {t('person.showArchived')}
              </label>
              <button type="button" onClick={() => setDialog({ kind: 'person', person: null })} className="inline-flex items-center gap-1.5 text-sm text-emerald-300 hover:text-emerald-200 cursor-pointer">
                <UserPlus size={16} aria-hidden="true" /> {t('person.add')}
              </button>
            </div>
            <ul className="space-y-2">
              {people.map(p => (
                <PersonCard
                  key={p.id}
                  person={p}
                  season={data.season}
                  own={ownIds.has(p.id)}
                  gifts={giftsFor(p.id)}
                  wishes={wishesFor(p.id)}
                  open={open.has(p.id)}
                  nameOf={nameOf}
                  onToggle={() => setOpen(s => { const n = new Set(s); if (n.has(p.id)) n.delete(p.id); else n.add(p.id); return n })}
                  onEdit={() => setDialog({ kind: 'person', person: p })}
                  onBudget={b => run(() => api.setPersonBudget(p.id, data.season, b))}
                  onAddWish={() => setDialog({ kind: 'wish', wish: null, personId: p.id })}
                  onEditWish={w => setDialog({ kind: 'wish', wish: w, personId: p.id })}
                  onReserve={w => run(() => api.reserveWish(w.id, !w.reserved))}
                  onMakeGift={w => setDialog({ kind: 'gift', gift: null, initial: { title: w.title, url: w.url, price_nok: w.price_nok, notes: w.notes, recipient_ids: [p.id], wish_id: w.id } })}
                  onAddGift={() => setDialog({ kind: 'gift', gift: null, initial: { recipient_ids: [p.id] } })}
                  onEditGift={g => setDialog({ kind: 'gift', gift: g })}
                  onAdvance={g => run(() => api.updateGift(g.id, { status: nextStatus(g.status) }))}
                />
              ))}
            </ul>
          </section>
        )}

        {tab === 'gifts' && (
          <GiftList
            gifts={gifts}
            nameOf={nameOf}
            filter={statusFilter}
            onFilter={setStatusFilter}
            onEdit={g => setDialog({ kind: 'gift', gift: g })}
            onAdvance={g => run(() => api.updateGift(g.id, { status: nextStatus(g.status) }))}
          />
        )}

        {tab === 'shopping' && <ShoppingList gifts={gifts} nameOf={nameOf} onEdit={g => setDialog({ kind: 'gift', gift: g })} />}

        {tab === 'kids' && <KidsView people={people} season={data.season} giftsFor={giftsFor} />}
      </div>

      <button
        type="button"
        onClick={() => setDialog({ kind: 'gift', gift: null })}
        className="fixed bottom-5 right-5 z-20 inline-flex items-center gap-2 px-4 py-3 rounded-full bg-emerald-600 hover:bg-emerald-500 shadow-lg cursor-pointer"
      >
        <Plus size={18} aria-hidden="true" /> {t('gift.add')}
      </button>

      {dialog?.kind === 'gift' && (
        <GiftDialog
          gift={dialog.gift}
          initial={dialog.initial}
          people={data.people}
          ownIds={ownIds}
          onSave={saveGift(dialog.gift)}
          onDelete={dialog.gift ? () => {
            const g = dialog.gift as Gift
            setDialog({ kind: 'confirm', message: t('form.confirmDeleteGift'), action: () => api.deleteGift(g.id) })
          } : undefined}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.kind === 'wish' && (
        <WishDialog
          wish={dialog.wish}
          personId={dialog.personId}
          onSave={in_ => run(() => (dialog.wish ? api.updateWish(dialog.wish.id, in_) : api.addWish(in_)))}
          onDelete={dialog.wish ? () => {
            const w = dialog.wish as Wish
            setDialog({ kind: 'confirm', message: t('form.confirmDeleteWish'), action: () => api.deleteWish(w.id) })
          } : undefined}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.kind === 'person' && (
        <PersonDialog
          person={dialog.person}
          groups={groups}
          onSave={in_ => run(() => (dialog.person ? api.updatePerson(dialog.person.id, in_) : api.addPerson(in_)))}
          onDelete={dialog.person ? () => {
            const p = dialog.person as Person
            setDialog({ kind: 'confirm', message: t('form.confirmDeletePerson', { name: p.name }), action: () => api.deletePerson(p.id) })
          } : undefined}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.kind === 'confirm' && (
        <ConfirmDialog
          open
          title={dialog.message}
          onClose={() => setDialog(null)}
          onConfirm={() => run(dialog.action)}
        />
      )}
    </div>
  )
}

function nextStatus(s: GiftStatus): GiftStatus {
  const i = STATUSES.indexOf(s)
  return STATUSES[Math.min(i + 1, STATUSES.length - 1)]
}

function Summary({ spent, budget, giftCount, bought, wrapped, onBudget }: {
  spent: number
  budget: number
  giftCount: number
  bought: number
  wrapped: number
  onBudget: (b: number) => void
}) {
  const { t } = useTranslation('verksted')
  const pct = budget > 0 ? Math.min(100, (spent / budget) * 100) : 0
  return (
    <section className="p-4 rounded-lg border border-gray-800 bg-gray-800/30 space-y-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="text-lg font-semibold">
          {t('summary.spent', { spent: kr(spent) })}{' '}
          <span className="text-sm font-normal text-gray-400">{budget > 0 ? t('summary.ofBudget', { budget: kr(budget) }) : t('summary.noBudget')}</span>
        </p>
        <label className="text-xs text-gray-400 flex items-center gap-2">
          {t('summary.setBudget')}
          <input
            type="number"
            min={0}
            defaultValue={budget || ''}
            key={budget}
            onBlur={e => { const v = Number(e.target.value) || 0; if (v !== budget) onBudget(v) }}
            className="w-24 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white"
          />
        </label>
      </div>
      {budget > 0 && (
        <div className="h-2 rounded-full bg-gray-700" role="progressbar" aria-valuemin={0} aria-valuemax={budget} aria-valuenow={Math.round(spent)}>
          <div className={`h-2 rounded-full ${spent > budget ? 'bg-red-500' : 'bg-emerald-500'}`} style={{ width: `${pct}%` }} />
        </div>
      )}
      <p className="text-xs text-gray-400">
        {t('summary.gifts', { count: giftCount })} · {t('summary.bought', { bought })} · {t('summary.wrapped', { wrapped })}
      </p>
    </section>
  )
}

function StatusChip({ status }: { status: GiftStatus }) {
  const { t } = useTranslation('verksted')
  return <span className={`px-2 py-0.5 rounded-full text-[11px] ${STATUS_STYLE[status]}`}>{statusLabel(t, status)}</span>
}

function GiftRow({ gift, nameOf, showFor, onEdit, onAdvance }: {
  gift: Gift
  nameOf: Map<number, string>
  showFor: boolean
  onEdit: () => void
  onAdvance?: () => void
}) {
  const { t } = useTranslation('verksted')
  const next = nextStatus(gift.status)
  return (
    <li className="flex items-center gap-2 py-1.5 text-sm">
      <StatusChip status={gift.status} />
      <button type="button" onClick={onEdit} className="min-w-0 flex-1 text-left truncate hover:text-emerald-300 cursor-pointer">
        {gift.title}
        {showFor && <span className="text-gray-400"> → {gift.recipient_ids.map(id => nameOf.get(id)).join(', ')}</span>}
        {gift.giver && <span className="text-xs text-gray-500"> · {t('gift.from')} {gift.giver}</span>}
      </button>
      {gift.url && (
        <a href={gift.url} target="_blank" rel="noopener noreferrer" aria-label={t('gift.link')} className="text-gray-500 hover:text-white">
          <ExternalLink size={13} />
        </a>
      )}
      <span className="tabular-nums text-gray-300 shrink-0">{gift.price_nok > 0 ? kr(gift.price_nok) : ''}</span>
      {onAdvance && gift.status !== 'given' && (
        <button
          type="button"
          onClick={onAdvance}
          aria-label={t('gift.next', { status: statusLabel(t, next) })}
          title={t('gift.next', { status: statusLabel(t, next) })}
          className="shrink-0 text-gray-500 hover:text-emerald-300 cursor-pointer"
        >
          <ChevronRight size={16} />
        </button>
      )}
    </li>
  )
}

function PersonCard(props: {
  person: Person
  season: number
  own: boolean
  gifts: Gift[]
  wishes: Wish[]
  open: boolean
  nameOf: Map<number, string>
  onToggle: () => void
  onEdit: () => void
  onBudget: (b: number) => void
  onAddWish: () => void
  onEditWish: (w: Wish) => void
  onReserve: (w: Wish) => void
  onMakeGift: (w: Wish) => void
  onAddGift: () => void
  onEditGift: (g: Gift) => void
  onAdvance: (g: Gift) => void
}) {
  const { t } = useTranslation('verksted')
  const { person: p, gifts, wishes } = props
  const spent = gifts.reduce((a, g) => a + shareOf(g), 0)
  const age = ageIn(p, props.season)
  const pct = p.budget_nok > 0 ? Math.min(100, (spent / p.budget_nok) * 100) : 0
  return (
    <li className={`rounded-lg border ${p.archived ? 'border-gray-800 opacity-60' : 'border-gray-800'} bg-gray-800/20`}>
      <button type="button" onClick={props.onToggle} aria-expanded={props.open} className="w-full flex items-center gap-3 p-3 text-left cursor-pointer">
        {props.open ? <ChevronDown size={16} className="text-gray-500" /> : <ChevronRight size={16} className="text-gray-500" />}
        <div className="min-w-0 flex-1">
          <p className="font-medium truncate">
            {p.name}
            {age !== null && age < 30 && <span className="ml-2 text-xs text-gray-500">{t('person.age', { age })}</span>}
            {p.archived && <span className="ml-2 text-xs text-gray-500">{t('person.archived')}</span>}
          </p>
          <p className="text-xs text-gray-400">
            {p.group && <span>{p.group} · </span>}
            {props.own ? t('person.wishes', { count: wishes.length }) : <>{t('person.gifts', { count: gifts.length })} · {t('person.wishes', { count: wishes.length })}</>}
          </p>
        </div>
        {!props.own && (
          <div className="w-28 sm:w-40 shrink-0 text-right">
            <p className="text-xs tabular-nums text-gray-300">
              {p.budget_nok > 0 ? t('person.budgetUsed', { spent: kr(spent), budget: kr(p.budget_nok) }) : t('person.noBudget', { spent: kr(spent) })}
            </p>
            {p.budget_nok > 0 && (
              <div className="mt-1 h-1.5 rounded-full bg-gray-700">
                <div className={`h-1.5 rounded-full ${spent > p.budget_nok ? 'bg-red-500' : 'bg-emerald-500'}`} style={{ width: `${pct}%` }} />
              </div>
            )}
          </div>
        )}
      </button>
      {props.open && (
        <div className="px-3 pb-3 space-y-4 border-t border-gray-800 pt-3">
          <div className="flex flex-wrap items-center gap-3 text-xs text-gray-400">
            {!props.own && (
              <label className="flex items-center gap-2">
                {t('person.budget')}
                <input
                  type="number"
                  min={0}
                  defaultValue={p.budget_nok || ''}
                  key={p.budget_nok}
                  onBlur={e => { const v = Number(e.target.value) || 0; if (v !== p.budget_nok) props.onBudget(v) }}
                  className="w-24 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white"
                />
              </label>
            )}
            <button type="button" onClick={props.onEdit} className="inline-flex items-center gap-1 hover:text-white cursor-pointer">
              <Pencil size={12} aria-hidden="true" /> {t('person.edit')}
            </button>
          </div>
          {(p.sizes || p.notes) && (
            <dl className="text-sm space-y-1">
              {p.sizes && <div><dt className="inline text-gray-400">{t('person.sizes')}: </dt><dd className="inline">{p.sizes}</dd></div>}
              {p.notes && <div><dt className="inline text-gray-400">{t('person.notes')}: </dt><dd className="inline whitespace-pre-line">{p.notes}</dd></div>}
            </dl>
          )}

          <div>
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold">{t('person.wishesTitle')}</h3>
              <button type="button" onClick={props.onAddWish} className="inline-flex items-center gap-1 text-xs text-emerald-300 hover:text-emerald-200 cursor-pointer">
                <Plus size={12} aria-hidden="true" /> {t('wish.add')}
              </button>
            </div>
            {wishes.length === 0 ? <p className="text-xs text-gray-500 mt-1">{t('person.noWishes')}</p> : (
              <ul className="mt-1 divide-y divide-gray-800">
                {wishes.map(w => (
                  <li key={w.id} className="flex flex-wrap items-center gap-2 py-1.5 text-sm">
                    <span className="inline-flex" aria-label={t('wish.priorityLevel', { count: w.priority })}>
                      {[1, 2, 3].map(n => <Star key={n} size={11} className={n <= w.priority ? 'fill-amber-400 text-amber-400' : 'text-gray-700'} />)}
                    </span>
                    <button type="button" onClick={() => props.onEditWish(w)} className={`min-w-0 flex-1 text-left truncate hover:text-emerald-300 cursor-pointer ${w.reserved ? 'text-gray-400' : ''}`}>
                      {w.title}
                    </button>
                    {w.url && <a href={w.url} target="_blank" rel="noopener noreferrer" aria-label={t('gift.link')} className="text-gray-500 hover:text-white"><ExternalLink size={13} /></a>}
                    {w.price_nok > 0 && <span className="tabular-nums text-gray-400 text-xs">{kr(w.price_nok)}</span>}
                    {!props.own && (w.gift_id ? (
                      <span className="text-xs text-emerald-300">{t('wish.hasGift')}</span>
                    ) : (
                      <>
                        <button type="button" onClick={() => props.onReserve(w)} className={`text-xs cursor-pointer ${w.reserved ? 'text-amber-300' : 'text-gray-400 hover:text-white'}`}>
                          {w.reserved ? t('wish.reserved') : t('wish.reserve')}
                        </button>
                        <button type="button" onClick={() => props.onMakeGift(w)} className="inline-flex items-center gap-1 text-xs text-emerald-300 hover:text-emerald-200 cursor-pointer">
                          <GiftIcon size={12} aria-hidden="true" /> {t('wish.makeGift')}
                        </button>
                      </>
                    ))}
                  </li>
                ))}
              </ul>
            )}
          </div>

          {!props.own && (
            <div>
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-semibold">{t('person.giftsTitle')}</h3>
                <button type="button" onClick={props.onAddGift} className="inline-flex items-center gap-1 text-xs text-emerald-300 hover:text-emerald-200 cursor-pointer">
                  <Plus size={12} aria-hidden="true" /> {t('gift.add')}
                </button>
              </div>
              {gifts.length === 0 ? <p className="text-xs text-gray-500 mt-1">{t('person.noGifts')}</p> : (
                <ul className="mt-1 divide-y divide-gray-800">
                  {gifts.map(g => (
                    <GiftRow key={g.id} gift={g} nameOf={props.nameOf} showFor={g.recipient_ids.length > 1} onEdit={() => props.onEditGift(g)} onAdvance={() => props.onAdvance(g)} />
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      )}
    </li>
  )
}

function GiftList({ gifts, nameOf, filter, onFilter, onEdit, onAdvance }: {
  gifts: Gift[]
  nameOf: Map<number, string>
  filter: GiftStatus | ''
  onFilter: (s: GiftStatus | '') => void
  onEdit: (g: Gift) => void
  onAdvance: (g: Gift) => void
}) {
  const { t } = useTranslation('verksted')
  const shown = filter ? gifts.filter(g => g.status === filter) : gifts
  return (
    <section className="space-y-3">
      <select value={filter} onChange={e => onFilter(e.target.value as GiftStatus | '')} className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm" aria-label={t('gift.status')}>
        <option value="">{t('gift.filterAll')}</option>
        {STATUSES.map(s => <option key={s} value={s}>{statusLabel(t, s)}</option>)}
      </select>
      {shown.length === 0 ? <p className="text-sm text-gray-400">{t('gift.none')}</p> : (
        <ul className="divide-y divide-gray-800 border border-gray-800 rounded-lg px-3">
          {[...shown].sort((a, b) => STATUSES.indexOf(a.status) - STATUSES.indexOf(b.status)).map(g => (
            <GiftRow key={g.id} gift={g} nameOf={nameOf} showFor onEdit={() => onEdit(g)} onAdvance={() => onAdvance(g)} />
          ))}
        </ul>
      )}
    </section>
  )
}

function ShoppingList({ gifts, nameOf, onEdit }: { gifts: Gift[]; nameOf: Map<number, string>; onEdit: (g: Gift) => void }) {
  const { t } = useTranslation('verksted')
  const todo = gifts.filter(g => g.status === 'idea' || g.status === 'planned')
  const byShop = new Map<string, Gift[]>()
  for (const g of todo) {
    const key = g.shop || ''
    byShop.set(key, [...(byShop.get(key) ?? []), g])
  }
  const total = todo.reduce((a, g) => a + g.price_nok, 0)
  return (
    <section className="space-y-3">
      <p className="text-xs text-gray-400">{t('shopping.intro')}</p>
      {todo.length === 0 ? <p className="text-sm text-gray-400">{t('shopping.empty')}</p> : (
        <>
          {[...byShop.entries()].sort(([a], [b]) => (a === '' ? 1 : b === '' ? -1 : a.localeCompare(b))).map(([shop, list]) => (
            <div key={shop} className="border border-gray-800 rounded-lg px-3 py-2">
              <h3 className="text-sm font-semibold">{shop || t('gift.noShop')}</h3>
              <ul className="divide-y divide-gray-800">
                {list.map(g => <GiftRow key={g.id} gift={g} nameOf={nameOf} showFor onEdit={() => onEdit(g)} />)}
              </ul>
            </div>
          ))}
          <p className="font-semibold">{t('shopping.total', { total: kr(total) })}</p>
        </>
      )}
    </section>
  )
}

function KidsView({ people, season, giftsFor }: { people: Person[]; season: number; giftsFor: (id: number) => Gift[] }) {
  const { t } = useTranslation('verksted')
  const kids = people.filter(p => { const a = ageIn(p, season); return a !== null && a < 18 && !p.archived })
  if (kids.length === 0) return <p className="text-sm text-gray-400">{t('kids.empty')}</p>
  const rows = kids.map(k => {
    const g = giftsFor(k.id)
    return { kid: k, count: g.length, value: g.reduce((a, x) => a + shareOf(x), 0) }
  })
  const maxValue = Math.max(1, ...rows.map(r => r.value))
  const maxCount = Math.max(1, ...rows.map(r => r.count))
  return (
    <section className="space-y-3">
      <p className="text-xs text-gray-400">{t('kids.intro')}</p>
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs text-gray-400">
            <th className="font-normal py-1"></th>
            <th className="font-normal py-1">{t('kids.count')}</th>
            <th className="font-normal py-1">{t('kids.value')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(r => (
            <tr key={r.kid.id} className="border-t border-gray-800">
              <td className="py-2 pr-2 whitespace-nowrap">{r.kid.name} <span className="text-xs text-gray-500">{ageIn(r.kid, season)}</span></td>
              <td className="py-2 pr-2 w-1/3">
                <div className="flex items-center gap-2">
                  <div className="h-2 rounded-full bg-sky-500" style={{ width: `${(r.count / maxCount) * 100}%`, minWidth: r.count ? 4 : 0 }} />
                  <span className="tabular-nums text-xs">{r.count}</span>
                </div>
              </td>
              <td className="py-2 w-1/3">
                <div className="flex items-center gap-2">
                  <div className="h-2 rounded-full bg-emerald-500" style={{ width: `${(r.value / maxValue) * 100}%`, minWidth: r.value ? 4 : 0 }} />
                  <span className="tabular-nums text-xs whitespace-nowrap">{kr(r.value)}</span>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
