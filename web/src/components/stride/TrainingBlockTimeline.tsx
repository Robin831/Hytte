import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Trophy, Flag } from 'lucide-react'
import { formatDate } from '../../utils/formatDate'
import type { MacroPlanView, MacroWeek } from '../../types/stride'
import { MS_PER_WEEK, groupWeekRuns, macroWeekForDate, mondayKeyOf, mondayToDate, sortMacroWeeks } from './macroPlan'
import type { MacroWeekRun } from './macroPlan'

interface Race {
  id: number
  name: string
  date: string
  priority: 'A' | 'B' | 'C'
  result_time: number | null
}

interface TrainingBlockTimelineProps {
  races: Race[]
  loading?: boolean
  // The active macro block, when the athlete has one. Its per-week phases and
  // mesocycles drive the timeline; without it the A-race heuristic below is
  // what draws the phases.
  macroPlan?: MacroPlanView | null
}

// The six phases a macro week can carry. The A-race fallback only ever produces
// the first four.
type Phase = 'base' | 'build' | 'peak' | 'taper' | 'race' | 'recovery'

interface PhaseStyle {
  bg: string
  text: string
  border: string
}

interface PhaseBlock {
  // Unique within the timeline: a macro block can revisit a phase (base →
  // build → recovery → build), so the phase name alone is not a stable key.
  key: string
  phase: string
  label: string
  startDate: Date
  endDate: Date
  widthPct: number
  offsetPct: number
}

const PHASE_STYLES: Record<Phase, PhaseStyle> = {
  base: {
    bg: 'bg-blue-500/25',
    text: 'text-blue-300',
    border: 'border-blue-500/40',
  },
  build: {
    bg: 'bg-green-500/25',
    text: 'text-green-300',
    border: 'border-green-500/40',
  },
  peak: {
    bg: 'bg-orange-500/25',
    text: 'text-orange-300',
    border: 'border-orange-500/40',
  },
  taper: {
    bg: 'bg-red-500/25',
    text: 'text-red-300',
    border: 'border-red-500/40',
  },
  race: {
    bg: 'bg-yellow-500/25',
    text: 'text-yellow-300',
    border: 'border-yellow-500/40',
  },
  recovery: {
    bg: 'bg-purple-500/25',
    text: 'text-purple-300',
    border: 'border-purple-500/40',
  },
}

const UNKNOWN_PHASE_STYLE: PhaseStyle = {
  bg: 'bg-gray-500/25',
  text: 'text-gray-300',
  border: 'border-gray-500/40',
}

// A macro week's phase comes from the server, so an unrecognised value renders
// in neutral grey rather than crashing on a missing palette entry.
function phaseStyle(phase: string): PhaseStyle {
  return PHASE_STYLES[phase as Phase] ?? UNKNOWN_PHASE_STYLE
}

// Standard phase durations in weeks (working backwards from race day)
const TAPER_WEEKS = 2
const PEAK_WEEKS = 4
const BUILD_WEEKS = 6

interface MacroTimeline {
  phases: PhaseBlock[]
  mesocycles: PhaseBlock[]
  currentPct: number
  currentPhase: string
  currentWeek: MacroWeek | null
  // Offset/width of the current week within the block, for the highlight box.
  currentWeekOffsetPct: number
  currentWeekWidthPct: number
  weeksLeft: number
  endDate: Date
  hasAnchorRace: boolean
  goalStatement: string
}

