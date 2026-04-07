import { useEffect, useReducer } from 'react'
import { Thermometer } from 'lucide-react'
import { useTranslation } from 'react-i18next'

interface ModuleReadings {
  Indoor: { Temperature: number; Humidity: number } | null
  Outdoor: { Temperature: number; Humidity: number } | null
  FetchedAt: string
}

type State = { loading: boolean; error: boolean; data: ModuleReadings | null }
type Action = { type: 'start' } | { type: 'done'; data: ModuleReadings } | { type: 'error' }

function reducer(_state: State, action: Action): State {
  switch (action.type) {
    case 'start': return { loading: true, error: false, data: _state.data }
    case 'done': return { loading: false, error: false, data: action.data }
    case 'error': return { loading: false, error: true, data: _state.data }
  }
}

export default function NetatmoWidget() {
  const { t } = useTranslation('today')
  const [{ loading, error, data }, dispatch] = useReducer(reducer, { loading: true, error: false, data: null })

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'start' })
    fetch('/api/netatmo/current', { credentials: 'include', signal: controller.signal })
      .then((r) => (r.ok ? (r.json() as Promise<ModuleReadings>) : Promise.reject()))
      .then((d) => dispatch({ type: 'done', data: d }))
      .catch(() => { if (!controller.signal.aborted) dispatch({ type: 'error' }) })
    return () => controller.abort()
  }, [])

  if (loading && !data) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Thermometer size={16} className="shrink-0" />
        <span>{t('netatmo.loading')}</span>
      </div>
    )
  }

  if (error && !data) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Thermometer size={16} className="shrink-0" />
        <span>{t('unavailable')}</span>
      </div>
    )
  }

  if (!data) return null

  const indoor = data.Indoor?.Temperature
  const outdoor = data.Outdoor?.Temperature

  return (
    <div className="flex items-center gap-2 text-sm">
      <Thermometer size={16} className="text-gray-400 shrink-0" />
      {indoor !== undefined && (
        <span>
          <span className="text-gray-500">{t('netatmo.indoor')}</span>{' '}
          <span className="text-gray-200">{indoor.toFixed(1)}°</span>
        </span>
      )}
      {outdoor !== undefined && (
        <span>
          <span className="text-gray-500">{t('netatmo.outdoor')}</span>{' '}
          <span className="text-gray-200">{outdoor.toFixed(1)}°</span>
        </span>
      )}
    </div>
  )
}
