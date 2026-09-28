import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router'
import { Radio } from 'lucide-react'
import LiveViewer from './live/LiveViewer'
import { publicEndpoints, type SessionDetail } from './live/liveApi'

// Share-link page for people without a Hytte account. Rendered outside the
// app layout and auth: the token in the URL is the only credential, and it
// stops working when the broadcast ends or the owner revokes it.
export default function PublicWatch() {
  const { t } = useTranslation('livestream')
  const { token = '' } = useParams()
  const endpoints = useMemo(() => publicEndpoints(token), [token])
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const session = detail?.session

  return (
    <div className="min-h-screen bg-gray-950 text-white">
      <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-4">
        <div className="flex items-center gap-2">
          <Radio size={22} className="shrink-0 text-red-500" />
          <div className="min-w-0">
            <h1 className="truncate text-xl font-semibold">
              {session
                ? session.title || t('public.liveFrom', { name: session.owner_name })
                : detail?.owner_name || t('title')}
            </h1>
            {session?.title && <div className="truncate text-sm text-gray-400">{session.owner_name}</div>}
          </div>
        </div>
        <LiveViewer key={token} endpoints={endpoints} isPublic onDetail={setDetail} />
        <p className="text-xs text-gray-500">{t('public.footer')}</p>
      </div>
    </div>
  )
}
