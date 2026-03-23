import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { Dumbbell, TrendingUp, Clock, Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import Widget from '../Widget'
import { timeAgo } from '../../utils/timeAgo'

interface Workout {
  id: number
  sport: string
  title: string
  started_at: string
  duration_seconds: number
  distance_meters: number
  avg_heart_rate: number
}

interface WeeklySummary {
  week_start: string
  total_duration_seconds: number
  total_distance_meters: number
  workout_count: number
  avg_heart_rate: number
}

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function formatDistance(meters: number): string {
  if (meters < 1000) return `${Math.round(meters)} m`
  return `${(meters / 1000).toFixed(1)} km`
}

type SportTranslationKey = 'widgets.training.sports.running' | 'widgets.training.sports.cycling' | 'widgets.training.sports.swimming' | 'widgets.training.sports.walking' | 'widgets.training.sports.hiking' | 'widgets.training.sports.default'

function sportKey(sport: string): SportTranslationKey | '' {
  switch (sport) {
    case 'running': return 'widgets.training.sports.running'
    case 'cycling': return 'widgets.training.sports.cycling'
    case 'swimming': return 'widgets.training.sports.swimming'
    case 'walking': return 'widgets.training.sports.walking'
    case 'hiking': return 'widgets.training.sports.hiking'
    default: return sport ? '' : 'widgets.training.sports.default'
  }
}

export default function FitnessWidget() {
  const { t } = useTranslation('dashboard')
  const { user } = useAuth()
  const [workouts, setWorkouts] = useState<Workout[]>([])
  const [summary, setSummary] = useState<WeeklySummary | null>(null)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()

    Promise.all([
      fetch('/api/training/workouts', { credentials: 'include', signal: controller.signal })
        .then(r => r.ok ? r.json() : null),
      fetch('/api/training/summary', { credentials: 'include', signal: controller.signal })
        .then(r => r.ok ? r.json() : null),
    ])
      .then(([wData, sData]) => {
        if (wData?.workouts) setWorkouts(wData.workouts.slice(0, 3))
        if (sData?.summaries?.length > 0) setSummary(sData.summaries[0])
        setLoaded(true)
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        console.error('FitnessWidget fetch error:', err)
        setLoaded(true)
      })

    return () => { controller.abort() }
  }, [user])

  if (!user || (!loaded)) return null
  if (loaded && workouts.length === 0) return null

  return (
    <Widget title={t('widgets.training.title')}>
      {summary && (
        <div className="grid grid-cols-3 gap-3 mb-4 pb-4 border-b border-gray-700">
          <div className="text-center">
            <div className="flex items-center justify-center gap-1 text-gray-400 mb-1">
              <Dumbbell size={12} />
            </div>
            <p className="text-lg font-bold tabular-nums">{summary.workout_count}</p>
            <p className="text-xs text-gray-500">{t('widgets.training.thisWeek')}</p>
          </div>
          <div className="text-center">
            <div className="flex items-center justify-center gap-1 text-gray-400 mb-1">
              <Clock size={12} />
            </div>
            <p className="text-lg font-bold tabular-nums">{formatDuration(summary.total_duration_seconds)}</p>
            <p className="text-xs text-gray-500">{t('widgets.training.duration')}</p>
          </div>
          <div className="text-center">
            <div className="flex items-center justify-center gap-1 text-gray-400 mb-1">
              <Route size={12} />
            </div>
            <p className="text-lg font-bold tabular-nums">{formatDistance(summary.total_distance_meters)}</p>
            <p className="text-xs text-gray-500">{t('widgets.training.distance')}</p>
          </div>
        </div>
      )}

      <div className="space-y-2">
        {workouts.map(w => (
          <div key={w.id} className="flex items-center gap-3 text-sm">
            <span className="text-xs font-medium text-gray-400 uppercase">{sportKey(w.sport) ? t(sportKey(w.sport) as SportTranslationKey) : w.sport}</span>
            <div className="flex-1 min-w-0">
              <p className="truncate text-gray-200">
                {w.title || w.sport}
              </p>
              <p className="text-xs text-gray-500">
                {timeAgo(w.started_at)}
                {w.distance_meters > 0 && ` · ${formatDistance(w.distance_meters)}`}
                {w.duration_seconds > 0 && ` · ${formatDuration(w.duration_seconds)}`}
              </p>
            </div>
            {w.avg_heart_rate > 0 && (
              <span className="text-xs text-gray-500 tabular-nums">{w.avg_heart_rate} bpm</span>
            )}
          </div>
        ))}
      </div>

      <Link
        to="/training"
        className="flex items-center gap-1 mt-4 text-xs text-blue-400 hover:text-blue-300"
      >
        <TrendingUp size={12} />
        {t('widgets.training.allWorkouts')}
      </Link>
    </Widget>
  )
}
