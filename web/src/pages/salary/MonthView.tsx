import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { formatMonthLabel, formatHours, formatCompact } from './types'
import type { CommissionTier } from './types'
import type { SalaryData } from './useSalaryData'
import VacationCard from './VacationCard'
import TrekktabellEditor from './TrekktabellEditor'
import AssignmentsList from './AssignmentsList'

interface MonthViewProps {
  salary: SalaryData
  selectedMonth: string
  currentMonthStr: string
  locale: string
  onChangeMonth: (delta: number) => void
}

// Calculate how far into a tier the current billable revenue is.
const getTierProgress = (tier: CommissionTier, billableRevenue: number): number => {
  if (billableRevenue <= tier.floor) return 0
  if (tier.ceiling === 0) return 100 // Unbounded — always full once reached
  const filled = Math.min(billableRevenue, tier.ceiling) - tier.floor
  const total = tier.ceiling - tier.floor
  return Math.min((filled / total) * 100, 100)
}

const getTierEarnings = (tier: CommissionTier, billableRevenue: number): number => {
  if (billableRevenue <= tier.floor) return 0
  const high = tier.ceiling === 0 ? billableRevenue : Math.min(billableRevenue, tier.ceiling)
  return (high - tier.floor) * tier.rate
}

const isTierActive = (tier: CommissionTier, billableRevenue: number): boolean =>
  billableRevenue > tier.floor && (tier.ceiling === 0 || billableRevenue <= tier.ceiling)

/**
 * Month tab: hero estimate card with prev/next navigation, manual override form,
 * commission tier progress, per-absence-day costs, and the vacation, trekktabell
 * and per-month assignment cards.
 */