// Builds the timeline from the macro block's week rows: per-week phases grouped
// into phase segments, the same weeks grouped again by mesocycle, and the
// position of the week the athlete is training now.
function buildMacroTimeline(view: MacroPlanView | null, today: Date): MacroTimeline | null {
  if (!view || view.weeks.length === 0) return null

  const weeks = sortMacroWeeks(view.weeks)
  const startDate = mondayToDate(weeks[0].week_start)
  const endDate = new Date(mondayToDate(weeks[weeks.length - 1].week_start).getTime() + MS_PER_WEEK)
  const totalMs = endDate.getTime() - startDate.getTime()
  if (totalMs <= 0) return null

  const toBlock = (run: MacroWeekRun, phase: string, label: string): PhaseBlock => {
    const blockStart = mondayToDate(run.startWeek.week_start)
    const blockEnd = new Date(blockStart.getTime() + run.weeks * MS_PER_WEEK)
    return {
      key: `${label}-${run.startWeek.week_start}`,
      phase,
      label,
      startDate: blockStart,
      endDate: blockEnd,
      offsetPct: ((blockStart.getTime() - startDate.getTime()) / totalMs) * 100,
      widthPct: ((blockEnd.getTime() - blockStart.getTime()) / totalMs) * 100,
    }
  }

  const phases = groupWeekRuns(weeks, w => w.phase).map(run => toBlock(run, run.value, run.value))
  const mesocycles = groupWeekRuns(weeks, w => w.mesocycle)
    .filter(run => run.value !== '')
    .map(run => toBlock(run, run.startWeek.phase, run.value))

  const todayMs = today.getTime()
  const currentWeek = macroWeekForDate(weeks, today)

  const currentPct = Math.max(0, Math.min(100, ((todayMs - startDate.getTime()) / totalMs) * 100))
  const currentWeekOffsetPct = currentWeek
    ? ((mondayToDate(currentWeek.week_start).getTime() - startDate.getTime()) / totalMs) * 100
    : 0
  const currentWeekWidthPct = (MS_PER_WEEK / totalMs) * 100

  // Weeks still ahead, counting the one in progress. Before the block starts
  // that is all of them; after it ends, none.
  const thisMonday = mondayKeyOf(today)
  const weeksLeft = weeks.filter(w => w.week_start >= thisMonday).length

  return {
    phases,
    mesocycles,
    currentPct,
    currentPhase: currentWeek?.phase ?? weeks[0].phase,
    currentWeek,
    currentWeekOffsetPct,
    currentWeekWidthPct,
    weeksLeft,
    endDate,
    hasAnchorRace: view.plan.goal.anchor_race_id != null || weeks.some(w => w.race_id != null),
    goalStatement: view.plan.goal.statement,
  }
}

