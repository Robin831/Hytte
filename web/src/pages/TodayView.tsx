import { lazy, Suspense, type ComponentType, type LazyExoticComponent } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import { useCurrentTime } from '../hooks/useCurrentTime'
import { formatDate, formatTime } from '../utils/formatDate'
import AmbientLine from '../components/today/AmbientLine'
import { TodayHeaderSkeleton, TodayGridSkeleton } from '../components/today/TodaySkeleton'

const ParentTodayView = lazy(() => import('../components/today/ParentTodayView'))
const KidTodayView = lazy(() => import('../components/today/KidTodayView'))
const GuestTodayView = lazy(() => import('../components/today/GuestTodayView'))

export type FamilyRole = 'parent' | 'child' | 'guest'

function useFamilyRole(): FamilyRole | null {
  const { user, loading, familyStatus } = useAuth()
  if (loading) return null
  if (!user) return 'guest'
  if (familyStatus?.is_child) return 'child'
  if (familyStatus?.is_parent) return 'parent'
  return 'guest'
}

const widgetsByRole: Record<FamilyRole, LazyExoticComponent<ComponentType>> = {
  parent: ParentTodayView,
  child: KidTodayView,
  guest: GuestTodayView,
}

export default function TodayView() {
  const { t } = useTranslation('today')
  const { user } = useAuth()
  const role = useFamilyRole()
  const now = useCurrentTime()

  if (role === null) {
    return (
      <div className="h-[calc(100dvh-3.5rem)] md:h-[100dvh] flex flex-col p-4 sm:p-6 overflow-hidden">
        <TodayHeaderSkeleton />
        <TodayGridSkeleton />
      </div>
    )
  }

  const timeStr = formatTime(now, { hour: '2-digit', minute: '2-digit' })
  const dateStr = formatDate(now, { weekday: 'long', month: 'long', day: 'numeric' })
  const firstName = user?.name?.trim().split(/\s+/)[0] || undefined

  const Widgets = widgetsByRole[role]

  return (
    <div className="h-[calc(100dvh-3.5rem)] md:h-[100dvh] flex flex-col p-4 sm:p-6 overflow-hidden">
      {/* Header: time + date, watch-face style */}
      <header className="text-center mb-4 sm:mb-6 shrink-0">
        <time className="text-4xl sm:text-5xl font-light tabular-nums tracking-tight">
          {timeStr}
        </time>
        <p className="text-sm sm:text-base text-gray-400 mt-1">
          {dateStr}
        </p>
        <AmbientLine firstName={firstName} />
        <p className="text-xs text-gray-500 mt-1">
          {t(`role.${role}`)}
        </p>
      </header>

      {/* Widget grid */}
      <Suspense fallback={<TodayGridSkeleton />}>
        <div className="grid grid-cols-2 gap-3 sm:gap-4 flex-1 min-h-0 auto-rows-fr">
          <Widgets />
        </div>
      </Suspense>
    </div>
  )
}
