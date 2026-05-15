import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ArrowLeft, ChevronRight, Loader2 } from 'lucide-react'
import ToastList from '../components/ToastList'
import { useToast } from '../hooks/useToast'
import ScanDetailModal from '../components/pokemon/ScanDetailModal'
import type { ScanDetailResolveBody } from '../components/pokemon/ScanDetailModal'

// buildManualEntryQuery joins whatever partial fields Claude could read into a
// single search string for AddCardPanel. Empty hints collapse to an empty
// return so the panel just opens blank rather than searching for "  ".
function buildManualEntryQuery(setName?: string, collectorNo?: string): string {
  const parts: string[] = []
  const trimmedSet = setName?.trim()
  const trimmedNo = collectorNo?.trim()
  if (trimmedSet) parts.push(trimmedSet)
  if (trimmedNo) parts.push(trimmedNo)
  return parts.join(' ')
}

// SCAN_POLL_MS keeps the page roughly in sync with the worker's progress.
// 30 s is fast enough that queued→processing→matched feels responsive while
// keeping the API chatter modest.
const SCAN_POLL_MS = 30000

// RESOLVED_WINDOW_DAYS scopes the "Recently resolved" filter to the past week
// so the list stays focused on what the user just acted on rather than turning
// into a long historical log.
const RESOLVED_WINDOW_DAYS = 7

interface VariantDTO {
  id: number
  kind: string
  price_eur: number
  price_nok: number | null
  owned?: boolean
  owned_id?: number | null
  quantity?: number
  condition?: string
  notes?: string
}

interface CardDTO {
  id: string
  set_id: string
  set_name?: string
  name: string
  collector_no: string
  rarity: string
  image_small_url: string
  image_large_url: string
  variants: VariantDTO[]
}

interface SetDTO {
  id: string
  name: string
}

type ScanStatus =
  | 'queued'
  | 'processing'
  | 'matched'
  | 'no_match'
  | 'failed'
  | 'added'
  | 'discarded'

interface ScanJob {
  id: number
  status: ScanStatus
  created_at: string
  processed_at?: string | null
  resolved_at?: string | null
  confidence?: number | null
  matched_card?: CardDTO | null
  set?: SetDTO | null
  error_message?: string
  has_image: boolean
  // Partial info Claude could read on a no_match scan. Either field may be
  // empty — the "Enter manually" action concatenates whatever is present into
  // an AddCardPanel pre-fill query.
  parsed_set_name?: string
  parsed_collector_no?: string
}

interface TodayUsage {
  used: number
  cap: number
}

type FilterKey = 'needsReview' | 'pending' | 'resolved'
const FILTERS: FilterKey[] = ['needsReview', 'pending', 'resolved']

// statusForFilter maps a UI filter chip to the server-side status set passed in
// ?status=. The backend will return resolved rows without time-windowing; we
// clip to the last RESOLVED_WINDOW_DAYS client-side for the resolved tab.
function statusForFilter(filter: FilterKey): string[] {
  switch (filter) {
    case 'needsReview':
      return ['matched', 'no_match', 'failed']
    case 'pending':
      return ['queued', 'processing']
    case 'resolved':
      return ['added', 'discarded']
  }
}

// elapsedSeconds returns the integer seconds elapsed between `since` and now,
// clamped to >= 0 so a slightly-skewed server clock doesn't render negative
// numbers in the pending spinner caption.
function elapsedSeconds(since: string, now: number): number {
  const t = Date.parse(since)
  if (Number.isNaN(t)) return 0
  return Math.max(0, Math.round((now - t) / 1000))
}

// confidencePercent converts the 0-1 model confidence into a 0-100 integer for
// display, returning null when the upstream value is missing or non-finite
// (NaN / Infinity) so we don't render "NaN%" if the backend ever sends a
// malformed number.
function confidencePercent(confidence: number | null | undefined): number | null {
  if (confidence == null || !Number.isFinite(confidence)) return null
  const pct = Math.round(confidence * 100)
  return Math.max(0, Math.min(100, pct))
}

