import { useCallback, useEffect, useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronUp, Search } from 'lucide-react'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../ui/dialog'
import { formatNumber } from '../../utils/formatDate'

const SEARCH_DEBOUNCE_MS = 250
const SEARCH_LIMIT = 20

export interface ScanDetailVariant {
  id: number
  kind: string
  price_eur: number
  price_nok: number | null
  owned?: boolean
}

export interface ScanDetailCard {
  id: string
  set_id: string
  set_name?: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: ScanDetailVariant[]
}

export interface ScanDetailSet {
  id: string
  name: string
}

export interface ScanDetailScan {
  id: number
  status: string
  confidence?: number | null
  matched_card?: ScanDetailCard | null
  set?: ScanDetailSet | null
  has_image: boolean
}

export interface ScanDetailResolveBody {
  action: 'add' | 'discard'
  variant_id?: number
  quantity?: number
  condition?: string
  notes?: string
  card_id?: string
}

interface ScanDetailModalProps {
  scan: ScanDetailScan
  busy: boolean
  onClose: () => void
  onResolve: (body: ScanDetailResolveBody) => void | Promise<void>
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

function confidencePercent(confidence: number | null | undefined): number | null {
  if (confidence == null || !Number.isFinite(confidence)) return null
  const pct = Math.round(confidence * 100)
  return Math.max(0, Math.min(100, pct))
}

// ScanDetailModal opens from a matched scan tile and gives the kid a fuller
// view of what the worker matched: a large scan image, the matched card
// metadata, variant pickers + Discard, and an opt-in "Wrong match?" section
// that lets them reassign the scan to a different card without having to
// discard and re-shoot. The reassignment posts a `card_id` override on the
// existing resolve endpoint. Scaffolding (focus trap, focus restoration,
// scroll lock, escape-to-close) is delegated to the shared Dialog component.
export default function ScanDetailModal({ scan, busy, onClose, onResolve }: ScanDetailModalProps) {
  const { t } = useTranslation('pokemon')
  const titleId = useId()
  const [wrongMatchOpen, setWrongMatchOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<ScanDetailCard[]>([])
  const [searching, setSearching] = useState(false)
  const [searchError, setSearchError] = useState('')
  const [overrideCard, setOverrideCard] = useState<ScanDetailCard | null>(null)

  const autoCard = scan.matched_card ?? null
  const activeCard = overrideCard ?? autoCard
  const autoVariant = autoCard?.variants?.[0]
  const pct = confidencePercent(scan.confidence)

  // Debounced /cards/search lookup. The previous timer and the in-flight
  // request are cancelled on every keystroke so stale results never overwrite
  // a fresher query.
  useEffect(() => {
    if (!wrongMatchOpen) return
    const q = query.trim()
    if (q === '') return
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      setSearching(true)
      setSearchError('')
      void (async () => {
        try {
          const res = await fetch(
            `/api/pokemon/cards/search?q=${encodeURIComponent(q)}&limit=${SEARCH_LIMIT}`,
            { credentials: 'include', signal: controller.signal },
          )
          if (!res.ok) throw new Error(t('scanned.detail.searchFailed'))
          const data: { cards?: ScanDetailCard[] } = await res.json()
          setResults(data.cards ?? [])
        } catch (err) {
          if ((err as { name?: string })?.name === 'AbortError' || controller.signal.aborted) return
          setSearchError(err instanceof Error ? err.message : t('scanned.detail.searchFailed'))
          setResults([])
        } finally {
          if (!controller.signal.aborted) setSearching(false)
        }
      })()
    }, SEARCH_DEBOUNCE_MS)
    return () => {
      controller.abort()
      window.clearTimeout(timer)
      setSearching(false)
    }
  }, [query, wrongMatchOpen, t])

  const handleSelectOverride = useCallback((card: ScanDetailCard) => {
    setOverrideCard(card)
  }, [])

  const handleRevertOverride = useCallback(() => {
    setOverrideCard(null)
  }, [])

