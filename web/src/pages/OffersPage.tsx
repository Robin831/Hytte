import { useState, useEffect, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, X, ShoppingCart, Check, RefreshCw, Star, Search } from 'lucide-react'
import { useAuth } from '../auth'

interface RankedOffer {
  id: string
  dealer_id: string
  dealer_name: string
  heading: string
  description: string
  price: number
  pre_price?: number
  currency: string
  unit_price?: number
  unit_label?: string
  image_url: string
  run_from: string
  run_till: string
  discount_pct?: number
  matched_keywords?: string[]
}

interface WatchlistEntry {
  id: number
  keyword: string
}

const HIDDEN_DEALERS_KEY = 'offers-hidden-dealers'

function loadHiddenDealers(): Set<string> {
  try {
    const raw = localStorage.getItem(HIDDEN_DEALERS_KEY)
    return new Set(raw ? (JSON.parse(raw) as string[]) : [])
  } catch {
    return new Set()
  }
}

export default function OffersPage() {
  const { t, i18n } = useTranslation(['offers', 'common'])
  const { user } = useAuth()

  const [offers, setOffers] = useState<RankedOffer[]>([])
  const [watchlist, setWatchlist] = useState<WatchlistEntry[]>([])
  const [fetchedAt, setFetchedAt] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [newKeyword, setNewKeyword] = useState('')
  const [saving, setSaving] = useState(false)
  const [search, setSearch] = useState('')
  const [hiddenDealers, setHiddenDealers] = useState<Set<string>>(loadHiddenDealers)
  const [addedToGrocery, setAddedToGrocery] = useState<Set<string>>(new Set())
  const [refreshing, setRefreshing] = useState(false)

  const load = useCallback(async (signal?: AbortSignal) => {
    const res = await fetch('/api/offers', { credentials: 'include', signal })
    if (!res.ok) throw new Error('fetch failed')
    const data = await res.json()
    return data as { offers: RankedOffer[]; watchlist: WatchlistEntry[]; fetched_at: string | null }
  }, [])

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    ;(async () => {
      try {
        const data = await load(controller.signal)
        if (controller.signal.aborted) return
        setOffers(data.offers)
        setWatchlist(data.watchlist)
        setFetchedAt(data.fetched_at)
      } catch {
        if (!controller.signal.aborted) setError(t('errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [user, load, t])

  const reload = useCallback(async () => {
    try {
      const data = await load()
      setOffers(data.offers)
      setWatchlist(data.watchlist)
      setFetchedAt(data.fetched_at)
    } catch {
      setError(t('errors.failedToLoad'))
    }
  }, [load, t])

  const addKeyword = async () => {
    const keyword = newKeyword.trim()
    if (!keyword || saving) return
    setSaving(true)
    setError('')
    try {
      const res = await fetch('/api/offers/watchlist', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ keyword }),
      })
      if (res.status === 409) {
        setNewKeyword('')
        return
      }
      if (!res.ok) throw new Error('add failed')
      setNewKeyword('')
      await reload()
    } catch {
      setError(t('errors.failedToSave'))
    } finally {
      setSaving(false)
    }
  }

  const removeKeyword = async (entry: WatchlistEntry) => {
    setError('')
    try {
      const res = await fetch(`/api/offers/watchlist/${entry.id}`, { method: 'DELETE', credentials: 'include' })
      if (!res.ok) throw new Error('delete failed')
      await reload()
    } catch {
      setError(t('errors.failedToSave'))
    }
  }

  const toggleDealer = (dealerId: string) => {
    setHiddenDealers(prev => {
      const next = new Set(prev)
      if (next.has(dealerId)) {
        next.delete(dealerId)
      } else {
        next.add(dealerId)
      }
      try {
        localStorage.setItem(HIDDEN_DEALERS_KEY, JSON.stringify([...next]))
      } catch {
        // localStorage unavailable — filter still works for this session
      }
      return next
    })
  }

  const addToGrocery = async (offer: RankedOffer) => {
    if (addedToGrocery.has(offer.id)) return
    setError('')
    try {
      const res = await fetch('/api/grocery/items', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: `${offer.heading} (${offer.dealer_name})` }),
      })
      if (!res.ok) throw new Error('add failed')
      setAddedToGrocery(prev => new Set(prev).add(offer.id))
    } catch {
      setError(t('errors.failedToAddGrocery'))
    }
  }

  const refresh = async () => {
    if (refreshing) return
    setRefreshing(true)
    setError('')
    try {
      const res = await fetch('/api/offers/refresh', { method: 'POST', credentials: 'include' })
      if (!res.ok) throw new Error('refresh failed')
      await reload()
    } catch {
      setError(t('errors.failedToRefresh'))
    } finally {
      setRefreshing(false)
    }
  }

  const dealers = useMemo(() => {
    const seen = new Map<string, string>()
    for (const o of offers) {
      if (!seen.has(o.dealer_id)) seen.set(o.dealer_id, o.dealer_name)
    }
    return [...seen.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [offers])

  const visible = useMemo(() => {
    const q = search.trim().toLowerCase()
    return offers.filter(o => {
      if (hiddenDealers.has(o.dealer_id)) return false
      if (q && !(o.heading + ' ' + o.description).toLowerCase().includes(q)) return false
      return true
    })
  }, [offers, hiddenDealers, search])

  const watched = visible.filter(o => (o.matched_keywords?.length ?? 0) > 0)
  const rest = visible.filter(o => (o.matched_keywords?.length ?? 0) === 0)

  const dateFmt = useMemo(() => new Intl.DateTimeFormat(i18n.language, { day: 'numeric', month: 'short' }), [i18n.language])
  const timeFmt = useMemo(() => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }), [i18n.language])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64" role="status">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500" />
      </div>
    )
  }

  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      <div className="flex items-center justify-between mb-1">
        <h1 className="text-2xl font-bold">{t('title')}</h1>
        {user?.is_admin && (
          <button
            onClick={refresh}
            disabled={refreshing}
            className="flex items-center gap-1.5 text-sm text-gray-400 hover:text-white disabled:opacity-50 cursor-pointer rounded-lg px-2 py-1.5 transition-colors"
            aria-label={t('refresh')}
          >
            <RefreshCw size={16} className={refreshing ? 'animate-spin' : ''} />
            <span className="hidden sm:inline">{t('refresh')}</span>
          </button>
        )}
      </div>
      {fetchedAt && (
        <p className="text-xs text-gray-500 mb-4">{t('fetchedAt', { time: timeFmt.format(new Date(fetchedAt)) })}</p>
      )}

      {error && (
        <div role="alert" className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-200 text-sm flex items-center justify-between">
          <span>{error}</span>
          <button onClick={() => setError('')} className="ml-2 text-red-400 hover:text-red-200 cursor-pointer" aria-label={t('common:actions.close')}>
            <X size={16} />
          </button>
        </div>
      )}

      {/* Watchlist editor */}
      <div className="bg-gray-800/60 border border-gray-700 rounded-xl p-4 mb-5">
        <h2 className="text-sm font-medium text-gray-300 mb-2 flex items-center gap-1.5">
          <Star size={15} className="text-amber-400" />
          {t('watchlist.title')}
        </h2>
        <div className="flex flex-wrap gap-1.5 mb-2">
          {watchlist.map(entry => (
            <span key={entry.id} className="inline-flex items-center gap-1 bg-amber-900/40 border border-amber-800 text-amber-200 rounded-full pl-3 pr-1.5 py-1 text-sm">
              {entry.keyword}
              <button
                onClick={() => removeKeyword(entry)}
                aria-label={t('watchlist.remove', { keyword: entry.keyword })}
                className="p-0.5 text-amber-400 hover:text-amber-100 cursor-pointer"
              >
                <X size={13} />
              </button>
            </span>
          ))}
          {watchlist.length === 0 && <span className="text-sm text-gray-500">{t('watchlist.empty')}</span>}
        </div>
        <div className="flex gap-2">
          <input
            type="text"
            value={newKeyword}
            onChange={e => setNewKeyword(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') addKeyword() }}
            placeholder={t('watchlist.placeholder')}
            aria-label={t('watchlist.placeholder')}
            className="flex-1 min-w-0 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
          />
          <button
            onClick={addKeyword}
            disabled={!newKeyword.trim() || saving}
            className="shrink-0 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg px-3 py-2 text-sm flex items-center gap-1.5 cursor-pointer transition-colors"
          >
            <Plus size={16} />
            <span className="hidden sm:inline">{t('watchlist.add')}</span>
          </button>
        </div>
      </div>

      {/* Search + dealer filter */}
      <div className="mb-4 space-y-2">
        <div className="relative">
          <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" aria-hidden="true" />
          <input
            type="search"
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder={t('searchPlaceholder')}
            aria-label={t('searchPlaceholder')}
            className="w-full bg-gray-800 border border-gray-700 rounded-lg pl-9 pr-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
          />
        </div>
        <div className="flex flex-wrap gap-1.5" role="group" aria-label={t('chainFilter')}>
          {dealers.map(([id, name]) => (
            <button
              key={id}
              onClick={() => toggleDealer(id)}
              aria-pressed={!hiddenDealers.has(id)}
              className={`rounded-full px-3 py-1 text-xs cursor-pointer transition-colors border ${
                hiddenDealers.has(id)
                  ? 'bg-gray-900 text-gray-600 border-gray-800 line-through'
                  : 'bg-gray-800 text-gray-300 border-gray-700 hover:text-white'
              }`}
            >
              {name}
            </button>
          ))}
        </div>
      </div>

      {offers.length === 0 ? (
        <div className="text-center py-12 text-gray-500">
          <p className="text-lg">{t('empty')}</p>
          <p className="text-sm mt-1">{t('emptyHint')}</p>
        </div>
      ) : (
        <>
          {watched.length > 0 && (
            <section className="mb-6">
              <h2 className="text-sm font-medium text-amber-300 mb-2 flex items-center gap-1.5">
                <Star size={15} />
                {t('watchedSection', { count: watched.length })}
              </h2>
              <div className="space-y-2">
                {watched.map(o => (
                  <OfferCard key={o.id} offer={o} dateFmt={dateFmt} added={addedToGrocery.has(o.id)} onAddToGrocery={addToGrocery} highlight />
                ))}
              </div>
            </section>
          )}

          <section>
            <h2 className="text-sm font-medium text-gray-400 mb-2">{t('topSection', { count: rest.length })}</h2>
            <div className="space-y-2">
              {rest.map(o => (
                <OfferCard key={o.id} offer={o} dateFmt={dateFmt} added={addedToGrocery.has(o.id)} onAddToGrocery={addToGrocery} />
              ))}
            </div>
          </section>
        </>
      )}
    </div>
  )
}

