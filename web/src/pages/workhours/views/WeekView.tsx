import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { formatDate } from '../../../utils/formatDate'
import { Skeleton } from '../../../components/ui/skeleton'
import type { DaySummary, LeaveDay, WeekSummaryResponse, WorkDay } from '../types'
import { addWeeks, formatHours, formatMins, getNorwegianHolidays, localDateStr, sessionRange, weekDays } from '../dateUtils'
import { useWorkHoursApi } from '../useWorkHoursApi'

export default function WeekView({
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
  const [data, setData] = useState<WeekSummaryResponse | null>(null)
  const [loading, setLoading] = useState(false)

  const loadWeek = useCallback(async (date: string, signal: AbortSignal) => {
    setLoading(true)
    try {
      const result = await api.getWeek(date, signal)
      if (signal.aborted) return
      setData(result)
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') setData(null)
    } finally {
      if (!signal.aborted) setLoading(false)
    }
  }, [api])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadWeek(currentDate, controller.signal)
    return () => controller.abort()
  }, [currentDate, loadWeek])

  const summaryMap = new Map<string, DaySummary>()
  const dayMap = new Map<string, WorkDay>()
  const leaveDayMap = new Map<string, LeaveDay>()
  if (data) {
    data.summaries.forEach(s => summaryMap.set(s.date, s))
    data.days.forEach(d => dayMap.set(d.date, d))
    data.leave_days?.forEach(ld => leaveDayMap.set(ld.date, ld))
  }

  // The week_start from the API is the Monday of the week.
  // Fallback: normalize currentDate to its Monday so weekDays() renders the right range.
  const weekStart = data?.week_start ?? (() => {
    const d = new Date(currentDate + 'T12:00:00')
    const dow = d.getDay() // 0=Sun, 1=Mon, …
    const offsetToMon = dow === 0 ? -6 : 1 - dow
    d.setDate(d.getDate() + offsetToMon)
    return localDateStr(d)
  })()

  // Norwegian holidays for both years that might span the week
  const weekYear = parseInt(weekStart.split('-')[0])
  const weekHolidays = new Map([
    ...getNorwegianHolidays(weekYear),
    ...getNorwegianHolidays(weekYear + 1),
  ])

  // Build 5 weekday rows
  const rows = weekDays(weekStart)

  // Weekly totals — sum only the Mon–Fri rows shown in the table
  let totalNet = 0
  let totalReported = 0
  let totalBalance = 0
  rows.forEach(dateStr => {
    const s = summaryMap.get(dateStr)
    if (s) {
      totalNet += s.net_minutes
      totalReported += s.reported_minutes
      totalBalance += s.balance_minutes
    }
  })

  const weekLabel = (() => {
    const start = new Date(weekStart + 'T12:00:00')
    // Friday is 4 days after Monday
    const friday = new Date(weekStart + 'T12:00:00')
    friday.setDate(friday.getDate() + 4)
    const shortOpts: Intl.DateTimeFormatOptions = { day: 'numeric', month: 'short' }
    const startShort = formatDate(start, shortOpts)
    const fridayShort = formatDate(friday, shortOpts)
    const year = formatDate(start, { year: 'numeric' })
    return `${startShort} – ${fridayShort}, ${year}`
  })()

  return (
    <div className="space-y-4">
      {/* Week navigation */}
      <div className="flex items-center justify-between bg-gray-800 rounded-lg p-3">
        <button
          type="button"
          onClick={() => setCurrentDate(d => addWeeks(d, -1))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:prevWeek')}
        >
          <ChevronLeft size={20} />
        </button>
        <span className="text-sm font-medium text-white">{weekLabel}</span>
        <button
          type="button"
          onClick={() => setCurrentDate(d => addWeeks(d, 1))}
          className="p-1 text-gray-400 hover:text-white transition-colors cursor-pointer"
          aria-label={t('workhours:nextWeek')}
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
          {/* Week table */}
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-xs text-gray-400 uppercase tracking-wide border-b border-gray-700">
                  <th className="text-left py-2 pr-3 font-medium">{t('workhours:day')}</th>
                  <th className="text-right py-2 px-2 font-medium">{t('workhours:startTime')}</th>
                  <th className="text-right py-2 px-2 font-medium">{t('workhours:endTime')}</th>
                  <th className="text-right py-2 px-2 font-medium">{t('workhours:net')}</th>
                  <th className="text-right py-2 px-2 font-medium">{t('workhours:reported')}</th>
                  <th className="text-right py-2 pl-2 font-medium">+/-</th>
                </tr>
              </thead>
              <tbody>
                {rows.map(dateStr => {
                  const summary = summaryMap.get(dateStr)
                  const wd = dayMap.get(dateStr)
                  const range = sessionRange(wd)
                  const leaveEntry = leaveDayMap.get(dateStr)
                  const d = new Date(dateStr + 'T12:00:00')
                  const dayLabel = formatDate(d, {
                    weekday: 'short',
                    day: 'numeric',
                    month: 'short',
                  })
                  const balance = summary?.balance_minutes ?? null
                  const holidayLabel = weekHolidays.get(dateStr)
                  const isDimmed = !!holidayLabel || !!leaveEntry

                  return (
                    <tr
                      key={dateStr}
                      className={`border-b border-gray-800 transition-colors ${isDimmed ? 'opacity-60' : ''}`}
                    >
                      <td className="py-2.5 pr-3 capitalize">
                        <button
                          type="button"
                          onClick={() => onSelectDay(dateStr)}
                          className="w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/70 rounded-sm"
                        >
                          <span className={isDimmed ? 'text-gray-500' : 'text-gray-300'}>{dayLabel}</span>
                          {holidayLabel && (
                            <span className="block text-[0.65rem] text-gray-500 truncate max-w-24" title={holidayLabel}>
                              {holidayLabel}
                            </span>
                          )}
                          {leaveEntry && !holidayLabel && (
                            <span className="block text-[0.65rem] text-gray-500 truncate max-w-24">
                              {t(`workhours:leaveType_${leaveEntry.leave_type}`)}
                            </span>
                          )}
                        </button>
                      </td>
                      <td className="py-2.5 px-2 text-right font-mono text-gray-300">
                        {range ? range.start : <span className="text-gray-600">—</span>}
                      </td>
                      <td className="py-2.5 px-2 text-right font-mono text-gray-300">
                        {range ? range.end : <span className="text-gray-600">—</span>}
                      </td>
                      <td className="py-2.5 px-2 text-right font-mono text-gray-300">
                        {summary ? formatMins(summary.net_minutes) : <span className="text-gray-600">—</span>}
                      </td>
                      <td className="py-2.5 px-2 text-right font-mono text-gray-300">
                        {summary ? formatHours(summary.reported_minutes) : <span className="text-gray-600">—</span>}
                      </td>
                      <td
                        className={`py-2.5 pl-2 text-right font-mono font-medium ${
                          balance === null
                            ? 'text-gray-600'
                            : balance > 0
                              ? 'text-green-400'
                              : balance < 0
                                ? 'text-red-400'
                                : 'text-gray-400'
                        }`}
                      >
                        {balance === null ? (
                          '—'
                        ) : (
                          <>
                            {balance > 0 ? '+' : ''}
                            {formatMins(balance)}
                          </>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
              {summaryMap.size > 0 && (
                <tfoot>
                  <tr className="border-t border-gray-600 text-gray-200 font-semibold">
                    <td className="pt-2.5 pr-3 text-xs uppercase tracking-wide text-gray-400">
                      {t('workhours:weeklyTotal')}
                    </td>
                    <td />
                    <td />
                    <td className="pt-2.5 px-2 text-right font-mono">{formatMins(totalNet)}</td>
                    <td className="pt-2.5 px-2 text-right font-mono">{formatHours(totalReported)}</td>
                    <td
                      className={`pt-2.5 pl-2 text-right font-mono ${
                        totalBalance > 0 ? 'text-green-400' : totalBalance < 0 ? 'text-red-400' : 'text-gray-400'
                      }`}
                    >
                      {totalBalance > 0 ? '+' : ''}
                      {formatMins(totalBalance)}
                    </td>
                  </tr>
                </tfoot>
              )}
            </table>
          </div>

          {/* Week flex balance */}
          {data && data.flex && (
            <div className="flex items-center justify-between bg-gray-800/50 rounded-lg px-4 py-3">
              <span className="text-sm text-gray-400">{t('workhours:flexPool')}</span>
              <span
                className={`font-mono text-sm font-semibold ${
                  data.flex.total_minutes < 0 ? 'text-red-400' : 'text-green-400'
                }`}
              >
                {data.flex.total_minutes > 0 ? '+' : ''}
                {formatMins(data.flex.total_minutes)}
              </span>
            </div>
          )}
        </>
      )}
    </div>
  )
}
