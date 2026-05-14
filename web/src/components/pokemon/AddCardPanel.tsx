import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Camera, Plus, Search, X } from 'lucide-react'
import { Dialog } from '../ui/dialog'
import ToastList from '../ToastList'
import { useToast } from '../../hooks/useToast'
import { formatNumber } from '../../utils/formatDate'
import { useAuth } from '../../auth'
import CardScanner from './CardScanner'
import CardLightbox from './CardLightbox'

interface Variant {
  id: number
  kind: string
  price_eur: number
  price_nok: number | null
  owned: boolean
  owned_id?: number | null
  quantity: number
  condition: string
  notes: string
}

interface Card {
  id: string
  set_id: string
  set_name?: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: Variant[]
}

const DEBOUNCE_MS = 200
const SEARCH_LIMIT = 20

function formatNok(amount: number | null | undefined): string {
  if (amount == null) return '—'
  return formatNumber(amount, {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

interface AddCardPanelProps {
  onAdded?: () => void
}

export default function AddCardPanel({ onAdded }: AddCardPanelProps) {
  const { t } = useTranslation('pokemon')
  const { toasts, showToast } = useToast()
  const { user } = useAuth()

  const [isOpen, setIsOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Card[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [variantCard, setVariantCard] = useState<Card | null>(null)
  const [adding, setAdding] = useState(false)
  const [scannerOpen, setScannerOpen] = useState(false)
  const [lightboxStartIndex, setLightboxStartIndex] = useState<number | null>(null)

  const close = useCallback(() => {
    setIsOpen(false)
    setScannerOpen(false)
    setQuery('')
    setResults([])
    setError('')
    setVariantCard(null)
    setLightboxStartIndex(null)
  }, [])

  // Debounced autocomplete: each keystroke schedules a 200ms timer; the
  // previous timer and any in-flight request are cancelled when the query
  // changes. Stale results from the previous query are cleared at the start
  // of each search so the dropdown never shows results that don't match the
  // current input value while loading.
  useEffect(() => {
    const q = query.trim()
    if (!isOpen || q === '') return
    const controller = new AbortController()
    const timer = setTimeout(() => {
      setLoading(true)
      setResults([])
      setError('')
      void (async () => {
        try {
          const res = await fetch(
            `/api/pokemon/cards/search?q=${encodeURIComponent(q)}&limit=${SEARCH_LIMIT}`,
            { credentials: 'include', signal: controller.signal },
          )
          if (!res.ok) throw new Error(t('addCard.errors.searchFailed'))
          const data: { cards?: Card[] } = await res.json()
          setResults(data.cards ?? [])
          setError('')
        } catch (err) {
          if (err instanceof Error && err.name === 'AbortError') return
          setError(err instanceof Error ? err.message : t('addCard.errors.searchFailed'))
          setResults([])
        } finally {
          if (!controller.signal.aborted) setLoading(false)
        }
      })()
    }, DEBOUNCE_MS)
    return () => {
      controller.abort()
      clearTimeout(timer)
    }
  }, [query, isOpen, t])

  const addCard = useCallback(
    async (card: Card, variantId: number) => {
      setAdding(true)
      try {
        const res = await fetch('/api/pokemon/collection', {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            card_id: card.id,
            variant_id: variantId,
            quantity: 1,
            condition: '',
            notes: '',
          }),
        })
        if (!res.ok) throw new Error(t('addCard.errors.addFailed'))
        showToast(t('addCard.toast.added', { name: card.name }), 'success')
        setVariantCard(null)
        onAdded?.()
      } catch (err) {
        showToast(err instanceof Error ? err.message : t('addCard.errors.addFailed'), 'error')
      } finally {
        setAdding(false)
      }
    },
    [onAdded, showToast, t],
  )

  const handleResultClick = (card: Card) => {
    if (card.variants.length === 0) return
    if (card.variants.length === 1) {
      const v = card.variants[0]
      if (v.owned) {
        showToast(t('addCard.toast.alreadyOwned', { name: card.name }), 'warning')
        return
      }
      void addCard(card, v.id)
    } else {
      setVariantCard(card)
    }
  }

  const trimmedQuery = query.trim()
  const showResults = !loading && !error && trimmedQuery !== '' && results.length > 0
  const showEmpty = !loading && !error && trimmedQuery !== '' && results.length === 0

  return (
    <>
      <button
        type="button"
        onClick={() => setIsOpen(true)}
        aria-label={t('addCard.openLabel')}
        data-testid="add-card-fab"
        className="fixed bottom-6 right-6 z-30 flex items-center justify-center h-12 w-12 rounded-full bg-emerald-600 hover:bg-emerald-500 text-white shadow-lg transition-colors cursor-pointer"
      >
        <Plus size={24} />
      </button>

      <Dialog
        open={isOpen && !scannerOpen}
        onClose={close}
        maxWidth="sm:max-w-lg"
        overlayClassName="items-end sm:items-center p-0 sm:p-4"
        className="rounded-t-2xl sm:rounded-2xl border-gray-800 max-h-[85vh] sm:max-h-[80vh]"
        aria-label={t('addCard.dialogLabel')}
      >
        <div className="flex items-center gap-2 px-4 py-3 border-b border-gray-800 shrink-0">
          <Search size={18} className="text-gray-400 shrink-0" aria-hidden="true" />
          <input
            type="text"
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder={t('addCard.placeholder')}
            aria-label={t('addCard.inputLabel')}
            className="flex-1 min-w-0 bg-transparent border-0 outline-none text-white placeholder-gray-500"
          />
          {user?.is_admin && (
            <button
              type="button"
              onClick={() => setScannerOpen(true)}
              aria-label={t('addCard.scan')}
              data-testid="add-card-scan"
              className="p-1 text-gray-400 hover:text-white cursor-pointer"
            >
              <Camera size={18} />
            </button>
          )}
          <button
            type="button"
            onClick={close}
            aria-label={t('addCard.close')}
            className="p-1 text-gray-400 hover:text-white cursor-pointer"
          >
            <X size={18} />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto" aria-busy={loading}>
          {error && trimmedQuery !== '' && (
            <p role="alert" className="px-4 py-3 text-sm text-red-300">{error}</p>
          )}
          {!error && loading && trimmedQuery !== '' && (
            <p className="px-4 py-3 text-sm text-gray-400">{t('addCard.searching')}</p>
          )}
          {showEmpty && (
            <p className="px-4 py-3 text-sm text-gray-400">{t('addCard.noResults')}</p>
          )}
          {showResults && (
            <ul aria-label={t('addCard.results')}>
              {results.map((card, i) => {
                const variant = card.variants[0]
                return (
                  <li key={card.id} className="flex items-stretch border-b border-gray-800 last:border-b-0 hover:bg-gray-800/60">
                    <button
                      type="button"
                      onClick={() => setLightboxStartIndex(i)}
                      aria-label={t('addCard.previewCard', { name: card.name })}
                      data-testid={`add-card-preview-${card.id}`}
                      className="shrink-0 flex items-center justify-center px-3 py-2 cursor-pointer"
                    >
                      <div className="h-14 w-10 shrink-0 flex items-center justify-center bg-gray-800/40 rounded overflow-hidden">
                        {card.image_small_url ? (
                          <img
                            src={card.image_small_url}
                            alt=""
                            loading="lazy"
                            className="max-h-full max-w-full object-contain"
                          />
                        ) : null}
                      </div>
                    </button>
                    <button
                      type="button"
                      onClick={() => handleResultClick(card)}
                      disabled={adding}
                      data-testid={`add-card-result-${card.id}`}
                      className="flex flex-1 min-w-0 items-center gap-3 pr-3 py-2 disabled:cursor-not-allowed text-left cursor-pointer"
                    >
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-white truncate">{card.name}</p>
                        <p className="text-xs text-gray-500 truncate">
                          {t('tile.collectorNo', { number: card.collector_no })}
                          {card.set_name ? ` · ${card.set_name}` : ''}
                        </p>
                      </div>
                      <span className="text-xs text-gray-300 shrink-0">{formatNok(variant?.price_nok)}</span>
                    </button>
                  </li>
                )
              })}
            </ul>
          )}
        </div>

        {variantCard && (
          <div
            role="group"
            aria-label={t('addCard.variantPickerLabel', { name: variantCard.name })}
            className="border-t border-gray-800 p-3 bg-gray-900/95 space-y-2 shrink-0"
          >
            <p className="text-sm text-gray-300">
              {t('addCard.variantPickerPrompt', { name: variantCard.name })}
            </p>
            <div className="flex flex-wrap gap-2">
              {variantCard.variants.map(v => (
                <button
                  key={v.id}
                  type="button"
                  onClick={() => {
                    if (v.owned) {
                      showToast(t('addCard.toast.alreadyOwned', { name: variantCard.name }), 'warning')
                      return
                    }
                    void addCard(variantCard, v.id)
                  }}
                  disabled={adding}
                  data-testid={`add-card-variant-${v.id}`}
                  className="flex items-center gap-2 px-3 py-1.5 rounded border border-gray-700 hover:border-emerald-500 hover:bg-emerald-500/10 disabled:cursor-not-allowed text-sm text-white cursor-pointer"
                >
                  <span>{t(`variantKind.${v.kind}`, { defaultValue: v.kind })}</span>
                  {v.owned && <span className="text-xs text-emerald-400">✓</span>}
                  <span className="text-xs text-gray-400">{formatNok(v.price_nok)}</span>
                </button>
              ))}
              <button
                type="button"
                onClick={() => setVariantCard(null)}
                disabled={adding}
                className="ml-auto px-3 py-1.5 text-sm text-gray-400 hover:text-white cursor-pointer"
              >
                {t('addCard.cancel')}
              </button>
            </div>
          </div>
        )}
      </Dialog>

      {scannerOpen && user?.is_admin && (
        <CardScanner
          onClose={() => setScannerOpen(false)}
          onAdded={onAdded}
          onEnterManually={prefill => {
            setScannerOpen(false)
            const seed = prefill.collectorNumber?.trim() || prefill.setName?.trim() || ''
            setQuery(seed)
            setResults([])
            setError('')
            setVariantCard(null)
          }}
        />
      )}

      {lightboxStartIndex != null && results.length > 0 && (
        <CardLightbox<Card>
          cards={results}
          startIndex={lightboxStartIndex}
          onClose={() => setLightboxStartIndex(null)}
          showPrice
          renderActionBar={(card) => {
            const ownedAlready = card.variants.length === 1 && card.variants[0]?.owned
            return (
              <div className="flex flex-wrap items-center gap-2 bg-gray-900/80 border border-gray-700 rounded-lg p-3 backdrop-blur-sm">
                {card.variants.length > 1 ? (
                  <>
                    <span className="text-xs text-gray-300 mr-1">
                      {t('addCard.variantPickerPrompt', { name: card.name })}
                    </span>
                    {card.variants.map(v => (
                      <button
                        key={v.id}
                        type="button"
                        onClick={() => {
                          if (v.owned) {
                            showToast(t('addCard.toast.alreadyOwned', { name: card.name }), 'warning')
                            return
                          }
                          void addCard(card, v.id)
                        }}
                        disabled={adding}
                        data-testid={`lightbox-add-variant-${v.id}`}
                        className="flex items-center gap-2 px-3 py-1.5 rounded border border-gray-700 hover:border-emerald-500 hover:bg-emerald-500/10 disabled:cursor-not-allowed text-sm text-white cursor-pointer"
                      >
                        <span>{t(`variantKind.${v.kind}`, { defaultValue: v.kind })}</span>
                        {v.owned && <span className="text-xs text-emerald-400">✓</span>}
                        <span className="text-xs text-gray-400">{formatNok(v.price_nok)}</span>
                      </button>
                    ))}
                  </>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      const v = card.variants[0]
                      if (!v) return
                      if (v.owned) {
                        showToast(t('addCard.toast.alreadyOwned', { name: card.name }), 'warning')
                        return
                      }
                      void addCard(card, v.id)
                    }}
                    disabled={adding || ownedAlready}
                    data-testid="lightbox-add-card"
                    className="flex items-center gap-2 px-3 py-1.5 rounded bg-emerald-600 hover:bg-emerald-500 disabled:bg-emerald-800/60 disabled:cursor-not-allowed text-sm text-white cursor-pointer"
                  >
                    {ownedAlready ? t('addCard.toast.alreadyOwned', { name: card.name }) : t('detail.markOwned')}
                  </button>
                )}
              </div>
            )
          }}
        />
      )}

      <ToastList toasts={toasts} />
    </>
  )
}
