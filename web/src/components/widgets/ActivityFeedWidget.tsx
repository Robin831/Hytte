import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { Dumbbell, FlaskConical, StickyNote, LinkIcon, Activity } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { useAuth } from '../../auth'
import Widget from '../Widget'
import { timeAgo } from '../../utils/timeAgo'

interface ActivityItem {
  type: string
  timestamp: string
  link?: string
  sport?: string
  title?: string
  comment?: string
  code?: string
}

const typeIcons: Record<string, typeof Activity> = {
  workout: Dumbbell,
  lactate: FlaskConical,
  note: StickyNote,
  link: LinkIcon,
}

const typeColors: Record<string, string> = {
  workout: 'text-green-400',
  lactate: 'text-purple-400',
  note: 'text-yellow-400',
  link: 'text-blue-400',
}

function sportLabel(t: TFunction, sport: string | undefined): string {
  if (!sport) {
    return t('activity.sports.default')
  }
  const translated = t(`activity.sports.${sport}`, { defaultValue: '' })
  if (translated) return translated
  return sport.charAt(0).toUpperCase() + sport.slice(1)
}

function renderLabel(t: TFunction, item: ActivityItem): string {
  switch (item.type) {
    case 'workout': {
      const sport = sportLabel(t, item.sport)
      if (item.title && item.title.length > 0) {
        return t('activity.workoutWithTitle', { sport, title: item.title })
      }
      return t('activity.workout', { sport })
    }
    case 'lactate':
      if (item.comment && item.comment.length > 0) {
        return t('activity.lactateWithComment', { comment: item.comment })
      }
      return t('activity.lactate')
    case 'note':
      if (item.title && item.title.length > 0) {
        return t('activity.noteWithTitle', { title: item.title })
      }
      return t('activity.note')
    case 'link': {
      const code = item.code ?? ''
      if (item.title && item.title.length > 0) {
        return t('activity.linkWithTitle', { code, title: item.title })
      }
      return t('activity.shortLink', { code })
    }
    default:
      return ''
  }
}

export default function ActivityFeedWidget() {
  const { t } = useTranslation('dashboard')
  const { t: tCommon } = useTranslation('common')
  const { user } = useAuth()
  const [items, setItems] = useState<ActivityItem[]>([])
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()

    fetch('/api/dashboard/activity', { credentials: 'include', signal: controller.signal })
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data?.items) setItems(data.items)
        setLoaded(true)
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        console.error('ActivityFeedWidget fetch error:', err)
        setLoaded(true)
      })

    return () => { controller.abort() }
  }, [user])

  if (!user || !loaded) return null
  if (loaded && items.length === 0) return null

  return (
    <Widget title={t('widgets.activity.title')}>
      <div className="space-y-3">
        {items.slice(0, 7).map((item, index) => {
          const Icon = typeIcons[item.type] || Activity
          const color = typeColors[item.type] || 'text-gray-400'
          const label = renderLabel(t, item)
          const itemKey = `${item.type}:${item.timestamp}:${item.code ?? ''}:${index}`
          const content = (
            <div className="flex items-start gap-2.5">
              <Icon size={14} className={`${color} mt-0.5 shrink-0`} />
              <div className="flex-1 min-w-0">
                <p className="text-sm text-gray-300 truncate">{label}</p>
                <p className="text-xs text-gray-600">{timeAgo(item.timestamp, tCommon)}</p>
              </div>
            </div>
          )

          if (item.link) {
            return (
              <Link key={itemKey} to={item.link} className="block hover:bg-gray-700/30 -mx-1 px-1 rounded transition-colors">
                {content}
              </Link>
            )
          }
          return <div key={itemKey}>{content}</div>
        })}
      </div>
    </Widget>
  )
}
