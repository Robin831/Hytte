import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import { formatDate, formatTime } from '../utils/formatDate'
import { getGreetingKey } from '../utils/greeting'
import { useNow } from '../hooks/useNow'
import TodayScheduleCard from '../components/home/TodayScheduleCard'
import WeatherCard from '../components/home/WeatherCard'

export default function HomePage() {
  const { t } = useTranslation('common')
  const auth = useAuth()
  const user = auth.user
  const hasFeature = auth.hasFeature ?? (() => false)
  const now = useNow()

  const firstName = user?.name.split(' ')[0] ?? ''
  const hour = now.getHours()

  const timeStr = formatTime(now, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
  const dateStr = formatDate(now, {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })

  return (
    <div className="p-4 sm:p-6">
      {/* Greeting header */}
      <header className="text-center py-6 sm:py-8">
        <div className="flex items-center justify-center gap-3 mb-4">
          {user?.picture ? (
            <img
              src={user.picture}
              alt={user.name}
              className="w-12 h-12 rounded-full"
              referrerPolicy="no-referrer"
            />
          ) : user ? (
            <div
              className="w-12 h-12 rounded-full bg-blue-600 flex items-center justify-center text-lg font-medium"
              role="img"
              aria-label={user.name}
            >
              {user.name.charAt(0).toUpperCase()}
            </div>
          ) : null}
        </div>
        <p className="text-gray-400 text-lg mb-4">
          {firstName
            ? t(getGreetingKey(hour, true), { name: firstName })
            : t(getGreetingKey(hour, false))}
        </p>
        <time className="block text-6xl font-bold tabular-nums tracking-tight mb-4" dateTime={now.toISOString()}>{timeStr}</time>
        <p className="text-gray-400 text-lg">{dateStr}</p>
      </header>

      {/* Today's briefing cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <WeatherCard />
        {hasFeature('calendar') && <TodayScheduleCard />}
      </div>
    </div>
  )
}
