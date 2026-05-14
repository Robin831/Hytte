import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Search, X } from 'lucide-react'
import ToastList from '../ToastList'
import { useToast } from '../../hooks/useToast'
import { formatNumber } from '../../utils/formatDate'

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

  const [isOpen, setIsOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Card[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [variantCard, setVariantCard] = useState<Card | null>(null)
  const [adding, setAdding] = useState(false)

  const inputRef = useRef<HTMLInputElement | null>(null)
  const overlayRef = useRef<HTMLDivElement | null>(null)

  const close = useCallback(() => {
    setIsOpen(false)
    setQuery('')
    setResults([])
    setError('')
    setVariantCard(null)
  }, [])

  // Autofocus the input when the panel opens and listen for Esc to close.
  useEffect(() => {
    if (!isOpen) return
    inputRef.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [isOpen, close])

  // Debounced autocomplete: each keystroke schedules a 200ms timer; the
  // previous timer and any in-flight request are cancelled when the query
  // changes, so only the latest non-empty query reaches the API.
  useEffect(() => {
    if (!isOpen) return
    const q = query.trim()
    if (q === '') {
      setResults([])
      setError('')
      setLoading(false)
      return
    }
    setLoading(true)
    const controller = new AbortController()
    const timer = setTimeout(() => {
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

  // Outside-click: ignore clicks whose target is inside the dialog. We compare
  // against overlayRef.current so clicks on the input or buttons don't close.
  const handleOverlayClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.target === overlayRef.current) close()
  }

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
      void addCard(card, card.variants[0].id)
    } else {
      setVariantCard(card)
    }
  }

  const trimmedQuery = query.trim()

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

      {isOpen && (
        <div
          ref={overlayRef}
          onClick={handleOverlayClick}
          role="presentation"
          data-testid="add-card-overlay"
          className="fixed inset-0 z-40 bg-black/60 flex items-end sm:items-center justify-center"
        >
          <div
            role="dialog"
            aria-modal="true"
            aria-label={t('addCard.dialogLabel')}
            className="w-full sm:max-w-lg bg-gray-900 border border-gray-800 rounded-t-2xl sm:rounded-2xl shadow-xl flex flex-col max-h-[85vh] sm:max-h-[80vh]"
          >
            <div className="flex items-center gap-2 px-4 py-3 border-b border-gray-800">
              <Search size={18} className="text-gray-400 shrink-0" aria-hidden="true" />
              <input
                ref={inputRef}
                type="text"
                value={query}
                onChange={e => setQuery(e.target.value)}
                placeholder={t('addCard.placeholder')}
                aria-label={t('addCard.inputLabel')}
                className="flex-1 min-w-0 bg-transparent border-0 outline-none text-white placeholder-gray-500"
              />
              <button
                type="button"
                onClick={close}
                aria-label={t('addCard.close')}
                className="p-1 text-gray-400 hover:text-white cursor-pointer"
              >
                <X size={18} />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto">
              {error && (
                <p role="alert" className="px-4 py-3 text-sm text-red-300">{error}</p>
              )}
              {!error && loading && trimmedQuery !== '' && (
                <p className="px-4 py-3 text-sm text-gray-400">{t('addCard.searching')}</p>
              )}
              {!error && !loading && trimmedQuery !== '' && results.length === 0 && (
                <p className="px-4 py-3 text-sm text-gray-400">{t('addCard.noResults')}</p>
              )}
              {!error && results.length > 0 && (
                <ul aria-label={t('addCard.results')}>
                  {results.map(card => {
                    const variant = card.variants[0]
                    return (
                      <li key={card.id}>
                        <button
                          type="button"
                          onClick={() => handleResultClick(card)}
                          disabled={adding}
                          data-testid={`add-card-result-${card.id}`}
                          className="flex w-full items-center gap-3 px-3 py-2 hover:bg-gray-800/60 disabled:cursor-not-allowed text-left cursor-pointer border-b border-gray-800 last:border-b-0"
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
                className="border-t border-gray-800 p-3 bg-gray-900/95 space-y-2"
              >
                <p className="text-sm text-gray-300">
                  {t('addCard.variantPickerPrompt', { name: variantCard.name })}
                </p>
                <div className="flex flex-wrap gap-2">
                  {variantCard.variants.map(v => (
                    <button
                      key={v.id}
                      type="button"
                      onClick={() => addCard(variantCard, v.id)}
                      disabled={adding}
                      data-testid={`add-card-variant-${v.id}`}
                      className="flex items-center gap-2 px-3 py-1.5 rounded border border-gray-700 hover:border-emerald-500 hover:bg-emerald-500/10 disabled:cursor-not-allowed text-sm text-white cursor-pointer"
                    >
                      <span>{t(`variantKind.${v.kind}`, { defaultValue: v.kind })}</span>
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
          </div>
        </div>
      )}

      <ToastList toasts={toasts} />
    </>
  )
}
