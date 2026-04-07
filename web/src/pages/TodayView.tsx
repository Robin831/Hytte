import { useState, useEffect } from 'react'
import type { ComponentType } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import { formatDate, formatTime } from '../utils/formatDate'
import {
  ClockWeatherWidget,
  NetatmoWidget,
  BusDepartureWidget,
  CalendarWidget,
  WorkHoursWidget,
  KidsSummaryWidget,
  MoonPhaseWidget,
} from '../components/today'

export type FamilyRole = 'parent' | 'child' | 'guest'

function useFamilyRole(): FamilyRole | null {
  const { user, loading, familyStatus } = useAuth()
  if (loading) return null
  if (!user) return 'guest'
  if (familyStatus?.is_child) return 'child'
  if (familyStatus?.is_parent) return 'parent'
  return 'guest'
}

function ParentWidgets() {
  const { t } = useTranslation('today')
  return (
    <>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.weather')}</h2>
        <ClockWeatherWidget />
        <div className="mt-2"><NetatmoWidget /></div>
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.calendar')}</h2>
        <CalendarWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.training')}</h2>
        <WorkHoursWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.family')}</h2>
        <BusDepartureWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.budget')}</h2>
        <MoonPhaseWidget />
      </div>
    </>
  )
}

function KidWidgets() {
  const { t } = useTranslation('today')
  return (
    <>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.stars')}</h2>
        <KidsSummaryWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.chores')}</h2>
        <CalendarWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.challenges')}</h2>
        <MoonPhaseWidget />
      </div>
    </>
  )
}

function GuestWidgets() {
  const { t } = useTranslation('today')
  return (
    <>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.weather')}</h2>
        <ClockWeatherWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.calendar')}</h2>
        <CalendarWidget />
      </div>
    </>
  )
}

const widgetsByRole: Record<FamilyRole, ComponentType> = {
  parent: ParentWidgets,
  child: KidWidgets,
  guest: GuestWidgets,
}

export default function TodayView() {
  const { t } = useTranslation('today')
  const role = useFamilyRole()
  const [now, setNow] = useState(() => new Date())

  useEffect(() => {
    let intervalId: ReturnType<typeof setInterval> | undefined

    const updateNow = () => setNow(new Date())
    const current = new Date()
    const msUntilNextMinute =
      (60 - current.getSeconds()) * 1000 - current.getMilliseconds()

    const timeoutId = setTimeout(() => {
      updateNow()
      intervalId = setInterval(updateNow, 60_000)
    }, msUntilNextMinute)

    return () => {
      clearTimeout(timeoutId)
      if (intervalId) {
        clearInterval(intervalId)
      }
    }
  }, [])

  if (role === null) return null

  const timeStr = formatTime(now, { hour: '2-digit', minute: '2-digit' })
  const dateStr = formatDate(now, { weekday: 'long', month: 'long', day: 'numeric' })

  const Widgets = widgetsByRole[role]

  return (
    <div className="h-[calc(100vh-3.5rem)] md:h-screen flex flex-col p-4 sm:p-6 overflow-hidden">
      {/* Header: time + date, watch-face style */}
      <header className="text-center mb-4 sm:mb-6 shrink-0">
        <time className="text-4xl sm:text-5xl font-light tabular-nums tracking-tight">
          {timeStr}
        </time>
        <p className="text-sm sm:text-base text-gray-400 mt-1">
          {dateStr}
        </p>
        <p className="text-xs text-gray-500 mt-1">
          {t(`role.${role}`)}
        </p>
      </header>

      {/* Widget grid */}
      <div className="grid grid-cols-2 gap-3 sm:gap-4 flex-1 min-h-0 auto-rows-fr">
        <Widgets />
      </div>
    </div>
  )
}
