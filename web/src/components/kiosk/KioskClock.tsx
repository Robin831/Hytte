import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

export default function KioskClock() {
  const { i18n } = useTranslation('kiosk')
  const [now, setNow] = useState(() => new Date())

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(id)
  }, [])

  const timeStr = now.toLocaleTimeString(i18n.language, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })

  const dateStr = now.toLocaleDateString(i18n.language, {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
  })

  return (
    <div className="flex flex-col items-center py-6">
      <div className="text-8xl font-mono font-bold tracking-wider text-white tabular-nums">
        {timeStr}
      </div>
      <div className="mt-2 text-2xl text-gray-300 capitalize">{dateStr}</div>
    </div>
  )
}