  const handleAddVariant = useCallback(
    (variantId: number) => {
      const body: ScanDetailResolveBody = {
        action: 'add',
        variant_id: variantId,
        quantity: 1,
        condition: '',
        notes: '',
      }
      if (overrideCard) body.card_id = overrideCard.id
      void onResolve(body)
    },
    [onResolve, overrideCard],
  )

  const handleDiscard = useCallback(() => {
    void onResolve({ action: 'discard' })
  }, [onResolve])

  const variantsToShow = activeCard?.variants ?? []

  const trimmedQuery = query.trim()
  const displayResults = trimmedQuery !== '' ? results : []
  const displaySearching = trimmedQuery !== '' ? searching : false
  const displaySearchError = trimmedQuery !== '' ? searchError : ''

  // Auto-match summary line shared by the header and the override revert hint.
  const autoMatchSummary = useMemo(() => {
    if (!autoCard) return ''
    const setLabel = scan.set?.name ?? autoCard.set_name ?? autoCard.set_id
    return `${autoCard.name} · ${setLabel} · ${t('tile.collectorNo', { number: autoCard.collector_no })}`
  }, [autoCard, scan.set?.name, t])

  return (
    <Dialog
      open={true}
      onClose={onClose}
      maxWidth="sm:max-w-lg"
      overlayClassName="items-end sm:items-center p-0 sm:p-4 bg-black/70"
      className="rounded-t-2xl sm:rounded-2xl border-gray-800 max-h-[95vh] sm:max-h-[90vh]"
      aria-labelledby={titleId}
    >
      <div data-testid="scan-detail-modal" className="contents">
        <DialogHeader
          id={titleId}
          title={t('scanned.detail.title')}
          onClose={onClose}
          closeLabel={t('scanned.detail.close')}
        />

        <DialogBody className="space-y-4">
          <div className="flex justify-center bg-black/40 rounded-lg overflow-hidden">
            {scan.has_image ? (
              <img
                src={`/api/pokemon/scans/${scan.id}/image`}
                alt={t('scanned.thumbnailAlt')}
                loading="eager"
                data-testid="scan-detail-image"
                className="max-h-[55vh] w-auto object-contain"
              />
            ) : (
              <div
                data-testid="scan-detail-image-placeholder"
                className="py-12 px-4 text-sm text-gray-500"
              >
                {t('scanned.thumbnailPlaceholder')}
              </div>
            )}
          </div>

          {autoCard ? (
            <div className="space-y-1" data-testid="scan-detail-automatch">
              <p className="text-xs uppercase tracking-wide text-gray-500">
                {t('scanned.detail.autoMatchLabel')}
              </p>
              <p className="text-base font-medium text-white truncate" title={autoCard.name}>
                {autoCard.name}
              </p>
              <p className="text-sm text-gray-400">
                {scan.set?.name ?? autoCard.set_name ?? autoCard.set_id}
                {' · '}
                {t('tile.collectorNo', { number: autoCard.collector_no })}
              </p>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-gray-300">
                {pct != null && <span>{t('scanned.confidence', { pct })}</span>}
                {autoVariant && <span>{formatNok(autoVariant.price_nok)}</span>}
              </div>
            </div>
          ) : (
            <p className="text-sm text-gray-400">{t('scanned.detail.noMatchInfo')}</p>
          )}

          {overrideCard && (
            <div
              data-testid="scan-detail-override-banner"
              className="px-3 py-2 rounded border border-amber-500/40 bg-amber-500/10 text-xs text-amber-100 flex flex-wrap items-center gap-x-3 gap-y-1"
            >
              <span>
                {t('scanned.detail.overrideActive', { name: overrideCard.name })}
              </span>
              <button
                type="button"
                onClick={handleRevertOverride}
                disabled={busy}
                data-testid="scan-detail-revert"
                className="underline hover:text-white disabled:opacity-60 cursor-pointer"
              >
                {t('scanned.detail.useAutoMatchInstead')}
              </button>
            </div>
          )}

          {activeCard && variantsToShow.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs uppercase tracking-wide text-gray-500">
                {overrideCard ? t('scanned.detail.addToOverride') : t('scanned.detail.addToAutoMatch')}
              </p>
              <div
                role="group"
                aria-label={t('scanned.action.pickVariant')}
                data-testid="scan-detail-variants"
                className="flex flex-wrap gap-2"
              >
                {variantsToShow.map(v => (
                  <button
                    key={v.id}
                    type="button"
                    onClick={() => handleAddVariant(v.id)}
                    disabled={busy}
                    data-testid={`scan-detail-variant-${v.id}`}
                    className="flex items-center gap-2 px-3 py-1.5 rounded border border-gray-700 hover:border-emerald-500 hover:bg-emerald-500/10 disabled:cursor-not-allowed text-sm text-white cursor-pointer"
                  >
                    <span>{t(`variantKind.${v.kind}`, { defaultValue: v.kind })}</span>
                    <span className="text-xs text-gray-400">{formatNok(v.price_nok)}</span>
                  </button>
                ))}
              </div>
            </div>
          )}

