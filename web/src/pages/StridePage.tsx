import { useState, useEffect, useCallback, useMemo, useRef, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, Plus, Trophy, Zap, ChevronRight, RefreshCw, History, Pencil, Loader2 } from 'lucide-react'
import { formatDate, formatDateTime, formatNumber } from '../utils/formatDate'
import type { StrideEvaluationRecord, StridePlan, WeekSummary } from '../types/stride'
import { ResponsiveContainer, AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip } from 'recharts'
import { TrainingBlockTimeline } from '../components/stride/TrainingBlockTimeline'
import StrideChatDrawer from '../components/stride/StrideChatDrawer'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../components/ui/dialog'
import { DayCard } from '../components/stride/DayCard'
import { WeekDetailsModal } from '../components/stride/WeekDetailsModal'
import { parseTargetTime } from './strideUtils'

interface Race {
  id: number
  user_id: number
  name: string
  date: string
  distance_m: number
  target_time: number | null
  priority: 'A' | 'B' | 'C'
  notes: string
  result_time: number | null
  created_at: string
}

type NoteScope = 'any' | 'nightly' | 'weekly'

interface Note {
  id: number
  user_id: number
  plan_id: number | null
  content: string
  target_date: string
  consumed_at: string | null
  consumed_by: string | null
  scope: NoteScope
  created_at: string
}


function formatDistance(meters: number): string {
  if (meters >= 1000) {
    return `${(meters / 1000).toFixed(1)} km`
  }
  return `${meters} m`
}

function formatDuration(seconds: number | null): string {
  if (seconds === null) return '—'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  return `${m}:${String(s).padStart(2, '0')}`
}

function priorityLabel(priority: string): { label: string; class: string } {
  switch (priority) {
    case 'A':
      return { label: 'A', class: 'bg-yellow-500/20 text-yellow-400 border border-yellow-500/30' }
    case 'B':
      return { label: 'B', class: 'bg-blue-500/20 text-blue-400 border border-blue-500/30' }
    case 'C':
      return { label: 'C', class: 'bg-gray-500/20 text-gray-400 border border-gray-500/30' }
    default:
      return { label: priority, class: 'bg-gray-500/20 text-gray-400' }
  }
}

function weeksUntil(dateStr: string): number {
  const target = new Date(`${dateStr}T00:00:00`)
  const now = new Date()
  const diff = target.getTime() - now.getTime()
  return Math.ceil(diff / (7 * 24 * 60 * 60 * 1000))
}

function noteScopeBadgeClass(scope: NoteScope): string {
  switch (scope) {
    case 'nightly':
      return 'bg-indigo-500/15 text-indigo-300 border border-indigo-500/30'
    case 'weekly':
      return 'bg-purple-500/15 text-purple-300 border border-purple-500/30'
    case 'any':
    default:
      return 'bg-gray-500/15 text-gray-300 border border-gray-500/30'
  }
}

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

interface WeekRowProps {
  week: WeekSummary
  onOpen?: (week: WeekSummary) => void
}

