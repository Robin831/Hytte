import { useState, useEffect } from 'react'

// Kiosk-local formatters — avoids importing utils/formatDate which
// depends on i18n (fails on Android 5 / old Firefox).
const DAYS = ['søndag', 'mandag', 'tirsdag', 'onsdag', 'torsdag', 'fredag', 'lørdag']
const MONTHS = ['januar', 'februar', 'mars', 'april', 'mai', 'juni', 'juli', 'august', 'september', 'oktober', 'november', 'desember']

function pad2(n: number): string { return String(n).padStart(2, '0') }

export default function KioskClock() {
  const [now, setNow] = useState(() => new Date())

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(id)
  }, [])

  const timeStr = `${pad2(now.getHours())}:${pad2(now.getMinutes())}:${pad2(now.getSeconds())}`
  const dateStr = `${DAYS[now.getDay()]} ${now.getDate()}. ${MONTHS[now.getMonth()]}`

  return (
    <div className="flex flex-col items-center py-6">
      <div className="text-8xl font-mono font-bold tracking-wider text-white tabular-nums">
        {timeStr}
      </div>
      <div className="mt-2 text-2xl text-gray-300 capitalize">{dateStr}</div>
    </div>
  )
}
