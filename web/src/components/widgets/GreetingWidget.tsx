import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import { formatDate, formatTime } from '../../utils/formatDate'
import { getGreetingKey } from '../../utils/greeting'
import { useCurrentTime } from '../../hooks/useCurrentTime'

/** One-line header: the greeting on the left, clock and date on the right. */
function GreetingWidget() {
  const { t } = useTranslation('common')
  const { user } = useAuth()
  const now = useCurrentTime()

  const firstName = user?.name.split(' ')[0] ?? ''
  const hour = now.getHours()
  const timeStr = formatTime(now, { hour: '2-digit', minute: '2-digit' })
  const dateStr = formatDate(now, { weekday: 'long', month: 'long', day: 'numeric' })

  return (
    <header className="col-span-full flex items-center justify-between gap-4 rounded-xl bg-gray-800 px-5 py-4">
      <p className="min-w-0 text-lg font-semibold">
        {firstName ? t(getGreetingKey(hour, true), { name: firstName }) : t(getGreetingKey(hour, false))}
      </p>
      <div className="shrink-0 text-right">
        <time className="block text-2xl font-bold tabular-nums leading-tight" dateTime={now.toISOString()}>
          {timeStr}
        </time>
        <p className="text-xs text-gray-400">{dateStr}</p>
      </div>
    </header>
  )
}

export default GreetingWidget