export function TrainingBlockTimeline({ races, loading, macroPlan }: TrainingBlockTimelineProps) {
  const { t } = useTranslation('stride')

  // Date-granular so the memos below do not recompute on every render.
  const todayStr = useMemo(() => {
    const now = new Date()
    const y = now.getFullYear()
    const m = String(now.getMonth() + 1).padStart(2, '0')
    const d = String(now.getDate()).padStart(2, '0')
    return `${y}-${m}-${d}`
  }, [])
  const today = useMemo(() => new Date(`${todayStr}T00:00:00`), [todayStr])

  // Nearest upcoming A-priority race that hasn't been completed
  const goalRace = useMemo(() => {
    return races
      .filter(r => r.priority === 'A' && r.date >= todayStr && r.result_time == null)
      .sort((a, b) => a.date.localeCompare(b.date))[0] ?? null
  }, [races, todayStr])

  const macro = useMemo(() => buildMacroTimeline(macroPlan ?? null, today), [macroPlan, today])

  const timeline = useMemo(() => {
    if (!goalRace) return null

    const raceDate = new Date(`${goalRace.date}T00:00:00`)
    raceDate.setHours(0, 0, 0, 0)

    const msToRace = raceDate.getTime() - today.getTime()
    const weeksToRace = Math.max(0, Math.ceil(msToRace / MS_PER_WEEK))

    // Race is today — return a sentinel so the UI can render a dedicated "race day" state
    if (weeksToRace === 0) {
      return { isRaceDay: true as const, raceDate, weeksToRace: 0 }
    }

    // Allocate phase weeks, capped to available time
    const taperWeeks = Math.min(TAPER_WEEKS, weeksToRace)
    const remaining1 = weeksToRace - taperWeeks
    const peakWeeks = Math.min(PEAK_WEEKS, remaining1)
    const remaining2 = remaining1 - peakWeeks
    const buildWeeks = Math.min(BUILD_WEEKS, remaining2)
    const baseWeeks = Math.max(0, remaining2 - buildWeeks)

    // Phase boundary dates (working backwards from race)
    const taperStart = new Date(raceDate.getTime() - taperWeeks * MS_PER_WEEK)
    const peakStart = new Date(taperStart.getTime() - peakWeeks * MS_PER_WEEK)
    const buildStart = new Date(peakStart.getTime() - buildWeeks * MS_PER_WEEK)
    const baseStart = new Date(buildStart.getTime() - baseWeeks * MS_PER_WEEK)

    const timelineStart = baseStart
    const totalMs = raceDate.getTime() - timelineStart.getTime()

    const makeBlock = (phase: Phase, start: Date, end: Date): PhaseBlock => ({
      key: phase,
      phase,
      label: phase,
      startDate: start,
      endDate: end,
      offsetPct: (start.getTime() - timelineStart.getTime()) / totalMs * 100,
      widthPct: (end.getTime() - start.getTime()) / totalMs * 100,
    })

    const phases: PhaseBlock[] = []
    if (baseWeeks > 0) phases.push(makeBlock('base', baseStart, buildStart))
    if (buildWeeks > 0) phases.push(makeBlock('build', buildStart, peakStart))
    if (peakWeeks > 0) phases.push(makeBlock('peak', peakStart, taperStart))
    if (taperWeeks > 0) phases.push(makeBlock('taper', taperStart, raceDate))

    // Current position as percentage across the full timeline (clamped 0–100)
    const currentPct = Math.max(0, Math.min(100,
      (today.getTime() - timelineStart.getTime()) / totalMs * 100
    ))

    // Which phase are we currently in?
    let currentPhase: Phase = 'base'
    if (weeksToRace <= taperWeeks) currentPhase = 'taper'
    else if (weeksToRace <= taperWeeks + peakWeeks) currentPhase = 'peak'
    else if (weeksToRace <= taperWeeks + peakWeeks + buildWeeks) currentPhase = 'build'

    return { isRaceDay: false as const, phases, currentPct, currentPhase, weeksToRace, raceDate, timelineStart }
  }, [goalRace, today])

  if (loading) return null

  // Macro block present — the coach's own periodisation wins over the heuristic.
  if (macro) {
    const currentStyles = phaseStyle(macro.currentPhase)
    return (
      <div className="bg-gray-800 rounded-xl border border-gray-700 p-4 space-y-3">
        {/* Header row */}
        <div className="flex items-center justify-between gap-2 flex-wrap">
          <h3 className="text-sm font-semibold text-gray-300 uppercase tracking-wide">
            {t('timeline.title')}
          </h3>
          <div className="flex items-center gap-2">
            <span
              className={`text-xs font-medium px-2 py-0.5 rounded-full border ${currentStyles.bg} ${currentStyles.text} ${currentStyles.border}`}
            >
              {t(`timeline.phases.${macro.currentPhase}`, { defaultValue: macro.currentPhase })}
            </span>
            <span className="text-base font-bold text-yellow-400">
              {/* An A-race still counts down to race day; a development block
                  counts down the weeks left in the horizon instead. */}
              {timeline
                ? t('timeline.weeksToGoal', { count: timeline.weeksToRace })
                : t('timeline.weeksLeft', { count: macro.weeksLeft })}
            </span>
          </div>
        </div>

        {/* Goal statement, or the goal race when the block is built around one */}
        {(goalRace || macro.goalStatement) && (
          <div className="flex items-center gap-1.5 text-xs text-gray-400 min-w-0">
            <Trophy size={12} className="text-yellow-400 flex-shrink-0" />
            {goalRace ? (
              <>
                <span className="font-medium text-gray-300 truncate">{goalRace.name}</span>
                <span className="flex-shrink-0 text-gray-600">·</span>
                <span className="flex-shrink-0">
                  {formatDate(`${goalRace.date}T00:00:00`, { month: 'short', day: 'numeric', year: 'numeric' })}
                </span>
              </>
            ) : (
              <span className="font-medium text-gray-300 truncate">{macro.goalStatement}</span>
            )}
          </div>
        )}

        {/* Timeline visualization */}
        <div className="space-y-1" role="img" aria-label={t('timeline.macroAriaLabel')}>
          {/* Mesocycle names above the track */}
          {macro.mesocycles.length > 0 && (
            <div className="relative h-5">
              {macro.mesocycles.map(block => {
                if (block.widthPct < 10) return null
                return (
                  <div
                    key={block.key}
                    className="absolute top-0 flex items-center overflow-hidden border-l border-gray-600/60 pl-1"
                    style={{ left: `${block.offsetPct}%`, width: `${block.widthPct}%` }}
                  >
                    <span className="text-xs font-medium text-gray-300 truncate">{block.label}</span>
                  </div>
                )
              })}
            </div>
          )}

          {/* Timeline track — one coloured segment per run of same-phase weeks */}
          <div className="relative h-8 rounded-lg overflow-hidden bg-gray-700/40">
            {macro.phases.map((block, i) => (
              <div
                key={block.key}
                className={`absolute top-0 h-full flex items-center justify-center overflow-hidden ${phaseStyle(block.phase).bg} ${i < macro.phases.length - 1 ? 'border-r border-gray-600/50' : ''}`}
                style={{ left: `${block.offsetPct}%`, width: `${block.widthPct}%` }}
              >
                {block.widthPct >= 10 && (
                  <span className={`text-xs font-medium ${phaseStyle(block.phase).text} truncate px-1`}>
                    {t(`timeline.phases.${block.phase}`, { defaultValue: block.phase })}
                  </span>
                )}
              </div>
            ))}

            {/* Current week outline */}
            {macro.currentWeek && (
              <div
                data-testid="timeline-current-week"
                title={t('timeline.currentWeek')}
                className="absolute top-0 h-full border border-yellow-400/70 rounded-sm z-10 pointer-events-none"
                style={{ left: `${macro.currentWeekOffsetPct}%`, width: `${macro.currentWeekWidthPct}%` }}
              />
            )}

            {/* Today marker — yellow vertical line */}
            {macro.currentPct > 0 && macro.currentPct < 99 && (
              <div
                className="absolute top-0 h-full w-px bg-yellow-400 z-20"
                style={{ left: `${macro.currentPct}%` }}
              />
            )}

            {/* Race flag at the right end when the block ends on a race */}
            {macro.hasAnchorRace && (
              <div className="absolute right-1 top-0 h-full flex items-center z-20 pointer-events-none">
                <Flag size={14} className="text-yellow-400" />
              </div>
            )}
          </div>

          {/* Date labels below the track */}
          <div className="relative h-5">
            {macro.currentPct >= 3 && macro.currentPct <= 90 && (
              <span
                className="absolute text-xs text-yellow-400/80 -translate-x-1/2 whitespace-nowrap"
                style={{ left: `${macro.currentPct}%` }}
              >
                {t('timeline.today')}
              </span>
            )}

            {/* Phase transition dates (hidden on mobile, shown on sm+) */}
            {macro.phases.map((block, i) => {
              if (i === 0) return null // skip first — would overlap with left edge
              const tooCloseToToday = Math.abs(block.offsetPct - macro.currentPct) < 12
              if (tooCloseToToday) return null
              return (
                <span
                  key={block.key}
                  className="absolute text-xs text-gray-500 -translate-x-1/2 whitespace-nowrap hidden sm:block"
                  style={{ left: `${block.offsetPct}%` }}
                >
                  {formatDate(block.startDate, { month: 'short', day: 'numeric' })}
                </span>
              )
            })}

            <span className="absolute text-xs text-gray-500 whitespace-nowrap" style={{ right: 0 }}>
              {formatDate(macro.endDate, { month: 'short', day: 'numeric' })}
            </span>
          </div>
        </div>
      </div>
    )
  }

  if (!goalRace || !timeline) {
    return (
      <div className="bg-gray-800/50 rounded-xl border border-gray-700 border-dashed px-4 py-5 text-center">
        <Trophy size={22} className="mx-auto text-gray-600 mb-2" />
        <p className="text-sm text-gray-400">{t('timeline.noGoalRace')}</p>
      </div>
    )
  }

  if (timeline.isRaceDay) {
    return (
      <div className="bg-gray-800 rounded-xl border border-yellow-500/40 p-4 text-center space-y-1">
        <Trophy size={22} className="mx-auto text-yellow-400 mb-2" />
        <p className="text-sm font-semibold text-yellow-300">{t('timeline.raceDay', { defaultValue: 'Race day!' })}</p>
        <p className="text-xs text-gray-400">{goalRace.name}</p>
      </div>
    )
  }

  const { phases, currentPct, currentPhase, weeksToRace, raceDate } = timeline
  const currentStyles = PHASE_STYLES[currentPhase]

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700 p-4 space-y-3">
      {/* Header row */}
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <h3 className="text-sm font-semibold text-gray-300 uppercase tracking-wide">
          {t('timeline.title')}
        </h3>
        <div className="flex items-center gap-2">
          <span
            className={`text-xs font-medium px-2 py-0.5 rounded-full border ${currentStyles.bg} ${currentStyles.text} ${currentStyles.border}`}
          >
            {t(`timeline.phases.${currentPhase}`, { defaultValue: currentPhase })}
          </span>
          <span className="text-base font-bold text-yellow-400">
            {t('timeline.weeksToGoal', { count: weeksToRace })}
          </span>
        </div>
      </div>

      {/* Goal race */}
      <div className="flex items-center gap-1.5 text-xs text-gray-400 min-w-0">
        <Trophy size={12} className="text-yellow-400 flex-shrink-0" />
        <span className="font-medium text-gray-300 truncate">{goalRace.name}</span>
        <span className="flex-shrink-0 text-gray-600">·</span>
        <span className="flex-shrink-0">
          {formatDate(`${goalRace.date}T00:00:00`, { month: 'short', day: 'numeric', year: 'numeric' })}
        </span>
      </div>

      {/* Timeline visualization */}
      <div className="space-y-1" role="img" aria-label={t('timeline.ariaLabel')}>
        {/* Phase name labels above the track */}
        <div className="relative h-5">
          {phases.map(block => {
            // Only render label if segment is wide enough to show text
            if (block.widthPct < 8) return null
            return (
              <div
                key={block.key}
                className="absolute top-0 flex items-center justify-center overflow-hidden"
                style={{ left: `${block.offsetPct}%`, width: `${block.widthPct}%` }}
              >
                <span className={`text-xs font-medium ${phaseStyle(block.phase).text} truncate px-1`}>
                  {t(`timeline.phases.${block.phase}`, { defaultValue: block.phase })}
                </span>
              </div>
            )
          })}
        </div>

        {/* Timeline track */}
        <div className="relative h-8 rounded-lg overflow-hidden bg-gray-700/40">
          {/* Phase colour blocks */}
          {phases.map((block, i) => (
            <div
              key={block.key}
              className={`absolute top-0 h-full ${phaseStyle(block.phase).bg} ${i < phases.length - 1 ? 'border-r border-gray-600/50' : ''}`}
              style={{ left: `${block.offsetPct}%`, width: `${block.widthPct}%` }}
            />
          ))}

          {/* Today marker — yellow dashed vertical line */}
          {currentPct > 0 && currentPct < 99 && (
            <div
              className="absolute top-0 h-full w-px bg-yellow-400 z-10"
              style={{ left: `${currentPct}%` }}
            />
          )}

          {/* Race flag at the right end */}
          <div className="absolute right-1 top-0 h-full flex items-center z-10 pointer-events-none">
            <Flag size={14} className="text-yellow-400" />
          </div>
        </div>

        {/* Date labels below the track */}
        <div className="relative h-5">
          {/* Today label — only when not too close to edges */}
          {currentPct >= 3 && currentPct <= 90 && (
            <span
              className="absolute text-xs text-yellow-400/80 -translate-x-1/2 whitespace-nowrap"
              style={{ left: `${currentPct}%` }}
            >
              {t('timeline.today')}
            </span>
          )}

          {/* Phase transition dates (hidden on mobile, shown on sm+) */}
          {phases.map((block, i) => {
            if (i === 0) return null // skip first — would overlap with left edge
            // Don't show if it would overlap with today label
            const tooCloseToToday = Math.abs(block.offsetPct - currentPct) < 12
            if (tooCloseToToday) return null
            return (
              <span
                key={block.key}
                className="absolute text-xs text-gray-500 -translate-x-1/2 whitespace-nowrap hidden sm:block"
                style={{ left: `${block.offsetPct}%` }}
              >
                {formatDate(block.startDate, { month: 'short', day: 'numeric' })}
              </span>
            )
          })}

          {/* Race date at far right */}
          <span
            className="absolute text-xs text-gray-500 whitespace-nowrap"
            style={{ right: 0 }}
          >
            {formatDate(raceDate, { month: 'short', day: 'numeric' })}
          </span>
        </div>
      </div>
    </div>
  )
}
