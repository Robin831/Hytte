import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import { formatDate, formatTime } from '../../utils/formatDate'
import { getGreetingKey } from '../../utils/greeting'
import { useNow } from '../../hooks/useNow'
import Widget from '../Widget'

function GreetingWidget() {
  const { t } = useTranslation('common')
  const { user } = useAuth()
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
    <Widget className="col-span-full">
      <div className="flex flex-col items-center justify-center py-8 text-center">
        <p className="text-gray-400 text-lg mb-4">
          {firstName
            ? t(getGreetingKey(hour, true), { name: firstName })
            : t(getGreetingKey(hour, false))}
        </p>
        <div className="text-6xl font-bold tabular-nums tracking-tight mb-4">{timeStr}</div>
        <p className="text-gray-400 text-lg">{dateStr}</p>
      </div>
    </Widget>
  )
}

export default GreetingWidget
