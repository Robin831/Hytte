import { useTranslation } from 'react-i18next'
import { formatDate, formatNumber } from '../../utils/formatDate'
import type { MacroWeek } from '../../types/stride'
import type { MacroWeekActual } from './macroWeekActuals'
import { phaseAccent } from './macroPlan'

// Badge colours per macro week status. A materialised week has a 7-day plan
// behind it, a planned one is still only a contract, and a skipped one passed
// without ever being turned into a plan.
const STATUS_BADGE: Record<string, string> = {
  materialised: 'bg-green-500/10 text-green-400 border-green-500/30',
  planned: 'bg-gray-700/60 text-gray-300 border-gray-600',
  skipped: 'bg-amber-500/10 text-amber-400 border-amber-500/30',
}

function statusBadge(status: string): string {
  return STATUS_BADGE[status] ?? 'bg-gray-700/60 text-gray-300 border-gray-600'
}

// One decimal is as much precision as a weekly volume carries — the target is
// the coach's round number and the actual comes off GPS distances.
function km(value: number): string {
  return formatNumber(value, { maximumFractionDigits: 1 })
}

interface StrideWeekRowProps {
  week: MacroWeek
  // What the athlete actually did that week, or undefined when the week has no
  // plan history yet (it is still ahead, or was never materialised).
  actual?: MacroWeekActual
  current?: boolean
}

// One row of the block's week list: the macro week's contract (phase, load,
// mesocycle, target volume and sessions, key sessions, intent) with the actuals
// measured against it.
export function StrideWeekRow({ week, actual, current = false }: StrideWeekRowProps) {
  const { t } = useTranslation('stride')

  const target = week.target_km
  // Clamped so an overshoot fills the track rather than overflowing it; the
  // overshoot is shown by the bar's colour and stated in the label either way.
  const fillPct = target > 0 && actual ? Math.min(actual.km / target, 1) * 100 : 0
  const over = actual !== undefined && target > 0 && actual.km > target

  const keySessions = week.key_sessions ?? []

  // No target is no ratio, so a rest week (or a week stored without a volume)
  // states both numbers and skips the bar rather than drawing a full track.
  const bar = actual !== undefined && target > 0 ? actual : null

  return (
    <li
      className={`rounded-lg border-l-4 ${phaseAccent(week.phase)} bg-gray-800 border border-gray-700 px-3 py-2 ${current ? 'ring-1 ring-yellow-400/60' : ''}`}
    >
      <div className="flex items-baseline justify-between gap-2 flex-wrap">
        <span className="text-sm font-medium text-gray-200">
          {t('longTermPlan.weeks.seq', { seq: week.seq })}
          <span className="text-gray-500 font-normal">
            {' · '}
            {formatDate(`${week.week_start}T00:00:00`, { month: 'short', day: 'numeric' })}
          </span>
        </span>
        <span className="flex items-center gap-1.5">
          {current && (
            <span className="text-xs font-medium px-1.5 py-0.5 rounded border bg-yellow-500/10 text-yellow-400 border-yellow-500/30">
              {t('longTermPlan.current')}
            </span>
          )}
          <span className={`text-xs font-medium px-1.5 py-0.5 rounded border ${statusBadge(week.status)}`}>
            {t(`longTermPlan.weeks.status.${week.status}`, { defaultValue: week.status })}
          </span>
        </span>
      </div>

      <div className="flex items-center gap-1.5 text-xs text-gray-500 mt-0.5 flex-wrap">
        <span>{t(`timeline.phases.${week.phase}`, { defaultValue: week.phase })}</span>
        <span className="text-gray-600">·</span>
        <span>{t(`longTermPlan.weeks.load.${week.load_level}`, { defaultValue: week.load_level })}</span>
        {week.mesocycle && (
          <>
            <span className="text-gray-600">·</span>
            <span>{week.mesocycle}</span>
          </>
        )}
      </div>

      {/* Target vs actual volume. A week with no history yet states the target
          on its own rather than drawing a bar that would read as "0 km done". */}
      <div className="mt-2">
        <div className="flex items-center justify-between gap-2 text-xs">
          <span className="text-gray-400">{t('longTermPlan.weeks.volume')}</span>
          <span className={over ? 'text-amber-300' : 'text-gray-300'}>
            {actual
              ? t('longTermPlan.weeks.km', { actual: km(actual.km), target: km(target) })
              : t('longTermPlan.weeks.kmTarget', { target: km(target) })}
          </span>
        </div>
        {bar && (
          <div
            className="mt-1 h-1.5 w-full rounded-full bg-gray-700 overflow-hidden"
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={target}
            aria-valuenow={bar.km}
            aria-valuetext={t('longTermPlan.weeks.km', { actual: km(bar.km), target: km(target) })}
            aria-label={t('longTermPlan.weeks.kmAria', { seq: week.seq })}
          >
            <div
              className={`h-full rounded-full ${over ? 'bg-amber-400' : 'bg-yellow-500'}`}
              style={{ width: `${fillPct}%` }}
            />
          </div>
        )}
      </div>

      <div className="flex items-center justify-between gap-2 text-xs mt-1.5">
        <span className="text-gray-400">{t('longTermPlan.weeks.sessions')}</span>
        <span className="text-gray-300">
          {actual
            ? t('longTermPlan.weeks.sessionCount', {
                completed: actual.sessionsCompleted,
                target: week.target_sessions,
              })
            : t('longTermPlan.weeks.sessionTarget', { target: week.target_sessions })}
        </span>
      </div>

      {keySessions.length > 0 && (
        <ul className="flex flex-wrap gap-1 mt-2">
          {keySessions.map((session, i) => (
            <li
              key={`${session.type}-${i}`}
              className="text-xs px-1.5 py-0.5 rounded bg-gray-700/60 text-gray-300"
            >
              {session.focus ? `${session.type}: ${session.focus}` : session.type}
            </li>
          ))}
        </ul>
      )}

      {week.intent && <p className="text-xs text-gray-400 mt-1.5">{week.intent}</p>}
    </li>
  )
}
