import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, Loader2 } from 'lucide-react'
import { formatDate } from '../../utils/formatDate'
import type { MacroWeek } from '../../types/stride'
import type { MacroWeekActual } from './macroWeekActuals'
import { StrideWeekRow } from './StrideWeekRow'

interface StrideWeekListProps {
  // The block's weeks, already sorted by Monday.
  weeks: MacroWeek[]
  actuals: Map<string, MacroWeekActual>
  // The week the athlete is training now, so the list can mark it.
  currentWeekStart?: string
  // Actuals are loaded separately from the block; the list still renders the
  // targets while they are in flight and says so.
  actualsLoading?: boolean
  actualsError?: boolean
  onRetryActuals?: () => void
}

// The block's 26 weeks, collapsed by default: the header carries the count and
// the block's date range, and expanding it reveals one StrideWeekRow per week.
export function StrideWeekList({
  weeks,
  actuals,
  currentWeekStart,
  actualsLoading = false,
  actualsError = false,
  onRetryActuals,
}: StrideWeekListProps) {
  const { t } = useTranslation('stride')
  const [open, setOpen] = useState(false)
  const listId = useId()

  if (weeks.length === 0) return null

  const first = weeks[0]
  const last = weeks[weeks.length - 1]
  const range = t('longTermPlan.weeks.range', {
    from: formatDate(`${first.week_start}T00:00:00`, { month: 'short', day: 'numeric' }),
    to: formatDate(`${last.week_start}T00:00:00`, { month: 'short', day: 'numeric', year: 'numeric' }),
  })

  return (
    <div className="mt-4">
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        aria-expanded={open}
        aria-controls={listId}
        className="w-full flex items-center justify-between gap-2 text-left px-3 py-2 rounded-lg bg-gray-800 border border-gray-700 hover:border-gray-600 transition-colors"
      >
        <span className="flex items-center gap-1.5 text-sm font-medium text-gray-200">
          {open ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
          {t('longTermPlan.weeks.title')}
        </span>
        <span className="text-xs text-gray-400 text-right">
          {t('longTermPlan.weekCount', { count: weeks.length })}
          <span className="hidden sm:inline">{' · '}{range}</span>
        </span>
      </button>

      {open && (
        <div id={listId}>
          {actualsLoading && (
            <p className="flex items-center gap-1.5 text-xs text-gray-500 mt-2">
              <Loader2 size={12} className="animate-spin" />
              {t('longTermPlan.weeks.actualsLoading')}
            </p>
          )}
          {actualsError && (
            <p className="flex items-center gap-2 text-xs text-red-400 mt-2" role="alert">
              {t('longTermPlan.weeks.actualsError')}
              {onRetryActuals && (
                <button
                  type="button"
                  onClick={onRetryActuals}
                  className="underline hover:text-red-300"
                >
                  {t('longTermPlan.weeks.retry')}
                </button>
              )}
            </p>
          )}

          <ol className="mt-2 space-y-2">
            {weeks.map(week => (
              <StrideWeekRow
                key={week.id || week.week_start}
                week={week}
                actual={actuals.get(week.week_start)}
                current={week.week_start === currentWeekStart}
              />
            ))}
          </ol>
        </div>
      )}
    </div>
  )
}
