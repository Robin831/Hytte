import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Eye, PlayCircle, Radio } from 'lucide-react'
import { formatBytes, formatDuration, listRecordings, listSessions, type LiveSession, type RecordingList } from './live/liveApi'

const POLL_MS = 10_000

export default function LivePage() {
  const { t, i18n } = useTranslation('livestream')
  const [sessions, setSessions] = useState<LiveSession[]>([])
  const [configured, setConfigured] = useState(true)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [recordings, setRecordings] = useState<RecordingList | null>(null)

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
    const loadRecordings = async () => {
      try {
        const data = await listRecordings()
        if (!cancelled) setRecordings(data)
      } catch {
        // Replays are secondary; the live list still works without them.
      }
    }
    load()
    loadRecordings()
    const id = setInterval(() => {
      load()
      loadRecordings()
    }, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  const timeFmt = new Intl.DateTimeFormat(i18n.language, { hour: '2-digit', minute: '2-digit' })
  const dateFmt = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })
  // A recording still in progress is the live stream above, not a replay yet.
  const replays = (recordings?.recordings ?? []).filter(r => r.status !== 'recording')

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

      {replays.length > 0 && recordings && (
        <section className="space-y-2 pt-2">
          <div className="flex items-baseline justify-between gap-2">
            <h2 className="text-lg font-semibold text-white">{t('replays.title')}</h2>
            <span className="text-xs text-gray-500">
              {t('replays.storage', {
                used: formatBytes(recordings.used_bytes, i18n.language),
                free: recordings.free_bytes !== undefined ? formatBytes(recordings.free_bytes, i18n.language) : '–',
              })}
            </span>
          </div>
          <ul className="space-y-2">
            {replays.map(r => (
              <li key={r.id}>
                <Link
                  to={`/live/replay/${r.id}`}
                  className="flex items-center gap-3 rounded-xl border border-gray-800 bg-gray-900 p-4 hover:border-gray-600"
                >
                  <PlayCircle size={22} className={r.status === 'ready' ? 'shrink-0 text-blue-400' : 'shrink-0 text-gray-600'} />
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium text-white">{r.title || t('list.untitled')}</div>
                    <div className="truncate text-sm text-gray-400">
                      {r.is_owner ? t('list.you') : r.owner_name} · {dateFmt.format(new Date(r.started_at))}
                      {r.status === 'ready' && r.duration_seconds > 0 && ` · ${formatDuration(r.duration_seconds)}`}
                    </div>
                  </div>
                  <span className="shrink-0 text-xs text-gray-500">
                    {r.status === 'ready' ? formatBytes(r.size_bytes, i18n.language) : t(`replays.status.${r.status}`)}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
