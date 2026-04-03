import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Settings, ChevronDown, ChevronUp } from 'lucide-react'

interface SalaryConfig {
  id: number
  user_id: number
  base_salary: number
  hourly_rate: number
  standard_hours: number
  currency: string
  effective_from: string
}

interface CommissionTier {
  id: number
  config_id: number
  floor: number
  ceiling: number // 0 = unbounded
  rate: number
}

interface SalaryRecord {
  id: number
  user_id: number
  month: string
  working_days: number
  hours_worked: number
  billable_hours: number
  internal_hours: number
  base_amount: number
  commission: number
  gross: number
  tax: number
  net: number
  vacation_days: number
  sick_days: number
  is_estimate: boolean
}

interface EstimateResponse {
  month: string
  config: SalaryConfig
  commission_tiers: CommissionTier[]
  estimate: SalaryRecord
  working_days: number
  working_days_done: number
  working_days_remaining: number
  hours_worked: number
  standard_hours_total: number
  billable_revenue: number
  absence_cost_per_day: number
}

function formatMonthLabel(month: string, locale: string): string {
  const [year, mon] = month.split('-').map(Number)
  const date = new Date(year, mon - 1, 1)
  return date.toLocaleDateString(locale, { month: 'long', year: 'numeric' })
}

export default function SalaryPage() {
  const { t, i18n } = useTranslation('salary')
  const locale = i18n.language

  const [estimate, setEstimate] = useState<EstimateResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showConfig, setShowConfig] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  // Config form state
  const [baseSalary, setBaseSalary] = useState('')
  const [hourlyRate, setHourlyRate] = useState('')
  const [standardHours, setStandardHours] = useState('7.5')
  const [currency, setCurrency] = useState('NOK')

  const formatCurrency = (amount: number) => {
    const curr = estimate?.config.currency ?? currency
    try {
      return new Intl.NumberFormat(undefined, {
        style: 'currency',
        currency: curr,
        maximumFractionDigits: 0,
      }).format(amount)
    } catch {
      return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(amount)
    }
  }

  const formatHours = (h: number) =>
    new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(h)

  const formatCompact = (n: number) =>
    new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(n / 1000) + 'k'

  useEffect(() => {
    let cancelled = false

    fetch('/api/salary/estimate/current', { credentials: 'include' })
      .then(async res => {
        if (res.status === 404) return null
        if (!res.ok) throw new Error(await res.text())
        return res.json() as Promise<EstimateResponse>
      })
      .then(data => {
        if (cancelled) return
        setEstimate(data)
        if (data) {
          setBaseSalary(String(data.config.base_salary))
          setHourlyRate(String(data.config.hourly_rate))
          setStandardHours(String(data.config.standard_hours))
          setCurrency(data.config.currency)
        } else {
          setShowConfig(true)
        }
      })
      .catch(err => {
        if (!cancelled) setError(err.message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => { cancelled = true }
  }, [])

  const handleSaveConfig = async () => {
    setSaving(true)
    setSaveError(null)
    let saved = false
    try {
      const res = await fetch('/api/salary/config', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          base_salary: parseFloat(baseSalary) || 0,
          hourly_rate: parseFloat(hourlyRate) || 0,
          standard_hours: isNaN(parseFloat(standardHours)) ? 7.5 : parseFloat(standardHours),
          currency: currency || 'NOK',
          effective_from: (() => {
            const d = new Date()
            return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
          })(),
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error((data as { error?: string }).error ?? 'Save failed')
      }
      saved = true
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t('errors.failedToSave'))
    } finally {
      setSaving(false)
    }

    if (!saved) return

    // Reload estimate independently of the save error handling.
    try {
      const estimateRes = await fetch('/api/salary/estimate/current', { credentials: 'include' })
      if (estimateRes.ok) {
        const data = await estimateRes.json() as EstimateResponse
        setEstimate(data)
        setShowConfig(false)
      }
    } catch {
      // Non-fatal: the save succeeded; the page will show stale data until reload.
    }
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

  if (loading) {
    return (
      <div className="p-6 text-gray-400">{t('title')}…</div>
    )
  }

  if (error) {
    return (
      <div className="p-6 text-red-400">{t('errors.failedToLoad')}: {error}</div>
    )
  }

  const noConfig = estimate === null

  return (
    <div className="p-4 md:p-6 max-w-3xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-white">{t('title')}</h1>
        {!noConfig && (
          <button
            onClick={() => setShowConfig(v => !v)}
            className="flex items-center gap-1.5 text-sm text-gray-400 hover:text-white transition-colors"
          >
            <Settings size={16} />
            {t('config.edit')}
            {showConfig ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
          </button>
        )}
      </div>

      {/* Config panel */}
      {(showConfig || noConfig) && (
        <div className="bg-gray-800 rounded-xl p-5 space-y-4">
          <h2 className="text-base font-medium text-white">
            {noConfig ? t('noConfig.title') : t('config.title')}
          </h2>
          {noConfig && (
            <p className="text-sm text-gray-400">{t('noConfig.hint')}</p>
          )}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('config.baseSalary')}</label>
              <input
                type="number"
                value={baseSalary}
                onChange={e => setBaseSalary(e.target.value)}
                className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                placeholder="0"
                min="0"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('config.hourlyRate')}</label>
              <input
                type="number"
                value={hourlyRate}
                onChange={e => setHourlyRate(e.target.value)}
                className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                placeholder="0"
                min="0"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('config.standardHours')}</label>
              <input
                type="number"
                value={standardHours}
                onChange={e => setStandardHours(e.target.value)}
                className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                placeholder="7.5"
                min="0"
                step="0.5"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('config.currency')}</label>
              <input
                type="text"
                value={currency}
                onChange={e => setCurrency(e.target.value.toUpperCase())}
                className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                placeholder="NOK"
                maxLength={3}
              />
            </div>
          </div>
          {saveError && <p className="text-sm text-red-400">{saveError}</p>}
          <div className="flex gap-3">
            <button
              onClick={handleSaveConfig}
              disabled={saving}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-sm rounded-lg transition-colors"
            >
              {saving ? '...' : t('config.save')}
            </button>
            {!noConfig && (
              <button
                onClick={() => setShowConfig(false)}
                className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-white text-sm rounded-lg transition-colors"
              >
                {t('config.cancel')}
              </button>
            )}
          </div>
        </div>
      )}

      {estimate && (
        <>
          {/* Hero card — this month's estimate */}
          <div className="bg-gray-800 rounded-xl p-5 space-y-4">
            <div className="flex items-center justify-between">
              <h2 className="text-base font-medium text-white">
                {formatMonthLabel(estimate.month, locale)}
              </h2>
              <span className="text-xs px-2 py-0.5 rounded-full bg-yellow-900/60 text-yellow-300">
                {t('hero.estimate')}
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
                  <span className="text-xs text-gray-500 ml-2">
                    {t('hero.base')} {formatCurrency(estimate.estimate.base_amount)}
                    {' + '}
                    {t('hero.commission')} {formatCurrency(estimate.estimate.commission)}
                  </span>
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

          {/* Commission tier progress bars */}
          {estimate.commission_tiers.length > 0 && (
            <div className="bg-gray-800 rounded-xl p-5 space-y-4">
              <h2 className="text-base font-medium text-white">{t('commission.title')}</h2>
              <div className="space-y-3">
                {estimate.commission_tiers.map((tier, idx) => {
                  const progress = getTierProgress(tier, estimate.billable_revenue)
                  const earnings = getTierEarnings(tier, estimate.billable_revenue)
                  const active = isTierActive(tier, estimate.billable_revenue)
                  const reached = estimate.billable_revenue > tier.floor
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
          {estimate.absence_cost_per_day > 0 && (
            <div className="bg-gray-800 rounded-xl p-5">
              <h2 className="text-base font-medium text-white mb-2">{t('absenceCost.title')}</h2>
              <p className="text-sm text-gray-300">
                {t('absenceCost.perDay', {
                  amount: formatCurrency(estimate.absence_cost_per_day),
                })}
              </p>
            </div>
          )}
        </>
      )}
    </div>
  )
}
