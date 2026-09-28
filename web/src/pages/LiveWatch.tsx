import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router'
import { ArrowLeft } from 'lucide-react'
import LiveViewer from './live/LiveViewer'
import { memberEndpoints, type SessionDetail } from './live/liveApi'

export default function LiveWatch() {
  const { t } = useTranslation('livestream')
  const { id } = useParams()
  const sessionId = Number(id)
  const validId = Number.isInteger(sessionId) && sessionId > 0
  const endpoints = useMemo(() => memberEndpoints(sessionId), [sessionId])
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const session = detail?.session

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
        <LiveViewer key={sessionId} endpoints={endpoints} onDetail={setDetail} />
      ) : (
        <div className="rounded-xl bg-black p-8 text-center text-gray-200" role="status">{t('watch.notFound')}</div>
      )}
    </div>
  )
}
