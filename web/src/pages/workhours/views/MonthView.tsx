import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { formatDate } from '../../../utils/formatDate'
import { Skeleton } from '../../../components/ui/skeleton'
import type { DaySummary, LeaveBalance, LeaveDay, MonthSummaryResponse } from '../types'
import {
  addMonths,
  buildMonthGrid,
  countWorkdaysInMonth,
  countWorkdaysUpToNow,
  dateToMonthStr,
  dayCellClass,
  formatHours,
  getNorwegianHolidays,
  localDateStr,
} from '../dateUtils'
import { useWorkHoursApi } from '../useWorkHoursApi'
import FlexTrendChart from '../components/FlexTrendChart'

export default function MonthView({
  currentDate,
  setCurrentDate,
  onSelectDay,
}: {
  currentDate: string
  setCurrentDate: (d: string | ((prev: string) => string)) => void
  onSelectDay: (date: string) => void
}) {
  const { t } = useTranslation(['workhours', 'common'])
  const api = useWorkHoursApi()
  const monthStr = dateToMonthStr(currentDate)
  const yearStr = monthStr.slice(0, 4)
  const [data, setData] = useState<MonthSummaryResponse | null>(null)
  const [leaveBalance, setLeaveBalance] = useState<LeaveBalance | null>(null)
  const [loading, setLoading] = useState(false)

  const loadMonth = useCallback(async (month: string, signal: AbortSignal) => {
    setLoading(true)
    try {
      const result = await api.getMonth(month, signal)
      if (signal.aborted) return
      setData(result)
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') setData(null)
    } finally {
      if (!signal.aborted) setLoading(false)
    }
  }, [api])

  const loadLeaveBalance = useCallback(async (year: string, signal: AbortSignal) => {
    try {
      const result = await api.getLeaveBalance(year, signal)
      if (signal.aborted) return
      setLeaveBalance(result)
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') setLeaveBalance(null)
    }
  }, [api])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadMonth(monthStr, controller.signal)
    loadLeaveBalance(yearStr, controller.signal)
    return () => controller.abort()
  }, [monthStr, yearStr, loadMonth, loadLeaveBalance])

  const summaryMap = new Map<string, DaySummary>()
  const leaveDayMap = new Map<string, LeaveDay>()
  if (data) {
    data.summaries.forEach(s => summaryMap.set(s.date, s))
    data.leave_days?.forEach(ld => leaveDayMap.set(ld.date, ld))
  }

  const monthLabel = formatDate(new Date(monthStr + '-01T12:00:00'), {
    month: 'long',
    year: 'numeric',
  })

  // Day-of-week header labels (Mon–Sun)
  const dowHeaders = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(2024, 0, 1 + i) // Jan 1 2024 is a Monday
    return formatDate(d, { weekday: 'short' })
  })

  const grid = buildMonthGrid(monthStr)

  // Norwegian holidays for this month's year (and adjacent year for Dec/Jan edge)
  const monthYear = parseInt(monthStr.split('-')[0])
  const monthHolidays = new Map([
    ...getNorwegianHolidays(monthYear),
    ...getNorwegianHolidays(monthYear + 1),
  ])
  const holidaySet = new Set(monthHolidays.keys())

  // Monthly totals
  const standard = data?.summaries[0]?.standard_minutes ?? 450
  const totalWorked = data ? data.summaries.reduce((sum, s) => sum + s.reported_minutes, 0) : 0
  const todayStr = localDateStr(new Date())
  const currentMonthStr = todayStr.length >= 7 ? todayStr.slice(0, 7) : monthStr
  const isCurrentMonth = monthStr === currentMonthStr
  const workdaysTarget = isCurrentMonth
    ? countWorkdaysUpToNow(monthStr, holidaySet)
    : countWorkdaysInMonth(monthStr, holidaySet)
  const totalTarget = workdaysTarget * standard
  const totalBalance = totalWorked - totalTarget

  const today = todayStr

  return (
    <div className="space-y-4">
      {/* Month navigation */}
      <div className="flex items-center justify-between bg-gray-800 rounded-lg p-3">
        <button
          type="button"
          onClick={() => setCurrentDate(addMonths(monthStr, -1) + '-01')}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:prevMonth')}
        >
          <ChevronLeft size={20} />
        </button>
        <span className="text-sm font-medium text-white capitalize">{monthLabel}</span>
        <button
          type="button"
          onClick={() => setCurrentDate(addMonths(monthStr, 1) + '-01')}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:nextMonth')}
        >
          <ChevronRight size={20} />
        </button>
      </div>

      {loading ? (
        <div role="status" aria-live="polite" className="inline-flex items-center gap-2">
          <Skeleton className="h-5 w-24" />
          <span className="sr-only">{t('common:skeleton.loading')}</span>
        </div>
      ) : (
        <>
          {/* Calendar grid */}
          <div>
            {/* Day-of-week headers */}
            <div className="grid grid-cols-7 gap-1 mb-1">
              {dowHeaders.map((h, i) => (
                <div
                  key={h}
                  className={`text-center text-xs font-medium py-1 ${
                    i >= 5 ? 'text-gray-600' : 'text-gray-400'
                  }`}
                >
                  {h}
                </div>
              ))}
            </div>
            {/* Weeks */}
            <div className="space-y-1">
              {grid.map((week, wi) => (
                <div key={week.find(d => d !== null) ?? wi} className="grid grid-cols-7 gap-1">
                  {week.map((dateStr, di) => {
                    if (!dateStr) {
                      return <div key={`pad-${di}`} className="aspect-square" />
                    }
                    const summary = summaryMap.get(dateStr)
                    const leaveEntry = leaveDayMap.get(dateStr)
                    const isWeekend = di >= 5
                    const isHoliday = !isWeekend && monthHolidays.has(dateStr)
                    const holidayLabel = monthHolidays.get(dateStr)
                    const isToday = dateStr === today
                    const dayNum = parseInt(dateStr.split('-')[2])
                    const cellClass = dayCellClass(summary, isWeekend, isHoliday, leaveEntry?.leave_type)
                    const isDisabled = isWeekend
                    const cellTitle = holidayLabel ?? (leaveEntry ? t(`workhours:leaveType_${leaveEntry.leave_type}`) : undefined)

                    return (
                      <button
                        key={dateStr}
                        type="button"
                        onClick={() => !isDisabled && onSelectDay(dateStr)}
                        disabled={isDisabled}
                        title={cellTitle}
                        className={`aspect-square rounded flex flex-col items-center justify-center text-xs transition-colors ${cellClass} ${
                          isDisabled ? 'cursor-default' : 'hover:ring-1 hover:ring-gray-500 cursor-pointer'
                        } ${isToday ? 'ring-1 ring-blue-500' : ''}`}
                        aria-label={cellTitle ? `${dateStr} – ${cellTitle}` : dateStr}
                      >
                        <span className="font-medium leading-none">{dayNum}</span>
                        {isHoliday ? (
                          <span className="text-[0.55rem] leading-tight mt-0.5 opacity-70 truncate max-w-full px-0.5 text-center">
                            {holidayLabel}
                          </span>
                        ) : leaveEntry ? (
                          <span className="text-[0.55rem] leading-tight mt-0.5 opacity-80 truncate max-w-full px-0.5 text-center">
                            {t(`workhours:leaveType_${leaveEntry.leave_type}`)}
                          </span>
                        ) : summary && summary.reported_minutes > 0 ? (
                          <span className="text-[0.6rem] leading-tight mt-0.5 opacity-80">
                            {formatHours(summary.reported_minutes)}
                          </span>
                        ) : null}
                      </button>
                    )
                  })}
                </div>
              ))}
            </div>
          </div>

          {/* Monthly totals */}
          <section className="bg-gray-800 rounded-lg p-4">
            <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide mb-3">
              {t('workhours:monthlyTotal')}
            </h2>
            <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
              <span className="text-gray-400">{t('workhours:worked')}</span>
              <span className="text-white font-mono text-right">{formatHours(totalWorked)}</span>

              <span className="text-gray-400">{t('workhours:target')}</span>
              <span className="text-white font-mono text-right">{formatHours(totalTarget)}</span>

              <span className="text-gray-400">{t('workhours:balance')}</span>
              <span
                className={`font-mono text-right ${
                  totalBalance > 0 ? 'text-green-400' : totalBalance < 0 ? 'text-red-400' : 'text-white'
                }`}
              >
                {totalBalance > 0 ? '+' : ''}
                {formatHours(totalBalance)}
              </span>
            </div>
          </section>

          {/* Leave balance */}
          {leaveBalance && leaveBalance.vacation_allowance > 0 && (
            <section className="bg-gray-800 rounded-lg p-4 space-y-3">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
                {t('workhours:leaveBalance')}
              </h2>
              <div className="space-y-2">
                {/* Vacation allowance bar */}
                <div>
                  <div className="flex justify-between text-sm mb-1">
                    <span className="text-gray-300">{t('workhours:leaveType_vacation')}</span>
                    <span className="text-gray-400 font-mono">
                      {t('workhours:leaveUsedOf', {
                        used: leaveBalance.vacation_used,
                        total: leaveBalance.vacation_allowance,
                      })}
                    </span>
                  </div>
                  <div className="h-1.5 bg-gray-700 rounded-full overflow-hidden">
                    <div
                      className="h-full bg-purple-500 rounded-full transition-all"
                      style={{
                        width: `${Math.min(100, (leaveBalance.vacation_used / leaveBalance.vacation_allowance) * 100)}%`,
                      }}
                    />
                  </div>
                </div>
                {/* Other leave types */}
                <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm pt-1">
                  {leaveBalance.sick_used > 0 && (
                    <>
                      <span className="text-gray-400">{t('workhours:leaveType_sick')}</span>
                      <span className="text-white font-mono text-right">{leaveBalance.sick_used} {t('workhours:days')}</span>
                    </>
                  )}
                  {leaveBalance.personal_used > 0 && (
                    <>
                      <span className="text-gray-400">{t('workhours:leaveType_personal')}</span>
                      <span className="text-white font-mono text-right">{leaveBalance.personal_used} {t('workhours:days')}</span>
                    </>
                  )}
                  {leaveBalance.public_holiday_used > 0 && (
                    <>
                      <span className="text-gray-400">{t('workhours:leaveType_public_holiday')}</span>
                      <span className="text-white font-mono text-right">{leaveBalance.public_holiday_used} {t('workhours:days')}</span>
                    </>
                  )}
                </div>
              </div>
            </section>
          )}

          {/* Flex trend chart */}
          {data && data.summaries.length > 0 && (
            <FlexTrendChart summaries={data.summaries} />
          )}
        </>
      )}
    </div>
  )
}
