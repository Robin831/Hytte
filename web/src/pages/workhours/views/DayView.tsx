import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Clock, Copy } from 'lucide-react'
import { Skeleton } from '../../../components/ui/skeleton'
import type { LeaveType } from '../types'
import { getNorwegianHolidays } from '../dateUtils'
import type { PunchOutResult } from '../useWorkHoursApi'
import type { PunchClock } from '../usePunchClock'
import { useDayEntry } from '../hooks/useDayEntry'
import { useFlex } from '../hooks/useFlex'
import SessionList from '../components/SessionList'
import DeductionList from '../components/DeductionList'
import LiveEstimateCard from '../components/LiveEstimateCard'
import DaySummaryCard from '../components/DaySummaryCard'
import DayNavigator from '../components/DayNavigator'
import FlexPoolCard from '../components/FlexPoolCard'

export default function DayView({
  currentDate,
  setCurrentDate,
  onNavigateToSettings,
  punch,
  lastPunchOut,
  onBusyChange,
}: {
  currentDate: string
  setCurrentDate: (d: string | ((prev: string) => string)) => void
  onNavigateToSettings: () => void
  punch: PunchClock
  /** The latest punch-out response from the page's punch clock; applied when it changes. */
  lastPunchOut: PunchOutResult | null
  /** Reports whether a day edit is saving, so the page's punch shortcut can wait for it. */
  onBusyChange: (busy: boolean) => void
}) {
  const { t } = useTranslation(['workhours', 'common'])

  const flexPool = useFlex()
  const flex = flexPool.flex
  const entry = useDayEntry({
    date: currentDate,
    openPunchStart: punch.openPunch?.start ?? null,
    lastPunchOut,
    onFlexChanged: flexPool.reload,
  })
  const { navHolidaySet, leaveDay, leaveSaving, sessions, summary } = entry

  useEffect(() => {
    onBusyChange(entry.saving)
    return () => onBusyChange(false)
  }, [entry.saving, onBusyChange])

  const handleFlexRedeem = async () => {
    if (await flexPool.redeem(currentDate)) entry.reload()
  }

  // Punch actions and day edits share the form, so either one in flight locks it.
  const busy = entry.saving || punch.saving

  // Live "if punched out now" totals: the shared calculateDay over this day's
  // saved data plus the open punch's elapsed time.
  const liveEstimate = punch.liveEstimate({
    sessions,
    deductions: entry.deductions,
    settings: entry.workSettings,
    lunch: entry.lunch,
    redeemedMinutes: entry.redeemedMinutes,
  })

  const currentYear = parseInt(currentDate.split('-')[0])
  const holidays = getNorwegianHolidays(currentYear)
  const holidayName = holidays.get(currentDate)

  return (
    <div className="space-y-6">
      <DayNavigator currentDate={currentDate} setCurrentDate={setCurrentDate} navHolidaySet={navHolidaySet} />

      {entry.loading ? (
        <div role="status" aria-live="polite" className="inline-flex items-center gap-2">
          <Skeleton className="h-5 w-24" />
          <span className="sr-only">{t('common:skeleton.loading')}</span>
        </div>
      ) : (
        <>
          {/* Holiday notice */}
          {holidayName && (
            <div className="flex items-center gap-2 bg-gray-800/60 rounded-lg px-3 py-2 text-sm text-gray-300">
              <span className="text-base">🎉</span>
              <span>{t('workhours:holidayLabel', { name: holidayName })}</span>
            </div>
          )}

          {/* Leave day marker */}
          <section className="space-y-2">
            <div className="flex items-center justify-between">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
                {t('workhours:leaveDay')}
              </h2>
              {leaveDay && (
                <button
                  type="button"
                  onClick={() => entry.setLeave(null)}
                  disabled={leaveSaving}
                  className="text-xs text-gray-500 hover:text-gray-300 transition-colors disabled:opacity-40 cursor-pointer"
                >
                  {t('workhours:removeLeave')}
                </button>
              )}
            </div>
            <div className="flex flex-wrap gap-2">
              {(['vacation', 'sick', 'personal', 'public_holiday'] as LeaveType[]).map(lt => (
                <button
                  key={lt}
                  type="button"
                  onClick={() => entry.setLeave(leaveDay?.leave_type === lt ? null : lt)}
                  disabled={leaveSaving}
                  className={`px-3 py-1.5 text-xs rounded transition-colors disabled:opacity-40 cursor-pointer border ${
                    leaveDay?.leave_type === lt
                      ? lt === 'vacation'
                        ? 'bg-purple-700 border-purple-500 text-white'
                        : lt === 'sick'
                          ? 'bg-orange-700 border-orange-500 text-white'
                          : lt === 'personal'
                            ? 'bg-teal-700 border-teal-500 text-white'
                            : 'bg-gray-700 border-gray-500 text-white'
                      : 'bg-gray-800 border-gray-700 text-gray-400 hover:text-gray-200 hover:border-gray-500'
                  }`}
                >
                  {t(`workhours:leaveType_${lt}`)}
                </button>
              ))}
            </div>
          </section>

          {/* Sessions */}
          <section className="space-y-3">
            <div className="flex items-center justify-between">
              <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
                {t('workhours:sessions')}
              </h2>
              <div className="flex gap-2">
                {/* Copy yesterday button */}
                <button
                  type="button"
                  onClick={entry.copyYesterday}
                  disabled={busy || sessions.length > 0}
                  className="flex items-center gap-1 px-2 py-1 text-xs text-gray-400 hover:text-gray-200 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-40"
                  aria-label={t('workhours:copyYesterday')}
                  title={t('workhours:copyYesterday')}
                >
                  <Copy size={12} />
                  {t('workhours:copyYesterday')}
                </button>
                {/* Punch clock button */}
                {punch.openPunch === null ? (
                  <button
                    type="button"
                    onClick={punch.punchIn}
                    disabled={busy}
                    className="flex items-center gap-1 px-2 py-1 text-xs text-green-400 hover:text-green-300 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-40"
                    aria-label={t('workhours:punchIn')}
                  >
                    <Clock size={12} />
                    {t('workhours:punchIn')}
                  </button>
                ) : (
                  <>
                    <div className="flex items-center gap-1 text-xs">
                      <label htmlFor="punch-start-time" className="text-gray-400">{t('workhours:punchStartLabel')}</label>
                      <input
                        id="punch-start-time"
                        key={punch.openPunch.start}
                        type="time"
                        defaultValue={punch.openPunch.start}
                        onBlur={e => punch.editStart(e.target.value)}
                        onKeyDown={e => {
                          if (e.key === 'Enter') {
                            e.currentTarget.blur()
                          }
                        }}
                        className="bg-gray-800 border border-gray-600 rounded px-1 py-0.5 text-xs text-white font-mono w-[5rem] focus:border-blue-500 focus:outline-none"
                      />
                    </div>
                    <button
                      type="button"
                      onClick={punch.punchOut}
                      disabled={busy}
                      className="flex items-center gap-1 px-2 py-1 text-xs text-red-400 hover:text-red-300 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-40 animate-pulse"
                      aria-label={t('workhours:punchOut')}
                    >
                      <Clock size={12} />
                      {t('workhours:punchOut')}
                    </button>
                    <button
                      type="button"
                      onClick={punch.cancelPunch}
                      disabled={busy}
                      className="flex items-center gap-1 px-2 py-1 text-xs text-gray-400 hover:text-gray-200 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-40"
                      aria-label={t('workhours:cancelPunch')}
                    >
                      {t('workhours:cancelPunch')}
                    </button>
                  </>
                )}
              </div>
            </div>

            {/* Null off the punch-in's own day; see usePunchClock's liveEstimate. */}
            {liveEstimate && <LiveEstimateCard liveEstimate={liveEstimate} />}

            <SessionList
              sessions={sessions}
              overlaps={entry.overlaps}
              grossMinutes={summary?.gross_minutes ?? 0}
              editSessionId={entry.editSessionId}
              edit={entry.edit}
              draft={entry.draft}
              conflict={entry.conflict}
              disabled={busy}
              onEditChange={entry.changeEdit}
              onDraftChange={entry.changeDraft}
              onStartEdit={entry.startEditSession}
              onCancelEdit={entry.cancelEditSession}
              onSaveEdit={entry.saveEditSession}
              onToggleInternal={entry.toggleInternal}
              onDelete={entry.deleteSession}
              onAdd={entry.addSession}
            />
          </section>

          <DeductionList
            deductions={entry.deductions}
            lunch={entry.lunch}
            lunchMinutes={summary?.lunch_minutes ?? 0}
            presets={entry.presets}
            sortedPresets={entry.sortedPresets}
            selectedPresetId={entry.selectedPresetId}
            draft={entry.deductionDraft}
            disabled={busy}
            onLunchToggle={entry.toggleLunch}
            onPresetToggle={entry.togglePreset}
            onPresetSelect={entry.selectPreset}
            onDraftChange={entry.changeDeductionDraft}
            onAdd={entry.addDeduction}
            onDelete={entry.deleteDeduction}
            onManagePresets={onNavigateToSettings}
          />

          {/* Summary card */}
          {summary && <DaySummaryCard summary={summary} />}

          {/* Flex pool running total */}
          {flex && <FlexPoolCard flex={flex} redeeming={flexPool.redeeming} onRedeem={handleFlexRedeem} />}
        </>
      )}
    </div>
  )
}
