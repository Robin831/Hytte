import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Trophy, Flag } from 'lucide-react'
import { formatDate } from '../../utils/formatDate'

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
}

type Phase = 'base' | 'build' | 'peak' | 'taper'

interface PhaseBlock {
  phase: Phase
  startDate: Date
  endDate: Date
  widthPct: number
  offsetPct: number
}

const PHASE_STYLES: Record<Phase, { bg: string; text: string; border: string }> = {
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
}

// Standard phase durations in weeks (working backwards from race day)
const TAPER_WEEKS = 2
const PEAK_WEEKS = 4
const BUILD_WEEKS = 6

const MS_PER_WEEK = 7 * 24 * 60 * 60 * 1000

export function TrainingBlockTimeline({ races, loading }: TrainingBlockTimelineProps) {
  const { t } = useTranslation('stride')

  const today = useMemo(() => {
    const d = new Date()
    d.setHours(0, 0, 0, 0)
    return d
  }, [])

  const todayStr = useMemo(() => {
    const y = today.getFullYear()
    const m = String(today.getMonth() + 1).padStart(2, '0')
    const d = String(today.getDate()).padStart(2, '0')
    return `${y}-${m}-${d}`
  }, [today])

  // Nearest upcoming A-priority race that hasn't been completed
  const goalRace = useMemo(() => {
    return races
      .filter(r => r.priority === 'A' && r.date >= todayStr && r.result_time == null)
      .sort((a, b) => a.date.localeCompare(b.date))[0] ?? null
  }, [races, todayStr])

  const timeline = useMemo(() => {
    if (!goalRace) return null

    const raceDate = new Date(`${goalRace.date}T00:00:00`)
    raceDate.setHours(0, 0, 0, 0)

    const msToRace = raceDate.getTime() - today.getTime()
    const weeksToRace = Math.max(0, Math.ceil(msToRace / MS_PER_WEEK))

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
      phase,
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

    return { phases, currentPct, currentPhase, weeksToRace, raceDate, timelineStart }
  }, [goalRace, today])

  if (loading) return null

  if (!goalRace || !timeline) {
    return (
      <div className="bg-gray-800/50 rounded-xl border border-gray-700 border-dashed px-4 py-5 text-center">
        <Trophy size={22} className="mx-auto text-gray-600 mb-2" />
        <p className="text-sm text-gray-400">{t('timeline.noGoalRace')}</p>
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
            {t(`timeline.phases.${currentPhase}`)}
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
      <div className="space-y-1" aria-label={t('timeline.ariaLabel')}>
        {/* Phase name labels above the track */}
        <div className="relative h-5">
          {phases.map(block => {
            // Only render label if segment is wide enough to show text
            if (block.widthPct < 8) return null
            return (
              <div
                key={block.phase}
                className="absolute top-0 flex items-center justify-center overflow-hidden"
                style={{ left: `${block.offsetPct}%`, width: `${block.widthPct}%` }}
              >
                <span className={`text-xs font-medium ${PHASE_STYLES[block.phase].text} truncate px-1`}>
                  {t(`timeline.phases.${block.phase}`)}
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
              key={block.phase}
              className={`absolute top-0 h-full ${PHASE_STYLES[block.phase].bg} ${i < phases.length - 1 ? 'border-r border-gray-600/50' : ''}`}
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
                key={block.phase}
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
