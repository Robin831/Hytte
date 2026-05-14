import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ArrowLeft, Check, Minus } from 'lucide-react'
import { Skeleton } from '../components/ui/skeleton'
import ToastList from '../components/ToastList'
import AddCardPanel from '../components/pokemon/AddCardPanel'
import CardLightbox from '../components/pokemon/CardLightbox'
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
const CONDITIONS = ['', 'mint', 'near_mint', 'lightly_played', 'moderately_played', 'heavily_played', 'damaged']

// VariantFilter narrows what "owned" means for the at-a-glance grid and the
// header completion stat. 'any' counts a card as owned if any variant is owned
// (today's default). 'all' is stricter: every variant must be owned. Any other
// value is a specific variant kind from the set (e.g. 'normal',
// 'reverse_holofoil').
type VariantFilter = 'any' | 'all' | string

// KNOWN_VARIANT_KIND_ORDER controls the chip ordering for the kinds we
// recognise; unknown kinds fall through to the end in alphabetical order.
const KNOWN_VARIANT_KIND_ORDER = ['normal', 'holofoil', 'reverse_holofoil', '1st_edition']

// computeCardOwnership returns the ownership view of a card under the active
// variant filter. `applicable` is false when the filter targets a specific
// kind and the card has no variant of that kind — the card is excluded from
// the completion denominator and rendered with a muted "not applicable"
// indicator.
interface CardOwnership {
  owned: boolean
  partial: boolean
  applicable: boolean
}

function computeCardOwnership(card: Card, filter: VariantFilter): CardOwnership {
  if (filter === 'any') {
    return {
      owned: card.variants.some(v => v.owned),
      partial: false,
      applicable: true,
    }
  }
  if (filter === 'all') {
    if (card.variants.length === 0) return { owned: false, partial: false, applicable: true }
    const ownedCount = card.variants.reduce((n, v) => (v.owned ? n + 1 : n), 0)
    return {
      owned: ownedCount === card.variants.length,
      partial: ownedCount > 0 && ownedCount < card.variants.length,
      applicable: true,
    }
  }
  const variant = card.variants.find(v => v.kind === filter)
  if (!variant) return { owned: false, partial: false, applicable: false }
  return { owned: variant.owned, partial: false, applicable: true }
}

// computeCompletion returns the owned / total counts under the active filter.
// 'any' counts every card; specific-kind filters only count cards that have
// that kind in their variants list (so unique variants like Reverse Holo
// don't drag the denominator down for ultra rares).
interface Completion {
  owned: number
  total: number
}

function computeCompletion(cards: Card[], filter: VariantFilter): Completion {
  if (filter === 'any') {
    let owned = 0
    for (const card of cards) {
      if (card.variants.some(v => v.owned)) owned++
    }
    return { owned, total: cards.length }
  }
  if (filter === 'all') {
    let owned = 0
    for (const card of cards) {
      if (card.variants.length === 0) continue
      if (card.variants.every(v => v.owned)) owned++
    }
    return { owned, total: cards.length }
  }
  let owned = 0
  let total = 0
  for (const card of cards) {
    const variant = card.variants.find(v => v.kind === filter)
    if (!variant) continue
    total++
    if (variant.owned) owned++
  }
  return { owned, total }
}

// computeSetValue sums the NOK price of the owned variants in scope for the
// active filter. 'any' sums every owned variant on every card (the
// pre-existing behaviour). Kind filters sum only that variant. 'all' sums
// every owned variant, same as 'any' — under the strictest filter we still
// reflect total monetary value.
function computeSetValue(cards: Card[], filter: VariantFilter): number {
  let total = 0
  for (const card of cards) {
    for (const variant of card.variants) {
      if (!variant.owned || variant.price_nok == null) continue
      if (filter !== 'any' && filter !== 'all' && variant.kind !== filter) continue
      total += variant.price_nok * Math.max(variant.quantity, 1)
    }
  }
  return total
}

