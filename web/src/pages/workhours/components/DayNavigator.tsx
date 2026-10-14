import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Calendar, ChevronLeft, ChevronRight } from 'lucide-react'
import { formatDate } from '../../../utils/formatDate'
import { buildNavHolidaySet, localDateStr, nextWeekday, prevWeekday } from '../dateUtils'

/** Previous/next weekday buttons around the date label, which opens a date picker. */
export default function DayNavigator({
  currentDate,
  setCurrentDate,
  navHolidaySet,
}: {
  currentDate: string
  setCurrentDate: (d: string | ((prev: string) => string)) => void
  /** Holidays skipped by the previous/next buttons, alongside weekends. */
  navHolidaySet: Set<string>
}) {
  const { t } = useTranslation(['workhours', 'common'])
  const datePickerRef = useRef<HTMLInputElement>(null)

  const dateLabel = formatDate(currentDate + 'T12:00:00', {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })

  return (
    <div className="flex items-center justify-between bg-gray-800 rounded-lg p-3">
      <button
        type="button"
        onClick={() => setCurrentDate(prev => prevWeekday(prev, navHolidaySet))}
        className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
        aria-label={t('workhours:prevDay')}
      >
        <ChevronLeft size={20} />
      </button>
      <div className="relative flex items-center gap-1">
        <button
          type="button"
          onClick={() => {
            const input = datePickerRef.current
            if (!input) return
            // Prefer native showPicker when available, fall back to click/focus for broader browser support
            if (typeof (input as HTMLInputElement).showPicker === 'function') {
              ;(input as HTMLInputElement).showPicker()
            } else {
              input.click()
              input.focus()
            }
          }}
          className="flex items-center gap-1 text-sm font-medium text-white capitalize hover:text-blue-300 transition-colors cursor-pointer"
          title={t('workhours:selectDate')}
        >
          <Calendar size={14} className="text-gray-400" />
          {dateLabel}
        </button>
        <input
          ref={datePickerRef}
          type="date"
          value={currentDate}
          onChange={e => {
            if (!e.target.value) return
            const d = new Date(e.target.value + 'T12:00:00')
            const pickedYear = d.getFullYear()
            const pickedHolidaySet = buildNavHolidaySet(pickedYear)
            while (d.getDay() === 0 || d.getDay() === 6 || pickedHolidaySet.has(localDateStr(d))) {
              d.setDate(d.getDate() - 1)
            }
            setCurrentDate(localDateStr(d))
          }}
          className="absolute left-0 top-0 opacity-0 pointer-events-none w-0 h-0"
          aria-hidden="true"
          tabIndex={-1}
        />
      </div>
      <button
        type="button"
        onClick={() => setCurrentDate(prev => nextWeekday(prev, navHolidaySet))}
        className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
        aria-label={t('workhours:nextDay')}
      >
        <ChevronRight size={20} />
      </button>
    </div>
  )
}
