import { useState, useEffect, useCallback, useMemo, useRef, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronRight, History, Loader2 } from 'lucide-react'
import { ResponsiveContainer, AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip } from 'recharts'
import { formatDate, formatNumber } from '../../utils/formatDate'
import type { WeekSummary } from '../../types/stride'

interface MonthSummary {
  month: string
  sessions_planned: number
  sessions_completed: number
  compliance_rate: number
}

const HISTORY_PAGE_SIZE = 12

// Zone palette: shares the easy/threshold/hard hues with the per-zone HR card on
// TrainingDetail (#22c55e Z1, #eab308 Z3, #ef4444 Z5).
const ZONE_COLOR_EASY = '#22c55e'
const ZONE_COLOR_THRESHOLD = '#eab308'
const ZONE_COLOR_HARD = '#ef4444'

function formatHHMM(totalSeconds: number): string {
  const safe = Math.max(0, Math.floor(totalSeconds))
  const h = Math.floor(safe / 3600)
  const m = Math.floor((safe % 3600) / 60)
  return `${h}:${String(m).padStart(2, '0')}`
}

interface ZoneBarProps {
  easy: number
  threshold: number
  hard: number
  totalSec: number
  zoneTooltip: string
  interactive: boolean
}

// Hover/focus state lives here so showing one row's tooltip does not re-render
// sibling WeekRows.
function ZoneBar({ easy, threshold, hard, totalSec, zoneTooltip, interactive }: ZoneBarProps) {
  const zoneTooltipId = useId()
  const [zoneTooltipVisible, setZoneTooltipVisible] = useState(false)

  return (
    <div className="relative mt-2">
      <div
        className="h-1.5 w-full flex rounded-full overflow-hidden bg-gray-700"
        role="img"
        aria-label={zoneTooltip}
        aria-describedby={totalSec > 0 ? zoneTooltipId : undefined}
        tabIndex={interactive ? -1 : 0}
        onMouseEnter={() => setZoneTooltipVisible(true)}
        onMouseLeave={() => setZoneTooltipVisible(false)}
        onFocus={() => setZoneTooltipVisible(true)}
        onBlur={() => setZoneTooltipVisible(false)}
      >
        {totalSec > 0 ? (
          <>
            {easy > 0 && <div style={{ flexBasis: `${(easy / totalSec) * 100}%`, backgroundColor: ZONE_COLOR_EASY }} className="h-full" />}
            {threshold > 0 && <div style={{ flexBasis: `${(threshold / totalSec) * 100}%`, backgroundColor: ZONE_COLOR_THRESHOLD }} className="h-full" />}
            {hard > 0 && <div style={{ flexBasis: `${(hard / totalSec) * 100}%`, backgroundColor: ZONE_COLOR_HARD }} className="h-full" />}
          </>
        ) : null}
      </div>
      {totalSec > 0 && (
        <div
          id={zoneTooltipId}
          role="tooltip"
          aria-hidden={!zoneTooltipVisible}
          className={
            'absolute left-0 bottom-full mb-2 px-2.5 py-1.5 bg-gray-700 border border-gray-600 text-gray-200 text-xs rounded-lg pointer-events-none z-10 whitespace-nowrap shadow-lg transition-opacity ' +
            (zoneTooltipVisible ? 'opacity-100 visible' : 'opacity-0 invisible')
          }
        >
          {zoneTooltip}
        </div>
      )}
    </div>
  )
}

interface WeekRowProps {
  week: WeekSummary
  onOpen?: (week: WeekSummary) => void
}

