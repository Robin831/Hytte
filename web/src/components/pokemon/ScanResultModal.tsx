import { useTranslation } from 'react-i18next'
import { Edit3, Plus, RotateCcw } from 'lucide-react'
import { formatNumber } from '../../utils/formatDate'

export interface ScanVariant {
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

export interface ScanCard {
  id: string
  set_id: string
  set_name?: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: ScanVariant[]
}

export interface ScanSet {
  id: string
  name: string
}

export interface ScanCandidate {
  card: ScanCard
  set?: ScanSet
  score: number
}

export type ScanResult =
  | { matched: true; confidence: number; candidates: ScanCandidate[] }
  | {
      matched: false
      confidence: number
      reason: string
      set_name?: string
      collector_number?: string
    }

interface ScanResultModalProps {
  result: ScanResult
  busy?: boolean
  onAddCandidate: (candidate: ScanCandidate) => void
  onTryAgain: () => void
  onEnterManually: (prefill: { setName?: string; collectorNumber?: string }) => void
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

// firstAddableVariant returns the first not-owned variant, or variants[0] when
// all are owned. The fallback is only used for price display; callers that add
// to the collection must check v.owned on the returned value.
function firstAddableVariant(card: ScanCard): ScanVariant | undefined {
  return card.variants.find(v => !v.owned) ?? card.variants[0]
}

export default function ScanResultModal({
  result,
  busy,
  onAddCandidate,
  onTryAgain,
  onEnterManually,
}: ScanResultModalProps) {
  const { t } = useTranslation('pokemon')

  if (result.matched && result.candidates.length === 1) {
    const candidate = result.candidates[0]
    const card = candidate.card
    const setName = candidate.set?.name ?? card.set_name ?? ''
    return (
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('scanner.result.singleLabel', { name: card.name })}
        data-testid="scan-result-modal"
        className="absolute inset-0 z-10 flex items-center justify-center bg-black/80 p-4"
      >
        <div className="w-full max-w-sm bg-gray-900 border border-gray-700 rounded-lg shadow-xl flex flex-col">
          <div className="flex gap-4 p-4">
            <div className="h-32 w-24 shrink-0 flex items-center justify-center bg-gray-800/40 rounded overflow-hidden">
              {card.image_small_url ? (
                <img
                  src={card.image_small_url}
                  alt=""
                  className="max-h-full max-w-full object-contain"
                />
              ) : null}
            </div>
            <div className="min-w-0 flex-1 space-y-1">
              <p className="text-base font-semibold text-white truncate">{card.name}</p>
              <p className="text-sm text-gray-400">
                {t('tile.collectorNo', { number: card.collector_no })}
              </p>
              {setName && <p className="text-sm text-gray-400 truncate">{setName}</p>}
              {(() => {
                const v = firstAddableVariant(card)
                return v ? (
                  <p className="text-xs text-gray-500">{formatNok(v.price_nok)}</p>
                ) : null
              })()}
            </div>
          </div>
          <div className="flex flex-col gap-2 p-4 border-t border-gray-800">
            <button
              type="button"
              onClick={() => onAddCandidate(candidate)}
              disabled={busy}
              data-testid="scan-result-add"
              className="flex items-center justify-center gap-2 px-4 py-2 rounded bg-emerald-600 hover:bg-emerald-500 disabled:opacity-60 disabled:cursor-not-allowed text-white text-sm font-medium cursor-pointer"
            >
              <Plus size={16} />
              {t('scanner.result.yesAdd')}
            </button>
            <button
              type="button"
              onClick={onTryAgain}
              disabled={busy}
              data-testid="scan-result-try-again"
              className="flex items-center justify-center gap-2 px-4 py-2 rounded border border-gray-700 hover:bg-gray-800 disabled:opacity-60 disabled:cursor-not-allowed text-white text-sm cursor-pointer"
            >
              <RotateCcw size={16} />
              {t('scanner.result.tryAgain')}
            </button>
          </div>
        </div>
      </div>
    )
  }

