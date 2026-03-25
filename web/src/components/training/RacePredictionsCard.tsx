import { useTranslation } from 'react-i18next'
import type { RacePredictions } from '../../types/training'

interface RacePredictionsCardProps {
  data: RacePredictions
}

export default function RacePredictionsCard({ data }: RacePredictionsCardProps) {
  const { t } = useTranslation('training')

  if (!data.predictions || data.predictions.length === 0) {
    return null
  }

  return (
    <div className="bg-gray-800 rounded-xl p-5 mb-6">
      <h2 className="text-sm font-semibold text-gray-400 mb-1">
        {t('trends.racePredictions.title')}
      </h2>
      {data.ref_workout_id != null && (
        <p className="text-xs text-gray-500 mb-3">
          {t('trends.racePredictions.basis', { id: data.ref_workout_id, time: data.ref_time })}
        </p>
      )}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-gray-400 text-xs border-b border-gray-700">
              <th className="text-left py-2 pr-4">{t('trends.racePredictions.distance')}</th>
              <th className="text-right py-2 pr-4">{t('trends.racePredictions.time')}</th>
              <th className="text-right py-2">{t('trends.racePredictions.pace')}</th>
            </tr>
          </thead>
          <tbody>
            {data.predictions.map((p) => (
              <tr key={p.distance} className="border-b border-gray-700/50">
                <td className="py-2 pr-4 font-medium">{p.distance}</td>
                <td className="py-2 pr-4 text-right text-green-400 font-mono">{p.predicted_time}</td>
                <td className="py-2 text-right text-gray-300 font-mono">{p.pace_per_km}/km</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-gray-500 mt-3">{t('trends.racePredictions.formula')}</p>
    </div>
  )
}
