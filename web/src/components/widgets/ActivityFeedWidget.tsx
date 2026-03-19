import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { Dumbbell, FlaskConical, StickyNote, LinkIcon, Activity } from 'lucide-react'
import { useAuth } from '../../auth'
import Widget from '../Widget'

interface ActivityItem {
  type: string
  title: string
  timestamp: string
  link?: string
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

function timeAgo(dateStr: string): string {
  const now = new Date()
  const date = new Date(dateStr)
  const diffMs = now.getTime() - date.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHours = Math.floor(diffMin / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  const diffDays = Math.floor(diffHours / 24)
  if (diffDays === 1) return 'yesterday'
  if (diffDays < 7) return `${diffDays}d ago`
  if (diffDays < 30) return `${Math.floor(diffDays / 7)}w ago`
  return new Date(dateStr).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

export default function ActivityFeedWidget() {
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
    <Widget title="Recent Activity">
      <div className="space-y-3">
        {items.slice(0, 7).map((item, i) => {
          const Icon = typeIcons[item.type] || Activity
          const color = typeColors[item.type] || 'text-gray-400'
          const content = (
            <div className="flex items-start gap-2.5">
              <Icon size={14} className={`${color} mt-0.5 shrink-0`} />
              <div className="flex-1 min-w-0">
                <p className="text-sm text-gray-300 truncate">{item.title}</p>
                <p className="text-xs text-gray-600">{timeAgo(item.timestamp)}</p>
              </div>
            </div>
          )

          if (item.link) {
            return (
              <Link key={i} to={item.link} className="block hover:bg-gray-700/30 -mx-1 px-1 rounded transition-colors">
                {content}
              </Link>
            )
          }
          return <div key={i}>{content}</div>
        })}
      </div>
    </Widget>
  )
}
