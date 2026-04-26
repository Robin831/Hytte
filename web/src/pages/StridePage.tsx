import { useState, useEffect, useCallback, useMemo, useRef, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, Plus, Trophy, Zap, ChevronDown, ChevronUp, RefreshCw, CheckCircle2, Circle, AlertTriangle, XCircle, History, Pencil } from 'lucide-react'
import { formatDate, formatDateTime } from '../utils/formatDate'
import type { StrideEvaluation, StrideEvaluationRecord, DayPlan } from '../types/stride'
import { ResponsiveContainer, AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip } from 'recharts'
import { TrainingBlockTimeline } from '../components/stride/TrainingBlockTimeline'
import StrideChatDrawer from '../components/stride/StrideChatDrawer'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../components/ui/dialog'

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

interface Plan {
  id: number
  user_id: number
  week_start: string
  week_end: string
  phase: string
  plan: DayPlan[]
  model: string
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

function complianceIcon(compliance: StrideEvaluation['compliance']) {
  switch (compliance) {
    case 'compliant':
      return <CheckCircle2 size={18} className="text-green-400" />
    case 'partial':
      return <AlertTriangle size={18} className="text-yellow-400" />
    case 'missed':
      return <XCircle size={18} className="text-red-400" />
    case 'bonus':
      return <CheckCircle2 size={18} className="text-blue-400" />
    case 'rest_day':
      return <CheckCircle2 size={18} className="text-gray-400" />
    default:
      return <Circle size={18} className="text-gray-400" />
  }
}

function complianceBadgeClass(compliance: StrideEvaluation['compliance']): string {
  switch (compliance) {
    case 'compliant':
      return 'bg-green-500/15 text-green-400 border-green-500/30'
    case 'partial':
      return 'bg-yellow-500/15 text-yellow-400 border-yellow-500/30'
    case 'missed':
      return 'bg-red-500/15 text-red-400 border-red-500/30'
    case 'bonus':
      return 'bg-blue-500/15 text-blue-400 border-blue-500/30'
    case 'rest_day':
      return 'bg-gray-500/15 text-gray-400 border-gray-500/30'
    default:
      return 'bg-gray-500/15 text-gray-400 border-gray-500/30'
  }
}

function flagIsSevere(flag: string): boolean {
  return flag === 'overtraining' || flag === 'injury_risk'
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

function DayCard({ day, completed, evaluation, changedDates, onRerun, rerunning }: { day: DayPlan; completed: boolean; evaluation?: StrideEvaluationRecord; changedDates?: Set<string>; onRerun?: (date: string) => void; rerunning?: boolean }) {
  const { t } = useTranslation('stride')
  const [expanded, setExpanded] = useState(false)

  const date = `${day.date}T00:00:00`
  const dayName = formatDate(date, { weekday: 'short' })
  const dateLabel = formatDate(date, { month: 'short', day: 'numeric' })

  const complianceLabel = evaluation ? t(`evaluation.${evaluation.eval.compliance}`) : null
  const hasExpandableContent = (!day.rest_day && !!day.session) || (!!evaluation && (day.rest_day || !day.session))
  const isHighlighted = changedDates?.has(day.date) ?? false

  return (
    <div className={`bg-gray-800 rounded-xl border border-gray-700 overflow-hidden transition-all duration-1000 ${isHighlighted ? 'ring-2 ring-yellow-400/50' : ''}`}>
      <div className="relative flex items-stretch">
        <button
          type="button"
          onClick={() => hasExpandableContent && setExpanded(v => !v)}
          className={`flex-1 min-w-0 flex items-center gap-3 p-3 text-left ${hasExpandableContent ? 'hover:bg-gray-700 active:bg-gray-600 cursor-pointer' : 'cursor-default'}`}
          aria-expanded={expanded && hasExpandableContent}
          aria-controls={`day-details-${day.date}`}
          disabled={!hasExpandableContent}
        >
          {/* Completion / evaluation indicator */}
          <div className="flex-shrink-0">
            {evaluation ? (
              complianceIcon(evaluation.eval.compliance)
            ) : completed ? (
              <CheckCircle2 size={18} className="text-green-400" />
            ) : (
              <Circle size={18} className="text-gray-600" />
            )}
          </div>

          {/* Day + date */}
          <div className="flex-shrink-0 w-16">
            <p className="text-xs font-semibold text-gray-400 uppercase">{dayName}</p>
            <p className="text-sm text-gray-300">{dateLabel}</p>
          </div>

          {/* Session summary + compliance badge + flag indicators */}
          <div className="flex-1 min-w-0 flex items-center gap-2 flex-wrap">
            {day.rest_day ? (
              <span className="text-xs font-medium px-2 py-0.5 rounded-full bg-gray-700 text-gray-400">{t('plan.restDay')}</span>
            ) : day.session ? (
              <p className="text-sm text-white truncate">{day.session.description}</p>
            ) : null}
            {evaluation && complianceLabel && (
              <span className={`text-xs font-medium px-2 py-0.5 rounded-full border ${complianceBadgeClass(evaluation.eval.compliance)}`}>
                {complianceLabel}
              </span>
            )}
            {evaluation && Array.isArray(evaluation.eval.flags) && evaluation.eval.flags.length > 0 && (
              <span className="flex items-center gap-1 text-xs text-yellow-400" aria-label={t('evaluation.warnings')}>
                <AlertTriangle size={12} />
                {evaluation.eval.flags.length}
              </span>
            )}
          </div>

          {/* Expand chevron */}
          {hasExpandableContent && (
            <div className="flex-shrink-0">
              {expanded ? (
                <ChevronUp size={16} className="text-gray-500" />
              ) : (
                <ChevronDown size={16} className="text-gray-500" />
              )}
            </div>
          )}
        </button>

        {/* Rerun coach evaluation for this day */}
        {onRerun && (
          <button
            type="button"
            onClick={() => onRerun(day.date)}
            disabled={rerunning}
            className="flex-shrink-0 px-3 flex items-center text-gray-500 hover:text-yellow-400 disabled:text-gray-600 disabled:cursor-not-allowed transition-colors border-l border-gray-700"
            aria-label={t('plan.rerunCoach')}
            title={t('plan.rerunCoach')}
          >
            <RefreshCw size={14} className={rerunning ? 'animate-spin' : ''} />
          </button>
        )}
      </div>

      {/* Accordion panel — CSS grid transition so expand/collapse animates smoothly on mobile */}
      <div
        id={`day-details-${day.date}`}
        className={`grid transition-[grid-template-rows] duration-200 ease-in-out ${
          expanded && hasExpandableContent ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]'
        }`}
        aria-hidden={!(expanded && hasExpandableContent)}
        // @ts-expect-error — `inert` is a valid HTML attribute not yet in React's typings
        inert={!(expanded && hasExpandableContent) ? '' : undefined}
      >
        <div className="overflow-hidden">
          <div className="px-4 pb-4 space-y-3 border-t border-gray-700 pt-3">
            {!day.rest_day && day.session && (
              <>
                {day.session.description && (
                  <p className="text-sm text-gray-200">{day.session.description}</p>
                )}
                {day.session.warmup && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.warmup')}</p>
                    <p className="text-sm text-gray-200">{day.session.warmup}</p>
                  </div>
                )}
                {day.session.main_set && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.mainSet')}</p>
                    <p className="text-sm text-gray-200">{day.session.main_set}</p>
                  </div>
                )}
                {day.session.cooldown && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.cooldown')}</p>
                    <p className="text-sm text-gray-200">{day.session.cooldown}</p>
                  </div>
                )}
                {day.session.strides && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.strides')}</p>
                    <p className="text-sm text-gray-200">{day.session.strides}</p>
                  </div>
                )}
                {day.session.target_hr_cap > 0 && (
                  <div>
                    <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('plan.targetHR')}</p>
                    <p className="text-sm text-gray-200">{t('plan.bpm', { value: day.session.target_hr_cap })}</p>
                  </div>
                )}
              </>
            )}

            {/* Stride evaluation section */}
            {evaluation && (() => {
              const flags = Array.isArray(evaluation.eval.flags) ? evaluation.eval.flags : []
              return (
                <div className={`space-y-2 ${!day.rest_day && day.session ? 'mt-3 pt-3 border-t border-gray-700' : ''}`}>
                  {evaluation.eval.notes && (
                    <div>
                      <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.coachNotes')}</p>
                      <p className="text-sm text-gray-200">{evaluation.eval.notes}</p>
                    </div>
                  )}
                  {flags.length > 0 && (
                    <div>
                      <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.warnings')}</p>
                      <div className="flex flex-wrap gap-1.5">
                        {flags.map(flag => (
                          <span
                            key={flag}
                            className={`inline-flex items-center gap-1 px-2 py-0.5 text-xs rounded-full border ${
                              flagIsSevere(flag)
                                ? 'bg-red-500/15 border-red-500/30 text-red-400'
                                : 'bg-yellow-500/15 border-yellow-500/30 text-yellow-400'
                            }`}
                          >
                            <AlertTriangle size={10} />
                            {t(`evaluation.flagLabels.${flag}`, { defaultValue: flag.replace(/_/g, ' ') })}
                          </span>
                        ))}
                      </div>
                    </div>
                  )}
                  {evaluation.eval.adjustments && (
                    <div>
                      <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.adjustments')}</p>
                      <p className="text-sm text-gray-400">{evaluation.eval.adjustments}</p>
                    </div>
                  )}
                </div>
              )
            })()}
          </div>
        </div>
      </div>
    </div>
  )
}

