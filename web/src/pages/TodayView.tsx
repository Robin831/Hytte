import { useState, useEffect } from 'react'
import type { ComponentType } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import { formatDate, formatTime } from '../utils/formatDate'

export type FamilyRole = 'parent' | 'kid' | 'guest'

function useFamilyRole(): FamilyRole {
  const { user, familyStatus } = useAuth()
  if (!user) return 'guest'
  if (familyStatus?.is_child) return 'kid'
  if (familyStatus?.is_parent) return 'parent'
  return 'guest'
}

function ParentWidgets() {
  const { t } = useTranslation('today')
  return (
    <>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.weather')}</h2>
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.calendar')}</h2>
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.training')}</h2>
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.family')}</h2>
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.budget')}</h2>
      </div>
    </>
  )
}

function KidWidgets() {
  const { t } = useTranslation('today')
  return (
    <>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.stars')}</h2>
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.chores')}</h2>
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.challenges')}</h2>
      </div>
    </>
  )
}

function GuestWidgets() {
  const { t } = useTranslation('today')
  return (
    <>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.weather')}</h2>
      </div>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">{t('widgets.calendar')}</h2>
      </div>
    </>
  )
}

const widgetsByRole: Record<FamilyRole, ComponentType> = {
  parent: ParentWidgets,
  kid: KidWidgets,
  guest: GuestWidgets,
}

export default function TodayView() {
  const { t } = useTranslation('today')
  const role = useFamilyRole()
  const [now, setNow] = useState(() => new Date())

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 60_000)
    return () => clearInterval(id)
  }, [])

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
