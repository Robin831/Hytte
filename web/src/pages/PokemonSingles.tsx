import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ArrowLeft, ExternalLink, Loader2, Plus, RefreshCw, ShoppingCart, Store, Trash2, X } from 'lucide-react'
import { useAuth } from '../auth'
import PokemonImage from '../components/pokemon/PokemonImage'
import { formatNok } from '../components/pokemon/format'
import { formatDateTime } from '../utils/formatDate'
import { bestOffer, type MissingCard } from '../components/pokemon/singles'

interface WantedSet {
  id: string
  name: string
  release_date: string
  logo_url: string
  symbol_url: string
  total_cards: number
  missing_count: number
}

interface Shop {
  id: number
  name: string
  base_url: string
  shipping_nok: number
  free_over_nok: number
  enabled: boolean
  last_checked_at: string | null
  last_error: string
}

interface SinglesPage {
  sets: WantedSet[]
  cards: MissingCard[]
  shops: Shop[]
  refreshing: boolean
}

interface BasketItem {
  card_id: string
  name: string
  variant_id: string
  print: string
  condition: string
  price_nok: number
  url: string
}

interface BasketShop {
  shop_id: number
  shop_name: string
  items: BasketItem[]
  subtotal_nok: number
  shipping_nok: number
  total_nok: number
  cart_url: string
}

interface Basket {
  shops: BasketShop[]
  unavailable: string[]
  total_nok: number
}

interface SetOption {
  id: string
  name: string
  release_date: string
}

type StockFilter = 'all' | 'inStock' | 'notInStock'
const STOCK_FILTERS: StockFilter[] = ['all', 'inStock', 'notInStock']

// "8. nov. 06:30" — explicit fields, since dateStyle/timeStyle can't be
// mixed with the hour12 option formatDateTime sets.
const CHECKED_FORMAT: Intl.DateTimeFormatOptions = { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }

function printLabel(t: TFunction<'pokemon'>, print: string): string {
  switch (print) {
    case 'normal': return t('singles.print.normal')
    case 'reverse': return t('singles.print.reverse')
    case 'holo': return t('singles.print.holo')
    default: return ''
  }
}

