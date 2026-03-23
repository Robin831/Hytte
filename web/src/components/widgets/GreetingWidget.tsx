import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import Widget from '../Widget'

function getGreetingKey(hour: number, named: boolean): string {
  if (hour < 12) return named ? 'greeting.morningNamed' : 'greeting.morning'
  if (hour < 17) return named ? 'greeting.afternoonNamed' : 'greeting.afternoon'
  return named ? 'greeting.eveningNamed' : 'greeting.evening'
}

function GreetingWidget() {
  const { t } = useTranslation('common')
  const { user } = useAuth()
  const [now, setNow] = useState(new Date())

  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null

    function start() {
      if (timer !== null) clearInterval(timer)
      timer = setInterval(() => setNow(new Date()), 1000)
    }
    function stop() {
      if (timer !== null) {
        clearInterval(timer)
        timer = null
      }
    }
    function handleVisibility() {
      if (document.hidden) stop()
      else { setNow(new Date()); start() }
    }

    if (!document.hidden) start()
    document.addEventListener('visibilitychange', handleVisibility)
    return () => {
      stop()
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  }, [])

  const firstName = user?.name.split(' ')[0] ?? ''
  const greetingKey = getGreetingKey(now.getHours(), !!firstName)

  const timeStr = now.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
  const dateStr = now.toLocaleDateString(undefined, {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })

  return (
    <Widget className="col-span-full">
      <div className="flex flex-col items-center justify-center py-8 text-center">
        <p className="text-gray-400 text-lg mb-4">
          {t(greetingKey, { name: firstName })}
        </p>
        <div className="text-6xl font-bold tabular-nums tracking-tight mb-4">{timeStr}</div>
        <p className="text-gray-400 text-lg">{dateStr}</p>
      </div>
    </Widget>
  )
}

export default GreetingWidget
