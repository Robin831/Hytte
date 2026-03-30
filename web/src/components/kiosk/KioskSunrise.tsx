import { useTranslation } from 'react-i18next'
import { Sunrise, Sunset } from 'lucide-react'

interface SunTimes {
  kind: string
  sunrise?: string
  sunset?: string
}

interface Props {
  sun?: SunTimes | null
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString('nb-NO', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

export default function KioskSunrise({ sun }: Props) {
  const { t } = useTranslation('kiosk')

  if (!sun) return null

  if (sun.kind === 'polarDay') {
    return (
      <div className="px-4 py-3 text-center text-yellow-300 text-lg">
        {t('polarDay')}
      </div>
    )
  }

  if (sun.kind === 'polarNight') {
    return (
      <div className="px-4 py-3 text-center text-blue-300 text-lg">
        {t('polarNight')}
      </div>
    )
  }

  if (!sun.sunrise || !sun.sunset) return null

  return (
    <div className="flex items-center justify-center gap-8 px-4 py-3 text-gray-300">
      <div className="flex items-center gap-2 text-lg">
        <Sunrise size={20} className="text-yellow-400" />
        <span>{formatTime(sun.sunrise)}</span>
      </div>
      <div className="flex items-center gap-2 text-lg">
        <Sunset size={20} className="text-orange-400" />
        <span>{formatTime(sun.sunset)}</span>
      </div>
    </div>
  )
}
