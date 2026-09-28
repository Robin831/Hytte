import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Eye, Radio } from 'lucide-react'
import { listSessions, type LiveSession } from './live/liveApi'

const POLL_MS = 10_000

export default function LivePage() {
  const { t, i18n } = useTranslation('livestream')
  const [sessions, setSessions] = useState<LiveSession[]>([])
  const [configured, setConfigured] = useState(true)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const data = await listSessions()
        if (cancelled) return
        setSessions(data.sessions)
        setConfigured(data.configured)
        setError(null)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    const id = setInterval(load, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  const timeFmt = new Intl.DateTimeFormat(i18n.language, { hour: '2-digit', minute: '2-digit' })

  return (
    <div className="p-4 md:p-6 max-w-3xl mx-auto space-y-4">
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-xl font-semibold text-white">{t('title')}</h1>
        {configured && (
          <Link
            to="/live/broadcast"
            className="flex items-center gap-1.5 rounded-lg bg-red-600 px-3 py-2 text-sm font-medium text-white hover:bg-red-500"
          >
            <Radio size={16} />
            {t('list.goLive')}
          </Link>
        )}
      </div>

      {!configured && <div className="text-sm text-yellow-400" role="status">{t('list.notConfigured')}</div>}
      {error && <div className="text-sm text-red-400" role="alert">{t('list.loadError')}: {error}</div>}

      {loading ? (
        <div className="text-gray-400" role="status" aria-busy="true">{t('title')}…</div>
      ) : sessions.length === 0 ? (
        <div className="rounded-xl border border-gray-800 bg-gray-900 p-6 text-center text-gray-400">{t('list.empty')}</div>
      ) : (
        <ul className="space-y-2">
          {sessions.map(s => (
            <li key={s.id}>
              <Link
                to={`/live/${s.id}`}
                className="flex items-center gap-3 rounded-xl border border-gray-800 bg-gray-900 p-4 hover:border-gray-600"
              >
                <span
                  className={`shrink-0 rounded px-2 py-1 text-xs font-bold text-white ${s.on_air ? 'bg-red-600' : 'bg-gray-600'}`}
                >
                  {s.on_air ? t('list.onAir') : t('list.connecting')}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium text-white">{s.title || t('list.untitled')}</div>
                  <div className="truncate text-sm text-gray-400">
                    {s.is_owner ? t('list.you') : s.owner_name} ·{' '}
                    {t('list.startedAt', { time: timeFmt.format(new Date(s.started_at)) })}
                  </div>
                </div>
                <span className="flex shrink-0 items-center gap-1 text-sm text-gray-400" aria-label={t('list.viewers', { count: s.viewers })}>
                  <Eye size={16} />
                  {s.viewers}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