function OfferCard({ offer, dateFmt, added, onAddToGrocery, highlight }: {
  offer: RankedOffer
  dateFmt: Intl.DateTimeFormat
  added: boolean
  onAddToGrocery: (offer: RankedOffer) => void
  highlight?: boolean
}) {
  const { t, i18n } = useTranslation('offers')
  const priceFmt = new Intl.NumberFormat(i18n.language, { minimumFractionDigits: 2, maximumFractionDigits: 2 })

  return (
    <div className={`flex items-center gap-3 rounded-xl border px-3 py-2.5 transition-colors ${
      highlight ? 'bg-amber-950/20 border-amber-900/50' : 'bg-gray-800/40 border-gray-700/60 hover:bg-gray-800'
    }`}>
      {offer.image_url ? (
        <img src={offer.image_url} alt="" loading="lazy" className="w-14 h-14 rounded-lg object-cover bg-gray-900 shrink-0" />
      ) : (
        <div className="w-14 h-14 rounded-lg bg-gray-900 shrink-0" aria-hidden="true" />
      )}

      <div className="flex-1 min-w-0">
        <p className="text-sm text-white truncate">{offer.heading}</p>
        <p className="text-xs text-gray-500 truncate">
          {offer.dealer_name}
          {' · '}{t('validTill', { date: dateFmt.format(new Date(offer.run_till)) })}
          {offer.matched_keywords && offer.matched_keywords.length > 0 && (
            <span className="text-amber-400"> · {offer.matched_keywords.join(', ')}</span>
          )}
        </p>
        <p className="text-xs text-gray-400 mt-0.5">
          <span className="text-base font-semibold text-white">{priceFmt.format(offer.price)}</span>
          {offer.pre_price ? (
            <span className="line-through text-gray-500 ml-1.5">{priceFmt.format(offer.pre_price)}</span>
          ) : null}
          {offer.unit_price ? (
            <span className="ml-1.5">{t('unitPrice', { price: priceFmt.format(offer.unit_price), unit: offer.unit_label })}</span>
          ) : null}
        </p>
      </div>

      {offer.discount_pct ? (
        <span className="shrink-0 bg-red-600/90 text-white text-xs font-semibold rounded-lg px-1.5 py-1">
          −{offer.discount_pct}%
        </span>
      ) : null}

      <button
        onClick={() => onAddToGrocery(offer)}
        aria-label={added ? t('addedToGrocery') : t('addToGrocery', { name: offer.heading })}
        className={`shrink-0 p-2 rounded-lg cursor-pointer transition-colors ${
          added ? 'text-green-400' : 'text-gray-400 hover:text-white hover:bg-gray-700'
        }`}
        disabled={added}
      >
        {added ? <Check size={18} /> : <ShoppingCart size={18} />}
      </button>
    </div>
  )
}
