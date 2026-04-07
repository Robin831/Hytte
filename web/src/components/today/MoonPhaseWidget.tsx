import { useEffect, useReducer } from 'react'
import { Moon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { usePreferredLocation } from '../../usePreferredLocation'

interface MoonInfo {
  phase: string
  illumination: number
  phase_value: number
}

interface NowResponse {
  moon: MoonInfo
}

type State = { loading: boolean; error: boolean; moon: MoonInfo | null }
type Action = { type: 'start' } | { type: 'done'; moon: MoonInfo } | { type: 'error' }

function reducer(_state: State, action: Action): State {
  switch (action.type) {
    case 'start': return { loading: true, error: false, moon: _state.moon }
    case 'done': return { loading: false, error: false, moon: action.moon }
    case 'error': return { loading: false, error: true, moon: _state.moon }
  }
}

const MOON_ICONS: Record<string, string> = {
  'New Moon': '🌑',
  'Waxing Crescent': '🌒',
  'First Quarter': '🌓',
  'Waxing Gibbous': '🌔',
  'Full Moon': '🌕',
  'Waning Gibbous': '🌖',
  'Last Quarter': '🌗',
  'Waning Crescent': '🌘',
}

export default function MoonPhaseWidget() {
  const { t } = useTranslation('today')
  const location = usePreferredLocation()
  const [{ loading, moon }, dispatch] = useReducer(reducer, { loading: true, error: false, moon: null })

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'start' })
    fetch(`/api/skywatch/now?lat=${location.lat}&lon=${location.lon}`, {
      credentials: 'include',
      signal: controller.signal,
    })
      .then((r) => (r.ok ? (r.json() as Promise<NowResponse>) : Promise.reject()))
      .then((d) => dispatch({ type: 'done', moon: d.moon }))
      .catch(() => { if (!controller.signal.aborted) dispatch({ type: 'error' }) })
    return () => controller.abort()
  }, [location.lat, location.lon])

  if (loading && !moon) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Moon size={16} className="shrink-0" />
        <span>{t('moon.loading')}</span>
      </div>
    )
  }

  if (!moon) return null

  const icon = MOON_ICONS[moon.phase] ?? '🌙'
  const pct = Math.round(moon.illumination * 100)
  const phaseKey = moon.phase.toLowerCase().replace(/\s+/g, '_')

  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="text-base leading-none" role="img" aria-label={moon.phase}>{icon}</span>
      <span className="text-gray-300">{t(`moon.phase.${phaseKey}`, moon.phase)}</span>
      <span className="text-gray-500">{pct}%</span>
    </div>
  )
}
