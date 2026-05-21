import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ArrowLeft, Check, Trophy } from 'lucide-react'
import { Skeleton } from '../components/ui/skeleton'
import { formatNumber } from '../utils/formatDate'

interface Variant {
  id: number
  kind: string
  price_eur: number
  price_nok: number | null
}

interface TopCard {
  id: string
  set_id: string
  set_name: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: Variant[]
  top_variant_kind: string
  owned_by_me: boolean
}

type Filter = 'all' | 'owned' | 'missing'
const FILTERS: Filter[] = ['all', 'owned', 'missing']

// filterToQuery maps a UI filter to the ?owned= query value expected by the
// backend. The "All" filter sends owned=any so the server returns all cards.
function filterToQuery(filter: Filter): string {
  if (filter === 'owned') return 'owned'
  if (filter === 'missing') return 'missing'
  return 'any'
}

// 0 means "upstream price missing" rather than "this card is free" — see
// CardLightbox.formatNok for the full reasoning.
function formatNok(amount: number | null | undefined): string {
  if (amount == null || amount === 0) return '—'
  return formatNumber(amount, {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

function formatEur(amount: number | null | undefined): string {
  if (amount == null || amount === 0) return '—'
  return formatNumber(amount, {
    style: 'currency',
    currency: 'EUR',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

// topVariant returns the variant identified by top_variant_kind, so the label
// and price always refer to the same variant. Falls back to max-price selection
// only when top_variant_kind is absent (e.g. legacy data with no kind set).
function topVariant(card: TopCard): Variant | undefined {
  if (card.variants.length === 0) return undefined
  if (card.top_variant_kind) {
    const match = card.variants.find(v => v.kind === card.top_variant_kind)
    if (match) return match
  }
  return card.variants.reduce((best, current) =>
    current.price_eur > best.price_eur ? current : best,
  )
}

interface TileProps {
  card: TopCard
  rank: number
  onClick: () => void
  t: TFunction<'pokemon'>
}

// buildTileLabel composes a rich accessible name from the rank, card metadata,
// top variant, price, and ownership state so screen-reader users get the same
// information as sighted users (rank badge, checkmark, prominent price).
function buildTileLabel(card: TopCard, rank: number, t: TFunction<'pokemon'>): string {
  const top = topVariant(card)
  const variantKey = card.top_variant_kind || top?.kind || ''
  const variantLabel = variantKey
    ? t(`variantKind.${variantKey}`, { defaultValue: variantKey })
    : ''
  const priceNok = top?.price_nok ?? null
  const priceEur = top?.price_eur ?? null
  let priceText = ''
  if (priceNok != null && priceEur != null) {
    priceText = t('top.priceLabelDetailed', {
      nok: Math.round(priceNok),
      eur: Math.round(priceEur),
    })
  } else if (priceNok != null) {
    priceText = t('top.priceLabel', { nok: Math.round(priceNok) })
  } else if (priceEur != null) {
    priceText = `€${Math.round(priceEur)}`
  }
  const ownership = t(card.owned_by_me ? 'top.ownership.owned' : 'top.ownership.missing')
  return t('top.tileLabel', {
    rank,
    name: card.name,
    set: card.set_name,
    number: card.collector_no,
    variant: variantLabel,
    price: priceText,
    ownership,
  })
}

function TopCardTile({ card, rank, onClick, t }: TileProps) {
  const top = topVariant(card)
  const priceNok = top?.price_nok ?? null
  const priceEur = top?.price_eur ?? null
  const variantLabel = card.top_variant_kind
    ? t(`variantKind.${card.top_variant_kind}`, { defaultValue: card.top_variant_kind })
    : ''
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={`top-card-tile-${card.id}`}
      aria-label={buildTileLabel(card, rank, t)}
      className={`flex flex-col gap-2 p-2 rounded-lg border bg-gray-800/40 transition-colors text-left cursor-pointer
        ${card.owned_by_me
          ? 'border-emerald-500/70 ring-1 ring-emerald-500/40 hover:bg-gray-800/70'
          : 'border-gray-800 hover:border-gray-700 hover:bg-gray-800/70'
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
        <span
          aria-hidden="true"
          className="absolute top-1 left-1 px-1.5 py-0.5 rounded bg-black/60 text-xs font-semibold text-amber-300"
        >
          #{rank}
        </span>
        {card.owned_by_me && (
          <span
            aria-hidden="true"
            data-testid={`top-card-owned-${card.id}`}
            className="absolute top-1 right-1 flex items-center justify-center h-5 w-5 rounded-full bg-emerald-500 text-white shadow"
          >
            <Check size={12} />
          </span>
        )}
      </div>
      <div className="min-w-0 space-y-0.5">
        <p className="text-sm font-medium text-white truncate" title={card.name}>{card.name}</p>
        <p className="text-xs text-gray-500 truncate" title={card.set_name}>{card.set_name}</p>
        <p className="text-xs text-gray-500">{t('tile.collectorNo', { number: card.collector_no })}</p>
        {variantLabel && (
          <p className="text-xs text-amber-300/90">{variantLabel}</p>
        )}
        <p className="text-base font-semibold text-white pt-0.5">{formatNok(priceNok)}</p>
        {priceEur != null && (
          <p className="text-xs text-gray-400">({formatEur(priceEur)})</p>
        )}
      </div>
    </button>
  )
}

function GridSkeleton() {
  return (
    <div
      className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3"
      aria-busy="true"
    >
      {Array.from({ length: 8 }).map((_, i) => (
        <div key={i} className="p-2 bg-gray-800/40 border border-gray-800 rounded-lg space-y-2">
          <Skeleton className="aspect-[5/7] w-full" />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-3 w-1/2" />
          <Skeleton className="h-5 w-2/3" />
        </div>
      ))}
    </div>
  )
}

export default function PokemonTopPage() {
  const { t } = useTranslation('pokemon')
  const navigate = useNavigate()

  const [cards, setCards] = useState<TopCard[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [attempt, setAttempt] = useState(0)
  const filterButtonRefs = useRef<Array<HTMLButtonElement | null>>([])

  const reload = useCallback(() => {
    setLoading(true)
    setError('')
    setAttempt(a => a + 1)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const url = `/api/pokemon/top?owned=${filterToQuery(filter)}`
        const res = await fetch(url, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('errors.failedToLoad'))
        const data: { cards?: TopCard[] } = await res.json()
        setCards(data.cards ?? [])
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : t('errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [filter, attempt, t])

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

  const openCard = useCallback((card: TopCard) => {
    navigate(`/pokemon/sets/${encodeURIComponent(card.set_id)}#card-${encodeURIComponent(card.collector_no)}`)
  }, [navigate])

  const visibleCards = useMemo(() => cards, [cards])

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <div className="max-w-5xl mx-auto px-4 py-6 space-y-6">
        <header className="space-y-3">
          <Link
            to="/pokemon"
            className="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-white transition-colors"
          >
            <ArrowLeft size={16} aria-hidden="true" />
            {t('top.back')}
          </Link>
          <div className="flex items-center gap-3">
            <Trophy size={28} className="text-amber-400 shrink-0" aria-hidden="true" />
            <div className="min-w-0">
              <h1 className="text-2xl font-semibold truncate">{t('top.title')}</h1>
              <p className="text-sm text-gray-400">{t('top.subtitle')}</p>
            </div>
          </div>

          <div
            role="radiogroup"
            aria-label={t('top.filterLabel')}
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
                  tabIndex={checked ? 0 : -1}
                  onClick={() => selectFilter(i)}
                  onKeyDown={e => handleFilterKeyDown(e, i)}
                  data-testid={`top-filter-${f}`}
                  className={`px-3 py-1 text-xs rounded cursor-pointer transition-colors
                    ${checked ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white'}`}
                >
                  {t(`top.filter.${f}`)}
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
              onClick={reload}
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
              <p className="text-sm text-gray-400">{t('top.empty')}</p>
            ) : (
              <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
                {visibleCards.map((card, i) => (
                  <TopCardTile
                    key={card.id}
                    card={card}
                    rank={i + 1}
                    onClick={() => openCard(card)}
                    t={t}
                  />
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