// availableVariantKinds returns the variant kinds present in the set, ordered
// by KNOWN_VARIANT_KIND_ORDER then alphabetically. Empty when no card has any
// variant (a degenerate set).
function availableVariantKinds(cards: Card[]): string[] {
  const seen = new Set<string>()
  for (const card of cards) {
    for (const variant of card.variants) {
      if (variant.kind) seen.add(variant.kind)
    }
  }
  return Array.from(seen).sort((a, b) => {
    const ia = KNOWN_VARIANT_KIND_ORDER.indexOf(a)
    const ib = KNOWN_VARIANT_KIND_ORDER.indexOf(b)
    const ra = ia === -1 ? KNOWN_VARIANT_KIND_ORDER.length : ia
    const rb = ib === -1 ? KNOWN_VARIANT_KIND_ORDER.length : ib
    if (ra !== rb) return ra - rb
    return a.localeCompare(b)
  })
}

// hasMultiVariantCard returns true when at least one card in the set carries
// more than one variant — used to decide whether the "All variants" chip
// would be meaningfully different from "Any" (it would not, if every card has
// a single variant).
function hasMultiVariantCard(cards: Card[]): boolean {
  return cards.some(c => c.variants.length > 1)
}

// availableVariantFilters builds the chip list dynamically. 'any' is always
// present; per-kind chips appear when at least one card carries that kind;
// 'all' only appears when there is at least one card with multiple variants.
function availableVariantFilters(cards: Card[]): VariantFilter[] {
  const filters: VariantFilter[] = ['any']
  for (const kind of availableVariantKinds(cards)) {
    filters.push(kind)
  }
  if (hasMultiVariantCard(cards)) filters.push('all')
  return filters
}

// variantFilterLabelKey maps a filter slug to its i18n key. Aggregate filters
// have explicit keys; per-kind filters reuse variantKind.* with a fallback
// for kinds we don't have a localized label for.
function variantFilterLabelKey(filter: VariantFilter): string {
  if (filter === 'any') return 'set.variantFilter.any'
  if (filter === 'all') return 'set.variantFilter.allVariants'
  if (filter === 'normal') return 'set.variantFilter.normal'
  if (filter === 'reverse_holofoil') return 'set.variantFilter.reverseHolo'
  return `variantKind.${filter}`
}

// variantCompletionLabelKey selects which sentence to render below the owned
// count, e.g. "12 / 195 cards (any variant)".
function variantCompletionLabelKey(filter: VariantFilter): string {
  if (filter === 'any') return 'set.completion.any'
  if (filter === 'all') return 'set.completion.allVariants'
  if (filter === 'normal') return 'set.completion.normal'
  if (filter === 'reverse_holofoil') return 'set.completion.reverseHolo'
  return 'set.completion.kind'
}

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
// the first variant as the canonical one. When a per-kind filter is active
// we prefer that variant so the tile's price reflects what the user is
// looking at.
function defaultVariant(card: Card, filter: VariantFilter): Variant | undefined {
  if (filter !== 'any' && filter !== 'all') {
    const v = card.variants.find(x => x.kind === filter)
    if (v) return v
  }
  return card.variants[0]
}

interface TileProps {
  card: Card
  filter: VariantFilter
  onClick: () => void
  t: TFunction<'pokemon'>
}