function WeekRow({ week, onOpen }: WeekRowProps) {
  const { t } = useTranslation('stride')
  const zoneTooltipId = useId()
  const openDescId = useId()
  const [zoneTooltipVisible, setZoneTooltipVisible] = useState(false)
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

function PlanHistory({ onOpenWeek }: PlanHistoryProps) {
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

interface EditNoteDialogProps {
  open: boolean
  onClose: () => void
  onSubmit: (e: React.FormEvent) => void
  content: string
  onContentChange: (v: string) => void
  targetDate: string
  onTargetDateChange: (v: string) => void
  scope: NoteScope
  onScopeChange: (v: NoteScope) => void
  submitting: boolean
  error: string
}

function EditNoteDialog({
  open, onClose, onSubmit, content, onContentChange, targetDate, onTargetDateChange,
  scope, onScopeChange, submitting, error,
}: EditNoteDialogProps) {
  const { t } = useTranslation('stride')
  const titleId = useId()
  return (
    <Dialog open={open} onClose={onClose} aria-labelledby={titleId}>
      <DialogHeader id={titleId} title={t('notes.editTitle')} onClose={onClose} />
      <DialogBody>
        <form id="edit-note-form" onSubmit={onSubmit} className="space-y-3">
          <textarea
            value={content}
            onChange={e => onContentChange(e.target.value)}
            rows={4}
            aria-label={t('notes.editTitle')}
            className="w-full bg-gray-800 border border-gray-700 rounded-xl px-4 py-3 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500 resize-none"
          />
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <label htmlFor="edit-note-target-date" className="text-xs text-gray-400 w-24">{t('notes.targetDate')}</label>
              <input
                id="edit-note-target-date"
                type="date"
                value={targetDate}
                onChange={e => onTargetDateChange(e.target.value)}
                className="flex-1 bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500"
              />
            </div>
            <div className="flex items-center gap-2">
              <label htmlFor="edit-note-scope" className="text-xs text-gray-400 w-24">{t('notes.scopeLabel')}</label>
              <select
                id="edit-note-scope"
                value={scope}
                onChange={e => onScopeChange(e.target.value as NoteScope)}
                className="flex-1 bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500"
              >
                <option value="any">{t('notes.scope.any')}</option>
                <option value="nightly">{t('notes.scope.nightly')}</option>
                <option value="weekly">{t('notes.scope.weekly')}</option>
              </select>
            </div>
          </div>
          {error && <p className="text-sm text-red-400">{error}</p>}
        </form>
      </DialogBody>
      <DialogFooter>
        <button
          type="button"
          onClick={onClose}
          className="px-4 py-2 text-sm text-gray-300 hover:text-white rounded-lg transition-colors"
        >
          {t('notes.cancel')}
        </button>
        <button
          type="submit"
          form="edit-note-form"
          disabled={submitting || !content.trim()}
          className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-lg transition-colors"
        >
          {submitting ? t('notes.saving') : t('notes.save')}
        </button>
      </DialogFooter>
    </Dialog>
  )
}

export default function StridePage() {
  const { t } = useTranslation('stride')

  const [races, setRaces] = useState<Race[]>([])
  const [notes, setNotes] = useState<Note[]>([])
  const [consumedNotes, setConsumedNotes] = useState<Note[]>([])
  const [currentPlan, setCurrentPlan] = useState<StridePlan | null>(null)
  const [changedDates, setChangedDates] = useState<Set<string>>(new Set())
  const highlightTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [previousPlanId, setPreviousPlanId] = useState<number | null>(null)
  const [chatPlanId, setChatPlanId] = useState<number | null>(null)
  const [hasAnyPlan, setHasAnyPlan] = useState(false)
  const [completedDates, setCompletedDates] = useState<Set<string>>(new Set())
  const [workoutIdToDate, setWorkoutIdToDate] = useState<Map<number, string>>(new Map())
  const [evaluations, setEvaluations] = useState<StrideEvaluationRecord[]>([])
  const [racesLoading, setRacesLoading] = useState(true)
  const [notesLoading, setNotesLoading] = useState(true)
  const [planLoading, setPlanLoading] = useState(true)
  const [planError, setPlanError] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [generateError, setGenerateError] = useState('')
  const [rerunningDate, setRerunningDate] = useState<string | null>(null)
  const [rerunError, setRerunError] = useState('')
  const rerunAbortRef = useRef<AbortController | null>(null)

  // Race form state
  const [showRaceForm, setShowRaceForm] = useState(false)
  const [raceName, setRaceName] = useState('')
  const [raceDate, setRaceDate] = useState('')
  const [raceDistanceKm, setRaceDistanceKm] = useState('')
  const [raceTargetTime, setRaceTargetTime] = useState('')
  const [racePriority, setRacePriority] = useState<'A' | 'B' | 'C'>('B')
  const [raceNotes, setRaceNotes] = useState('')
  const [raceSubmitting, setRaceSubmitting] = useState(false)
  const [raceError, setRaceError] = useState('')

  // Note form state
  const [noteContent, setNoteContent] = useState('')
  const [noteTargetDate, setNoteTargetDate] = useState(() => {
    const now = new Date()
    return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  })
  const [noteScope, setNoteScope] = useState<NoteScope>('any')
  const [noteSubmitting, setNoteSubmitting] = useState(false)

  // Week details modal state — opened when the user clicks a Plan History row.
  const [selectedWeek, setSelectedWeek] = useState<WeekSummary | null>(null)

  // Note edit modal state
  const [editingNote, setEditingNote] = useState<Note | null>(null)
  const [editContent, setEditContent] = useState('')
  const [editTargetDate, setEditTargetDate] = useState('')
  const [editScope, setEditScope] = useState<NoteScope>('any')
  const [editSubmitting, setEditSubmitting] = useState(false)
  const [editError, setEditError] = useState('')
  const [olderNotesOpen, setOlderNotesOpen] = useState(false)

  const loadRaces = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/stride/races', { credentials: 'include', signal })
      if (!res.ok) {
        throw new Error(`Failed to load races: ${res.status} ${res.statusText}`)
      }
      const data = await res.json()
      if (!signal?.aborted) {
        setRaces(data.races ?? [])
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load races', error)
    } finally {
      if (!signal?.aborted) {
        setRacesLoading(false)
      }
    }
  }, [])

  const loadNotes = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/stride/notes?status=all', { credentials: 'include', signal })
      if (!res.ok) {
        throw new Error(`Failed to load notes: ${res.status} ${res.statusText}`)
      }
      const data = await res.json()
      if (!signal?.aborted) {
        const allNotes = data.notes ?? []
        setNotes(allNotes.filter((n: { consumed_at: string | null }) => !n.consumed_at))
        setConsumedNotes(allNotes.filter((n: { consumed_at: string | null }) => !!n.consumed_at))
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load notes', error)
    } finally {
      if (!signal?.aborted) {
        setNotesLoading(false)
      }
    }
  }, [])

  const loadHasAnyPlan = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/stride/plans?limit=1', { credentials: 'include', signal })
      if (!res.ok) return
      const data = await res.json()
      if (!signal?.aborted) {
        setHasAnyPlan((data.total ?? 0) > 0)
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to check plan existence', error)
    }
  }, [])

  const loadCurrentPlan = useCallback(async (signal?: AbortSignal) => {
    setPlanError(false)
    try {
      const res = await fetch('/api/stride/plans/current', { credentials: 'include', signal })
      if (res.status === 404) {
        if (!signal?.aborted) {
          setCurrentPlan(null)
          setPreviousPlanId(null)
        }
        return
      }
      if (!res.ok) {
        throw new Error(`Failed to load plan: ${res.status} ${res.statusText}`)
      }
      const data = await res.json()
      if (!signal?.aborted) {
        const plan: StridePlan | null = data.plan ?? null
        setCurrentPlan(plan)
        // Fetch the two most recent plans so we can identify the previous one.
        // This lets us load its evaluations (e.g. Sunday workout feedback that
        // was evaluated after the new plan was generated).
        if (plan) {
          try {
            const listRes = await fetch('/api/stride/plans?limit=2&offset=0', { credentials: 'include', signal })
            if (listRes.ok) {
              const listData = await listRes.json()
              const plans: StridePlan[] = listData.plans ?? []
              const prev = plans.find((p: StridePlan) => p.id !== plan.id)
              setPreviousPlanId(prev?.id ?? null)
            }
          } catch {
            // Non-fatal — we just won't show previous-plan evaluations.
          }
        }
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load current plan', error)
      if (!signal?.aborted) setPlanError(true)
    } finally {
      if (!signal?.aborted) {
        setPlanLoading(false)
      }
    }
  }, [])

  const loadWorkouts = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/training/workouts', { credentials: 'include', signal })
      if (!res.ok) return
      const data = await res.json()
      if (!signal?.aborted) {
        const workouts: Array<{ id: number; started_at: string }> = data.workouts ?? []
        const dates = new Set<string>(
          workouts.map(w => {
            const d = new Date(w.started_at)
            return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
          })
        )
        const idToDate = new Map<number, string>(
          workouts.map(w => {
            const d = new Date(w.started_at)
            return [w.id, `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`]
          })
        )
        setCompletedDates(dates)
        setWorkoutIdToDate(idToDate)
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load workouts for completion status', error)
    }
  }, [])

  // Returns evaluations for a plan as a list (doesn't set state).
  // Used by the merged fetch that combines current + previous plan evals.
  async function loadEvaluationsForPlan(planId: number, signal?: AbortSignal): Promise<StrideEvaluationRecord[]> {
    try {
      const res = await fetch(`/api/stride/evaluations?plan_id=${planId}`, { credentials: 'include', signal })
      if (!res.ok) return []
      const data = await res.json()
      return (data.evaluations ?? []) as StrideEvaluationRecord[]
    } catch {
      return []
    }
  }

  useEffect(() => {
    return () => {
      if (highlightTimerRef.current) clearTimeout(highlightTimerRef.current)
      if (rerunAbortRef.current) rerunAbortRef.current.abort()
    }
  }, [])

  // Sync chatPlanId to current plan — when a new plan is generated or loaded,
  // the chat switches to the fresh conversation automatically.
  useEffect(() => {
    if (currentPlan) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- chatPlanId tracks which plan's chat is shown; it can also be toggled to previousPlanId, so it cannot be purely derived
      setChatPlanId(currentPlan.id)
    }
  }, [currentPlan?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    loadRaces(controller.signal)
    loadNotes(controller.signal)
    loadCurrentPlan(controller.signal)
    loadHasAnyPlan(controller.signal)
    loadWorkouts(controller.signal)
    return () => { controller.abort() }
  }, [loadRaces, loadNotes, loadCurrentPlan, loadHasAnyPlan, loadWorkouts])

  const planId = currentPlan?.id

  useEffect(() => {
    if (!planId) return
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset before fetch; AbortController prevents stale updates on unmount
    setEvaluations([])
    const controller = new AbortController()
    // Load evaluations for both current and previous plan so that feedback
    // from the outgoing week (e.g. Sunday's workout evaluated Monday 01:00,
    // still linked to the old plan_id) is visible in the current week view.
    const fetchAll = async () => {
      const results = await Promise.all([
        loadEvaluationsForPlan(planId, controller.signal),
        previousPlanId ? loadEvaluationsForPlan(previousPlanId, controller.signal) : Promise.resolve([]),
      ])
      if (!controller.signal.aborted) {
        // Merge and de-duplicate by id; current plan takes precedence.
        const byId = new Map<number, StrideEvaluationRecord>()
        for (const e of [...results[1], ...results[0]]) byId.set(e.id, e)
        setEvaluations(Array.from(byId.values()))
      }
    }
    fetchAll()
    return () => { controller.abort() }
  }, [planId, previousPlanId])

  async function handleRerunDay(date: string) {
    setRerunError('')
    setRerunningDate(date)
    if (rerunAbortRef.current) rerunAbortRef.current.abort()
    const controller = new AbortController()
    rerunAbortRef.current = controller
    try {
      const res = await fetch(`/api/stride/days/${date}/reevaluate`, {
        method: 'POST',
        credentials: 'include',
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setRerunError(data.error ?? t('plan.rerunError'))
        return
      }
      if (currentPlan) {
        const [curr, prev] = await Promise.all([
          loadEvaluationsForPlan(currentPlan.id, controller.signal),
          previousPlanId ? loadEvaluationsForPlan(previousPlanId, controller.signal) : Promise.resolve([]),
        ])
        if (controller.signal.aborted) return
        const byId = new Map<number, StrideEvaluationRecord>()
        for (const e of [...prev, ...curr]) byId.set(e.id, e)
        setEvaluations(Array.from(byId.values()))
      }
      await loadNotes(controller.signal)
      if (controller.signal.aborted) return
      setChangedDates(new Set([date]))
      if (highlightTimerRef.current) clearTimeout(highlightTimerRef.current)
      highlightTimerRef.current = setTimeout(() => setChangedDates(new Set()), 3000)
    } catch (err) {
      if ((err as { name?: string })?.name === 'AbortError') return
      setRerunError(t('plan.rerunError'))
    } finally {
      if (rerunAbortRef.current === controller) {
        rerunAbortRef.current = null
      }
      if (!controller.signal.aborted) {
        setRerunningDate(null)
      }
    }
  }

  async function handleGeneratePlan() {
    setGenerateError('')
    setGenerating(true)
    try {
      const weekMode = hasAnyPlan ? 'next' : 'current'
      const res = await fetch(`/api/stride/plans/generate?week=${weekMode}`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setGenerateError(data.error ?? t('plan.generateError'))
        return
      }
      const data = await res.json()
      const newPlan: StridePlan | null = data.plan ?? null
      if (newPlan) {
        // The current plan becomes the previous one; update before replacing.
        setCurrentPlan(prev => {
          if (prev && prev.id !== newPlan.id) {
            setPreviousPlanId(prev.id)
          }
          return newPlan
        })
      } else {
        setCurrentPlan(null)
      }
    } catch {
      setGenerateError(t('plan.generateError'))
    } finally {
      setGenerating(false)
    }
  }

  async function handleCreateRace(e: React.FormEvent) {
    e.preventDefault()
    setRaceError('')
    setRaceSubmitting(true)
    try {
      const distanceM = parseFloat(raceDistanceKm) * 1000
      if (isNaN(distanceM) || distanceM <= 0) {
        setRaceError(t('races.form.error.invalidDistance'))
        return
      }

      const targetTime = parseTargetTime(raceTargetTime)
      if (raceTargetTime.trim() !== '' && targetTime === null) {
        setRaceError(t('races.form.error.invalidTargetTime'))
        return
      }

      const payload = {
        name: raceName,
        date: raceDate,
        distance_m: distanceM,
        target_time: targetTime,
        priority: racePriority,
        notes: raceNotes,
      }

      const res = await fetch('/api/stride/races', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })

      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setRaceError(data.error ?? t('races.form.error.create'))
        return
      }

      setRaceName('')
      setRaceDate('')
      setRaceDistanceKm('')
      setRaceTargetTime('')
      setRacePriority('B')
      setRaceNotes('')
      setShowRaceForm(false)
      await loadRaces()
    } catch {
      setRaceError(t('races.form.error.create'))
    } finally {
      setRaceSubmitting(false)
    }
  }

  async function handleDeleteRace(id: number) {
    try {
      const res = await fetch(`/api/stride/races/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setRaceError(data.error ?? t('races.form.error.delete'))
        return
      }
      setRaces(prev => prev.filter(r => r.id !== id))
    } catch {
      setRaceError(t('races.form.error.delete'))
    }
  }

  async function handleCreateNote(e: React.FormEvent) {
    e.preventDefault()
    if (!noteContent.trim()) return
    setNoteSubmitting(true)
    try {
      const res = await fetch('/api/stride/notes', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: noteContent, target_date: noteTargetDate, scope: noteScope }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        console.error('Failed to create note', data.error ?? res.statusText)
        return
      }
      setNoteContent('')
      setNoteScope('any')
      await loadNotes()
    } catch (error) {
      console.error('Failed to create note', error)
    } finally {
      setNoteSubmitting(false)
    }
  }

  function openEditNote(note: Note) {
    setEditingNote(note)
    setEditContent(note.content)
    setEditTargetDate(note.target_date)
    setEditScope(note.scope ?? 'any')
    setEditError('')
  }

  function closeEditNote() {
    setEditingNote(null)
    setEditError('')
  }

  async function handleEditNoteSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!editingNote) return
    if (!editContent.trim()) return
    setEditSubmitting(true)
    setEditError('')
    try {
      const res = await fetch(`/api/stride/notes/${editingNote.id}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          content: editContent,
          target_date: editTargetDate,
          scope: editScope,
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        if (res.status === 409) {
          setEditError(t('notes.editError.consumed'))
        } else {
          setEditError(data.error ?? t('notes.editError.generic'))
        }
        return
      }
      closeEditNote()
      await loadNotes()
    } catch (error) {
      console.error('Failed to update note', error)
      setEditError(t('notes.editError.generic'))
    } finally {
      setEditSubmitting(false)
    }
  }

  async function handleDeleteNote(id: number) {
    try {
      const res = await fetch(`/api/stride/notes/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        console.error('Failed to delete note', data.error)
        return
      }
      setNotes(prev => prev.filter(n => n.id !== id))
      setConsumedNotes(prev => prev.filter(n => n.id !== id))
    } catch (error) {
      console.error('Failed to delete note', error)
    }
  }

  const now = new Date()
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  const upcomingRaces = races.filter(r => r.date >= today)
  const pastRaces = races.filter(r => r.date < today)

  // Sort plan days Monday–Sunday
  const sortedPlanDays = currentPlan
    ? [...currentPlan.plan].sort((a, b) => a.date.localeCompare(b.date))
    : []

  // Partition notes into "active" (target_date within the last 7 days or in the future)
  // and "older" so the Coach Notes section stays compact when weeks of history accumulate.
  // Subtract 6 days so the window is exactly 7 calendar days inclusive (today + prior 6).
  // Depend on `today` so the cutoff updates if the page stays mounted across a day boundary.
  const noteRecentCutoff = useMemo(() => {
    const d = new Date()
    d.setDate(d.getDate() - 6)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }, [today])

  const { activeNotes, olderNotes, activeConsumedNotes, olderConsumedNotes } = useMemo(() => {
    const isRecent = (n: Note) => (n.target_date ?? '') >= noteRecentCutoff
    return {
      activeNotes: notes.filter(isRecent),
      olderNotes: notes.filter(n => !isRecent(n)),
      activeConsumedNotes: consumedNotes.filter(isRecent),
      olderConsumedNotes: consumedNotes.filter(n => !isRecent(n)),
    }
  }, [notes, consumedNotes, noteRecentCutoff])
  const olderNoteCount = olderNotes.length + olderConsumedNotes.length

  // Map each plan day date to its newest stride evaluation (via workout date lookup,
  // or via eval.date for rest_day/missed evaluations without a workout).
  // evaluations is ordered created_at DESC so the first entry per date is the newest.
  const dayEvaluationMap = useMemo(() => {
    const map = new Map<string, StrideEvaluationRecord>()
    for (const rec of evaluations) {
      if (rec.workout_id != null) {
        const date = workoutIdToDate.get(rec.workout_id)
        if (date && !map.has(date)) map.set(date, rec)
      } else if (rec.eval.date && !map.has(rec.eval.date)) {
        map.set(rec.eval.date, rec)
      }
    }
    return map
  }, [evaluations, workoutIdToDate])

  return (
    <div className="max-w-2xl mx-auto px-4 py-6 space-y-8">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Zap size={28} className="text-yellow-400" />
        <div>
          <h1 className="text-2xl font-bold text-white">{t('title')}</h1>
          <p className="text-sm text-gray-400">{t('subtitle')}</p>
        </div>
      </div>

      {/* Training Block Timeline */}
      <TrainingBlockTimeline races={races} loading={racesLoading} />

      {/* Weekly Plan */}
      <section>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">{t('plan.title')}</h2>
          <button
            type="button"
            onClick={handleGeneratePlan}
            disabled={generating}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white rounded-lg transition-colors"
          >
            <RefreshCw size={14} className={generating ? 'animate-spin' : ''} />
            {generating ? t('plan.generating') : t('plan.generate')}
          </button>
        </div>

        {generateError && (
          <p className="mb-3 text-sm text-red-400">{generateError}</p>
        )}

        {rerunError && (
          <p className="mb-3 text-sm text-red-400">{rerunError}</p>
        )}

        {planLoading ? (
          <p className="text-sm text-gray-400">{t('loading')}</p>
        ) : planError ? (
          <p className="text-sm text-red-400">{t('plan.loadError')}</p>
        ) : currentPlan === null ? (
          <div className="flex flex-col items-center justify-center py-10 text-center bg-gray-800/50 rounded-xl border border-gray-700 border-dashed">
            <Zap size={32} className="text-gray-600 mb-3" />
            <p className="text-sm font-medium text-gray-300 mb-1">{t('plan.empty')}</p>
            <p className="text-xs text-gray-500">{t('plan.emptyHint')}</p>
          </div>
        ) : (
          <div>
            {/* Week header */}
            <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500">
              <span>
                {t('plan.weekOf', {
                  start: formatDate(`${currentPlan.week_start}T00:00:00`, { month: 'short', day: 'numeric' }),
                  end: formatDate(`${currentPlan.week_end}T00:00:00`, { month: 'short', day: 'numeric' }),
                })}
              </span>
              {currentPlan.phase && (
                <span className="px-1.5 py-0.5 bg-yellow-500/10 text-yellow-500 rounded">{t('plan.phase', { phase: currentPlan.phase })}</span>
              )}
              <span>
                {t('plan.generatedAt', {
                  date: formatDate(currentPlan.created_at, { dateStyle: 'medium' }),
                })}
              </span>
            </div>

            {/* Day cards */}
            <div className="space-y-2">
              {sortedPlanDays.map(day => (
                <DayCard
                  key={day.date}
                  day={day}
                  completed={completedDates.has(day.date)}
                  evaluation={dayEvaluationMap.get(day.date)}
                  changedDates={changedDates}
                  onRerun={handleRerunDay}
                  rerunning={rerunningDate === day.date}
                />
              ))}
            </div>

            {/* Chat drawer */}
            <StrideChatDrawer
              planId={chatPlanId ?? currentPlan.id}
              currentPlanId={currentPlan.id}
              onViewPreviousChat={previousPlanId ? () => {
                setChatPlanId(prev =>
                  prev === currentPlan.id ? previousPlanId : currentPlan.id
                )
              } : undefined}
              onPlanUpdated={(newPlan) => {
                if (currentPlan) {
                  const oldMap = new Map(currentPlan.plan.map(d => [d.date, JSON.stringify(d)]))
                  const changed = new Set<string>()
                  for (const day of newPlan) {
                    if (oldMap.get(day.date) !== JSON.stringify(day)) {
                      changed.add(day.date)
                    }
                  }
                  if (changed.size > 0) {
                    setChangedDates(changed)
                    if (highlightTimerRef.current) clearTimeout(highlightTimerRef.current)
                    highlightTimerRef.current = setTimeout(() => setChangedDates(new Set()), 3000)
                  }
                }
                setCurrentPlan(prev => prev ? { ...prev, plan: newPlan } : prev)
              }}
            />
          </div>
        )}
      </section>

      {/* Race Calendar */}
      <section>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white flex items-center gap-2">
            <Trophy size={18} className="text-yellow-400" />
            {t('races.title')}
          </h2>
          <button
            type="button"
            onClick={() => setShowRaceForm(v => !v)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors"
          >
            <Plus size={14} />
            {t('races.add')}
          </button>
        </div>

        {/* Race form */}
        {showRaceForm && (
          <form onSubmit={handleCreateRace} className="mb-4 p-4 bg-gray-800 rounded-xl border border-gray-700 space-y-3">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <label htmlFor="race-name" className="block text-xs text-gray-400 mb-1">{t('races.form.name')}</label>
                <input
                  id="race-name"
                  type="text"
                  value={raceName}
                  onChange={e => setRaceName(e.target.value)}
                  required
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
                  placeholder={t('races.form.namePlaceholder')}
                />
              </div>
              <div>
                <label htmlFor="race-date" className="block text-xs text-gray-400 mb-1">{t('races.form.date')}</label>
                <input
                  id="race-date"
                  type="date"
                  value={raceDate}
                  onChange={e => setRaceDate(e.target.value)}
                  required
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white focus:outline-none focus:border-blue-500"
                />
              </div>
              <div>
                <label htmlFor="race-distance" className="block text-xs text-gray-400 mb-1">{t('races.form.distance')}</label>
                <input
                  id="race-distance"
                  type="number"
                  step="0.001"
                  min="0.001"
                  value={raceDistanceKm}
                  onChange={e => setRaceDistanceKm(e.target.value)}
                  required
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
                  placeholder="42.195"
                />
              </div>
              <div>
                <label htmlFor="race-target" className="block text-xs text-gray-400 mb-1">{t('races.form.targetTime')}</label>
                <input
                  id="race-target"
                  type="text"
                  value={raceTargetTime}
                  onChange={e => setRaceTargetTime(e.target.value)}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
                  placeholder="3:30:00"
                />
              </div>
              <div>
                <label htmlFor="race-priority" className="block text-xs text-gray-400 mb-1">{t('races.form.priority')}</label>
                <select
                  id="race-priority"
                  value={racePriority}
                  onChange={e => setRacePriority(e.target.value as 'A' | 'B' | 'C')}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white focus:outline-none focus:border-blue-500"
                >
                  <option value="A">{t('races.form.priorityA')}</option>
                  <option value="B">{t('races.form.priorityB')}</option>
                  <option value="C">{t('races.form.priorityC')}</option>
                </select>
              </div>
            </div>
            <div>
              <label htmlFor="race-notes" className="block text-xs text-gray-400 mb-1">{t('races.form.notes')}</label>
              <input
                id="race-notes"
                type="text"
                value={raceNotes}
                onChange={e => setRaceNotes(e.target.value)}
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
                placeholder={t('races.form.notesPlaceholder')}
              />
            </div>
            {raceError && <p className="text-sm text-red-400">{raceError}</p>}
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={raceSubmitting}
                className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-lg transition-colors"
              >
                {raceSubmitting ? t('races.form.saving') : t('races.form.save')}
              </button>
              <button
                type="button"
                onClick={() => { setShowRaceForm(false); setRaceError('') }}
                className="px-4 py-2 text-sm bg-gray-700 hover:bg-gray-600 text-white rounded-lg transition-colors"
              >
                {t('races.form.cancel')}
              </button>
            </div>
          </form>
        )}

        {racesLoading ? (
          <p className="text-sm text-gray-400">{t('loading')}</p>
        ) : upcomingRaces.length === 0 ? (
          <p className="text-sm text-gray-500">{t('races.empty')}</p>
        ) : (
          <div className="space-y-2">
            {upcomingRaces.map(race => {
              const weeks = weeksUntil(race.date)
              const p = priorityLabel(race.priority)
              return (
                <div key={race.id} className="flex items-center gap-3 p-3 bg-gray-800 rounded-xl border border-gray-700 group">
                  <span className={`text-xs font-semibold px-2 py-0.5 rounded-full ${p.class}`}>{p.label}</span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-white truncate">{race.name}</p>
                    <p className="text-xs text-gray-400">
                      {formatDate(`${race.date}T00:00:00`, { dateStyle: 'medium' })}
                      {' · '}
                      {formatDistance(race.distance_m)}
                      {race.target_time != null && ` · ${t('races.target')}: ${formatDuration(race.target_time)}`}
                      {race.result_time != null && ` · ${t('races.result')}: ${formatDuration(race.result_time)}`}
                      {weeks > 0 && ` · ${t('races.weeksAway', { count: weeks })}`}
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => handleDeleteRace(race.id)}
                    className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
                    aria-label={t('races.delete')}
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              )
            })}
          </div>
        )}

        {/* Past races */}
        {pastRaces.length > 0 && (
          <details className="mt-4">
            <summary className="text-sm text-gray-500 cursor-pointer hover:text-gray-300">{t('races.past', { count: pastRaces.length })}</summary>
            <div className="mt-2 space-y-2">
              {pastRaces.map(race => {
                const p = priorityLabel(race.priority)
                return (
                  <div key={race.id} className="flex items-center gap-3 p-3 bg-gray-800/50 rounded-xl border border-gray-700/50 group opacity-60">
                    <span className={`text-xs font-semibold px-2 py-0.5 rounded-full ${p.class}`}>{p.label}</span>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-white truncate">{race.name}</p>
                      <p className="text-xs text-gray-400">
                        {formatDate(`${race.date}T00:00:00`, { dateStyle: 'medium' })}
                        {' · '}
                        {formatDistance(race.distance_m)}
                        {race.target_time != null && ` · ${t('races.target')}: ${formatDuration(race.target_time)}`}
                        {race.result_time != null && ` · ${t('races.result')}: ${formatDuration(race.result_time)}`}
                      </p>
                    </div>
                    <button
                      onClick={() => handleDeleteRace(race.id)}
                      className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
                      aria-label={t('races.delete')}
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                )
              })}
            </div>
          </details>
        )}
      </section>

      {/* Coach Notes */}
      <section>
        <h2 className="text-lg font-semibold text-white mb-4">{t('notes.title')}</h2>
        <form onSubmit={handleCreateNote} className="mb-4">
          <textarea
            value={noteContent}
            onChange={e => setNoteContent(e.target.value)}
            placeholder={t('notes.placeholder')}
            aria-label={t('notes.title')}
            rows={3}
            className="w-full bg-gray-800 border border-gray-700 rounded-xl px-4 py-3 text-base sm:text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500 resize-none"
          />
          <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-3">
              <div className="flex items-center gap-2">
                <label htmlFor="note-target-date" className="text-xs text-gray-400">{t('notes.targetDate')}</label>
                <input
                  id="note-target-date"
                  type="date"
                  value={noteTargetDate}
                  onChange={e => setNoteTargetDate(e.target.value)}
                  className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500"
                />
              </div>
              <div className="flex items-center gap-2">
                <label htmlFor="note-scope" className="text-xs text-gray-400">{t('notes.scopeLabel')}</label>
                <select
                  id="note-scope"
                  value={noteScope}
                  onChange={e => setNoteScope(e.target.value as NoteScope)}
                  className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-blue-500"
                >
                  <option value="any">{t('notes.scope.any')}</option>
                  <option value="nightly">{t('notes.scope.nightly')}</option>
                  <option value="weekly">{t('notes.scope.weekly')}</option>
                </select>
              </div>
            </div>
            <button
              type="submit"
              disabled={noteSubmitting || !noteContent.trim()}
              className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-lg transition-colors"
            >
              {noteSubmitting ? t('notes.saving') : t('notes.add')}
            </button>
          </div>
        </form>

        {notesLoading ? (
          <p className="text-sm text-gray-400">{t('loading')}</p>
        ) : notes.length === 0 && consumedNotes.length === 0 ? (
          <p className="text-sm text-gray-500">{t('notes.empty')}</p>
        ) : (() => {
            const renderActiveNote = (note: Note) => {
              const scope: NoteScope = note.scope ?? 'any'
              return (
                <div key={note.id} className="flex items-start gap-3 p-3 bg-gray-800 rounded-xl border border-gray-700 group">
                  <div className="flex-1 min-w-0 space-y-1">
                    <p className="text-sm text-gray-200 whitespace-pre-wrap">{note.content}</p>
                    <span className={`inline-flex items-center px-1.5 py-0.5 text-xs rounded ${noteScopeBadgeClass(scope)}`}>
                      {t(`notes.scope.${scope}`)}
                    </span>
                  </div>
                  <div className="flex-shrink-0 flex flex-col items-end gap-1">
                    <div className="flex items-center gap-1">
                      <button
                        type="button"
                        onClick={() => openEditNote(note)}
                        className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-blue-400 transition-all"
                        aria-label={t('notes.edit')}
                      >
                        <Pencil size={14} />
                      </button>
                      <button
                        type="button"
                        onClick={() => handleDeleteNote(note.id)}
                        className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
                        aria-label={t('notes.delete')}
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                    <span className="text-xs text-gray-500">
                      {note.target_date && <span className="mr-1">{formatDate(`${note.target_date}T00:00:00`)}</span>}
                      {formatDateTime(note.created_at, { dateStyle: 'short', timeStyle: 'short' })}
                    </span>
                  </div>
                </div>
              )
            }

            const renderConsumedNote = (note: Note) => {
              const scope: NoteScope = note.scope ?? 'any'
              return (
                <div key={note.id} className="flex items-start gap-3 p-3 bg-gray-800/60 rounded-xl border border-gray-700/50 group">
                  <div className="flex-1 min-w-0 space-y-1">
                    <p className="text-sm text-gray-400 whitespace-pre-wrap">{note.content}</p>
                    <span className={`inline-flex items-center px-1.5 py-0.5 text-xs rounded ${noteScopeBadgeClass(scope)} opacity-70`}>
                      {t(`notes.scope.${scope}`)}
                    </span>
                  </div>
                  <div className="flex-shrink-0 flex flex-col items-end gap-1">
                    <button
                      type="button"
                      onClick={() => handleDeleteNote(note.id)}
                      className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
                      aria-label={t('notes.delete')}
                    >
                      <Trash2 size={14} />
                    </button>
                    <div className="flex flex-col items-end gap-0.5">
                      {(() => {
                        const consumedByLabel = note.consumed_by === 'nightly'
                          ? t('notes.consumedByProcess.nightly')
                          : note.consumed_by === 'weekly'
                            ? t('notes.consumedByProcess.weekly')
                            : note.consumed_by === 'manual'
                              ? t('notes.consumedByProcess.manual')
                              : null
                        const consumedDate = note.consumed_at ? formatDate(note.consumed_at) : null
                        if (!consumedByLabel || !consumedDate) return null
                        return (
                          <span className="inline-flex items-center gap-1 px-1.5 py-0.5 text-xs rounded bg-gray-700/50 text-gray-400">
                            {t('notes.consumedBy', { process: consumedByLabel, date: consumedDate })}
                          </span>
                        )
                      })()}
                      <span className="text-xs text-gray-500">
                        {note.target_date && <span className="mr-1">{formatDate(`${note.target_date}T00:00:00`)}</span>}
                        {formatDateTime(note.created_at, { dateStyle: 'short', timeStyle: 'short' })}
                      </span>
                    </div>
                  </div>
                </div>
              )
            }

            return (
              <>
                {activeNotes.length > 0 && (
                  <>
                    <h3 className="text-sm font-medium text-gray-400 mb-2">{t('notes.activeLabel')}</h3>
                    <div className="space-y-2 mb-4">
                      {activeNotes.map(renderActiveNote)}
                    </div>
                  </>
                )}

                {activeConsumedNotes.length > 0 && (
                  <>
                    <h3 className="text-sm font-medium text-gray-400 mb-2">{t('notes.historyLabel')}</h3>
                    <div className="space-y-2">
                      {activeConsumedNotes.map(renderConsumedNote)}
                    </div>
                  </>
                )}

                {olderNoteCount > 0 && (
                  <details
                    className="mt-4"
                    open={olderNotesOpen}
                  >
                    <summary
                      className="text-sm text-gray-500 cursor-pointer hover:text-gray-300"
                      onClick={e => { e.preventDefault(); setOlderNotesOpen(prev => !prev) }}
                    >
                      {t('notes.olderNotes', { count: olderNoteCount })}
                    </summary>
                    <div className="mt-2 space-y-2">
                      {olderNotes.map(renderActiveNote)}
                      {olderConsumedNotes.map(renderConsumedNote)}
                    </div>
                  </details>
                )}
              </>
            )
          })()}
      </section>

      {/* Plan History */}
      <section>
        <h2 className="text-lg font-semibold text-white flex items-center gap-2 mb-4">
          <History size={18} className="text-gray-400" />
          {t('history.title')}
        </h2>
        <PlanHistory onOpenWeek={setSelectedWeek} />
      </section>

      {/* Edit-note modal */}
      <EditNoteDialog
        open={editingNote !== null}
        onClose={closeEditNote}
        onSubmit={handleEditNoteSubmit}
        content={editContent}
        onContentChange={setEditContent}
        targetDate={editTargetDate}
        onTargetDateChange={setEditTargetDate}
        scope={editScope}
        onScopeChange={setEditScope}
        submitting={editSubmitting}
        error={editError}
      />

      {/* Week details modal */}
      <WeekDetailsModal
        week={selectedWeek}
        workoutIdToDate={workoutIdToDate}
        onClose={() => setSelectedWeek(null)}
      />
    </div>
  )
}