async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    credentials: 'include',
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
  })
  if (!res.ok) {
    let msg = ''
    try {
      msg = ((await res.json()) as { error?: string }).error ?? ''
    } catch {
      // not JSON
    }
    throw new Error(msg || `HTTP ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export default function PokemonSingles() {
  const { t } = useTranslation('pokemon')
  const { user } = useAuth()
  const isAdmin = !!user?.is_admin
  const [page, setPage] = useState<SinglesPage | null>(null)
  const [error, setError] = useState('')
  const [allSets, setAllSets] = useState<SetOption[]>([])
  const [addSetId, setAddSetId] = useState('')
  const [filter, setFilter] = useState<StockFilter>('all')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [basket, setBasket] = useState<Basket | null>(null)
  const [basketLoading, setBasketLoading] = useState(false)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const data = await api<SinglesPage>('/api/pokemon/singles', { signal })
      setPage(data)
      setError('')
    } catch (e) {
      if (signal?.aborted) return
      setError(e instanceof Error ? e.message : t('singles.errors.load'))
    }
  }, [t])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    load(controller.signal)
    // The sets endpoint pages 50 at a time.
    ;(async () => {
      const all: SetOption[] = []
      for (let offset = 0; offset < 2000; offset += 50) {
        const d = await api<{ sets: SetOption[] }>(`/api/pokemon/sets?limit=50&offset=${offset}`, { signal: controller.signal })
        all.push(...(d.sets ?? []))
        if ((d.sets ?? []).length < 50) break
      }
      setAllSets(all)
    })().catch(() => {})
    return () => controller.abort()
  }, [load])

  // While the shops are being checked, poll so new offers appear.
  useEffect(() => {
    if (!page?.refreshing) return
    const id = window.setInterval(() => { load() }, 5000)
    return () => window.clearInterval(id)
  }, [page?.refreshing, load])

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('singles.errors.save'))
    } finally {
      setBusy(false)
    }
  }

  const addSet = () => {
    if (!addSetId) return
    const id = addSetId
    setAddSetId('')
    run(() => api('/api/pokemon/singles/sets', { method: 'POST', body: JSON.stringify({ set_id: id }) }))
  }
  const removeSet = (id: string) => run(() => api(`/api/pokemon/singles/sets/${encodeURIComponent(id)}`, { method: 'DELETE' }))
  const refresh = () => run(() => api('/api/pokemon/singles/refresh', { method: 'POST' }))

  const cards = useMemo(() => {
    const list = page?.cards ?? []
    if (filter === 'inStock') return list.filter(c => bestOffer(c))
    if (filter === 'notInStock') return list.filter(c => !bestOffer(c))
    return list
  }, [page, filter])

  const lastChecked = useMemo(() => {
    const times = (page?.shops ?? []).map(s => s.last_checked_at).filter((x): x is string => !!x).sort()
    return times.length ? times[times.length - 1] : null
  }, [page])

  const toggle = (id: string) => {
    setBasket(null)
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }
  const selectAllInStock = () => {
    setBasket(null)
    setSelected(new Set((page?.cards ?? []).filter(c => bestOffer(c)).map(c => c.id)))
  }
  const clearSelection = () => {
    setBasket(null)
    setSelected(new Set())
  }

  const planBasket = async () => {
    setBasketLoading(true)
    try {
      setBasket(await api<Basket>('/api/pokemon/singles/basket', { method: 'POST', body: JSON.stringify({ card_ids: [...selected] }) }))
    } catch (e) {
      setError(e instanceof Error ? e.message : t('singles.errors.basket'))
    } finally {
      setBasketLoading(false)
    }
  }

  const wantedIds = new Set(page?.sets.map(s => s.id) ?? [])
  const setOptions = allSets.filter(s => !wantedIds.has(s.id))
  const cardsById = new Map((page?.cards ?? []).map(c => [c.id, c]))
  const setNames = new Map((page?.sets ?? []).map(s => [s.id, s.name]))

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <div className={`max-w-5xl mx-auto px-4 py-6 space-y-6 ${selected.size > 0 ? 'pb-28' : ''}`}>
        <header className="space-y-2">
          <Link to="/pokemon" className="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-white transition-colors">
            <ArrowLeft size={16} aria-hidden="true" />
            {t('singles.back')}
          </Link>
          <div className="flex items-center gap-3">
            <ShoppingCart size={28} className="text-emerald-400 shrink-0" aria-hidden="true" />
            <div className="min-w-0">
              <h1 className="text-2xl font-semibold">{t('singles.title')}</h1>
              <p className="text-sm text-gray-400">{t('singles.subtitle')}</p>
            </div>
          </div>
        </header>

        {error && (
          <div role="alert" className="px-3 py-2 bg-red-900/40 border border-red-800 text-red-300 text-sm rounded flex items-center justify-between gap-3">
            <span>{error}</span>
            <button type="button" onClick={() => setError('')} aria-label={t('singles.dismiss')} className="cursor-pointer">
              <X size={16} />
            </button>
          </div>
        )}

        {/* Wanted sets */}
        <section aria-labelledby="singles-sets" className="space-y-3">
          <h2 id="singles-sets" className="text-lg font-semibold">{t('singles.sets.title')}</h2>
          {page && page.sets.length === 0 && <p className="text-sm text-gray-400">{t('singles.sets.empty')}</p>}
          <div className="flex flex-wrap gap-2">
            {page?.sets.map(s => (
              <span key={s.id} className="inline-flex items-center gap-2 pl-3 pr-1 py-1 rounded-full bg-gray-800 border border-gray-700 text-sm">
                <span>{s.name}</span>
                <span className="text-xs text-gray-400">{t('singles.sets.missing', { count: s.missing_count, total: s.total_cards })}</span>
                <button
                  type="button"
                  onClick={() => removeSet(s.id)}
                  disabled={busy}
                  aria-label={t('singles.sets.remove', { name: s.name })}
                  className="p-1 rounded-full text-gray-400 hover:text-red-300 hover:bg-gray-700 cursor-pointer disabled:opacity-50"
                >
                  <X size={14} />
                </button>
              </span>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor="singles-add-set" className="sr-only">{t('singles.sets.addLabel')}</label>
            <select
              id="singles-add-set"
              value={addSetId}
              onChange={e => setAddSetId(e.target.value)}
              className="max-w-full sm:max-w-xs flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm"
            >
              <option value="">{t('singles.sets.choose')}</option>
              {setOptions.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
            <button
              type="button"
              onClick={addSet}
              disabled={!addSetId || busy}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm bg-emerald-600 hover:bg-emerald-500 rounded disabled:opacity-50 cursor-pointer"
            >
              <Plus size={16} aria-hidden="true" /> {t('singles.sets.add')}
            </button>
          </div>
        </section>

        {page && page.sets.length > 0 && (
          <section aria-labelledby="singles-cards" className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 id="singles-cards" className="text-lg font-semibold">{t('singles.cards.title', { count: page.cards.length })}</h2>
              <div className="flex items-center gap-2 text-xs text-gray-400">
                {page.refreshing ? (
                  <span className="inline-flex items-center gap-1"><Loader2 size={14} className="animate-spin" aria-hidden="true" />{t('singles.checking')}</span>
                ) : lastChecked ? (
                  <span>{t('singles.lastChecked', { when: formatDateTime(new Date(lastChecked), CHECKED_FORMAT) })}</span>
                ) : null}
                <button
                  type="button"
                  onClick={refresh}
                  disabled={busy || page.refreshing}
                  className="inline-flex items-center gap-1 px-2 py-1 rounded bg-gray-800 border border-gray-700 hover:bg-gray-700 text-gray-200 disabled:opacity-50 cursor-pointer"
                >
                  <RefreshCw size={14} aria-hidden="true" /> {t('singles.refresh')}
                </button>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <div role="radiogroup" aria-label={t('singles.filter.label')} className="inline-flex p-0.5 rounded-md bg-gray-800/60 border border-gray-800">
                {STOCK_FILTERS.map(f => (
                  <button
                    key={f}
                    type="button"
                    role="radio"
                    aria-checked={filter === f}
                    onClick={() => setFilter(f)}
                    className={`px-3 py-1 text-xs rounded cursor-pointer ${filter === f ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'}`}
                  >
                    {t(`singles.filter.${f}`)}
                  </button>
                ))}
              </div>
              <button type="button" onClick={selectAllInStock} className="text-xs text-emerald-300 hover:text-emerald-200 cursor-pointer">
                {t('singles.selectInStock')}
              </button>
              {selected.size > 0 && (
                <button type="button" onClick={clearSelection} className="text-xs text-gray-400 hover:text-white cursor-pointer">
                  {t('singles.clearSelection')}
                </button>
              )}
            </div>

            {cards.length === 0 ? (
              <p className="text-sm text-gray-400">{page.cards.length === 0 ? t('singles.cards.complete') : t('singles.cards.noneMatch')}</p>
            ) : (
              <ul className="divide-y divide-gray-800 border border-gray-800 rounded-lg">
                {cards.map((c, i) => {
                  const best = bestOffer(c)
                  const others = c.offers.filter(o => o !== best)
                  const showSet = page.sets.length > 1 && (i === 0 || cards[i - 1].set_id !== c.set_id)
                  return (
                    <li key={c.id}>
                      {showSet && <p className="px-3 pt-3 text-xs font-semibold uppercase tracking-wide text-gray-500">{setNames.get(c.set_id)}</p>}
                      <div className="flex gap-3 p-3">
                        <input
                          type="checkbox"
                          checked={selected.has(c.id)}
                          onChange={() => toggle(c.id)}
                          aria-label={t('singles.select', { name: c.name, number: c.collector_no })}
                          className="mt-1 h-4 w-4 shrink-0 accent-emerald-500"
                        />
                        <div className="w-12 sm:w-14 shrink-0 aspect-[5/7] bg-gray-800/60 rounded overflow-hidden flex items-center justify-center">
                          <PokemonImage
                            src={c.image_small_url}
                            alt={c.name}
                            className="max-h-full max-w-full object-contain"
                            fallback={<span className="text-[10px] text-gray-500">{c.collector_no}</span>}
                          />
                        </div>
                        <div className="min-w-0 flex-1 space-y-1">
                          <p className="text-sm font-medium truncate">
                            {c.name} <span className="text-gray-500">#{c.collector_no}</span>
                          </p>
                          {c.rarity && <p className="text-xs text-gray-500">{c.rarity}</p>}
                          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                            {best ? (
                              <a href={best.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-emerald-300 hover:text-emerald-200">
                                <Store size={12} aria-hidden="true" />
                                <span className="font-semibold">{formatNok(best.price_nok)}</span>
                                <span>{best.shop_name}</span>
                                {(best.print || best.condition) && (
                                  <span className="text-gray-400">({[best.print && printLabel(t, best.print), best.condition].filter(Boolean).join(', ')})</span>
                                )}
                                <ExternalLink size={11} aria-hidden="true" />
                              </a>
                            ) : (
                              <span className="text-gray-500">{t('singles.notInStock')}</span>
                            )}
                            {c.cardmarket_url ? (
                              <a href={c.cardmarket_url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-blue-300 hover:text-blue-200">
                                {t('singles.cardmarket', { price: formatNok(c.cardmarket_nok) })}
                                <ExternalLink size={11} aria-hidden="true" />
                              </a>
                            ) : (
                              <span className="text-gray-500">{t('singles.cardmarketPrice', { price: formatNok(c.cardmarket_nok) })}</span>
                            )}
                          </div>
                          {others.length > 0 && (
                            <details className="text-xs text-gray-400">
                              <summary className="cursor-pointer hover:text-gray-200">{t('singles.otherOffers', { count: others.length })}</summary>
                              <ul className="mt-1 space-y-0.5 pl-2">
                                {others.map(o => (
                                  <li key={`${o.shop_id}-${o.variant_id}`}>
                                    <a href={o.url} target="_blank" rel="noopener noreferrer" className={`hover:text-white ${o.available ? '' : 'line-through opacity-60'}`}>
                                      {formatNok(o.price_nok)} · {o.shop_name}
                                      {(o.print || o.condition) && ` (${[o.print && printLabel(t, o.print), o.condition].filter(Boolean).join(', ')})`}
                                      {!o.available && ` — ${t('singles.soldOut')}`}
                                    </a>
                                  </li>
                                ))}
                              </ul>
                            </details>
                          )}
                        </div>
                      </div>
                    </li>
                  )
                })}
              </ul>
            )}
          </section>
        )}

        {basket && (
          <section aria-labelledby="singles-basket" className="space-y-3 p-4 border border-emerald-700/50 bg-emerald-900/10 rounded-lg">
            <div className="flex items-center justify-between gap-2">
              <h2 id="singles-basket" className="text-lg font-semibold">{t('singles.basket.title')}</h2>
              <button type="button" onClick={() => setBasket(null)} aria-label={t('singles.dismiss')} className="text-gray-400 hover:text-white cursor-pointer"><X size={16} /></button>
            </div>
            {basket.shops.map(s => (
              <div key={s.shop_id} className="space-y-1.5 border-t border-gray-800 pt-3 first:border-t-0 first:pt-0">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <h3 className="font-semibold">{s.shop_name}</h3>
                  <span className="text-sm text-gray-300">
                    {t('singles.basket.shopTotal', { subtotal: formatNok(s.subtotal_nok), shipping: s.shipping_nok > 0 ? formatNok(s.shipping_nok) : t('singles.basket.free'), total: formatNok(s.total_nok) })}
                  </span>
                </div>
                <ul className="text-sm text-gray-300 space-y-0.5">
                  {s.items.map(it => (
                    <li key={it.variant_id} className="flex justify-between gap-2">
                      <span className="truncate">{it.name}{it.print && ` (${printLabel(t, it.print)})`}</span>
                      <span className="shrink-0">{formatNok(it.price_nok)}</span>
                    </li>
                  ))}
                </ul>
                <a
                  href={s.cart_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm bg-emerald-600 hover:bg-emerald-500 rounded"
                >
                  <ShoppingCart size={16} aria-hidden="true" /> {t('singles.basket.checkout', { shop: s.shop_name })}
                </a>
              </div>
            ))}
            {basket.unavailable.length > 0 && (
              <div className="border-t border-gray-800 pt-3 space-y-1">
                <h3 className="text-sm font-semibold">{t('singles.basket.unavailable')}</h3>
                <ul className="text-sm text-gray-400 space-y-0.5">
                  {basket.unavailable.map(id => {
                    const c = cardsById.get(id)
                    if (!c) return null
                    return (
                      <li key={id}>
                        {c.name} #{c.collector_no}
                        {c.cardmarket_url && (
                          <> — <a href={c.cardmarket_url} target="_blank" rel="noopener noreferrer" className="text-blue-300 hover:text-blue-200">{t('singles.cardmarket', { price: formatNok(c.cardmarket_nok) })}</a></>
                        )}
                      </li>
                    )
                  })}
                </ul>
              </div>
            )}
            {basket.shops.length > 0 && (
              <p className="border-t border-gray-800 pt-3 font-semibold">{t('singles.basket.total', { total: formatNok(basket.total_nok) })}</p>
            )}
          </section>
        )}

        <ShopsSection shops={page?.shops ?? []} isAdmin={isAdmin} onChanged={() => load()} onError={setError} />
      </div>

      {selected.size > 0 && (
        <div className="fixed bottom-0 inset-x-0 z-20 bg-gray-950/95 border-t border-gray-800 backdrop-blur">
          <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between gap-3">
            <span className="text-sm">{t('singles.selected', { count: selected.size })}</span>
            <button
              type="button"
              onClick={planBasket}
              disabled={basketLoading}
              className="inline-flex items-center gap-1.5 px-3 py-2 text-sm bg-emerald-600 hover:bg-emerald-500 rounded disabled:opacity-50 cursor-pointer"
            >
              {basketLoading ? <Loader2 size={16} className="animate-spin" aria-hidden="true" /> : <ShoppingCart size={16} aria-hidden="true" />}
              {t('singles.basket.plan')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function ShopsSection({ shops, isAdmin, onChanged, onError }: {
  shops: Shop[]
  isAdmin: boolean
  onChanged: () => void
  onError: (msg: string) => void
}) {
  const { t } = useTranslation('pokemon')
  const [url, setUrl] = useState('')
  const [adding, setAdding] = useState(false)

  const call = async (input: string, init: RequestInit) => {
    try {
      await api(input, init)
      onChanged()
    } catch (e) {
      onError(e instanceof Error ? e.message : t('singles.errors.save'))
    }
  }

  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!url.trim()) return
    setAdding(true)
    await call('/api/pokemon/singles/shops', { method: 'POST', body: JSON.stringify({ url }) })
    setUrl('')
    setAdding(false)
  }

  const patch = (id: number, body: Record<string, unknown>) =>
    call(`/api/pokemon/singles/shops/${id}`, { method: 'PATCH', body: JSON.stringify(body) })

  return (
    <details className="border border-gray-800 rounded-lg">
      <summary className="px-4 py-3 cursor-pointer text-sm font-semibold inline-flex items-center gap-2">
        <Store size={16} aria-hidden="true" /> {t('singles.shops.title', { count: shops.length })}
      </summary>
      <div className="px-4 pb-4 space-y-3">
        <p className="text-xs text-gray-400">{t('singles.shops.intro')}</p>
        <ul className="space-y-2">
          {shops.map(s => (
            <li key={s.id} className="p-2 rounded bg-gray-800/40 border border-gray-800 text-sm space-y-1.5">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <a href={s.base_url} target="_blank" rel="noopener noreferrer" className={`font-medium hover:text-emerald-300 ${s.enabled ? '' : 'text-gray-500 line-through'}`}>
                  {s.name}
                </a>
                <span className="text-xs text-gray-400">
                  {s.last_error ? <span className="text-amber-300">{t('singles.shops.error')}</span> : s.last_checked_at ? t('singles.lastChecked', { when: formatDateTime(new Date(s.last_checked_at), CHECKED_FORMAT) }) : t('singles.shops.notChecked')}
                </span>
              </div>
              {isAdmin ? (
                <div className="flex flex-wrap items-center gap-3 text-xs text-gray-300">
                  <label className="inline-flex items-center gap-1">
                    {t('singles.shops.shipping')}
                    <input
                      type="number"
                      min={0}
                      defaultValue={s.shipping_nok}
                      onBlur={e => { const v = Number(e.target.value); if (v !== s.shipping_nok) patch(s.id, { shipping_nok: v }) }}
                      className="w-16 bg-gray-900 border border-gray-700 rounded px-1 py-0.5"
                    />
                  </label>
                  <label className="inline-flex items-center gap-1">
                    {t('singles.shops.freeOver')}
                    <input
                      type="number"
                      min={0}
                      defaultValue={s.free_over_nok}
                      onBlur={e => { const v = Number(e.target.value); if (v !== s.free_over_nok) patch(s.id, { free_over_nok: v }) }}
                      className="w-20 bg-gray-900 border border-gray-700 rounded px-1 py-0.5"
                    />
                  </label>
                  <label className="inline-flex items-center gap-1 cursor-pointer">
                    <input type="checkbox" checked={s.enabled} onChange={e => patch(s.id, { enabled: e.target.checked })} className="accent-emerald-500" />
                    {t('singles.shops.enabled')}
                  </label>
                  <button
                    type="button"
                    onClick={() => { if (window.confirm(t('singles.shops.confirmRemove', { name: s.name }))) call(`/api/pokemon/singles/shops/${s.id}`, { method: 'DELETE' }) }}
                    aria-label={t('singles.shops.remove', { name: s.name })}
                    className="text-gray-500 hover:text-red-300 cursor-pointer"
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              ) : (
                <p className="text-xs text-gray-400">
                  {t('singles.shops.shippingInfo', { shipping: formatNok(s.shipping_nok) })}
                  {s.free_over_nok > 0 && ` · ${t('singles.shops.freeOverInfo', { amount: formatNok(s.free_over_nok) })}`}
                </p>
              )}
              {s.last_error && <p className="text-xs text-amber-300/80 break-words">{s.last_error}</p>}
            </li>
          ))}
        </ul>
        {isAdmin && (
          <form onSubmit={add} className="flex flex-wrap items-center gap-2">
            <label htmlFor="singles-shop-url" className="sr-only">{t('singles.shops.addLabel')}</label>
            <input
              id="singles-shop-url"
              value={url}
              onChange={e => setUrl(e.target.value)}
              placeholder={t('singles.shops.placeholder')}
              className="flex-1 min-w-0 bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm"
            />
            <button type="submit" disabled={adding || !url.trim()} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm bg-gray-700 hover:bg-gray-600 rounded disabled:opacity-50 cursor-pointer">
              {adding ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Plus size={14} aria-hidden="true" />}
              {t('singles.shops.add')}
            </button>
          </form>
        )}
      </div>
    </details>
  )
}
