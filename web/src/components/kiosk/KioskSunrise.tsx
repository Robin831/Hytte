import { Sunrise, Sunset } from 'lucide-react'
import type { SunTimes } from './nightMode'

// Kiosk-local time formatter — avoids importing utils/formatDate which
// depends on i18n (fails on Android 5 / old Firefox).
function kioskFormatTime(dateStr: string): string {
  const d = new Date(dateStr)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

interface Props {
  // Shared with nightMode.ts, which reads the same payload field to decide
  // whether the screen is in its night window.
  sun?: SunTimes | null
  // Night mode: render the reduced-contrast palette so a wall-mounted screen
  // does not light up a dark room. Defaults to the normal daytime palette.
  dimmed?: boolean
}

export default function KioskSunrise({ sun, dimmed = false }: Props) {
  if (!sun) return null

  if (sun.kind === 'polarDay') {
    return (
      <div
        data-dimmed={dimmed ? 'true' : 'false'}
        className={`px-4 py-3 text-center text-lg ${
          dimmed ? 'text-yellow-700' : 'text-yellow-300'
        }`}
      >
        Midnattssol
      </div>
    )
  }

  if (sun.kind === 'polarNight') {
    return (
      <div
        data-dimmed={dimmed ? 'true' : 'false'}
        className={`px-4 py-3 text-center text-lg ${dimmed ? 'text-blue-800' : 'text-blue-300'}`}
      >
        Mørketid
      </div>
    )
  }

  if (!sun.sunrise || !sun.sunset) return null

  return (
    <div
      data-dimmed={dimmed ? 'true' : 'false'}
      className={`flex items-center justify-center gap-8 px-4 py-3 ${
        dimmed ? 'text-gray-600' : 'text-gray-300'
      }`}
    >
      <div className="flex items-center gap-2 text-lg">
        <Sunrise size={20} className={dimmed ? 'text-yellow-700' : 'text-yellow-400'} />
        <span>{kioskFormatTime(sun.sunrise)}</span>
      </div>
      <div className="flex items-center gap-2 text-lg">
        <Sunset size={20} className={dimmed ? 'text-orange-800' : 'text-orange-400'} />
        <span>{kioskFormatTime(sun.sunset)}</span>
      </div>
    </div>
  )
}
