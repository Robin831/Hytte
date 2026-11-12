import { useTranslation } from 'react-i18next'
import { WEEK_MONDAY_FIRST, daysInMonth, isLeap, monthDoomsday, solve, weekdayOf, type SimpleDate, type Weekday } from './doomsday'
import { centuryStart, dayMonthText, monthName, weekdayName } from './format'

/** Monday-first weekday buttons; keys 1–7 are wired by the caller. */
export function WeekdayButtons({ onPick, disabled, picked, correct }: {
  onPick: (wd: Weekday) => void
  disabled?: boolean
  picked?: Weekday | null
  correct?: Weekday | null
}) {
  const { i18n } = useTranslation('doomsday')
  return (
    <div className="grid grid-cols-4 sm:grid-cols-7 gap-2">
      {WEEK_MONDAY_FIRST.map((wd, i) => {
        let style = 'bg-gray-800 border-gray-700 hover:border-blue-500'
        if (correct != null && wd === correct) style = 'bg-emerald-700 border-emerald-500'
        else if (picked != null && wd === picked) style = 'bg-red-800 border-red-600'
        return (
          <button
            key={wd}
            type="button"
            disabled={disabled}
            onClick={() => onPick(wd)}
            className={`relative rounded-lg border px-2 py-3 text-sm font-medium text-white cursor-pointer disabled:cursor-default ${style}`}
          >
            <span className="absolute top-1 left-1.5 text-[10px] text-gray-500">{i + 1}</span>
            {weekdayName(wd, i18n.language)}
          </button>
        )
      })}
    </div>
  )
}

/** The full Odd + 11 solution for a date, one line per step. */
export function SolutionView({ date }: { date: SimpleDate }) {
  const { t, i18n } = useTranslation('doomsday')
  const lang = i18n.language
  const s = solve(date)
  const wd = (w: Weekday) => weekdayName(w, lang)
  const { odd } = s
  return (
    <ol className="space-y-1.5 text-sm text-gray-200 list-decimal pl-5">
      <li>{t('solution.anchor', { century: centuryStart(date.y), day: wd(s.anchor) })}</li>
      <li>
        {t('solution.lastTwo', { t: odd.t })}{' '}
        {odd.oddFirst ? t('solution.oddAdd', { n: odd.afterFirst }) : t('solution.even')}{' '}
        {t('solution.half', { n: odd.half })}{' '}
        {odd.oddSecond ? t('solution.oddAdd2', { n: odd.afterSecond }) : t('solution.even2')}{' '}
        {t('solution.mod', { n: odd.afterSecond, m: odd.mod })}{' '}
        {t('solution.forward', { m: odd.mod, f: odd.forward, anchor: wd(s.anchor), day: wd(s.yearDay) })}
        <span className="block text-gray-400">{t('solution.yearDay', { year: date.y, day: wd(s.yearDay) })}</span>
      </li>
      <li>
        {t('solution.month', { month: monthName(date.m, lang), date: dayMonthText(s.monthDay, date.m, lang) })}
        {date.m <= 2 && (
          <span className="text-gray-400"> {s.leap ? t('solution.leapNote', { year: date.y }) : t('solution.notLeapNote', { year: date.y })}</span>
        )}
      </li>
      <li>
        {t('solution.count', { d: date.d, md: s.monthDay, diff: s.diff })}
        {(s.diff < 0 || s.diff >= 7) && t('solution.countMod', { mod: s.diffMod })}
        {t('solution.countResult', { yearDay: wd(s.yearDay), mod: s.diffMod, day: wd(s.answer) })}
      </li>
      <li className="font-semibold list-none -ml-5">{t('solution.answer', { day: wd(s.answer) })}</li>
    </ol>
  )
}

/** The month as a small calendar, Monday first, marking the date and the doomsday dates. */
export function MiniCalendar({ date }: { date: SimpleDate }) {
  const { i18n } = useTranslation('doomsday')
  const first = weekdayOf({ y: date.y, m: date.m, d: 1 })
  const lead = (first + 6) % 7 // Monday-first offset
  const days = daysInMonth(date.y, date.m)
  const dd = monthDoomsday(date.m, isLeap(date.y))
  const cells: (number | null)[] = [...Array(lead).fill(null), ...Array.from({ length: days }, (_, i) => i + 1)]
  return (
    <div className="inline-block">
      <p className="text-xs text-gray-400 mb-1">{monthName(date.m, i18n.language)} {date.y}</p>
      <div className="grid grid-cols-7 gap-0.5 text-[11px] text-center">
        {WEEK_MONDAY_FIRST.map(w => (
          <span key={w} className="text-gray-500 w-7">{weekdayName(w, i18n.language, 'short').slice(0, 2)}</span>
        ))}
        {cells.map((d, i) => {
          if (d === null) return <span key={`e${i}`} />
          const isDate = d === date.d
          const isDoom = (d - dd) % 7 === 0
          return (
            <span
              key={d}
              className={`w-7 py-0.5 rounded ${isDate ? 'bg-blue-600 text-white font-semibold' : isDoom ? 'bg-amber-900/70 text-amber-200' : 'text-gray-300'}`}
            >
              {d}
            </span>
          )
        })}
      </div>
    </div>
  )
}
