import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ArrowLeft, Check, X } from 'lucide-react'
import { Skeleton } from '../components/ui/skeleton'
import ToastList from '../components/ToastList'
import { useToast } from '../hooks/useToast'
import { formatDate, formatNumber } from '../utils/formatDate'

interface Variant {
  id: number
  kind: string
  price_eur: number
  price_nok: number | null
  price_at?: string | null
  owned: boolean
  owned_id?: number | null
  quantity: number
  condition: string
  notes: string
  acquired_at?: string | null
}

interface Card {
  id: string
  set_id: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: Variant[]
}

interface PokemonSet {
  id: string
  name: string
  series: string
  release_date: string
  total_cards: number
  symbol_url: string
  logo_url: string
  owned_count: number
}

type Filter = 'all' | 'owned' | 'missing'
const FILTERS: Filter[] = ['all', 'owned', 'missing']

const CONDITIONS = [
  '',
  'mint',
  'near_mint',
  'lightly_played',
  'moderately_played',
  'heavily_played',
  'damaged',
] as const

// parseReleaseDate accepts the "YYYY/MM/DD" or "YYYY-MM-DD" formats returned
// by pokemontcg.io and renders a localized date. Falls back to the raw string
// when parsing fails so we never render "Invalid Date".
function formatReleaseDate(raw: string): string {
  const m = raw.match(/^(\d{4})[/-](\d{1,2})[/-](\d{1,2})$/)
  let d: Date
  if (m) {
    d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  } else {
    d = new Date(raw)
  }
  if (Number.isNaN(d.getTime())) return raw
  try {
    return formatDate(d, { dateStyle: 'medium' })
  } catch {
    return raw
  }
}

