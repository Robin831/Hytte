import { useState, useEffect } from 'react'
import { formatTime, formatDate } from '../../utils/formatDate'

export default function KioskClock() {
  const [now, setNow] = useState(() => new Date())

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(id)
  }, [])

  const timeStr = formatTime(now, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })

  const dateStr = formatDate(now, {
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
