import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Trophy } from 'lucide-react'
import { formatNumber } from '../utils/formatDate'

interface BeatParentStatus {
  child_distance_raw: number
  child_distance_scaled: number
  parent_distance: number
  is_beating_parent: boolean
}

const KM_FORMAT: Intl.NumberFormatOptions = {
  style: 'unit',
  unit: 'kilometer',
  maximumFractionDigits: 1,
  minimumFractionDigits: 1,
}

const KM_NUM_FORMAT: Intl.NumberFormatOptions = {
  maximumFractionDigits: 1,
  minimumFractionDigits: 1,
}

function fmtKm(meters: number): string {
  return formatNumber(meters / 1000, KM_FORMAT)
}

function fmtKmNum(meters: number): string {
  return formatNumber(meters / 1000, KM_NUM_FORMAT)
}

export default function BeatMyParent() {
  const { t } = useTranslation('common')
  const [status, setStatus] = useState<BeatParentStatus | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const controller = new AbortController()
    let isMounted = true

    fetch('/api/stars/beat-parent', {
      credentials: 'include',
      signal: controller.signal,
    })
      .then(res => (res.ok ? res.json() : null))
      .then(data => {
        if (!isMounted) return
        setStatus(data)
      })
      .catch(error => {
        if (error?.name === 'AbortError' || !isMounted) return
        setStatus(null)
      })
      .finally(() => {
        if (!isMounted) return
        setLoading(false)
      })

    return () => {
      isMounted = false
      controller.abort()
    }
  }, [])

  if (loading) {
    return <div className="h-32 rounded-xl bg-gray-800 animate-pulse" />
  }

  if (!status) {
    return null
  }

  const maxDist = Math.max(status.child_distance_scaled, status.parent_distance, 1)
  const childBarPct = Math.min((status.child_distance_scaled / maxDist) * 100, 100)
  const parentBarPct = Math.min((status.parent_distance / maxDist) * 100, 100)

  return (
    <div className="bg-gray-800/60 rounded-xl border border-gray-700 p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide">
          {t('stars.beatParent.title')}
        </h2>
        {status.is_beating_parent && (
          <div className="flex items-center gap-1.5 bg-yellow-500/10 border border-yellow-500/30 rounded-lg px-3 py-1">
            <Trophy size={14} className="text-yellow-400" />
            <span className="text-yellow-300 text-xs font-semibold">
              {t('stars.beatParent.champion')}
            </span>
          </div>
        )}
      </div>

      <p
        className={`text-center text-lg font-bold mb-5 ${
          status.is_beating_parent ? 'text-green-400' : 'text-blue-300'
        }`}
      >
        {status.is_beating_parent
          ? t('stars.beatParent.winning')
          : t('stars.beatParent.keepGoing')}
      </p>

      <div className="grid grid-cols-2 gap-6">
        {/* Child bar (scaled) */}
        <div>
          <div className="flex justify-between text-xs mb-1.5">
            <span className="text-white font-medium">{t('stars.beatParent.you')}</span>
            <span className="text-gray-300">
              {fmtKm(status.child_distance_scaled)}
              {status.child_distance_raw !== status.child_distance_scaled && (
                <span className="text-gray-500 ml-1">
                  ({t('stars.beatParent.rawKm', { km: fmtKmNum(status.child_distance_raw) })})
                </span>
              )}
            </span>
          </div>
          <div
            className="h-4 bg-gray-700 rounded-full overflow-hidden"
            role="progressbar"
            aria-valuenow={Math.round(childBarPct)}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={t('stars.beatParent.you')}
          >
            <div
              className={`h-full rounded-full transition-all duration-700 ${
                status.is_beating_parent ? 'bg-green-500' : 'bg-blue-500'
              }`}
              style={{ width: `${childBarPct}%` }}
            />
          </div>
        </div>

        {/* Parent bar (raw) */}
        <div>
          <div className="flex justify-between text-xs mb-1.5">
            <span className="text-gray-300 font-medium">{t('stars.beatParent.parent')}</span>
            <span className="text-gray-300">{fmtKm(status.parent_distance)}</span>
          </div>
          <div
            className="h-4 bg-gray-700 rounded-full overflow-hidden"
            role="progressbar"
            aria-valuenow={Math.round(parentBarPct)}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={t('stars.beatParent.parent')}
          >
            <div
              className="h-full rounded-full bg-orange-500 transition-all duration-700"
              style={{ width: `${parentBarPct}%` }}
            />
          </div>
        </div>
      </div>
    </div>
  )
}
