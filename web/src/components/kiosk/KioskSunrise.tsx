import { Sunrise, Sunset } from 'lucide-react'
// Kiosk-local time formatter — avoids importing utils/formatDate which
// depends on i18n (fails on Android 5 / old Firefox).
function kioskFormatTime(dateStr: string): string {
  const d = new Date(dateStr)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

interface SunTimes {
  kind: string
  sunrise?: string
  sunset?: string
}

interface Props {
  sun?: SunTimes | null
}

export default function KioskSunrise({ sun }: Props) {
  if (!sun) return null

  if (sun.kind === 'polarDay') {
    return (
      <div className="px-4 py-3 text-center text-yellow-300 text-lg">
        Midnattssol
      </div>
    )
  }

  if (sun.kind === 'polarNight') {
    return (
      <div className="px-4 py-3 text-center text-blue-300 text-lg">
        Mørketid
      </div>
    )
  }

  if (!sun.sunrise || !sun.sunset) return null

  return (
    <div className="flex items-center justify-center gap-8 px-4 py-3 text-gray-300">
      <div className="flex items-center gap-2 text-lg">
        <Sunrise size={20} className="text-yellow-400" />
        <span>{kioskFormatTime(sun.sunrise)}</span>
      </div>
      <div className="flex items-center gap-2 text-lg">
        <Sunset size={20} className="text-orange-400" />
        <span>{kioskFormatTime(sun.sunset)}</span>
      </div>
    </div>
  )
}