interface StatusPillProps {
  status: ScanStatus
  t: TFunction<'pokemon'>
}

function StatusPill({ status, t }: StatusPillProps) {
  const colorClass = ((): string => {
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
  })()
  const labelKey = ((): string => {
    switch (status) {
      case 'no_match':
        return 'scanned.status.noMatch'
      default:
        return `scanned.status.${status}`
    }
  })()
  return (
    <span
      data-testid={`scan-status-pill-${status}`}
      className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${colorClass}`}
    >
      {t(labelKey as 'scanned.status.queued')}
    </span>
  )
}

interface ThumbnailProps {
  scan: ScanJob
  alt: string
  placeholder: string
}

// Thumbnail renders the scan image when the backend reports has_image; falls
// back to a neutral placeholder for resolved rows whose image was deleted or
// for rows whose <img> errors at load time (e.g. file removed between the list
// fetch and the actual image GET).
function Thumbnail({ scan, alt, placeholder }: ThumbnailProps) {
  const [errored, setErrored] = useState(false)
  const showImage = scan.has_image && !errored
  return (
    <div
      data-testid={`scan-thumbnail-${scan.id}`}
      className="h-16 w-12 sm:h-20 sm:w-14 shrink-0 rounded overflow-hidden bg-gray-800/60 border border-gray-800 flex items-center justify-center"
    >
      {showImage ? (
        <img
          src={`/api/pokemon/scans/${scan.id}/image`}
          alt={alt}
          loading="lazy"
          onError={() => setErrored(true)}
          className="max-h-full max-w-full object-cover"
        />
      ) : (
        <span className="px-1 text-[10px] text-center text-gray-500 leading-tight">
          {placeholder}
        </span>
      )}
    </div>
  )
}

interface ScanRowProps {
  scan: ScanJob
  busy: boolean
  now: number
  highlighted: boolean
  rowRef?: (el: HTMLLIElement | null) => void
  onResolve: (scan: ScanJob, body: ResolveBody) => Promise<void>
  onEnterManually: (scan: ScanJob) => void
  onOpenDetail: (scan: ScanJob) => void
  t: TFunction<'pokemon'>
}

interface ResolveBody {
  action: 'add' | 'discard' | 'retry'
  variant_id?: number
  quantity?: number
  condition?: string
  notes?: string
  // card_id, when set on action=add, asks the backend to override the
  // auto-matched card with this one before adding to the collection. Used by
  // the scan detail modal's "Wrong match?" flow.
  card_id?: string
}

function ScanRow({ scan, busy, now, highlighted, rowRef, onResolve, onEnterManually, onOpenDetail, t }: ScanRowProps) {
  const handleDiscard = () => {
    void onResolve(scan, { action: 'discard' })
  }

  const handleRetry = () => {
    void onResolve(scan, { action: 'retry' })
  }

  const renderBody = () => {
    if (scan.status === 'matched' && scan.matched_card) {
      const card = scan.matched_card
      const pct = confidencePercent(scan.confidence)
      return (
        <div className="min-w-0 flex flex-col gap-0.5">
          <p className="text-sm font-medium text-white truncate" title={card.name}>
            {card.name}
          </p>
          <p className="text-xs text-gray-400 truncate">
            {scan.set?.name ?? card.set_name ?? card.set_id}
            {' · '}
            {t('tile.collectorNo', { number: card.collector_no })}
          </p>
          {pct != null && (
            <p className="text-xs text-gray-300">{t('scanned.confidence', { pct })}</p>
          )}
          <p className="text-xs text-gray-500">{t('scanned.tapToReview')}</p>
        </div>
      )
    }
    if (scan.status === 'no_match') {
      const pct = confidencePercent(scan.confidence)
      return (
        <div className="min-w-0 flex flex-col gap-0.5">
          {pct != null && (
            <p className="text-sm text-gray-200">{t('scanned.confidence', { pct })}</p>
          )}
          {scan.error_message && (
            <p className="text-xs text-amber-200/80 break-words">{scan.error_message}</p>
          )}
        </div>
      )
    }
    if (scan.status === 'failed') {
      return (
        <div className="min-w-0 flex flex-col gap-0.5">
          <p className="text-xs text-red-200 break-words">
            {scan.error_message || t('scanner.errors.scanFailed')}
          </p>
        </div>
      )
    }
    if (scan.status === 'queued' || scan.status === 'processing') {
      const seconds = elapsedSeconds(scan.created_at, now)
      return (
        <div className="min-w-0 flex items-center gap-2 text-xs text-gray-400">
          <Loader2 size={14} className="animate-spin shrink-0" aria-hidden="true" />
          <span>{t('scanned.elapsed', { seconds })}</span>
        </div>
      )
    }
    if (scan.status === 'added' && scan.matched_card) {
      const card = scan.matched_card
      return (
        <div className="min-w-0 flex flex-col gap-0.5">
          <p className="text-sm text-gray-200 truncate" title={card.name}>
            {card.name}
          </p>
          <p className="text-xs text-gray-500 truncate">
            {scan.set?.name ?? card.set_name ?? card.set_id}
            {' · '}
            {t('tile.collectorNo', { number: card.collector_no })}
          </p>
        </div>
      )
    }
    // discarded or added without matched_card (defensive)
    return null
  }

  const renderActions = () => {
    if (scan.status === 'matched') {
      // The matched-tile action row was moved into the detail modal so the
      // tile is just a clickable launchpad. The detail modal owns the variant
      // picker, Discard, and the "Wrong match?" reassign flow.
      return null
    }
    if (scan.status === 'no_match') {
      return (
        <div className="flex flex-wrap items-center gap-2 mt-2">
          <button
            type="button"
            onClick={handleRetry}
            disabled={busy}
            data-testid={`scan-action-retry-${scan.id}`}
            className="px-3 py-1.5 text-xs rounded border border-gray-700 hover:border-gray-500 disabled:opacity-60 disabled:cursor-not-allowed text-gray-200 cursor-pointer"
          >
            {t('scanned.action.retry')}
          </button>
          <button
            type="button"
            onClick={handleDiscard}
            disabled={busy}
            data-testid={`scan-action-discard-${scan.id}`}
            className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:opacity-60 disabled:cursor-not-allowed text-white cursor-pointer"
          >
            {t('scanned.action.discard')}
          </button>
          <button
            type="button"
            onClick={() => onEnterManually(scan)}
            data-testid={`scan-action-manual-${scan.id}`}
            className="px-3 py-1.5 text-xs rounded border border-gray-700 hover:border-gray-500 text-gray-200 cursor-pointer"
          >
            {t('scanned.action.enterManually')}
          </button>
        </div>
      )
    }
    if (scan.status === 'failed') {
      return (
        <div className="flex flex-wrap items-center gap-2 mt-2">
          <button
            type="button"
            onClick={handleRetry}
            disabled={busy}
            data-testid={`scan-action-retry-${scan.id}`}
            className="px-3 py-1.5 text-xs rounded border border-gray-700 hover:border-gray-500 disabled:opacity-60 disabled:cursor-not-allowed text-gray-200 cursor-pointer"
          >
            {t('scanned.action.retry')}
          </button>
          <button
            type="button"
            onClick={handleDiscard}
            disabled={busy}
            data-testid={`scan-action-discard-${scan.id}`}
            className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:opacity-60 disabled:cursor-not-allowed text-white cursor-pointer"
          >
            {t('scanned.action.discard')}
          </button>
        </div>
      )
    }
    return null
  }

  const isMatched = scan.status === 'matched' && scan.matched_card
  const containerClass = `flex flex-col sm:flex-row gap-3 p-3 bg-gray-800/40 border rounded-lg transition-shadow duration-700 ${
    highlighted
      ? 'border-emerald-400 ring-2 ring-emerald-400/70 shadow-lg shadow-emerald-500/20'
      : 'border-gray-800'
  }`

  return (
    <li
      ref={rowRef}
      data-testid={`scan-row-${scan.id}`}
      data-status={scan.status}
      data-highlighted={highlighted ? 'true' : undefined}
      className={containerClass}
    >
      {isMatched ? (
        <button
          type="button"
          onClick={() => onOpenDetail(scan)}
          aria-label={t('scanned.detail.openLabel', { name: scan.matched_card?.name ?? '' })}
          data-testid={`scan-open-detail-${scan.id}`}
          className="flex gap-3 min-w-0 flex-1 text-left cursor-pointer hover:bg-gray-800/20 -m-1 p-1 rounded"
        >
          <Thumbnail
            scan={scan}
            alt={t('scanned.thumbnailAlt')}
            placeholder={t('scanned.thumbnailPlaceholder')}
          />
          <div className="flex flex-col gap-1.5 min-w-0 flex-1">
            <StatusPill status={scan.status} t={t} />
            {renderBody()}
          </div>
          <ChevronRight size={18} className="self-center text-gray-500 shrink-0" aria-hidden="true" />
        </button>
      ) : (
        <div className="flex gap-3 min-w-0 flex-1">
          <Thumbnail
            scan={scan}
            alt={t('scanned.thumbnailAlt')}
            placeholder={t('scanned.thumbnailPlaceholder')}
          />
          <div className="flex flex-col gap-1.5 min-w-0 flex-1">
            <StatusPill status={scan.status} t={t} />
            {renderBody()}
            {renderActions()}
          </div>
        </div>
      )}
    </li>
  )
}

export default function PokemonScannedPage() {
  const { t } = useTranslation('pokemon')
  const { toasts, showToast } = useToast()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const focusParam = searchParams.get('focus')
  const focusedId = useMemo(() => {
    if (!focusParam) return null
    const parsed = parseInt(focusParam, 10)
    return Number.isFinite(parsed) && parsed > 0 ? parsed : null
  }, [focusParam])

  const [filter, setFilter] = useState<FilterKey>('needsReview')
  const [scans, setScans] = useState<ScanJob[]>([])
  const [today, setToday] = useState<TodayUsage | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busyId, setBusyId] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const [highlightedId, setHighlightedId] = useState<number | null>(null)
  const [detailScanId, setDetailScanId] = useState<number | null>(null)
  const rowRefs = useRef<Map<number, HTMLLIElement>>(new Map())
  const focusHandledRef = useRef<number | null>(null)

  // tick the clock once per second so the "elapsed time" caption on pending
  // rows updates without forcing a full refetch.
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])

  // attemptRef bumps on every explicit refetch (mount, filter change, resolve,
  // poll tick) so the fetch effect re-runs without us needing a separate
  // useEffect for each trigger.
  const [attempt, setAttempt] = useState(0)
  const isSilentPollRef = useRef(false)
  const refetch = useCallback(() => {
    isSilentPollRef.current = false
    setAttempt(a => a + 1)
  }, [])
  // silentRefetch is used by the background poll — it does not trigger the
  // loading skeleton, so the scan list stays mounted during the refresh.
  const silentRefetch = useCallback(() => {
    isSilentPollRef.current = true
    setAttempt(a => a + 1)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    const isSilent = isSilentPollRef.current
    isSilentPollRef.current = false
    const statuses = statusForFilter(filter).join(',')
    ;(async () => {
      if (!isSilent) {
        setLoading(true)
        setError('')
      }
      try {
        const res = await fetch(
          `/api/pokemon/scans?status=${encodeURIComponent(statuses)}`,
          { credentials: 'include', signal: controller.signal },
        )
        if (!res.ok) throw new Error(t('scanned.loadError'))
        const data: { scans?: ScanJob[]; today?: TodayUsage } = await res.json()
        setScans(data.scans ?? [])
        setToday(data.today ?? null)
        // clear any previous error when a silent poll succeeds
        if (isSilent) setError('')
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        // background poll failures are swallowed — existing data stays visible
        if (!isSilent) setError(err instanceof Error ? err.message : t('scanned.loadError'))
      } finally {
        if (!controller.signal.aborted && !isSilent) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [filter, attempt, t])

  // Poll while the page is visible so the kid sees queued→processing→matched
  // transitions without manually refreshing. We pause polling when the tab is
  // hidden to avoid wasting bandwidth on background tabs.
  useEffect(() => {
    if (typeof document === 'undefined') return
    let intervalId: ReturnType<typeof window.setInterval> | null = null

    const start = () => {
      if (intervalId !== null) return
      intervalId = window.setInterval(() => {
        if (document.visibilityState !== 'visible') return
        silentRefetch()
      }, SCAN_POLL_MS)
    }
    const stop = () => {
      if (intervalId !== null) {
        window.clearInterval(intervalId)
        intervalId = null
      }
    }

    if (document.visibilityState === 'visible') start()

    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        start()
        silentRefetch()
      } else {
        stop()
      }
    }
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      stop()
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [silentRefetch])

  const visibleScans = useMemo(() => {
    if (filter !== 'resolved') return scans
    const cutoff = now - RESOLVED_WINDOW_DAYS * 24 * 60 * 60 * 1000
    return scans.filter(s => {
      const ts = s.resolved_at ? Date.parse(s.resolved_at) : Date.parse(s.created_at)
      return !Number.isNaN(ts) && ts >= cutoff
    })
  }, [scans, filter, now])

  // When ?focus=N is present (e.g. opened from a scan-result push), scroll the
  // matching row into view and apply a brief highlight so the kid can tell
  // which result is theirs. focusHandledRef ensures we only scroll once per
  // focus id even though visibleScans churns on every background poll.
  useEffect(() => {
    if (focusedId == null) return
    if (loading) return
    if (focusHandledRef.current === focusedId) return
    const target = visibleScans.find(s => s.id === focusedId)
    if (!target) return
    const el = rowRefs.current.get(focusedId)
    if (!el) return
    focusHandledRef.current = focusedId
    el.scrollIntoView({ behavior: 'smooth', block: 'center' })
    setHighlightedId(focusedId)
    // Strip ?focus= once the focus is consumed so a refresh does not re-trigger
    // the highlight after the user has moved on.
    const next = new URLSearchParams(searchParams)
    next.delete('focus')
    setSearchParams(next, { replace: true })
  }, [focusedId, loading, visibleScans, searchParams, setSearchParams])

  // Auto-clear the row highlight after a short window so the visual nudge
  // fades back to normal styling without lingering forever. Split from the
  // scroll effect so the timer is not cancelled by unrelated re-renders
  // (e.g. when the background poll refreshes visibleScans).
  useEffect(() => {
    if (highlightedId == null) return
    const timer = window.setTimeout(() => setHighlightedId(null), 2500)
    return () => window.clearTimeout(timer)
  }, [highlightedId])

  const handleEnterManually = useCallback(
    (scan: ScanJob) => {
      const query = buildManualEntryQuery(scan.parsed_set_name, scan.parsed_collector_no)
      navigate('/pokemon', { state: { addCardQuery: query } })
    },
    [navigate],
  )

  const handleOpenDetail = useCallback((scan: ScanJob) => {
    setDetailScanId(scan.id)
  }, [])

  const handleCloseDetail = useCallback(() => {
    setDetailScanId(null)
  }, [])

  const handleResolve = useCallback(
    async (scan: ScanJob, body: ResolveBody) => {
      setBusyId(scan.id)
      try {
        const res = await fetch(`/api/pokemon/scans/${scan.id}/resolve`, {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        })
        if (!res.ok) throw new Error(t('scanned.action.actionFailed'))
        if (body.action === 'add') showToast(t('scanned.toast.added'), 'success')
        else if (body.action === 'discard') showToast(t('scanned.toast.discarded'), 'success')
        else if (body.action === 'retry') showToast(t('scanned.toast.retried'), 'success')
        refetch()
      } catch (err) {
        showToast(
          err instanceof Error ? err.message : t('scanned.action.actionFailed'),
          'error',
        )
      } finally {
        setBusyId(prev => (prev === scan.id ? null : prev))
      }
    },
    [refetch, showToast, t],
  )

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <div className="max-w-3xl mx-auto px-4 py-6 space-y-5">
        <header className="space-y-3">
          <Link
            to="/pokemon"
            className="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-white transition-colors"
          >
            <ArrowLeft size={16} />
            {t('detail.back')}
          </Link>

          <div className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-2">
            <div>
              <h1 className="text-2xl font-semibold">{t('scanned.title')}</h1>
              <p className="text-sm text-gray-400">{t('scanned.subtitle')}</p>
            </div>
            {today && (
              <span
                data-testid="scanned-today-usage"
                className="inline-flex items-center px-3 py-1 rounded-full bg-gray-800/60 border border-gray-700 text-xs text-gray-200"
              >
                {t('scanned.todayUsage', { used: today.used, cap: today.cap })}
              </span>
            )}
          </div>

          <div
            role="radiogroup"
            aria-label={t('scanned.filter.label')}
            data-testid="scanned-filter"
            className="inline-flex flex-wrap gap-1.5"
          >
            {FILTERS.map(f => {
              const checked = f === filter
              return (
                <button
                  key={f}
                  type="button"
                  role="radio"
                  aria-checked={checked}
                  data-filter={f}
                  data-testid={`scanned-filter-${f}`}
                  onClick={() => setFilter(f)}
                  className={`px-3 py-1.5 text-xs rounded-full border cursor-pointer transition-colors ${
                    checked
                      ? 'bg-emerald-600/30 border-emerald-500/70 text-white'
                      : 'bg-gray-800/60 border-gray-700 text-gray-300 hover:text-white hover:border-gray-600'
                  }`}
                >
                  {t(`scanned.filter.${f}` as 'scanned.filter.needsReview')}
                </button>
              )
            })}
          </div>
        </header>

        {error && (
          <div
            role="alert"
            className="px-3 py-2 bg-red-900/40 border border-red-800 text-red-300 text-sm rounded flex items-center justify-between gap-3"
          >
            <span>{error}</span>
            <button
              type="button"
              onClick={refetch}
              className="px-2 py-1 text-xs bg-red-800/60 hover:bg-red-700 text-white rounded transition-colors cursor-pointer"
            >
              {t('retry')}
            </button>
          </div>
        )}

        {loading && !error && (
          <ul aria-busy="true" className="space-y-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <li
                key={i}
                className="h-24 rounded-lg bg-gray-800/40 border border-gray-800 animate-pulse"
                data-testid="scanned-skeleton"
              />
            ))}
          </ul>
        )}

        {!loading && !error && visibleScans.length === 0 && (
          <p
            data-testid="scanned-empty"
            className="text-sm text-gray-400 py-6 text-center"
          >
            {t('scanned.empty')}
          </p>
        )}

        {!loading && !error && visibleScans.length > 0 && (
          <ul className="space-y-3" data-testid="scanned-list">
            {visibleScans.map(scan => (
              <ScanRow
                key={scan.id}
                scan={scan}
                busy={busyId === scan.id}
                now={now}
                highlighted={highlightedId === scan.id}
                rowRef={el => {
                  if (el) {
                    rowRefs.current.set(scan.id, el)
                  } else {
                    rowRefs.current.delete(scan.id)
                  }
                }}
                onResolve={handleResolve}
                onEnterManually={handleEnterManually}
                onOpenDetail={handleOpenDetail}
                t={t}
              />
            ))}
          </ul>
        )}
      </div>
      {detailScanId != null && (() => {
        const scan = scans.find(s => s.id === detailScanId)
        if (!scan) return null
        return (
          <ScanDetailModal
            scan={scan}
            busy={busyId === scan.id}
            onClose={handleCloseDetail}
            onResolve={async (body: ScanDetailResolveBody) => {
              await handleResolve(scan, body)
              handleCloseDetail()
            }}
          />
        )
      })()}
      <ToastList toasts={toasts} />
    </div>
  )
}
