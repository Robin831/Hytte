import { useTranslation } from 'react-i18next'
import { Sparkles, TrendingDown, TrendingUp } from 'lucide-react'
import type { RacePrediction, RacePredictions } from '../../types/training'
import { formatDate } from '../../utils/formatDate'

interface RacePredictionsCardProps {
  data: RacePredictions
}

// deltaSeconds returns the change vs the previous snapshot for a distance:
// negative = faster (improvement). Null when there is nothing to compare.
function deltaSeconds(p: RacePrediction, previous?: RacePrediction[]): number | null {
  if (!previous || p.time_seconds == null) return null
  const prev = previous.find((q) => q.distance === p.distance)
  if (!prev || prev.time_seconds == null) return null
  const d = p.time_seconds - prev.time_seconds
  return d === 0 ? null : d
}

function formatDelta(seconds: number): string {
  const abs = Math.abs(seconds)
  const m = Math.floor(abs / 60)
  const s = abs % 60
  const body = m > 0 ? `${m}:${String(s).padStart(2, '0')}` : `${s}s`
  return `${seconds < 0 ? '−' : '+'}${body}`
}

const CONFIDENCE_CLASSES: Record<string, string> = {
  high: 'bg-green-500/10 text-green-400',
  medium: 'bg-yellow-500/10 text-yellow-400',
  low: 'bg-gray-600/30 text-gray-400',
}

// RacePredictionsCard renders the stored weekly prediction snapshot: per-
// distance times with confidence and the change since the previous snapshot,
// plus the coach's rationale for how the estimate was set.
export default function RacePredictionsCard({ data }: RacePredictionsCardProps) {
  const { t } = useTranslation('training')

  return (
    <div className="bg-gray-800 rounded-xl p-5 mb-6">
      <div className="flex items-center justify-between gap-2 mb-1">
        <h2 className="text-sm font-semibold text-gray-400">
          {t('trends.racePredictions.title')}
        </h2>
        {data.method === 'ai' && (
          <span className="inline-flex items-center gap-1 rounded-full bg-purple-500/10 px-2 py-0.5 text-[10px] font-medium text-purple-300">
            <Sparkles size={10} aria-hidden />
            {t('trends.racePredictions.methodAi')}
          </span>
        )}
      </div>
      {data.as_of && (
        <p className="text-xs text-gray-500 mb-3">
          {t('trends.racePredictions.asOf', {
            date: formatDate(data.as_of, { year: 'numeric', month: 'short', day: 'numeric' }),
          })}
        </p>
      )}
      {!data.predictions || data.predictions.length === 0 ? (
        <>
          <p className="text-gray-400 text-sm mt-2">{data.message ?? t('trends.racePredictions.noData')}</p>
          <p className="text-gray-500 text-xs mt-1">{t('trends.racePredictions.noDataHint')}</p>
        </>
      ) : (
        <>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-gray-400 text-xs border-b border-gray-700">
                  <th className="text-left py-2 pr-4">{t('trends.racePredictions.distance')}</th>
                  <th className="text-right py-2 pr-4">{t('trends.racePredictions.time')}</th>
                  <th className="text-right py-2 pr-4 hidden sm:table-cell">{t('trends.racePredictions.pace')}</th>
                  <th className="text-right py-2">{t('trends.racePredictions.change')}</th>
                </tr>
              </thead>
              <tbody>
                {data.predictions.map((p) => {
                  const delta = deltaSeconds(p, data.previous)
                  return (
                    <tr key={p.distance} className="border-b border-gray-700/50">
                      <td className="py-2 pr-4 font-medium">
                        <span className="flex items-center gap-2">
                          {p.distance}
                          {p.confidence && (
                            <span
                              className={`hidden sm:inline-flex rounded-full px-1.5 py-0.5 text-[10px] ${CONFIDENCE_CLASSES[p.confidence] ?? CONFIDENCE_CLASSES.low}`}
                              title={t('trends.racePredictions.confidenceLabel')}
                            >
                              {t(`trends.racePredictions.confidence.${p.confidence}`, { defaultValue: p.confidence })}
                            </span>
                          )}
                        </span>
                      </td>
                      <td className="py-2 pr-4 text-right text-green-400 font-mono">{p.predicted_time}</td>
                      <td className="py-2 pr-4 text-right text-gray-300 font-mono hidden sm:table-cell">
                        {p.pace_per_km}{t('units.pace')}
                      </td>
                      <td className="py-2 text-right font-mono text-xs">
                        {delta === null ? (
                          <span className="text-gray-600">–</span>
                        ) : delta < 0 ? (
                          <span className="inline-flex items-center gap-1 text-green-400">
                            <TrendingDown size={12} aria-hidden />
                            {formatDelta(delta)}
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 text-red-400">
                            <TrendingUp size={12} aria-hidden />
                            {formatDelta(delta)}
                          </span>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          {data.rationale && (
            <p className="text-xs text-gray-400 mt-3 leading-relaxed">{data.rationale}</p>
          )}
        </>
      )}
    </div>
  )
}