  if (result.matched) {
    return (
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('scanner.result.multiLabel')}
        data-testid="scan-result-modal"
        className="absolute inset-0 z-10 flex items-center justify-center bg-black/80 p-4"
      >
        <div className="w-full max-w-sm max-h-[85vh] bg-gray-900 border border-gray-700 rounded-lg shadow-xl flex flex-col">
          <div className="px-4 py-3 border-b border-gray-800 shrink-0">
            <p className="text-sm text-gray-300">{t('scanner.result.pickCandidate')}</p>
          </div>
          <ul
            aria-label={t('scanner.result.candidatesList')}
            className="flex-1 overflow-y-auto divide-y divide-gray-800"
          >
            {result.candidates.map(candidate => {
              const card = candidate.card
              const setName = candidate.set?.name ?? card.set_name ?? ''
              const variant = firstAddableVariant(card)
              return (
                <li key={card.id}>
                  <button
                    type="button"
                    onClick={() => onAddCandidate(candidate)}
                    disabled={busy}
                    data-testid={`scan-result-candidate-${card.id}`}
                    className="flex w-full items-center gap-3 px-3 py-2 hover:bg-gray-800/60 disabled:opacity-60 disabled:cursor-not-allowed text-left cursor-pointer"
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
                        {setName ? ` · ${setName}` : ''}
                      </p>
                    </div>
                    <span className="text-xs text-gray-300 shrink-0">
                      {formatNok(variant?.price_nok)}
                    </span>
                  </button>
                </li>
              )
            })}
          </ul>
          <div className="p-3 border-t border-gray-800 shrink-0">
            <button
              type="button"
              onClick={onTryAgain}
              disabled={busy}
              data-testid="scan-result-try-again"
              className="flex w-full items-center justify-center gap-2 px-4 py-2 rounded border border-gray-700 hover:bg-gray-800 disabled:opacity-60 disabled:cursor-not-allowed text-white text-sm cursor-pointer"
            >
              <RotateCcw size={16} />
              {t('scanner.result.tryAgain')}
            </button>
          </div>
        </div>
      </div>
    )
  }

  const confidencePct = Math.round(result.confidence * 100)
  const prefill = {
    setName: result.set_name,
    collectorNumber: result.collector_number,
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={t('scanner.result.unmatchedLabel')}
      data-testid="scan-result-modal"
      className="absolute inset-0 z-10 flex items-center justify-center bg-black/80 p-4"
    >
      <div className="w-full max-w-sm bg-gray-900 border border-gray-700 rounded-lg shadow-xl flex flex-col">
        <div className="px-4 py-4 space-y-2">
          <p className="text-base font-semibold text-white">
            {t('scanner.result.noMatch')}
          </p>
          <p className="text-sm text-gray-300">
            {t('scanner.result.confidence', { percent: confidencePct })}
          </p>
          {result.reason && (
            <p className="text-sm text-gray-400 break-words">{result.reason}</p>
          )}
        </div>
        <div className="flex flex-col gap-2 p-4 border-t border-gray-800">
          <button
            type="button"
            onClick={onTryAgain}
            disabled={busy}
            data-testid="scan-result-try-again"
            className="flex items-center justify-center gap-2 px-4 py-2 rounded bg-emerald-600 hover:bg-emerald-500 disabled:opacity-60 disabled:cursor-not-allowed text-white text-sm font-medium cursor-pointer"
          >
            <RotateCcw size={16} />
            {t('scanner.result.tryAgain')}
          </button>
          <button
            type="button"
            onClick={() => onEnterManually(prefill)}
            disabled={busy}
            data-testid="scan-result-enter-manually"
            className="flex items-center justify-center gap-2 px-4 py-2 rounded border border-gray-700 hover:bg-gray-800 disabled:opacity-60 disabled:cursor-not-allowed text-white text-sm cursor-pointer"
          >
            <Edit3 size={16} />
            {t('scanner.result.enterManually')}
          </button>
        </div>
      </div>
    </div>
  )
}
