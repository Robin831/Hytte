import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ChevronRight, Loader2 } from 'lucide-react'

// CardDTO and ScanJob types mirror the shapes used by PokemonScanned.tsx.
// They are duplicated here (rather than imported) to keep this component
// independently testable without pulling the whole page module.
export interface ScanPageGridCard {
  id: string
  set_id: string
  set_name?: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: Array<{
    id: number
    kind: string
    price_eur: number
    price_nok: number | null
    owned?: boolean
    owned_id?: number | null
    quantity?: number
    condition?: string
    notes?: string
  }>
}

export type ScanPageGridStatus =
  | 'queued'
  | 'processing'
  | 'matched'
  | 'no_match'
  | 'failed'
  | 'added'
  | 'discarded'

export interface ScanPageGridChild {
  id: number
  status: ScanPageGridStatus
  created_at: string
  processed_at?: string | null
  resolved_at?: string | null
  confidence?: number | null
  matched_card?: ScanPageGridCard | null
  set?: { id: string; name: string } | null
  error_message?: string
  has_image: boolean
  parsed_set_name?: string
  parsed_collector_no?: string
}

export interface ScanPageGridResolveBody {
  action: 'add' | 'discard' | 'retry'
  variant_id?: number
  quantity?: number
  condition?: string
  notes?: string
  card_id?: string
}

export interface ScanPageGridParent {
  id: number
  expected_count: number
  matched_count: number
  created_at: string
  children: ScanPageGridChild[]
}

interface ScanPageGridProps {
  page: ScanPageGridParent
  busyChildId: number | null
  highlighted: boolean
  blockRef?: (el: HTMLDivElement | null) => void
  onResolveChild: (child: ScanPageGridChild, body: ScanPageGridResolveBody) => Promise<void> | void
  onOpenDetail: (child: ScanPageGridChild) => void
  onEnterManually: (child: ScanPageGridChild) => void
  onAddAllMatched: (page: ScanPageGridParent) => Promise<void> | void
  onDiscardPage: (page: ScanPageGridParent) => Promise<void> | void
}

// statusToBadgeClass keeps the per-status colour mapping next to the cell
// rendering so the grid stays visually consistent with the list-view pill
// even though it's a smaller, in-cell badge.
function statusToBadgeClass(status: ScanPageGridStatus): string {
  switch (status) {
    case 'queued':
      return 'bg-gray-700 text-gray-200'
    case 'processing':
      return 'bg-blue-600/30 text-blue-200 border border-blue-500/40'
    case 'matched':
      return 'bg-emerald-600/30 text-emerald-200 border border-emerald-500/40'
    case 'no_match':
      return 'bg-amber-600/30 text-amber-200 border border-amber-500/40'
    case 'failed':
      return 'bg-red-700/40 text-red-200 border border-red-600/50'
    case 'added':
      return 'bg-gray-700 text-gray-300'
    case 'discarded':
      return 'bg-gray-800 text-gray-400'
  }
}

// gridDimensions picks the rows/cols layout from the page's expected_count:
//   4  → 2 × 2
//   6  → 2 × 3
//   12 → 3 × 4 (larger binder page)
// Everything else falls back to 3 × 3 (default 9-pocket page).
function gridDimensions(expectedCount: number): { rows: number; cols: number } {
  if (expectedCount === 12) return { rows: 3, cols: 4 }
  if (expectedCount === 4) return { rows: 2, cols: 2 }
  if (expectedCount === 6) return { rows: 2, cols: 3 }
  return { rows: 3, cols: 3 }
}

interface CellThumbnailProps {
  child: ScanPageGridChild
  t: TFunction<'pokemon'>
}

// CellThumbnail renders a fixed-aspect 3:4 image area so every cell has the
// same visual footprint regardless of whether the action bar pushes the
// surrounding cell taller.
function CellThumbnail({ child, t }: CellThumbnailProps) {
  const [errored, setErrored] = useState(false)
  const showImage = child.has_image && !errored
  return (
    <div className="relative aspect-[3/4] w-full bg-gray-900/60 flex items-center justify-center overflow-hidden">
      {showImage ? (
        <img
          src={`/api/pokemon/scans/${child.id}/image`}
          alt={t('scanned.thumbnailAlt')}
          loading="lazy"
          onError={() => setErrored(true)}
          className="max-h-full max-w-full object-contain"
        />
      ) : (
        <span className="px-1 text-[10px] text-center text-gray-500 leading-tight">
          {t('scanned.thumbnailPlaceholder')}
        </span>
      )}
      {(child.status === 'queued' || child.status === 'processing') && (
        <div className="absolute inset-0 flex items-center justify-center bg-gray-950/40">
          <Loader2 size={20} className="animate-spin text-gray-300" aria-hidden="true" />
        </div>
      )}
    </div>
  )
}