          <div className="pt-2">
            <button
              type="button"
              onClick={() => setWrongMatchOpen(prev => !prev)}
              aria-expanded={wrongMatchOpen}
              data-testid="scan-detail-wrong-match-toggle"
              className="flex items-center gap-2 text-sm text-gray-300 hover:text-white cursor-pointer"
            >
              {wrongMatchOpen ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
              <span>{t('scanned.detail.wrongMatch')}</span>
            </button>

            {wrongMatchOpen && (
              <div
                data-testid="scan-detail-wrong-match-panel"
                className="mt-3 space-y-3 border-t border-gray-800 pt-3"
              >
                {autoCard && (
                  <p className="text-xs text-gray-500">
                    {t('scanned.detail.autoMatchHint', { match: autoMatchSummary })}
                  </p>
                )}
                <div className="flex items-center gap-2 px-3 py-2 rounded border border-gray-800 bg-gray-800/60">
                  <Search size={16} className="text-gray-400 shrink-0" aria-hidden="true" />
                  <input
                    type="text"
                    value={query}
                    onChange={e => setQuery(e.target.value)}
                    placeholder={t('scanned.detail.searchPlaceholder')}
                    aria-label={t('scanned.detail.searchPlaceholder')}
                    data-testid="scan-detail-search-input"
                    className="flex-1 min-w-0 bg-transparent border-0 outline-none text-sm text-white placeholder-gray-500"
                  />
                </div>
                {displaySearchError && (
                  <p role="alert" className="text-sm text-red-300">
                    {displaySearchError}
                  </p>
                )}
                {!displaySearchError && displaySearching && (
                  <p className="text-sm text-gray-400">{t('addCard.searching')}</p>
                )}
                {!displaySearchError && !displaySearching && trimmedQuery !== '' && displayResults.length === 0 && (
                  <p className="text-sm text-gray-400">{t('addCard.noResults')}</p>
                )}
                {displayResults.length > 0 && (
                  <ul
                    aria-label={t('addCard.results')}
                    data-testid="scan-detail-search-results"
                    className="divide-y divide-gray-800 border border-gray-800 rounded"
                  >
                    {displayResults.map(card => (
                      <li key={card.id} className="flex items-center gap-3 px-2 py-2 hover:bg-gray-800/60">
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
                        <button
                          type="button"
                          onClick={() => handleSelectOverride(card)}
                          disabled={busy}
                          data-testid={`scan-detail-pick-${card.id}`}
                          className="flex-1 min-w-0 text-left cursor-pointer disabled:cursor-not-allowed"
                        >
                          <p className="text-sm font-medium text-white truncate" title={card.name}>
                            {card.name}
                          </p>
                          <p className="text-xs text-gray-500 truncate">
                            {card.set_name ?? card.set_id}
                            {' · '}
                            {t('tile.collectorNo', { number: card.collector_no })}
                          </p>
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </div>
        </DialogBody>

        <DialogFooter>
          <button
            type="button"
            onClick={handleDiscard}
            disabled={busy}
            data-testid="scan-detail-discard"
            className="px-3 py-1.5 text-sm rounded bg-gray-700 hover:bg-gray-600 disabled:opacity-60 disabled:cursor-not-allowed text-white cursor-pointer"
          >
            {t('scanned.action.discard')}
          </button>
        </DialogFooter>
      </div>
    </Dialog>
  )
}
