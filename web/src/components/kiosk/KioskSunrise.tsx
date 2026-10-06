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
  // does not light up a dark room, in a compact layout that gives up vertical
  // space to the rest of the page. Defaults to the normal daytime layout.
  dimmed?: boolean
}

export default function KioskSunrise({ sun, dimmed = false }: Props) {
  if (!sun) return null

  // The night variant is also the compact one: tighter padding and one step
  // down in type and icon size, still large enough to read across the room.
  const padding = dimmed ? 'py-1.5' : 'py-3'
  const textSize = dimmed ? 'text-base' : 'text-lg'
  const iconSize = dimmed ? 16 : 20

  if (sun.kind === 'polarDay') {
    return (
      <div
        data-dimmed={dimmed ? 'true' : 'false'}
        className={`px-4 ${padding} text-center ${textSize} ${
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
        className={`px-4 ${padding} text-center ${textSize} ${
          dimmed ? 'text-blue-800' : 'text-blue-300'
        }`}
      >
        Mørketid
      </div>
    )
  }

  if (!sun.sunrise || !sun.sunset) return null

  return (
    <div
      data-dimmed={dimmed ? 'true' : 'false'}
      className={`flex items-center justify-center ${dimmed ? 'gap-6' : 'gap-8'} px-4 ${padding} ${
        dimmed ? 'text-gray-600' : 'text-gray-300'
      }`}
    >
      <div className={`flex items-center gap-2 ${textSize}`}>
        <Sunrise size={iconSize} className={dimmed ? 'text-yellow-700' : 'text-yellow-400'} />
        <span>{kioskFormatTime(sun.sunrise)}</span>
      </div>
      <div className={`flex items-center gap-2 ${textSize}`}>
        <Sunset size={iconSize} className={dimmed ? 'text-orange-800' : 'text-orange-400'} />
        <span>{kioskFormatTime(sun.sunset)}</span>
      </div>
    </div>
  )
}
