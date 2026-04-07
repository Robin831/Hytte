import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Flame } from 'lucide-react'
import KidsSummaryWidget from './KidsSummaryWidget'
import CalendarWidget from './CalendarWidget'
import MoonPhaseWidget from './MoonPhaseWidget'

interface StreakInfo {
  current_count: number
  longest_count: number
}

interface StreaksData {
  daily_workout: StreakInfo
}

function StreakBadge() {
  const { t } = useTranslation('today')
  const [streak, setStreak] = useState<number | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/stars/streaks', { credentials: 'include', signal: controller.signal })
      .then((r) => (r.ok ? (r.json() as Promise<StreaksData>) : Promise.reject()))
      .then((d) => setStreak(d.daily_workout?.current_count ?? 0))
      .catch(() => {
        if (!controller.signal.aborted) setStreak(0)
      })
    return () => controller.abort()
  }, [])

  if (streak === null) return null
  if (streak === 0) return null

  return (
    <div className="flex items-center gap-1.5 text-sm">
      <Flame size={16} className="text-orange-400 shrink-0" />
      <span className="text-gray-200">
        {t('streak.days', { count: streak })}
      </span>
    </div>
  )
}

export default function KidTodayView() {
  const { t } = useTranslation('today')
  return (
    <>
      <div className="bg-gray-800 rounded-xl p-4 col-span-2">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.stars')}</h2>
        <div className="space-y-2">
          <KidsSummaryWidget />
          <StreakBadge />
        </div>
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.calendar')}</h2>
        <CalendarWidget />
      </div>
      <div className="bg-gray-800 rounded-xl p-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-2">{t('widgets.skywatch')}</h2>
        <MoonPhaseWidget />
      </div>
    </>
  )
}