export default function MonthView({ salary, selectedMonth, currentMonthStr, locale, onChangeMonth }: MonthViewProps) {
  const { t } = useTranslation('salary')
  const { estimate, vacation, formatCurrency, saveOverride } = salary

  // Manual override form state (for past estimate months).
  const [showOverride, setShowOverride] = useState(false)
  const [overrideBillableHours, setOverrideBillableHours] = useState('')
  const [overrideInternalHours, setOverrideInternalHours] = useState('')
  const [overrideVacationDays, setOverrideVacationDays] = useState('')
  const [overrideSickDays, setOverrideSickDays] = useState('')
  const [overrideGross, setOverrideGross] = useState('')
  const [overrideNet, setOverrideNet] = useState('')
  const [savingOverride, setSavingOverride] = useState(false)
  const [overrideError, setOverrideError] = useState<string | null>(null)

  const resetOverrideForm = useCallback(() => {
    setOverrideBillableHours('')
    setOverrideInternalHours('')
    setOverrideVacationDays('')
    setOverrideSickDays('')
    setOverrideGross('')
    setOverrideNet('')
    setOverrideError(null)
  }, [])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset form on month change
    setShowOverride(false)
    resetOverrideForm()
  }, [selectedMonth, resetOverrideForm])

  const handleSaveOverride = async () => {
    setSavingOverride(true)
    setOverrideError(null)

    const billableText = overrideBillableHours.trim()
    const internalText = overrideInternalHours.trim()
    const vacDaysText = overrideVacationDays.trim()
    const sickDaysText = overrideSickDays.trim()
    const grossText = overrideGross.trim()
    const netText = overrideNet.trim()

    if (!billableText || !grossText || !netText) {
      setOverrideError(t('override.saveError'))
      setSavingOverride(false)
      return
    }

    const parseNum = (s: string) => Number(s.replace(',', '.'))
    const billable = parseNum(billableText)
    const internal = internalText ? parseNum(internalText) : 0
    const vacDays = vacDaysText ? Number.parseInt(vacDaysText, 10) : 0
    const sickDays = sickDaysText ? Number.parseInt(sickDaysText, 10) : 0
    const gross = parseNum(grossText)
    const net = parseNum(netText)

    if ([billable, gross, net].some(value => Number.isNaN(value))) {
      setOverrideError(t('override.saveError'))
      setSavingOverride(false)
      return
    }

    const hoursWorked = billable + internal
    const tax = Math.max(0, gross - net)

    try {
      await saveOverride({
        hours_worked: hoursWorked,
        billable_hours: billable,
        internal_hours: internal,
        base_amount: gross,
        commission: 0,
        gross,
        tax,
        net,
        vacation_days: vacDays,
        sick_days: sickDays,
      })
    } catch (err) {
      setOverrideError(err instanceof Error ? err.message : t('override.saveError'))
      setSavingOverride(false)
      return
    }
    setSavingOverride(false)
    setShowOverride(false)
    resetOverrideForm()
  }

  if (!estimate) return null

  return (
    <>
      {/* Hero card — month estimate with prev/next navigation */}
      <div className="bg-gray-800 rounded-xl p-5 space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => onChangeMonth(-1)}
              className="p-1 rounded hover:bg-gray-700 text-gray-400 hover:text-white transition-colors"
              aria-label={t('month.prev')}
            >
              <ChevronLeft size={16} />
            </button>
            <h2 className="text-base font-medium text-white">
              {formatMonthLabel(estimate.month, locale)}
            </h2>
            <button
              type="button"
              onClick={() => onChangeMonth(1)}
              className="p-1 rounded hover:bg-gray-700 text-gray-400 hover:text-white transition-colors"
              aria-label={t('month.next')}
            >
              <ChevronRight size={16} />
            </button>
          </div>
          <span className={`text-xs px-2 py-0.5 rounded-full ${estimate.estimate.is_estimate ? 'bg-yellow-900/60 text-yellow-300' : 'bg-green-900/60 text-green-300'}`}>
            {estimate.estimate.is_estimate ? t('hero.estimate') : t('hero.actual')}
          </span>
        </div>

        {/* Gross / Tax / Net */}
        <div className="space-y-2">
          <div className="flex justify-between items-baseline">
            <span className="text-sm text-gray-400">{t('hero.gross')}</span>
            <div className="text-right">
              <span className="text-lg font-semibold text-white">
                {formatCurrency(estimate.estimate.gross)}
              </span>
              {estimate.estimate.is_estimate && (
                <span className="text-xs text-gray-500 ml-2">
                  {t('hero.base')} {formatCurrency(estimate.estimate.base_amount)}
                  {' + '}
                  {t('hero.commission')} {formatCurrency(estimate.estimate.commission)}
                </span>
              )}
            </div>
          </div>

          <div className="flex justify-between items-baseline">
            <span className="text-sm text-gray-400">{t('hero.tax')}</span>
            <div className="text-right">
              <span className="text-base font-medium text-red-400">
                −{formatCurrency(estimate.estimate.tax)}
              </span>
              {estimate.estimate.gross > 0 && (
                <span className="text-xs text-gray-500 ml-2">
                  {t('hero.taxRate', {
                    rate: ((estimate.estimate.tax / estimate.estimate.gross) * 100).toFixed(1),
                  })}
                </span>
              )}
            </div>
          </div>

          <div className="border-t border-gray-700 pt-2 flex justify-between items-baseline">
            <span className="text-sm font-medium text-gray-300">{t('hero.net')}</span>
            <span className="text-xl font-bold text-green-400">
              {formatCurrency(estimate.estimate.net)}
            </span>
          </div>
        </div>

        {/* Hours */}
        <div className="space-y-1">
          <div className="flex justify-between text-sm">
            <span className="text-gray-400">{t('hours.title')}</span>
            <span className="text-gray-300">
              {t('hours.worked', {
                worked: formatHours(estimate.hours_worked),
                total: formatHours(estimate.standard_hours_total),
              })}
              {estimate.standard_hours_total > 0 && (
                <span className="text-gray-500 ml-2">
                  {t('hours.utilization', {
                    pct: ((estimate.hours_worked / estimate.standard_hours_total) * 100).toFixed(0),
                  })}
                </span>
              )}
            </span>
          </div>
          <div className="w-full bg-gray-700 rounded-full h-1.5">
            <div
              className="bg-blue-500 h-1.5 rounded-full transition-all"
              style={{
                width: `${estimate.standard_hours_total > 0
                  ? Math.min((estimate.hours_worked / estimate.standard_hours_total) * 100, 100)
                  : 0}%`,
              }}
            />
          </div>
          {estimate.internal_hours_worked > 0 && (
            <div className="flex justify-between text-xs text-gray-500 pt-0.5">
              <span className="text-purple-400">{t('hours.internal')}</span>
              <span className="text-purple-400">
                {formatHours(estimate.internal_hours_worked)}
                {estimate.internal_revenue > 0 && (
                  <span className="ml-1">({formatCurrency(estimate.internal_revenue)})</span>
                )}
              </span>
            </div>
          )}
        </div>

        {/* Working days */}
        <div className="flex justify-between text-sm">
          <span className="text-gray-400">{t('workingDays.title')}</span>
          <span className="text-gray-300">
            {t('workingDays.summary', {
              done: estimate.working_days_done,
              remaining: estimate.working_days_remaining,
            })}
            <span className="text-gray-500 ml-1">
              ({t('workingDays.total', { total: estimate.working_days })})
            </span>
          </span>
        </div>
      </div>

      {/* Manual override panel — shown for past estimate months */}
      {estimate.estimate.is_estimate && selectedMonth < currentMonthStr && (
        <div className="bg-gray-800 rounded-xl p-5 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-base font-medium text-white">{t('override.title')}</h2>
            <button
              type="button"
              onClick={() => {
                if (showOverride) resetOverrideForm()
                setShowOverride(v => !v)
                setOverrideError(null)
              }}
              className="text-xs text-gray-400 hover:text-white transition-colors"
            >
              {showOverride ? t('override.cancel') : t('override.enter')}
            </button>
          </div>
          {!showOverride && (
            <p className="text-sm text-gray-400">{t('override.hint')}</p>
          )}
          {showOverride && (
            <div className="space-y-4">
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
                <div>
                  <label htmlFor="override-billable-hours" className="block text-xs text-gray-400 mb-1">{t('override.billableHours')}</label>
                  <input
                    id="override-billable-hours"
                    type="number"
                    value={overrideBillableHours}
                    onChange={e => setOverrideBillableHours(e.target.value)}
                    className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                    placeholder="0"
                    min="0"
                    step="0.5"
                  />
                </div>
                <div>
                  <label htmlFor="override-internal-hours" className="block text-xs text-gray-400 mb-1">{t('override.internalHours')}</label>
                  <input
                    id="override-internal-hours"
                    type="number"
                    value={overrideInternalHours}
                    onChange={e => setOverrideInternalHours(e.target.value)}
                    className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                    placeholder="0"
                    min="0"
                    step="0.5"
                  />
                </div>
                <div>
                  <label htmlFor="override-vacation-days" className="block text-xs text-gray-400 mb-1">{t('override.vacationDays')}</label>
                  <input
                    id="override-vacation-days"
                    type="number"
                    value={overrideVacationDays}
                    onChange={e => setOverrideVacationDays(e.target.value)}
                    className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                    placeholder="0"
                    min="0"
                  />
                </div>
                <div>
                  <label htmlFor="override-sick-days" className="block text-xs text-gray-400 mb-1">{t('override.sickDays')}</label>
                  <input
                    id="override-sick-days"
                    type="number"
                    value={overrideSickDays}
                    onChange={e => setOverrideSickDays(e.target.value)}
                    className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                    placeholder="0"
                    min="0"
                  />
                </div>
                <div>
                  <label htmlFor="override-actual-gross" className="block text-xs text-gray-400 mb-1">{t('override.actualGross')}</label>
                  <input
                    id="override-actual-gross"
                    type="number"
                    value={overrideGross}
                    onChange={e => setOverrideGross(e.target.value)}
                    className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                    placeholder="0"
                    min="0"
                  />
                </div>
                <div>
                  <label htmlFor="override-actual-net" className="block text-xs text-gray-400 mb-1">{t('override.actualNet')}</label>
                  <input
                    id="override-actual-net"
                    type="number"
                    value={overrideNet}
                    onChange={e => setOverrideNet(e.target.value)}
                    className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                    placeholder="0"
                    min="0"
                  />
                </div>
              </div>
              {overrideError && <p className="text-sm text-red-400">{overrideError}</p>}
              <div className="flex gap-3">
                <button
                  type="button"
                  onClick={handleSaveOverride}
                  disabled={savingOverride}
                  className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-sm rounded-lg transition-colors"
                >
                  {savingOverride ? '...' : t('override.save')}
                </button>
                <button
                  type="button"
                  onClick={() => { setShowOverride(false); resetOverrideForm() }}
                  className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-white text-sm rounded-lg transition-colors"
                >
                  {t('override.cancel')}
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Commission tier progress bars */}
      {estimate.commission_tiers.length > 0 && (
        <div className="bg-gray-800 rounded-xl p-5 space-y-4">
          <div className="flex justify-between items-baseline">
            <h2 className="text-base font-medium text-white">{t('commission.title')}</h2>
            <div className="flex items-center gap-2">
              {(estimate.estimate.vacation_days > 0 || estimate.estimate.sick_days > 0) && (
                <span className="text-xs text-amber-400 bg-amber-900/30 px-2 py-0.5 rounded">
                  {t('commission.adjustedForAbsence', {
                    count: estimate.estimate.vacation_days + estimate.estimate.sick_days,
                  })}
                </span>
              )}
              {estimate.internal_revenue > 0 && (
                <span className="text-xs text-purple-400">
                  {t('commission.internalRevenue', { amount: formatCurrency(estimate.internal_revenue) })}
                </span>
              )}
            </div>
          </div>
          <div className="space-y-3">
            {(estimate.adjusted_commission_tiers ?? estimate.commission_tiers ?? []).map((tier, idx) => {
              const totalRevenue = estimate.billable_revenue + estimate.internal_revenue
              const progress = getTierProgress(tier, totalRevenue)
              const earnings = getTierEarnings(tier, totalRevenue)
              const active = isTierActive(tier, totalRevenue)
              const reached = totalRevenue > tier.floor
              const rangeLabel = tier.ceiling === 0
                ? t('commission.tierUnbounded', { floor: formatCompact(tier.floor) })
                : t('commission.tierRange', {
                    floor: formatCompact(tier.floor),
                    ceiling: formatCompact(tier.ceiling),
                  })

              return (
                <div key={tier.id} className="space-y-1">
                  <div className="flex justify-between items-center text-sm">
                    <div className="flex items-center gap-2">
                      <span className={active ? 'text-white font-medium' : 'text-gray-400'}>
                        {t('commission.tier', { n: idx + 1 })}
                      </span>
                      <span className="text-xs text-gray-500">{rangeLabel}</span>
                      <span className={`text-xs px-1.5 py-0.5 rounded ${
                        tier.rate === 0
                          ? 'bg-gray-700 text-gray-400'
                          : active
                          ? 'bg-blue-900/60 text-blue-300'
                          : 'bg-gray-700 text-gray-500'
                      }`}>
                        {t('commission.rate', { rate: (tier.rate * 100).toFixed(0) })}
                      </span>
                    </div>
                    <span className={reached ? 'text-green-400 font-medium' : 'text-gray-500'}>
                      {reached
                        ? t('commission.earnings', { amount: formatCurrency(earnings) })
                        : t('commission.inactive')}
                    </span>
                  </div>
                  <div className="w-full bg-gray-700 rounded-full h-2">
                    <div
                      className={`h-2 rounded-full transition-all ${
                        active ? 'bg-blue-500' : reached ? 'bg-green-600' : 'bg-gray-600'
                      }`}
                      style={{ width: `${progress}%` }}
                    />
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* Per-absence-day cost */}
      {(estimate.sick_day_cost !== 0 || estimate.vacation_day_cost !== 0 || estimate.extra_hour_net !== 0) && (
        <div className="bg-gray-800 rounded-xl p-5">
          <h2 className="text-base font-medium text-white mb-2">{t('absenceCost.title')}</h2>
          <div className="space-y-1">
            <p className="text-sm text-gray-300">
              {t('absenceCost.perSickDay', {
                amount: formatCurrency(Math.abs(estimate.sick_day_cost)),
                sign: estimate.sick_day_cost >= 0 ? '−' : '+',
              })}
            </p>
            <p className="text-sm text-gray-300">
              {t('absenceCost.perVacationDay', {
                amount: formatCurrency(estimate.vacation_day_cost),
              })}
            </p>
            {estimate.extra_hour_net !== 0 && (
              <p className="text-sm text-gray-300">
                {t('absenceCost.perExtraHour', {
                  amount: formatCurrency(estimate.extra_hour_net),
                })}
              </p>
            )}
          </div>
        </div>
      )}

      {/* Vacation tracker */}
      {vacation && <VacationCard vacation={vacation} formatCurrency={formatCurrency} />}

      {/* Trekktabell parameters */}
      <TrekktabellEditor salary={salary} />

      {/* Trekktabell assignments — per-month table number selection */}
      <AssignmentsList salary={salary} />
    </>
  )
}
