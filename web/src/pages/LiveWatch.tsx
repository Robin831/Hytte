import { useCallback, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router'
import { ArrowLeft } from 'lucide-react'
import LiveViewer from './live/LiveViewer'
import { listSessions, memberEndpoints, type SessionDetail } from './live/liveApi'

export default function LiveWatch() {
  const { t } = useTranslation('livestream')
  const { id } = useParams()
  const sessionId = Number(id)
  const validId = Number.isInteger(sessionId) && sessionId > 0
  const endpoints = useMemo(() => memberEndpoints(sessionId), [sessionId])
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const session = detail?.session
  const navigate = useNavigate()
  const lookingRef = useRef(false)

  // If the broadcaster had to start a new broadcast (phone was away too long,
  // or they went live again), follow them to it instead of stopping here.
  const handleDetail = useCallback((d: SessionDetail) => {
    setDetail(d)
    const s = d.session
    if (!s || s.status !== 'ended' || lookingRef.current) return
    lookingRef.current = true
    listSessions()
      .then(list => {
        const next = list.sessions.find(x => x.user_id === s.user_id && x.id !== s.id && x.status === 'live')
        if (next) navigate(`/live/${next.id}`, { replace: true })
      })
      .catch(() => {})
      .finally(() => { lookingRef.current = false })
  }, [navigate])

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-4">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold text-white">{session?.title || t('list.untitled')}</h1>
          {session && <div className="truncate text-sm text-gray-400">{session.owner_name}</div>}
        </div>
        <Link to="/live" className="flex shrink-0 items-center gap-1.5 text-sm text-gray-400 hover:text-white">
          <ArrowLeft size={16} />
          {t('watch.back')}
        </Link>
      </div>
      {validId ? (
        <LiveViewer key={sessionId} endpoints={endpoints} onDetail={handleDetail} />
      ) : (
        <div className="rounded-xl bg-black p-8 text-center text-gray-200" role="status">{t('watch.notFound')}</div>
      )}
    </div>
  )
}
