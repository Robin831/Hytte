import { useTranslation } from 'react-i18next'
import type { VacationResponse } from './types'

interface VacationCardProps {
  vacation: VacationResponse
  formatCurrency: (amount: number) => string
}

/** Vacation tracker card: days used/remaining plus accrued feriepenger. */
export default function VacationCard({ vacation, formatCurrency }: VacationCardProps) {
  const { t } = useTranslation('salary')

  return (
    <div className="bg-gray-800 rounded-xl p-5 space-y-3">
      <h2 className="text-base font-medium text-white">{t('vacation.title')}</h2>
      <div className="space-y-1">
        <div className="flex justify-between text-sm">
          <span className="text-gray-400">
            {t('vacation.used', {
              used: vacation.days_used,
              allowance: vacation.days_allowance,
            })}
          </span>
          <span className="text-gray-300">
            {t('vacation.remaining', { remaining: vacation.days_remaining })}
          </span>
        </div>
        <div className="w-full bg-gray-700 rounded-full h-2">
          <div
            className="bg-emerald-500 h-2 rounded-full transition-all"
            style={{
              width: `${Math.min((vacation.days_used / vacation.days_allowance) * 100, 100)}%`,
            }}
          />
        </div>
      </div>
      {vacation.feriepenger_accrued > 0 && (
        <div className="text-sm text-gray-400">
          <span className="text-gray-300 font-medium">{t('vacation.feriepenger')}: </span>
          {t('vacation.feriepengerAccrued', {
            amount: formatCurrency(vacation.feriepenger_accrued),
            pct: vacation.feriepenger_pct.toFixed(1),
          })}
        </div>
      )}
    </div>
  )
}