interface CellProps {
  child: ScanPageGridChild | null
  busy: boolean
  onResolve: (child: ScanPageGridChild, body: ScanPageGridResolveBody) => Promise<void> | void
  onOpenDetail: (child: ScanPageGridChild) => void
  onEnterManually: (child: ScanPageGridChild) => void
  t: TFunction<'pokemon'>
}

function ScanPageCell({ child, busy, onResolve, onOpenDetail, onEnterManually, t }: CellProps) {
  // Empty placeholder cells (page captured fewer rows than the configured
  // layout) keep the grid shape intact without ever being interactive.
  if (!child) {
    return (
      <div
        data-testid="scan-page-cell-empty"
        className="aspect-[3/4] rounded border border-dashed border-gray-800 bg-gray-900/30"
      />
    )
  }

  const handleDiscard = () => {
    void onResolve(child, { action: 'discard' })
  }
  const handleRetry = () => {
    void onResolve(child, { action: 'retry' })
  }

  const card = child.matched_card ?? null
  const isMatched = child.status === 'matched' && card != null

  const wrapperClass =
    'rounded border border-gray-800 bg-gray-800/40 overflow-hidden flex flex-col text-left'

  const labelBlock = (
    <div className="flex flex-col gap-1 p-1.5">
      <span
        data-testid={`scan-page-cell-pill-${child.id}`}
        className={`self-start inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium ${statusToBadgeClass(child.status)}`}
      >
        {t(`scanned.status.${child.status === 'no_match' ? 'noMatch' : child.status}` as 'scanned.status.queued')}
      </span>
      {isMatched && card != null && (
        <span className="text-[11px] text-white font-medium truncate">{card.name}</span>
      )}
      {isMatched && (
        <span className="text-[10px] text-gray-300 flex items-center gap-0.5">
          {t('scanned.tapToReview')}
          <ChevronRight size={12} aria-hidden="true" />
        </span>
      )}
    </div>
  )

  if (isMatched) {
    return (
      <button
        type="button"
        onClick={() => onOpenDetail(child)}
        data-testid={`scan-page-cell-${child.id}`}
        data-status={child.status}
        aria-label={t('scanned.detail.openLabel', { name: card?.name ?? '' })}
        className={`${wrapperClass} hover:border-emerald-500/60 transition-colors cursor-pointer`}
      >
        <CellThumbnail child={child} t={t} />
        {labelBlock}
      </button>
    )
  }

  const showActions = child.status === 'no_match' || child.status === 'failed'

  return (
    <div
      data-testid={`scan-page-cell-${child.id}`}
      data-status={child.status}
      className={wrapperClass}
    >
      <CellThumbnail child={child} t={t} />
      {labelBlock}
      {showActions && (
        <div
          data-testid={`scan-page-cell-actions-${child.id}`}
          className="flex flex-wrap items-center justify-center gap-1 p-1.5 bg-gray-950/40 border-t border-gray-800"
        >
          <button
            type="button"
            onClick={handleRetry}
            disabled={busy}
            data-testid={`scan-page-cell-retry-${child.id}`}
            className="px-1.5 py-1 text-[10px] rounded border border-gray-600 hover:border-gray-400 disabled:opacity-60 disabled:cursor-not-allowed text-gray-100 cursor-pointer bg-gray-900/80"
          >
            {t('scanned.action.retry')}
          </button>
          <button
            type="button"
            onClick={handleDiscard}
            disabled={busy}
            data-testid={`scan-page-cell-discard-${child.id}`}
            className="px-1.5 py-1 text-[10px] rounded bg-gray-700 hover:bg-gray-600 disabled:opacity-60 disabled:cursor-not-allowed text-white cursor-pointer"
          >
            {t('scanned.action.discard')}
          </button>
          {child.status === 'no_match' && (
            <button
              type="button"
              onClick={() => onEnterManually(child)}
              data-testid={`scan-page-cell-manual-${child.id}`}
              className="px-1.5 py-1 text-[10px] rounded border border-gray-600 hover:border-gray-400 text-gray-100 cursor-pointer bg-gray-900/80"
            >
              {t('scanned.action.enterManually')}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// ScanPageGrid renders a single pokemon_scan_pages parent as a CSS grid
// matching the captured layout (3×3 for 9, 4×3 for 12, etc.) and gives the
// kid two parent-level actions:
//   • "Add all matched" — runs the existing /resolve POST on every child
//     currently in the matched state, letting them accept a whole page in
//     one click without opening each cell.
//   • "Discard page" — calls DELETE /api/pokemon/scans/pages/{id} to drop
//     the parent + the still-pending children.
//
// Per-cell Add / Discard / Manual-search actions reuse the same resolve
// callbacks as the standalone list rows so the matching, queueing, and
// detail-modal flows behave identically inside and outside a page block.
export default function ScanPageGrid({
  page,
  busyChildId,
  highlighted,
  blockRef,
  onResolveChild,
  onOpenDetail,
  onEnterManually,
  onAddAllMatched,
  onDiscardPage,
}: ScanPageGridProps) {
  const { t } = useTranslation('pokemon')

  const { rows, cols } = useMemo(() => gridDimensions(page.expected_count), [page.expected_count])
  const totalCells = rows * cols

  // The captured ordering of children comes from the upload's cells array,
  // but the API returns them sorted by id. We render in array order so the
  // visual grid stays stable across polls; missing slots get placeholder
  // cells so the layout is preserved even if a row count is short.
  const cells: Array<ScanPageGridChild | null> = useMemo(() => {
    const list: Array<ScanPageGridChild | null> = []
    for (let i = 0; i < totalCells; i++) {
      list.push(page.children[i] ?? null)
    }
    return list
  }, [page.children, totalCells])

  const matchedCount = useMemo(
    () => page.children.filter(c => c.status === 'matched' && c.matched_card).length,
    [page.children],
  )

  const [confirmDiscard, setConfirmDiscard] = useState(false)

  const containerClass = `rounded-lg border bg-gray-900/40 transition-shadow duration-700 ${
    highlighted
      ? 'border-emerald-400 ring-2 ring-emerald-400/70 shadow-lg shadow-emerald-500/20'
      : 'border-gray-800'
  }`

  return (
    <div
      ref={blockRef}
      data-testid={`scan-page-${page.id}`}
      data-highlighted={highlighted ? 'true' : undefined}
      className={containerClass}
    >
      <div className="flex items-center justify-between gap-2 px-3 py-2 border-b border-gray-800">
        <div className="flex items-center gap-2 text-xs text-gray-300">
          <span className="inline-flex items-center px-2 py-0.5 rounded-full bg-gray-800/70 border border-gray-700">
            {t('scanned.page.label')}
          </span>
          <span data-testid={`scan-page-progress-${page.id}`}>
            {t('scanned.page.progress', {
              matched: page.matched_count,
              total: page.expected_count,
            })}
          </span>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <button
            type="button"
            onClick={() => void onAddAllMatched(page)}
            disabled={matchedCount === 0}
            data-testid={`scan-page-add-all-${page.id}`}
            className="px-2.5 py-1 text-xs rounded bg-emerald-600/30 border border-emerald-500/60 text-emerald-100 hover:bg-emerald-600/50 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
          >
            {t('scanned.page.addAllMatched', { count: matchedCount })}
          </button>
          <button
            type="button"
            onClick={() => setConfirmDiscard(true)}
            data-testid={`scan-page-discard-${page.id}`}
            className="px-2.5 py-1 text-xs rounded bg-gray-700 hover:bg-gray-600 text-white cursor-pointer"
          >
            {t('scanned.page.discardPage')}
          </button>
        </div>
      </div>
      <div
        className="grid gap-2 p-3"
        style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
      >
        {cells.map((child, idx) => (
          <ScanPageCell
            key={child ? child.id : `empty-${idx}`}
            child={child}
            busy={child != null && busyChildId === child.id}
            onResolve={onResolveChild}
            onOpenDetail={onOpenDetail}
            onEnterManually={onEnterManually}
            t={t}
          />
        ))}
      </div>
      {confirmDiscard && (
        <div className="px-3 py-2 border-t border-gray-800 bg-gray-900/70 flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs text-gray-200">{t('scanned.page.confirmDiscard')}</p>
          <div className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={() => setConfirmDiscard(false)}
              data-testid={`scan-page-discard-cancel-${page.id}`}
              className="px-2.5 py-1 text-xs rounded border border-gray-700 hover:border-gray-500 text-gray-200 cursor-pointer"
            >
              {t('scanned.page.cancel')}
            </button>
            <button
              type="button"
              onClick={() => {
                setConfirmDiscard(false)
                void onDiscardPage(page)
              }}
              data-testid={`scan-page-discard-confirm-${page.id}`}
              className="px-2.5 py-1 text-xs rounded bg-red-700/60 border border-red-600/70 hover:bg-red-700 text-white cursor-pointer"
            >
              {t('scanned.page.discardConfirm')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