function formatNok(amount: number | null | undefined): string {
  if (amount == null) return '—'
  return formatNumber(amount, {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

// defaultVariant returns the variant we display on a card tile. The backend
// orders variants by kind, so "normal" precedes "reverse_holofoil"; we treat
// the first variant as the canonical one.
function defaultVariant(card: Card): Variant | undefined {
  return card.variants[0]
}

function cardIsOwned(card: Card): boolean {
  return card.variants.some(v => v.owned)
}

// ownedSetValue sums the NOK price of every owned variant. Missing rates are
// silently skipped — the caller already surfaces "rate unavailable" via the
// X-Pokemon-Rate-Missing response header, so we don't double-warn here.
function ownedSetValue(cards: Card[]): number {
  let total = 0
  for (const card of cards) {
    for (const variant of card.variants) {
      if (variant.owned && variant.price_nok != null) {
        total += variant.price_nok * Math.max(variant.quantity, 1)
      }
    }
  }
  return total
}

function ownedCardCount(cards: Card[]): number {
  let count = 0
  for (const card of cards) {
    if (cardIsOwned(card)) count++
  }
  return count
}

interface TileProps {
  card: Card
  onClick: () => void
  t: TFunction<'pokemon'>
}

function CardTile({ card, onClick, t }: TileProps) {
  const owned = cardIsOwned(card)
  const variant = defaultVariant(card)
  const price = variant?.price_nok
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={`card-tile-${card.id}`}
      aria-label={t('tile.openCard', { name: card.name, number: card.collector_no })}
      aria-pressed={owned}
      className={`flex flex-col gap-2 p-2 rounded-lg border bg-gray-800/40 transition-colors text-left cursor-pointer
        ${owned
          ? 'border-emerald-500/70 ring-1 ring-emerald-500/40 hover:bg-gray-800/70'
          : 'border-gray-800 opacity-60 hover:opacity-100 hover:border-gray-700 hover:bg-gray-800/70'
        }`}
    >
      <div className="relative aspect-[5/7] flex items-center justify-center bg-gray-900/40 rounded overflow-hidden">
        {card.image_small_url ? (
          <img
            src={card.image_small_url}
            alt=""
            loading="lazy"
            className="max-h-full max-w-full object-contain"
          />
        ) : (
          <span className="text-xs text-gray-500">{card.collector_no}</span>
        )}
        {owned && (
          <span
            aria-hidden="true"
            className="absolute top-1 right-1 flex items-center justify-center h-5 w-5 rounded-full bg-emerald-500 text-white shadow"
          >
            <Check size={12} />
          </span>
        )}
      </div>
      <div className="min-w-0">
        <p className="text-sm font-medium text-white truncate" title={card.name}>{card.name}</p>
        <p className="text-xs text-gray-500">{t('tile.collectorNo', { number: card.collector_no })}</p>
        <p className="text-xs text-gray-300 mt-0.5">{formatNok(price)}</p>
      </div>
    </button>
  )
}

interface DetailPanelProps {
  card: Card
  onClose: () => void
  onSave: (variantId: number, payload: SavePayload) => Promise<void>
  onUnmark: (collectionId: number) => Promise<void>
  saving: boolean
  t: TFunction<'pokemon'>
}

interface SavePayload {
  quantity: number
  condition: string
  notes: string
}

function DetailPanel({ card, onClose, onSave, onUnmark, saving, t }: DetailPanelProps) {
  const initialVariantId = useMemo(() => {
    const ownedV = card.variants.find(v => v.owned)
    return (ownedV ?? card.variants[0])?.id ?? 0
  }, [card])
  const [selectedVariantId, setSelectedVariantId] = useState<number>(initialVariantId)
  const selected = useMemo(
    () => card.variants.find(v => v.id === selectedVariantId) ?? card.variants[0],
    [card, selectedVariantId],
  )
  const [quantity, setQuantity] = useState<number>(Math.max(selected?.quantity ?? 1, 1))
  const [condition, setCondition] = useState<string>(selected?.condition ?? '')
  const [notes, setNotes] = useState<string>(selected?.notes ?? '')

  // Re-sync form when the selected variant changes. Using the "adjust during
  // render" pattern (store prev id, update state inline) avoids an extra
  // render cycle compared to a useEffect approach.
  const [prevVariantId, setPrevVariantId] = useState(selectedVariantId)
  if (selectedVariantId !== prevVariantId && selected) {
    setPrevVariantId(selectedVariantId)
    setQuantity(Math.max(selected.quantity || 1, 1))
    setCondition(selected.condition || '')
    setNotes(selected.notes || '')
  }

  if (!selected) return null

  const handleSave = (e: React.FormEvent) => {
    e.preventDefault()
    void onSave(selected.id, { quantity, condition, notes })
  }

  const handleUnmark = () => {
    if (selected.owned_id != null) {
      void onUnmark(selected.owned_id)
    }
  }

  return (
    <div className="bg-gray-800/60 border border-gray-700 rounded-lg p-4 space-y-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-lg font-semibold text-white truncate">{card.name}</h2>
          <p className="text-xs text-gray-400">
            {t('tile.collectorNo', { number: card.collector_no })}
            {card.rarity ? ` · ${card.rarity}` : ''}
          </p>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('detail.close')}
          className="p-1 text-gray-400 hover:text-white cursor-pointer"
        >
          <X size={18} />
        </button>
      </div>

      <div className="flex flex-col sm:flex-row gap-4">
        <div className="sm:w-48 shrink-0">
          {card.image_large_url ? (
            <img
              src={card.image_large_url}
              alt=""
              loading="lazy"
              className="w-full rounded shadow"
            />
          ) : (
            <div className="aspect-[5/7] bg-gray-900/60 rounded" />
          )}
        </div>

        <form onSubmit={handleSave} className="flex-1 min-w-0 space-y-3">
          <fieldset className="space-y-1.5">
            <legend className="text-sm font-medium text-gray-200">{t('detail.variant')}</legend>
            <div className="flex flex-wrap gap-2">
              {card.variants.map(v => {
                const checked = v.id === selectedVariantId
                return (
                  <label
                    key={v.id}
                    className={`flex items-center gap-2 px-2.5 py-1.5 rounded border cursor-pointer text-sm
                      ${checked ? 'border-emerald-500 bg-emerald-500/10 text-white' : 'border-gray-700 bg-gray-900/40 text-gray-300 hover:border-gray-600'}`}
                  >
                    <input
                      type="radio"
                      name={`variant-${card.id}`}
                      value={v.id}
                      checked={checked}
                      onChange={() => setSelectedVariantId(v.id)}
                      className="sr-only"
                    />
                    <span>{t(`variantKind.${v.kind}`, { defaultValue: v.kind })}</span>
                    <span className="text-xs text-gray-400">{formatNok(v.price_nok)}</span>
                    {v.owned && (
                      <span aria-hidden="true" className="text-emerald-400"><Check size={14} /></span>
                    )}
                  </label>
                )
              })}
            </div>
          </fieldset>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <label className="block text-sm">
              <span className="block text-gray-200 mb-1">{t('detail.quantity')}</span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setQuantity(q => Math.max(1, q - 1))}
                  aria-label={t('detail.decreaseQuantity')}
                  className="px-2 py-1 bg-gray-700 hover:bg-gray-600 text-white rounded cursor-pointer"
                >
                  −
                </button>
                <input
                  type="number"
                  min={1}
                  value={quantity}
                  onChange={e => {
                    const n = Number.parseInt(e.target.value, 10)
                    setQuantity(Number.isFinite(n) && n > 0 ? n : 1)
                  }}
                  className="w-16 px-2 py-1 bg-gray-900 border border-gray-700 rounded text-white text-center"
                  aria-label={t('detail.quantity')}
                />
                <button
                  type="button"
                  onClick={() => setQuantity(q => q + 1)}
                  aria-label={t('detail.increaseQuantity')}
                  className="px-2 py-1 bg-gray-700 hover:bg-gray-600 text-white rounded cursor-pointer"
                >
                  +
                </button>
              </div>
            </label>

            <label className="block text-sm">
              <span className="block text-gray-200 mb-1">{t('detail.condition')}</span>
              <select
                value={condition}
                onChange={e => setCondition(e.target.value)}
                className="w-full px-2 py-1.5 bg-gray-900 border border-gray-700 rounded text-white"
              >
                {CONDITIONS.map(c => (
                  <option key={c || 'unset'} value={c}>
                    {c ? t(`condition.${c}`) : t('condition.unset')}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <label className="block text-sm">
            <span className="block text-gray-200 mb-1">{t('detail.notes')}</span>
            <textarea
              value={notes}
              onChange={e => setNotes(e.target.value)}
              rows={2}
              className="w-full px-2 py-1.5 bg-gray-900 border border-gray-700 rounded text-white"
              placeholder={t('detail.notesPlaceholder')}
            />
          </label>

          <div className="flex flex-wrap items-center gap-2">
            <button
              type="submit"
              disabled={saving}
              className="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-500 disabled:bg-emerald-800/60 disabled:cursor-not-allowed text-white rounded text-sm cursor-pointer"
            >
              {selected.owned ? t('detail.update') : t('detail.markOwned')}
            </button>
            {selected.owned && selected.owned_id != null && (
              <button
                type="button"
                onClick={handleUnmark}
                disabled={saving}
                className="px-3 py-1.5 bg-red-700/80 hover:bg-red-600 disabled:bg-red-900/60 disabled:cursor-not-allowed text-white rounded text-sm cursor-pointer"
              >
                {t('detail.unmark')}
              </button>
            )}
          </div>
        </form>
      </div>
    </div>
  )
}

function GridSkeleton() {
  return (
    <div
      className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-3"
      aria-busy="true"
    >
      {Array.from({ length: 12 }).map((_, i) => (
        <div key={i} className="p-2 bg-gray-800/40 border border-gray-800 rounded-lg space-y-2">
          <Skeleton className="aspect-[5/7] w-full" />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-3 w-1/2" />
        </div>
      ))}
    </div>
  )
}

export default function PokemonSetPage() {
  const { t } = useTranslation('pokemon')
  const { id } = useParams<{ id: string }>()
  const setId = id ?? ''
  const { toasts, showToast } = useToast()

  const [set, setSet] = useState<PokemonSet | null>(null)
  const [cards, setCards] = useState<Card[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [expandedCardId, setExpandedCardId] = useState<string | null>(null)
  const [savingCardId, setSavingCardId] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)
  const cardsRef = useRef<Card[]>([])
  useLayoutEffect(() => { cardsRef.current = cards })
  const filterButtonRefs = useRef<Array<HTMLButtonElement | null>>([])

  // selectFilter implements the WAI-ARIA radiogroup keyboard pattern: focus the
  // target radio and select it. Used by both pointer clicks and arrow/Home/End
  // keys so the visible state and DOM focus stay in lockstep.
  const selectFilter = useCallback((index: number) => {
    const next = FILTERS[index]
    if (!next) return
    setFilter(next)
    filterButtonRefs.current[index]?.focus()
  }, [])

  const handleFilterKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLButtonElement>, index: number) => {
      switch (e.key) {
        case 'ArrowRight':
        case 'ArrowDown':
          e.preventDefault()
          selectFilter((index + 1) % FILTERS.length)
          break
        case 'ArrowLeft':
        case 'ArrowUp':
          e.preventDefault()
          selectFilter((index - 1 + FILTERS.length) % FILTERS.length)
          break
        case 'Home':
          e.preventDefault()
          selectFilter(0)
          break
        case 'End':
          e.preventDefault()
          selectFilter(FILTERS.length - 1)
          break
      }
    },
    [selectFilter],
  )

  const load = useCallback(() => {
    setLoading(true)
    setError('')
    setAttempt(a => a + 1)
  }, [])

  useEffect(() => {
    if (!setId) return
    const controller = new AbortController()
    ;(async () => {
      try {
        const cardsRes = await fetch(`/api/pokemon/sets/${encodeURIComponent(setId)}/cards`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!cardsRes.ok) throw new Error(t('errors.failedToLoad'))
        const data: { set?: PokemonSet | null; cards?: Card[] } = await cardsRes.json()

        setCards(data.cards ?? [])
        setSet(data.set ?? null)
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : t('errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [setId, attempt, t])

  const ownedCount = useMemo(() => ownedCardCount(cards), [cards])
  const setValueNok = useMemo(() => ownedSetValue(cards), [cards])
  const totalCards = set?.total_cards ?? cards.length

  const visibleCards = useMemo(() => {
    if (filter === 'owned') return cards.filter(cardIsOwned)
    if (filter === 'missing') return cards.filter(c => !cardIsOwned(c))
    return cards
  }, [cards, filter])

  const expandedCard = useMemo(
    () => (expandedCardId ? cards.find(c => c.id === expandedCardId) ?? null : null),
    [cards, expandedCardId],
  )

  const updateCardVariants = useCallback(
    (cardId: string, mutate: (variants: Variant[]) => Variant[]) => {
      setCards(prev => prev.map(c => (c.id === cardId ? { ...c, variants: mutate(c.variants) } : c)))
    },
    [],
  )

  const handleSave = useCallback(
    async (cardId: string, variantId: number, payload: SavePayload) => {
      const previousCards = cardsRef.current
      const previousCard = previousCards.find(c => c.id === cardId)
      const previousVariant = previousCard?.variants.find(v => v.id === variantId)
      if (!previousCard || !previousVariant) return

      setSavingCardId(cardId)
      updateCardVariants(cardId, vs =>
        vs.map(v =>
          v.id === variantId
            ? { ...v, owned: true, quantity: payload.quantity, condition: payload.condition, notes: payload.notes }
            : v,
        ),
      )

      try {
        const res = await fetch('/api/pokemon/collection', {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            card_id: cardId,
            variant_id: variantId,
            quantity: payload.quantity,
            condition: payload.condition,
            notes: payload.notes,
          }),
        })
        if (!res.ok) throw new Error(t('errors.markFailed'))
        const data: { item?: { id?: number; quantity?: number; condition?: string; notes?: string; acquired_at?: string } } = await res.json()
        const item = data.item ?? {}
        updateCardVariants(cardId, vs =>
          vs.map(v =>
            v.id === variantId
              ? {
                  ...v,
                  owned: true,
                  owned_id: item.id ?? v.owned_id ?? null,
                  quantity: item.quantity ?? payload.quantity,
                  condition: item.condition ?? payload.condition,
                  notes: item.notes ?? payload.notes,
                  acquired_at: item.acquired_at ?? v.acquired_at ?? null,
                }
              : v,
          ),
        )
        showToast(t('toast.marked'), 'success')
      } catch (err) {
        updateCardVariants(cardId, vs => vs.map(v => (v.id === variantId ? { ...previousVariant } : v)))
        showToast(err instanceof Error ? err.message : t('errors.markFailed'), 'error')
      } finally {
        setSavingCardId(prev => (prev === cardId ? null : prev))
      }
    },
    [showToast, t, updateCardVariants],
  )

  const handleUnmark = useCallback(
    async (cardId: string, collectionId: number) => {
      const previousCards = cardsRef.current
      const previousCard = previousCards.find(c => c.id === cardId)
      const previousVariant = previousCard?.variants.find(v => v.owned_id === collectionId)
      if (!previousCard || !previousVariant) return

      setSavingCardId(cardId)
      updateCardVariants(cardId, vs =>
        vs.map(v =>
          v.owned_id === collectionId
            ? { ...v, owned: false, owned_id: null, quantity: 0, condition: '', notes: '', acquired_at: null }
            : v,
        ),
      )

      try {
        const res = await fetch(`/api/pokemon/collection/${collectionId}`, {
          method: 'DELETE',
          credentials: 'include',
        })
        if (!res.ok && res.status !== 204) throw new Error(t('errors.unmarkFailed'))
        showToast(t('toast.unmarked'), 'success')
      } catch (err) {
        updateCardVariants(cardId, vs => vs.map(v => (v.id === previousVariant.id ? { ...previousVariant } : v)))
        showToast(err instanceof Error ? err.message : t('errors.unmarkFailed'), 'error')
      } finally {
        setSavingCardId(prev => (prev === cardId ? null : prev))
      }
    },
    [showToast, t, updateCardVariants],
  )

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <div className="max-w-5xl mx-auto px-4 py-6 space-y-6">
        <header className="space-y-3">
          <Link
            to="/pokemon"
            className="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-white transition-colors"
          >
            <ArrowLeft size={16} />
            {t('detail.back')}
          </Link>

          <div className="flex flex-col sm:flex-row sm:items-center gap-4">
            <div className="h-14 flex items-center">
              {set?.logo_url ? (
                <img
                  src={set.logo_url}
                  alt=""
                  className="max-h-14 max-w-[180px] object-contain"
                  loading="lazy"
                />
              ) : null}
            </div>
            <div className="min-w-0">
              <h1 className="text-2xl font-semibold truncate">{set?.name ?? setId}</h1>
              {set?.release_date && (
                <p className="text-xs text-gray-500">{formatReleaseDate(set.release_date)}</p>
              )}
            </div>
          </div>

          <dl className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            <div className="px-3 py-2 bg-gray-800/40 border border-gray-800 rounded">
              <dt className="text-xs text-gray-500">{t('detail.ownership')}</dt>
              <dd
                className="text-sm font-medium text-white"
                data-testid="owned-count"
              >
                {t('detail.ownedOf', { owned: ownedCount, total: totalCards })}
              </dd>
            </div>
            <div className="px-3 py-2 bg-gray-800/40 border border-gray-800 rounded">
              <dt className="text-xs text-gray-500">{t('detail.setValue')}</dt>
              <dd className="text-sm font-medium text-white" data-testid="set-value">{formatNok(setValueNok)}</dd>
            </div>
            <div className="px-3 py-2 bg-gray-800/40 border border-gray-800 rounded col-span-2 sm:col-span-1">
              <dt className="text-xs text-gray-500">{t('detail.totalCards')}</dt>
              <dd className="text-sm font-medium text-white">{totalCards}</dd>
            </div>
          </dl>

          <div
            role="radiogroup"
            aria-label={t('detail.filter')}
            className="inline-flex p-0.5 rounded-md bg-gray-800/60 border border-gray-800"
          >
            {FILTERS.map((f, i) => {
              const checked = f === filter
              return (
                <button
                  key={f}
                  ref={el => { filterButtonRefs.current[i] = el }}
                  type="button"
                  role="radio"
                  aria-checked={checked}
                  // Only the checked radio is in the tab sequence so Tab moves
                  // focus into and out of the group as a single stop; arrow
                  // keys then move between radios per the WAI-ARIA pattern.
                  tabIndex={checked ? 0 : -1}
                  onClick={() => selectFilter(i)}
                  onKeyDown={e => handleFilterKeyDown(e, i)}
                  className={`px-3 py-1 text-xs rounded cursor-pointer transition-colors
                    ${checked ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'}`}
                >
                  {t(`detail.filters.${f}`)}
                </button>
              )
            })}
          </div>
        </header>

        {error && (
          <div role="alert" className="px-3 py-2 bg-red-900/40 border border-red-800 text-red-300 text-sm rounded flex items-center justify-between gap-3">
            <span>{error}</span>
            <button
              type="button"
              onClick={load}
              className="px-2 py-1 text-xs bg-red-800/60 hover:bg-red-700 text-white rounded transition-colors cursor-pointer"
            >
              {t('retry')}
            </button>
          </div>
        )}

        {loading && !error && <GridSkeleton />}

        {!loading && !error && (
          <>
            {visibleCards.length === 0 ? (
              <p className="text-sm text-gray-400">{t('detail.empty')}</p>
            ) : (
              <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-3">
                {visibleCards.map(card => (
                  <CardTile
                    key={card.id}
                    card={card}
                    onClick={() => setExpandedCardId(prev => (prev === card.id ? null : card.id))}
                    t={t}
                  />
                ))}
              </div>
            )}

            {expandedCard && (
              <DetailPanel
                key={expandedCard.id}
                card={expandedCard}
                onClose={() => setExpandedCardId(null)}
                onSave={(variantId, payload) => handleSave(expandedCard.id, variantId, payload)}
                onUnmark={(collectionId) => handleUnmark(expandedCard.id, collectionId)}
                saving={savingCardId === expandedCard.id}
                t={t}
              />
            )}
          </>
        )}
      </div>
      <ToastList toasts={toasts} />
    </div>
  )
}