function WeekRow({ week, onOpen }: WeekRowProps) {
  const { t } = useTranslation('stride')
  const openDescId = useId()

  // Derived display values depend only on the immutable `week` prop (and `t` for
  // localized strings), so memoize them to avoid recomputing on unrelated re-renders.
  const {
    pct,
    chipClass,
    easy,
    threshold,
    hard,
    totalSec,
    distanceKm,
    weekLabel,
    openLabel,
    zoneTooltip,
  } = useMemo(() => {
    const pct = Math.min(Math.max(Math.round(Number(week.completion_rate) || 0), 0), 100)
    const chipClass = pct >= 80
      ? 'bg-green-500/20 text-green-300 border border-green-500/30'
      : pct >= 50
        ? 'bg-yellow-500/20 text-yellow-300 border border-yellow-500/30'
        : 'bg-red-500/20 text-red-300 border border-red-500/30'

    const easy = Math.max(0, week.easy_seconds ?? 0)
    const threshold = Math.max(0, week.threshold_seconds ?? 0)
    const hard = Math.max(0, week.hard_seconds ?? 0)
    const totalSec = easy + threshold + hard
    const distanceKm = Math.max(0, week.total_distance_meters ?? 0) / 1000

    const easyPct = totalSec > 0 ? Math.round((easy / totalSec) * 100) : 0
    const thresholdPct = totalSec > 0 ? Math.round((threshold / totalSec) * 100) : 0
    const hardPct = totalSec > 0 ? Math.max(0, 100 - easyPct - thresholdPct) : 0

    const weekLabel = t('plan.weekOf', {
      start: formatDate(`${week.week_start}T00:00:00`, { month: 'short', day: 'numeric' }),
      end: formatDate(`${week.week_end}T00:00:00`, { month: 'short', day: 'numeric' }),
    })
    const openLabel = t('history.week.openAria', { week: weekLabel, defaultValue: 'Open week {{week}}' })
    const zoneTooltip = t('history.week.zoneSplit', {
      easy: easyPct,
      threshold: thresholdPct,
      hard: hardPct,
      defaultValue: 'Easy {{easy}}% · Threshold {{threshold}}% · Hard {{hard}}%',
    })

    return { pct, chipClass, easy, threshold, hard, totalSec, distanceKm, weekLabel, openLabel, zoneTooltip }
  }, [week, t])

  // Row click handler: parent wires the actual navigation/drawer in the follow-up
  // sub-task; if no handler is provided we render as a static row instead of a button.
  const interactive = typeof onOpen === 'function'

  const content = (
    <>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 min-w-0">
        <p className="text-sm font-medium text-white truncate">{weekLabel}</p>
        <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${chipClass}`}>{pct}%</span>
        {week.phase && (
          <span className="text-xs px-1.5 py-0.5 bg-yellow-500/10 text-yellow-500 rounded">{week.phase}</span>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 mt-1 text-xs text-gray-400">
        <span>
          <span className="text-gray-500">{t('history.week.totalDistance')}:</span>{' '}
          <span className="text-gray-200">{formatNumber(distanceKm, { minimumFractionDigits: 1, maximumFractionDigits: 1 })} km</span>
        </span>
        <span>
          <span className="text-gray-500">{t('history.week.totalTime')}:</span>{' '}
          <span className="text-gray-200">{formatHHMM(totalSec)}</span>
        </span>
      </div>
      <ZoneBar
        easy={easy}
        threshold={threshold}
        hard={hard}
        totalSec={totalSec}
        zoneTooltip={zoneTooltip}
        interactive={interactive}
      />
    </>
  )

  const rowInner = (
    <div className="flex items-center gap-3 p-3">
      <div className="flex-1 min-w-0">{content}</div>
      <ChevronRight
        size={20}
        className={`flex-shrink-0 ${interactive ? 'text-gray-500' : 'text-gray-600'}`}
        aria-hidden="true"
      />
    </div>
  )

  if (interactive) {
    return (
      <button
        type="button"
        onClick={() => onOpen!(week)}
        aria-describedby={openDescId}
        className="w-full text-left bg-gray-800 rounded-xl border border-gray-700 hover:border-gray-600 transition-colors"
      >
        {rowInner}
        <span id={openDescId} className="sr-only">{openLabel}</span>
      </button>
    )
  }

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700">
      {rowInner}
    </div>
  )
}

interface PlanHistoryProps {
  onOpenWeek?: (week: WeekSummary) => void
}

export function PlanHistory({ onOpenWeek }: PlanHistoryProps) {
  const { t } = useTranslation('stride')
  const [weeks, setWeeks] = useState<WeekSummary[]>([])
  const [months, setMonths] = useState<MonthSummary[]>([])
  const [offset, setOffset] = useState(0)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadMoreError, setLoadMoreError] = useState(false)
  const loadMoreControllerRef = useRef<AbortController | null>(null)

  const fetchWeeks = useCallback(async (
    pageOffset: number,
    signal?: AbortSignal,
  ): Promise<{ weeks: WeekSummary[]; months: MonthSummary[]; hasMore: boolean } | null> => {
    const res = await fetch(`/api/stride/history?limit=${HISTORY_PAGE_SIZE}&offset=${pageOffset}`, {
      credentials: 'include',
      signal,
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const json = await res.json()
    if (signal?.aborted) return null
    return {
      weeks: (json.weeks ?? []) as WeekSummary[],
      months: (json.months ?? []) as MonthSummary[],
      hasMore: Boolean(json.has_more),
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const result = await fetchWeeks(0, controller.signal)
        if (!result || controller.signal.aborted) return
        setWeeks(result.weeks)
        setMonths(result.months)
        setHasMore(result.hasMore)
        setOffset(result.weeks.length)
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        if (!controller.signal.aborted) setError(true)
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => {
      controller.abort()
      loadMoreControllerRef.current?.abort()
      loadMoreControllerRef.current = null
    }
  }, [fetchWeeks])

  const handleLoadMore = useCallback(async () => {
    loadMoreControllerRef.current?.abort()
    const controller = new AbortController()
    loadMoreControllerRef.current = controller
    setLoadMoreError(false)
    setLoadingMore(true)
    try {
      const result = await fetchWeeks(offset, controller.signal)
      if (!result || controller.signal.aborted) return
      setWeeks(prev => [...prev, ...result.weeks])
      setHasMore(result.hasMore)
      setOffset(prev => prev + result.weeks.length)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      if (!controller.signal.aborted) setLoadMoreError(true)
    } finally {
      if (!controller.signal.aborted) setLoadingMore(false)
      if (loadMoreControllerRef.current === controller) {
        loadMoreControllerRef.current = null
      }
    }
  }, [fetchWeeks, offset])

  const chartData = useMemo(() => {
    // Reverse to chronological order for the chart (oldest first).
    return [...weeks].reverse().map(w => ({
      label: formatDate(`${w.week_start}T00:00:00`, { month: 'short', day: 'numeric' }),
      rate: Math.min(Math.round(w.completion_rate), 100),
    }))
  }, [weeks])

  const formatMonth = (month: string) => {
    const [year, m] = month.split('-')
    return formatDate(new Date(Number(year), Number(m) - 1, 1), { month: 'short', year: 'numeric' })
  }

  if (loading) return <p className="text-sm text-gray-400">{t('loading')}</p>
  if (error) return <p className="text-sm text-red-400">{t('history.loadError')}</p>
  if (weeks.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-center bg-gray-800/50 rounded-xl border border-gray-700 border-dashed">
        <History size={28} className="text-gray-600 mb-2" />
        <p className="text-sm text-gray-400">{t('history.empty')}</p>
      </div>
    )
  }

  return (
    <div className="space-y-5">
      {/* Trend chart */}
      {chartData.length >= 2 && (
        <div className="bg-gray-800 rounded-xl border border-gray-700 p-4">
          <p className="text-xs font-semibold text-gray-400 uppercase mb-3">{t('history.chart.title')}</p>
          <div className="w-full h-48" role="img" aria-label={t('history.chart.ariaLabel')}>
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 4, right: 4, bottom: 0, left: -20 }}>
                <defs>
                  <linearGradient id="completionGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#eab308" stopOpacity={0.3} />
                    <stop offset="95%" stopColor="#eab308" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="label" tick={{ fontSize: 10, fill: '#9ca3af' }} tickLine={false} />
                <YAxis domain={[0, 100]} tick={{ fontSize: 10, fill: '#9ca3af' }} tickLine={false} tickFormatter={v => `${v}%`} />
                <Tooltip
                  contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: '8px', fontSize: 12 }}
                  labelStyle={{ color: '#f3f4f6' }}
                  itemStyle={{ color: '#eab308' }}
                  formatter={(value) => [`${value ?? 0}%`, t('history.chart.completionRate')]}
                />
                <Area type="monotone" dataKey="rate" stroke="#eab308" strokeWidth={2} fill="url(#completionGradient)" dot={{ r: 3, fill: '#eab308' }} activeDot={{ r: 5 }} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Monthly compliance */}
      {months.length > 0 && (
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
          {months.map(m => (
            <div key={m.month} className="bg-gray-800 rounded-xl border border-gray-700 p-3 text-center">
              <p className="text-xs text-gray-400 mb-1">{formatMonth(m.month)}</p>
              <p className="text-lg font-bold text-white">{Math.round(m.compliance_rate)}%</p>
              <p className="text-xs text-gray-500">{t('history.month.sessions', { completed: m.sessions_completed, planned: m.sessions_planned })}</p>
            </div>
          ))}
        </div>
      )}

      {/* Dense week list — vertical column of summary rows. Wraps cleanly at 375px. */}
      <div
        className="flex flex-col gap-2"
        aria-label={t('history.week.listLabel', { defaultValue: 'Weekly completion history' })}
      >
        {weeks.map(w => (
          <WeekRow key={w.plan_id} week={w} onOpen={onOpenWeek} />
        ))}
      </div>

      {/* Load more / inline error & retry */}
      {hasMore && (
        <div className="flex flex-col items-center gap-2">
          {loadMoreError && (
            <p className="text-sm text-red-400" role="alert">{t('history.loadError')}</p>
          )}
          <button
            type="button"
            onClick={handleLoadMore}
            disabled={loadingMore}
            className="inline-flex items-center gap-2 px-4 py-2 text-sm bg-gray-800 hover:bg-gray-700 disabled:opacity-60 text-white border border-gray-700 rounded-lg transition-colors"
          >
            {loadingMore ? (
              <>
                <Loader2 size={16} className="animate-spin" />
                {t('history.week.loadingMore')}
              </>
            ) : (
              t('history.week.loadMore')
            )}
          </button>
        </div>
      )}
    </div>
  )
}
