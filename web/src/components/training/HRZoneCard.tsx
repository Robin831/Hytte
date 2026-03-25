import { useTranslation } from 'react-i18next'
import type { ZoneDistribution } from '../../types/training'
import { formatNumber } from '../../utils/formatDate'

const zoneColors = ['#22c55e', '#84cc16', '#eab308', '#f97316', '#ef4444']

interface Props {
  zones: ZoneDistribution[]
  thresholdContext?: string | null
  hrDrift?: number | null
}

export default function HRZoneCard({ zones, thresholdContext, hrDrift }: Props) {
  const { t } = useTranslation('training')

  if (zones.length === 0) return null

  const driftLevel =
    hrDrift === null || hrDrift === undefined
      ? null
      : hrDrift < 5
        ? 'low'
        : hrDrift < 10
          ? 'moderate'
          : 'high'

  return (
    <div className="bg-gray-800 rounded-xl p-6 mb-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold">{t('detail.zones.title')}</h2>
        {hrDrift !== null && hrDrift !== undefined && (
          <span
            className={`text-xs px-2.5 py-1 rounded-full font-medium ${
              driftLevel === 'low'
                ? 'bg-green-500/15 text-green-400'
                : driftLevel === 'moderate'
                  ? 'bg-yellow-500/15 text-yellow-400'
                  : 'bg-red-500/15 text-red-400'
            }`}
          >
            {t(`detail.zones.drift.${driftLevel as 'low' | 'moderate' | 'high'}`, {
              value: formatNumber(hrDrift, { minimumFractionDigits: 1, maximumFractionDigits: 1 }),
            })}
          </span>
        )}
      </div>

      {/* Single stacked horizontal bar */}
      {(() => {
        const nonZeroZones = zones.filter(z => z.percentage > 0)
        const hasNonZeroZones = nonZeroZones.length > 0

        const ariaLabel = hasNonZeroZones
          ? nonZeroZones
              .map(z =>
                t('detail.zones.zoneLabel', {
                  zone: z.zone,
                  name: z.name,
                  pct: z.percentage.toFixed(1),
                }),
              )
              .join(', ')
          : t('detail.zones.noZoneData')

        return (
          <div className="flex h-7 rounded-lg overflow-hidden mb-5" role="img" aria-label={ariaLabel}>
            {hasNonZeroZones ? (
              zones.map((z, i) =>
                z.percentage > 0 ? (
                  <div
                    key={z.zone}
                    style={{ width: `${z.percentage}%`, backgroundColor: zoneColors[i] ?? '#6b7280' }}
                    title={t('detail.zones.zoneLabel', {
                      zone: z.zone,
                      name: z.name,
                      pct: z.percentage.toFixed(1),
                    })}
                  />
                ) : null,
              )
            ) : (
              <div className="flex-1 bg-gray-700" aria-hidden="true" />
            )}
          </div>
        )
      })()}

      {/* Per-zone legend rows */}
      <div className="space-y-2">
        {zones.map((z, i) => {
          const isFirstZone = i === 0
          const isLastZone = i === zones.length - 1
          const bpmRange = isLastZone
            ? `>${z.min_hr}`
            : isFirstZone
              ? `<${z.max_hr}`
              : `${z.min_hr}–<${z.max_hr}`
          const totalSecs = Math.round(z.duration_seconds ?? 0)
          const mins = Math.floor(totalSecs / 60)
          const secs = totalSecs % 60
          const timeStr = t('detail.zones.zoneTime', { min: mins, sec: String(secs).padStart(2, '0') })
          return (
            <div key={z.zone} className="flex items-center gap-3">
              <div
                className="w-3 h-3 rounded-sm shrink-0"
                style={{ backgroundColor: zoneColors[i] ?? '#6b7280' }}
              />
              <span className="text-xs text-gray-400 w-24 shrink-0">
                Z{z.zone} {z.name}
              </span>
              <span className="text-xs text-gray-500 w-20 shrink-0 tabular-nums">
                {bpmRange} {t('units.bpm')}
              </span>
              <div className="flex-1 bg-gray-700 rounded-full h-2 overflow-hidden">
                <div
                  className="h-full rounded-full"
                  style={{
                    width: `${Math.max(z.percentage, 1)}%`,
                    backgroundColor: zoneColors[i] ?? '#6b7280',
                  }}
                />
              </div>
              <span className="text-xs text-gray-400 w-12 text-right shrink-0">
                {z.percentage.toFixed(1)}%
              </span>
              <span className="text-xs text-gray-500 w-16 text-right shrink-0 tabular-nums">
                {timeStr}
              </span>
            </div>
          )
        })}
      </div>

      {thresholdContext && (
        <p className="mt-4 text-xs text-gray-500 leading-relaxed">{thresholdContext}</p>
      )}
    </div>
  )
}
