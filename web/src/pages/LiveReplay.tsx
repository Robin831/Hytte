import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router'
import { ArrowLeft, Dumbbell, Trash2 } from 'lucide-react'
import RunStatsBar from './live/RunStatsBar'
import {
  computeRunStats,
  deleteRecording,
  formatBytes,
  formatDuration,
  formatKm,
  getRecording,
  getTrack,
  type Recording,
  type TrackPoint,
} from './live/liveApi'

const LiveMap = lazy(() => import('./live/LiveMap'))

const PROCESSING_POLL_MS = 5_000

export default function LiveReplay() {
  const { t, i18n } = useTranslation('livestream')
  const { id } = useParams()
  const recordingId = Number(id)
  const navigate = useNavigate()
  const videoRef = useRef<HTMLVideoElement>(null)

  const [recording, setRecording] = useState<Recording | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [points, setPoints] = useState<TrackPoint[]>([])
  const [videoTime, setVideoTime] = useState(0)
  const [deleting, setDeleting] = useState(false)

  const status = recording?.status
  useEffect(() => {
    if (!Number.isInteger(recordingId) || recordingId <= 0) return
    let cancelled = false
    const load = () =>
      getRecording(recordingId)
        .then(r => { if (!cancelled) setRecording(r) })
        .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : String(err)) })
    load()
    // Keep polling while the replay is still being built.
    if (status && status !== 'recording' && status !== 'processing') return () => { cancelled = true }
    const pollId = setInterval(load, PROCESSING_POLL_MS)
    return () => {
      cancelled = true
      clearInterval(pollId)
    }
  }, [recordingId, status])

  const hasTrack = !!recording?.has_track
  useEffect(() => {
    if (!hasTrack) return
    let cancelled = false
    getTrack(`/api/live/recordings/${recordingId}/track`)
      .then(p => { if (!cancelled) setPoints(p) })
      .catch(() => {})
    return () => { cancelled = true }
  }, [recordingId, hasTrack])

  // Map the video position to a GPS fix. The replay starts when the phone
  // went on air, which is close to the first fix; good enough for a marker.
  const marker = useMemo(() => {
    if (points.length === 0) return null
    const start = Date.parse(points[0].t)
    const target = start + videoTime * 1000
    let best = points[0]
    for (const p of points) {
      if (Date.parse(p.t) <= target) best = p
      else break
    }
    return { lat: best.lat, lon: best.lon }
  }, [points, videoTime])

  const stats = useMemo(() => computeRunStats(points), [points])

  const remove = async () => {
    if (!recording || !window.confirm(t('replay.confirmDelete'))) return
    setDeleting(true)
    try {
      await deleteRecording(recording.id)
      navigate('/live')
    } catch (err) {
      setDeleting(false)
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const dateFmt = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-4">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold text-white">{recording?.title || t('list.untitled')}</h1>
          {recording && (
            <div className="truncate text-sm text-gray-400">
              {recording.owner_name} · {dateFmt.format(new Date(recording.started_at))}
            </div>
          )}
        </div>
        <Link to="/live" className="flex shrink-0 items-center gap-1.5 text-sm text-gray-400 hover:text-white">
          <ArrowLeft size={16} />
          {t('watch.back')}
        </Link>
      </div>

      {error && <div className="text-sm text-red-400" role="alert">{error}</div>}

      {recording?.status === 'ready' ? (
        <video
          ref={videoRef}
          src={`/api/live/recordings/${recording.id}/video`}
          controls
          playsInline
          preload="metadata"
          onTimeUpdate={e => setVideoTime(e.currentTarget.currentTime)}
          className="w-full aspect-video max-h-[80vh] rounded-xl bg-black object-contain"
        />
      ) : recording ? (
        <div className="rounded-xl bg-black p-8 text-center text-gray-200" role="status">
          {recording.status === 'failed' ? t('replay.failed', { error: recording.error || '' }) : t('replay.processing')}
        </div>
      ) : (
        !error && <div className="text-gray-400" role="status" aria-busy="true">{t('title')}…</div>
      )}

      {recording && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-gray-400">
          {recording.duration_seconds > 0 && <span>{formatDuration(recording.duration_seconds)}</span>}
          {recording.size_bytes > 0 && <span>{formatBytes(recording.size_bytes, i18n.language)}</span>}
          {recording.workout && (
            recording.workout.linkable ? (
              <Link to={`/training/${recording.workout.id}`} className="flex items-center gap-1.5 text-blue-300 hover:text-blue-200">
                <Dumbbell size={16} />
                {recording.workout.title} · {formatKm(recording.workout.distance_meters, i18n.language)} km
              </Link>
            ) : (
              <span className="flex items-center gap-1.5">
                <Dumbbell size={16} />
                {recording.workout.title} · {formatKm(recording.workout.distance_meters, i18n.language)} km
              </span>
            )
          )}
          {recording.is_owner && recording.status !== 'recording' && recording.status !== 'processing' && (
            <button
              type="button"
              onClick={remove}
              disabled={deleting}
              className="ml-auto flex items-center gap-1.5 rounded-lg bg-gray-800 px-3 py-2 text-red-300 hover:bg-gray-700 disabled:opacity-50"
            >
              <Trash2 size={16} />
              {t('replay.delete')}
            </button>
          )}
        </div>
      )}

      {points.length > 0 && (
        <>
          <RunStatsBar stats={stats} />
          <Suspense fallback={<div className="h-72 rounded-xl bg-gray-800" />}>
            <LiveMap points={points} marker={marker} className="h-72 md:h-96" ariaLabel={t('map.label')} />
          </Suspense>
        </>
      )}
    </div>
  )
}