function CardTile({ card, filter, onClick, t }: TileProps) {
  const ownership = computeCardOwnership(card, filter)
  const { owned, partial, applicable } = ownership
  const variant = defaultVariant(card, filter)
  const price = variant?.price_nok
  // Border state:
  // - `owned` → emerald ring
  // - `partial` (under 'all' filter, some but not every variant) → amber ring
  // - `!applicable` (per-kind filter, kind not present on this card) → muted
  //   border, no checkmark; tile is informative but not counted
  // - otherwise → default missing styling
  let tileClass: string
  if (!applicable) {
    tileClass = 'border-gray-800 opacity-40 hover:opacity-70 hover:border-gray-700 hover:bg-gray-800/70'
  } else if (owned) {
    tileClass = 'border-emerald-500/70 ring-1 ring-emerald-500/40 hover:bg-gray-800/70'
  } else if (partial) {
    tileClass = 'border-amber-500/70 ring-1 ring-amber-500/40 hover:bg-gray-800/70'
  } else {
    tileClass = 'border-gray-800 opacity-60 hover:opacity-100 hover:border-gray-700 hover:bg-gray-800/70'
  }
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={`card-tile-${card.id}`}
      aria-label={t('tile.openCard', { name: card.name, number: card.collector_no })}
      aria-pressed={applicable ? owned : false}
      data-ownership={!applicable ? 'na' : owned ? 'owned' : partial ? 'partial' : 'missing'}
      className={`flex flex-col gap-2 p-2 rounded-lg border bg-gray-800/40 transition-colors text-left cursor-pointer ${tileClass}`}
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
        {applicable && owned && (
          <span
            aria-hidden="true"
            className="absolute top-1 right-1 flex items-center justify-center h-5 w-5 rounded-full bg-emerald-500 text-white shadow"
          >
            <Check size={12} />
          </span>
        )}
        {applicable && !owned && partial && (
          <span
            aria-hidden="true"
            className="absolute top-1 right-1 flex items-center justify-center h-5 w-5 rounded-full bg-amber-500 text-white shadow"
          >
            <Minus size={12} />
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

interface SavePayload {
  quantity: number
  condition: string
  notes: string
}

interface LightboxActionBarProps {
  card: Card
  onSave: (variantId: number, payload: SavePayload) => Promise<void>
  onUnmark: (collectionId: number) => Promise<void>
  saving: boolean
  t: TFunction<'pokemon'>
}

// LightboxActionBar renders the add/edit/remove controls inside the CardLightbox.
function LightboxActionBar({ card, onSave, onUnmark, saving, t }: LightboxActionBarProps) {
  const initialVariantId = useMemo(() => {
    const ownedV = card.variants.find(v => v.owned)
    return (ownedV ?? card.variants[0])?.id ?? 0
  }, [card])
  const [selectedVariantId, setSelectedVariantId] = useState<number>(initialVariantId)
  const selected = useMemo(
    () => card.variants.find(v => v.id === selectedVariantId) ?? card.variants[0],
    [card, selectedVariantId],
  )

  // Re-sync the selection when the user navigates to a different card.
  const [prevCardId, setPrevCardId] = useState(card.id)
  if (card.id !== prevCardId) {
    setPrevCardId(card.id)
    setSelectedVariantId(initialVariantId)
  }

  // Editable fields — kept in local state so the user can change them before saving.
  const [editQuantity, setEditQuantity] = useState(Math.max(selected?.quantity || 1, 1))
  const [editCondition, setEditCondition] = useState(selected?.condition || '')
  const [editNotes, setEditNotes] = useState(selected?.notes || '')
  // Sync edit fields whenever the selected variant changes (card navigation or variant pick).
  const [prevSelectedId, setPrevSelectedId] = useState(selectedVariantId)
  if (selectedVariantId !== prevSelectedId) {
    setPrevSelectedId(selectedVariantId)
    setEditQuantity(Math.max(selected?.quantity || 1, 1))
    setEditCondition(selected?.condition || '')
    setEditNotes(selected?.notes || '')
  }

  if (!selected) return null

  const handleSave = (e: React.FormEvent) => {
    e.preventDefault()
    void onSave(selected.id, {
      quantity: editQuantity,
      condition: editCondition,
      notes: editNotes,
    })
  }

  const handleUnmark = () => {
    if (selected.owned_id != null) {
      void onUnmark(selected.owned_id)
    }
  }

  return (
    <form
      onSubmit={handleSave}
      className="flex flex-col gap-2 bg-gray-900/80 border border-gray-700 rounded-lg p-3 backdrop-blur-sm"
    >
      {card.variants.length > 1 && (
        <fieldset className="space-y-1.5">
          <legend className="text-xs font-medium text-gray-300">{t('detail.variant')}</legend>
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
      )}

      {selected.owned && (
        <>
          <div className="flex items-center gap-2">
            <span className="text-xs text-gray-300 w-20 shrink-0">{t('detail.quantity')}</span>
            <div className="flex items-center gap-1">
              <button
                type="button"
                onClick={() => setEditQuantity(q => Math.max(1, q - 1))}
                disabled={saving || editQuantity <= 1}
                aria-label={t('detail.decreaseQuantity')}
                className="w-7 h-7 flex items-center justify-center rounded border border-gray-700 text-gray-300 hover:border-gray-500 hover:text-white disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
              >−</button>
              <input
                type="number"
                min={1}
                max={99}
                value={editQuantity}
                onChange={e => setEditQuantity(Math.max(1, Math.min(99, Number(e.target.value) || 1)))}
                disabled={saving}
                className="w-12 text-center bg-gray-800 border border-gray-700 rounded text-sm text-white py-0.5"
              />
              <button
                type="button"
                onClick={() => setEditQuantity(q => Math.min(99, q + 1))}
                disabled={saving || editQuantity >= 99}
                aria-label={t('detail.increaseQuantity')}
                className="w-7 h-7 flex items-center justify-center rounded border border-gray-700 text-gray-300 hover:border-gray-500 hover:text-white disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
              >+</button>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs text-gray-300 w-20 shrink-0">{t('detail.condition')}</span>
            <select
              value={editCondition}
              onChange={e => setEditCondition(e.target.value)}
              disabled={saving}
              className="flex-1 bg-gray-800 border border-gray-700 rounded text-sm text-white py-0.5 px-1"
            >
              {CONDITIONS.map(c => (
                <option key={c} value={c}>
                  {t(`condition.${c || 'unset'}` as never)}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-xs text-gray-300">{t('detail.notes')}</span>
            <textarea
              value={editNotes}
              onChange={e => setEditNotes(e.target.value)}
              disabled={saving}
              rows={2}
              placeholder={t('detail.notesPlaceholder')}
              className="w-full bg-gray-800 border border-gray-700 rounded text-sm text-white px-2 py-1 resize-none"
            />
          </div>
        </>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {!selected.owned && (
          <button
            type="submit"
            disabled={saving}
            className="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-500 disabled:bg-emerald-800/60 disabled:cursor-not-allowed text-white rounded text-sm cursor-pointer"
          >
            {t('detail.markOwned')}
          </button>
        )}
        {selected.owned && (
          <button
            type="submit"
            disabled={saving}
            className="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-500 disabled:bg-emerald-800/60 disabled:cursor-not-allowed text-white rounded text-sm cursor-pointer"
          >
            {t('detail.update')}
          </button>
        )}
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
  const [searchParams, setSearchParams] = useSearchParams()

  const [set, setSet] = useState<PokemonSet | null>(null)
  const [cards, setCards] = useState<Card[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [lightboxStartIndex, setLightboxStartIndex] = useState<number | null>(null)
  const [savingCardId, setSavingCardId] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)
  const cardsRef = useRef<Card[]>([])
  useLayoutEffect(() => { cardsRef.current = cards })
  const filterButtonRefs = useRef<Array<HTMLButtonElement | null>>([])
  const variantFilterButtonRefs = useRef<Array<HTMLButtonElement | null>>([])

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

  // Variant filter state lives in the URL (?variant=...). Default is 'any'.
  // We read raw, then validate against the set's available filters on render
  // so an unknown or stale value gracefully falls back to 'any' without
  // touching the URL until the user picks a real chip.
  const rawVariantParam = searchParams.get('variant')
  const variantFilters = useMemo(() => availableVariantFilters(cards), [cards])
  const variantFilter: VariantFilter =
    rawVariantParam && variantFilters.includes(rawVariantParam) ? rawVariantParam : 'any'

  const selectVariantFilter = useCallback(
    (next: VariantFilter) => {
      setSearchParams(
        prev => {
          const params = new URLSearchParams(prev)
          if (next === 'any') {
            params.delete('variant')
          } else {
            params.set('variant', next)
          }
          return params
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const handleVariantFilterKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLButtonElement>, index: number) => {
      const move = (next: number) => {
        const f = variantFilters[next]
        if (!f) return
        e.preventDefault()
        selectVariantFilter(f)
        variantFilterButtonRefs.current[next]?.focus()
      }
      switch (e.key) {
        case 'ArrowRight':
        case 'ArrowDown':
          move((index + 1) % variantFilters.length)
          break
        case 'ArrowLeft':
        case 'ArrowUp':
          move((index - 1 + variantFilters.length) % variantFilters.length)
          break
        case 'Home':
          move(0)
          break
        case 'End':
          move(variantFilters.length - 1)
          break
      }
    },
    [variantFilters, selectVariantFilter],
  )

  const completion = useMemo(() => computeCompletion(cards, variantFilter), [cards, variantFilter])
  const setValueNok = useMemo(() => computeSetValue(cards, variantFilter), [cards, variantFilter])
  // We expose the variant-aware denominator on the header so the user always
  // knows what they're looking at. For the "Total cards" tile we still show
  // the printed set total when known, since that's a property of the set
  // itself rather than the active filter.
  const totalCards = set?.total_cards ?? cards.length

  const visibleCards = useMemo(() => {
    if (filter === 'owned') {
      return cards.filter(c => {
        const o = computeCardOwnership(c, variantFilter)
        return o.applicable && o.owned
      })
    }
    if (filter === 'missing') {
      return cards.filter(c => {
        const o = computeCardOwnership(c, variantFilter)
        return o.applicable && !o.owned
      })
    }
    return cards
  }, [cards, filter, variantFilter])

  // Clamp the lightbox start index to the visible list length during render so
  // we never pass a stale out-of-range pointer to CardLightbox.
  const lightboxSafeIndex =
    lightboxStartIndex != null && visibleCards.length > 0
      ? Math.min(lightboxStartIndex, visibleCards.length - 1)
      : null

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
                {t('detail.ownedOf', { owned: completion.owned, total: completion.total })}
              </dd>
              <dd
                className="text-[11px] text-gray-400 mt-0.5"
                data-testid="owned-count-label"
              >
                {t(variantCompletionLabelKey(variantFilter), {
                  owned: completion.owned,
                  total: completion.total,
                })}
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

          {variantFilters.length > 1 && (
            <div className="flex flex-wrap items-center gap-2">
              <span
                id="variant-filter-label"
                className="text-xs text-gray-400"
              >
                {t('set.variantFilter.label')}
              </span>
              <div
                role="radiogroup"
                aria-labelledby="variant-filter-label"
                className="flex flex-wrap gap-1.5"
                data-testid="variant-filter"
              >
                {variantFilters.map((f, i) => {
                  const checked = f === variantFilter
                  return (
                    <button
                      key={f}
                      ref={el => { variantFilterButtonRefs.current[i] = el }}
                      type="button"
                      role="radio"
                      aria-checked={checked}
                      tabIndex={checked ? 0 : -1}
                      onClick={() => selectVariantFilter(f)}
                      onKeyDown={e => handleVariantFilterKeyDown(e, i)}
                      data-variant={f}
                      className={`px-2.5 py-1 text-xs rounded-full border cursor-pointer transition-colors
                        ${checked
                          ? 'bg-emerald-600/30 border-emerald-500/70 text-white'
                          : 'bg-gray-800/60 border-gray-700 text-gray-300 hover:text-white hover:border-gray-600'}`}
                    >
                      {t(variantFilterLabelKey(f), { defaultValue: f })}
                    </button>
                  )
                })}
              </div>
            </div>
          )}

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
                {visibleCards.map((card, i) => (
                  <CardTile
                    key={card.id}
                    card={card}
                    filter={variantFilter}
                    onClick={() => setLightboxStartIndex(i)}
                    t={t}
                  />
                ))}
              </div>
            )}
          </>
        )}
      </div>
      {lightboxSafeIndex != null && (
        <CardLightbox<Card>
          cards={visibleCards}
          startIndex={lightboxSafeIndex}
          onClose={() => setLightboxStartIndex(null)}
          showPrice
          renderActionBar={(card) => (
            <LightboxActionBar
              card={card}
              onSave={(variantId, payload) => handleSave(card.id, variantId, payload)}
              onUnmark={(collectionId) => handleUnmark(card.id, collectionId)}
              saving={savingCardId === card.id}
              t={t}
            />
          )}
        />
      )}
      <ToastList toasts={toasts} />
      <AddCardPanel onAdded={load} />
    </div>
  )
}