interface WeekSummary {
  plan_id: number
  week_start: string
  week_end: string
  phase: string
  sessions_planned: number
  sessions_completed: number
  completion_rate: number
}

interface MonthSummary {
  month: string
  sessions_planned: number
  sessions_completed: number
  compliance_rate: number
}

interface PlanHistoryData {
  weeks: WeekSummary[]
  months: MonthSummary[]
}

function PlanHistory() {
  const { t } = useTranslation('stride')
  const [data, setData] = useState<PlanHistoryData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/stride/history?limit=12', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const json = await res.json()
        if (!controller.signal.aborted) {
          setData({ weeks: json.weeks ?? [], months: json.months ?? [] })
        }
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        if (!controller.signal.aborted) setError(true)
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [])

  const chartData = useMemo(() => {
    if (!data) return []
    // Reverse to chronological order for the chart (oldest first).
    return [...data.weeks].reverse().map(w => ({
      label: formatDate(`${w.week_start}T00:00:00`, { month: 'short', day: 'numeric' }),
      rate: Math.min(Math.round(w.completion_rate), 100),
    }))
  }, [data])

  const formatMonth = (month: string) => {
    const [year, m] = month.split('-')
    return formatDate(new Date(Number(year), Number(m) - 1, 1), { month: 'short', year: 'numeric' })
  }

  if (loading) return <p className="text-sm text-gray-400">{t('loading')}</p>
  if (error) return <p className="text-sm text-red-400">{t('history.loadError')}</p>
  if (!data || data.weeks.length === 0) {
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
      {data.months.length > 0 && (
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
          {data.months.map(m => (
            <div key={m.month} className="bg-gray-800 rounded-xl border border-gray-700 p-3 text-center">
              <p className="text-xs text-gray-400 mb-1">{formatMonth(m.month)}</p>
              <p className="text-lg font-bold text-white">{Math.round(m.compliance_rate)}%</p>
              <p className="text-xs text-gray-500">{t('history.month.sessions', { completed: m.sessions_completed, planned: m.sessions_planned })}</p>
            </div>
          ))}
        </div>
      )}

      {/* Week list — horizontally scrollable with snap so users can swipe between weeks on mobile */}
      <div
        className="flex gap-3 overflow-x-auto snap-x snap-mandatory pb-2 -mx-4 px-4 sm:mx-0 sm:px-0 sm:flex-col sm:gap-2 sm:overflow-x-visible sm:snap-none"
        aria-label={t('history.week.listLabel', { defaultValue: 'Weekly completion history' })}
      >
        {data.weeks.map(w => {
          const pct = Math.min(Math.max(Math.round(Number(w.completion_rate) || 0), 0), 100)
          const barColor = pct >= 80 ? 'bg-green-500' : pct >= 50 ? 'bg-yellow-500' : 'bg-red-500'
          return (
            <div
              key={w.plan_id}
              className="snap-start flex-shrink-0 w-64 sm:w-auto bg-gray-800 rounded-xl border border-gray-700 p-3"
            >
              <div className="flex items-center justify-between mb-2">
                <div>
                  <p className="text-sm font-medium text-white">
                    {t('plan.weekOf', {
                      start: formatDate(`${w.week_start}T00:00:00`, { month: 'short', day: 'numeric' }),
                      end: formatDate(`${w.week_end}T00:00:00`, { month: 'short', day: 'numeric' }),
                    })}
                  </p>
                  {w.phase && (
                    <span className="text-xs px-1.5 py-0.5 bg-yellow-500/10 text-yellow-500 rounded">{w.phase}</span>
                  )}
                </div>
                <div className="text-right">
                  <p className="text-lg font-bold text-white">{pct}%</p>
                  <p className="text-xs text-gray-400">{t('history.week.sessions', { completed: w.sessions_completed, planned: w.sessions_planned })}</p>
                </div>
              </div>
              {w.sessions_planned > 0 && (
                <div
                  className="h-1.5 bg-gray-700 rounded-full overflow-hidden"
                  role="progressbar"
                  aria-valuenow={Math.min(pct, 100)}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-label={t('history.week.completionProgress', { pct: Math.min(pct, 100) })}
                >
                  <div className={`h-full rounded-full transition-all ${barColor}`} style={{ width: `${Math.min(pct, 100)}%` }} />
                </div>
              )}
            </div>
          )
        })}
      </div>
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
  const [currentPlan, setCurrentPlan] = useState<Plan | null>(null)
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

  // Note edit modal state
  const [editingNote, setEditingNote] = useState<Note | null>(null)
  const [editContent, setEditContent] = useState('')
  const [editTargetDate, setEditTargetDate] = useState('')
  const [editScope, setEditScope] = useState<NoteScope>('any')
  const [editSubmitting, setEditSubmitting] = useState(false)
  const [editError, setEditError] = useState('')

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
        const plan: Plan | null = data.plan ?? null
        setCurrentPlan(plan)
        // Fetch the two most recent plans so we can identify the previous one.
        // This lets us load its evaluations (e.g. Sunday workout feedback that
        // was evaluated after the new plan was generated).
        if (plan) {
          try {
            const listRes = await fetch('/api/stride/plans?limit=2&offset=0', { credentials: 'include', signal })
            if (listRes.ok) {
              const listData = await listRes.json()
              const plans: Plan[] = listData.plans ?? []
              const prev = plans.find((p: Plan) => p.id !== plan.id)
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

  // Parse "H:MM:SS" or "M:SS" target time string to seconds
  function parseTargetTime(s: string): number | null {
    if (!s.trim()) return null
    const parts = s.trim().split(':').map(Number)
    if (parts.some(isNaN)) return null
    if (parts.length === 3) return parts[0] * 3600 + parts[1] * 60 + parts[2]
    if (parts.length === 2) return parts[0] * 3600 + parts[1] * 60 // H:MM for race times
    return null
  }

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
      const newPlan: Plan | null = data.plan ?? null
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

  // Evaluations from the previous plan that don't match any day in the current
  // plan (e.g. Sunday workout evaluated Monday 01:00, still linked to old plan).
  // These would otherwise be invisible because no DayCard renders for their date.
  const previousPlanEvals = useMemo(() => {
    if (!currentPlan || !previousPlanId) return []
    const currentDates = new Set(sortedPlanDays.map(d => d.date))
    const orphans: Array<{ date: string; eval: StrideEvaluationRecord }> = []
    for (const rec of evaluations) {
      if (rec.plan_id !== previousPlanId) continue
      let date: string | undefined
      if (rec.workout_id != null) {
        date = workoutIdToDate.get(rec.workout_id)
      } else if (rec.eval.date) {
        date = rec.eval.date
      }
      if (date && !currentDates.has(date)) {
        orphans.push({ date, eval: rec })
      }
    }
    // Only keep evaluations with substantive feedback (skip plain rest_day confirmations).
    return orphans
      .filter(o => o.eval.eval.compliance !== 'rest_day' || o.eval.eval.notes || (o.eval.eval.flags && o.eval.eval.flags.length > 0))
      .sort((a, b) => a.date.localeCompare(b.date))
  }, [currentPlan, previousPlanId, evaluations, sortedPlanDays, workoutIdToDate])

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
        ) : (
          <>
            {notes.length > 0 && (
              <>
                <h3 className="text-sm font-medium text-gray-400 mb-2">{t('notes.activeLabel')}</h3>
                <div className="space-y-2 mb-4">
                  {notes.map(note => {
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
                  })}
                </div>
              </>
            )}

            {consumedNotes.length > 0 && (
              <>
                <h3 className="text-sm font-medium text-gray-400 mb-2">{t('notes.historyLabel')}</h3>
                <div className="space-y-2">
                  {consumedNotes.map(note => {
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
                  })}
                </div>
              </>
            )}
          </>
        )}
      </section>

      {/* Plan History */}
      <section>
        <h2 className="text-lg font-semibold text-white flex items-center gap-2 mb-4">
          <History size={18} className="text-gray-400" />
          {t('history.title')}
        </h2>
        <PlanHistory />
      </section>

      {/* Previous week feedback — evaluations linked to the old plan whose
          dates fall outside the current plan (e.g. Sunday workout eval). */}
      {previousPlanEvals.length > 0 && (
        <section>
          <h2 className="text-lg font-semibold text-white flex items-center gap-2 mb-4">
            <AlertTriangle size={18} className="text-yellow-400" />
            {t('plan.previousWeekFeedback')}
          </h2>
          <div className="space-y-2">
            {previousPlanEvals.map(({ date, eval: rec }) => {
              const dateStr = `${date}T00:00:00`
              const flags = Array.isArray(rec.eval.flags) ? rec.eval.flags : []
              return (
                <div key={rec.id} className="bg-gray-800 rounded-xl border border-gray-700 p-3 space-y-2">
                  <div className="flex items-center gap-3">
                    {complianceIcon(rec.eval.compliance)}
                    <div className="flex-shrink-0">
                      <p className="text-xs font-semibold text-gray-400 uppercase">{formatDate(dateStr, { weekday: 'short' })}</p>
                      <p className="text-sm text-gray-300">{formatDate(dateStr, { month: 'short', day: 'numeric' })}</p>
                    </div>
                    <div className="flex-1 min-w-0 flex items-center gap-2 flex-wrap">
                      <span className="text-sm text-gray-300">{rec.eval.planned_type.replace(/_/g, ' ')}</span>
                      <span className={`text-xs font-medium px-2 py-0.5 rounded-full border ${complianceBadgeClass(rec.eval.compliance)}`}>
                        {t(`evaluation.${rec.eval.compliance}`)}
                      </span>
                      {flags.length > 0 && (
                        <span className="flex items-center gap-1 text-xs text-yellow-400">
                          <AlertTriangle size={12} />
                          {flags.length}
                        </span>
                      )}
                    </div>
                  </div>
                  {rec.eval.notes && (
                    <div>
                      <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.coachNotes')}</p>
                      <p className="text-sm text-gray-200">{rec.eval.notes}</p>
                    </div>
                  )}
                  {flags.length > 0 && (
                    <div className="flex flex-wrap gap-1.5">
                      {flags.map(flag => (
                        <span
                          key={flag}
                          className={`inline-flex items-center gap-1 px-2 py-0.5 text-xs rounded-full border ${
                            flagIsSevere(flag)
                              ? 'bg-red-500/15 border-red-500/30 text-red-400'
                              : 'bg-yellow-500/15 border-yellow-500/30 text-yellow-400'
                          }`}
                        >
                          <AlertTriangle size={10} />
                          {t(`evaluation.flagLabels.${flag}`, { defaultValue: flag.replace(/_/g, ' ') })}
                        </span>
                      ))}
                    </div>
                  )}
                  {rec.eval.adjustments && (
                    <div>
                      <p className="text-xs font-semibold text-gray-500 uppercase mb-1">{t('evaluation.adjustments')}</p>
                      <p className="text-sm text-gray-400">{rec.eval.adjustments}</p>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </section>
      )}

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
    </div>
  )
}
