import { useTranslation } from 'react-i18next'
import type { VacationResponse } from './types'

interface VacationCardProps {
  vacation: VacationResponse
  formatCurrency: (amount: number) => string
}

function computeProjectedFullYearFeriepenger(
  grossYtd: number,
  feriepengerPct: number,
  selectedYear: number,
  now: Date,
): number {
  if (grossYtd <= 0) return 0

  let fractionElapsed: number
  if (selectedYear < now.getFullYear()) {
    fractionElapsed = 1
  } else if (selectedYear > now.getFullYear()) {
    fractionElapsed = 0
  } else {
    const startOfYear = new Date(selectedYear, 0, 1).getTime()
    const startOfNextYear = new Date(selectedYear + 1, 0, 1).getTime()
    fractionElapsed = (now.getTime() - startOfYear) / (startOfNextYear - startOfYear)
  }
  if (fractionElapsed <= 0) return 0

  const projectedGross = grossYtd / fractionElapsed
  return projectedGross * (feriepengerPct / 100)
}

export default function VacationCard({ vacation, formatCurrency }: VacationCardProps) {
  const { t } = useTranslation('salary')

  const projected = computeProjectedFullYearFeriepenger(
    vacation.gross_ytd,
    vacation.feriepenger_pct,
    vacation.year,
    new Date(),
  )
  const feriepengerFill =
    projected > 0
      ? Math.min(Math.max((vacation.feriepenger_accrued / projected) * 100, 0), 100)
      : 0

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
              width: `${vacation.days_allowance > 0 ? Math.min((vacation.days_used / vacation.days_allowance) * 100, 100) : 0}%`,
            }}
          />
        </div>
      </div>
      {vacation.feriepenger_accrued > 0 && (
        <div className="space-y-1">
          <div className="text-sm text-gray-400">
            <span className="text-gray-300 font-medium">{t('vacation.feriepenger')}: </span>
            {t('vacation.feriepengerAccrued', {
              amount: formatCurrency(vacation.feriepenger_accrued),
              pct: vacation.feriepenger_pct.toFixed(1),
            })}
          </div>
          {projected > 0 && (
            <>
              <div className="flex justify-between text-sm">
                <span className="text-gray-400">{t('vacation.feriepengerProgress')}</span>
                <span className="text-gray-300">
                  {t('vacation.feriepengerProjected', {
                    amount: formatCurrency(projected),
                  })}
                </span>
              </div>
              <div
                className="w-full bg-gray-700 rounded-full h-2"
                role="progressbar"
                aria-label={t('vacation.feriepengerProgressAria')}
                aria-valuenow={Math.round(feriepengerFill)}
                aria-valuemin={0}
                aria-valuemax={100}
              >
                <div
                  className="bg-amber-500 h-2 rounded-full transition-all"
                  style={{ width: `${feriepengerFill}%` }}
                />
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
