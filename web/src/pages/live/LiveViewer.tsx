import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { PlayCircle } from 'lucide-react'
import LivePlayer from './LivePlayer'
import RunStatsBar from './RunStatsBar'
import { computeRunStats, getTrack, type SessionDetail, type TrackPoint, type ViewerEndpoints } from './liveApi'

// Leaflet is only needed when the broadcaster shares their location.
const LiveMap = lazy(() => import('./LiveMap'))

const TRACK_POLL_MS = 5_000

interface LiveViewerProps {
  endpoints: ViewerEndpoints
  /** Share-link visitors cannot open member-only pages such as the replay. */
  isPublic?: boolean
  onDetail?: (detail: SessionDetail) => void
}

/** LiveViewer is the video plus, when shared, the live route map and run stats. */
export default function LiveViewer({ endpoints, isPublic = false, onDetail }: LiveViewerProps) {
  const { t } = useTranslation('livestream')
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const [points, setPoints] = useState<TrackPoint[]>([])
  const lastIdRef = useRef(0)

  const handleDetail = useCallback((d: SessionDetail) => {
    setDetail(d)
    onDetail?.(d)
  }, [onDetail])

  const session = detail?.session
  const locationOn = !!session?.location
  const live = session?.status === 'live'

  useEffect(() => {
    if (!locationOn) return
    let cancelled = false
    const load = async () => {
      try {
        const fresh = await getTrack(endpoints.track, lastIdRef.current)
        if (cancelled || fresh.length === 0) return
        lastIdRef.current = fresh[fresh.length - 1].id ?? lastIdRef.current
        setPoints(prev => [...prev, ...fresh])
      } catch {
        // Try again on the next tick.
      }
    }
    load()
    if (!live) return () => { cancelled = true }
    const id = setInterval(load, TRACK_POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [endpoints.track, locationOn, live])

  const stats = computeRunStats(points)

  return (
    <div className="space-y-3">
      <LivePlayer endpoints={endpoints} onDetail={handleDetail} />

      {session?.status === 'ended' && !isPublic && detail?.recording_id && (
        <Link
          to={`/live/replay/${detail.recording_id}`}
          className="flex items-center gap-2 rounded-xl border border-gray-800 bg-gray-900 p-3 text-sm text-blue-300 hover:border-gray-600"
        >
          <PlayCircle size={18} />
          {detail.recording_status === 'ready' ? t('watch.watchReplay') : t('watch.replayProcessing')}
        </Link>
      )}

      {locationOn && (
        <>
          <RunStatsBar stats={stats} />
          {points.length > 0 ? (
            <Suspense fallback={<div className="h-72 rounded-xl bg-gray-800" />}>
              <LiveMap points={points} follow={live} className="h-72 md:h-96" ariaLabel={t('map.label')} />
            </Suspense>
          ) : (
            <div className="rounded-xl border border-gray-800 bg-gray-900 p-4 text-sm text-gray-400">{t('map.waiting')}</div>
          )}
        </>
      )}
    </div>
  )
}
