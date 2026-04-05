import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Settings, ChevronDown, ChevronUp, ChevronLeft, ChevronRight } from 'lucide-react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ReferenceLine,
  ResponsiveContainer,
} from 'recharts'

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

interface MonthProjection {
  month: string
  working_days: number
  hours_worked: number
  standard_hours_total: number
  billable_revenue: number
  utilization_pct: number
  base_amount: number
  commission: number
  gross: number
  tax: number
  net: number
  is_estimate: boolean
  is_current: boolean
  is_future: boolean
  record_id?: number
}

interface YearTotals {
  hours_worked: number
  billable_revenue: number
  base_amount: number
  commission: number
  gross: number
  tax: number
  net: number
}

interface YearEstimateResponse {
  year: number
  months: MonthProjection[]
  totals: YearTotals
}

interface TaxBracket {
  id: number
  user_id: number
  year: number
  income_from: number
  income_to: number // 0 = unbounded
  rate: number
}

interface TaxTableResponse {
  year: number
  brackets: TaxBracket[]
}

interface VacationResponse {
  year: number
  days_allowance: number
  days_used: number
  days_remaining: number
  gross_ytd: number
  feriepenger_pct: number
  feriepenger_accrued: number
}

type Tab = 'month' | 'year'

function formatMonthLabel(month: string, locale: string): string {
  const [year, mon] = month.split('-').map(Number)
  const date = new Date(year, mon - 1, 1)
  return date.toLocaleDateString(locale, { month: 'long', year: 'numeric' })
}

function shortMonthLabel(month: string, locale: string): string {
  const [year, mon] = month.split('-').map(Number)
  return new Date(year, mon - 1, 1).toLocaleDateString(locale, { month: 'short' })
}

function addMonth(month: string, delta: number): string {
  const [y, m] = month.split('-').map(Number)
  const d = new Date(y, m - 1 + delta, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

export default function SalaryPage() {
  const { t, i18n } = useTranslation('salary')
  const locale = i18n.language

  const [activeTab, setActiveTab] = useState<Tab>('month')
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

  // Month/year navigation state
  const currentYear = new Date().getFullYear()
  const currentMonthStr = (() => {
    const d = new Date()
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
  })()
  const getYearFromMonth = (month: string) => {
    const parsedYear = Number.parseInt(month.split('-')[0] ?? '', 10)
    return Number.isNaN(parsedYear) ? currentYear : parsedYear
  }
  const [selectedMonth, setSelectedMonth] = useState(currentMonthStr)
  const [selectedYear, setSelectedYear] = useState(() => getYearFromMonth(currentMonthStr))
  const [yearData, setYearData] = useState<YearEstimateResponse | null>(null)
  const [yearLoading, setYearLoading] = useState(false)
  const [yearError, setYearError] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [confirmError, setConfirmError] = useState<string | null>(null)

  // Vacation state
  const [vacation, setVacation] = useState<VacationResponse | null>(null)

  // Tax table state
  const [taxTable, setTaxTable] = useState<TaxTableResponse | null>(null)
  const [showTaxEditor, setShowTaxEditor] = useState(false)
  const [taxEditorBrackets, setTaxEditorBrackets] = useState<TaxBracket[]>([])
  const [savingTax, setSavingTax] = useState(false)
  const [taxSaveError, setTaxSaveError] = useState<string | null>(null)

  // Budget sync state
  const [syncing, setSyncing] = useState<string | null>(null)
  const [syncResults, setSyncResults] = useState<Record<string, string>>({})
  const [syncErrors, setSyncErrors] = useState<Record<string, string>>({})

  // Manual override form state (for past estimate months)
  const [showOverride, setShowOverride] = useState(false)
  const [overrideBillableHours, setOverrideBillableHours] = useState('')
  const [overrideInternalHours, setOverrideInternalHours] = useState('')
  const [overrideVacationDays, setOverrideVacationDays] = useState('')
  const [overrideSickDays, setOverrideSickDays] = useState('')
  const [overrideGross, setOverrideGross] = useState('')
  const [overrideNet, setOverrideNet] = useState('')
  const [savingOverride, setSavingOverride] = useState(false)
  const [overrideError, setOverrideError] = useState<string | null>(null)

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
    setLoading(true)
    setError(null)
    setEstimate(null)

    fetch(`/api/salary/estimate/month?month=${selectedMonth}`, { credentials: 'include' })
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
        }
      })
      .catch(err => {
        if (!cancelled) setError(err.message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => { cancelled = true }
  }, [selectedMonth])

  useEffect(() => {
    setSelectedYear(getYearFromMonth(selectedMonth))
    setShowOverride(false)
    setOverrideError(null)
  }, [selectedMonth]) // eslint-disable-line react-hooks/exhaustive-deps

  // Load vacation data when estimate is available (has config).
  useEffect(() => {
    if (!estimate) return
    let cancelled = false

    fetch(`/api/salary/vacation?year=${selectedYear}`, { credentials: 'include' })
      .then(async res => {
        if (!res.ok) throw new Error(await res.text())
        return res.json() as Promise<VacationResponse>
      })
      .then(data => { if (!cancelled) setVacation(data) })
      .catch(err => { if (!cancelled) console.error('Failed to load vacation data:', err) })

    return () => { cancelled = true }
  }, [estimate, selectedYear])

  // Load tax table when estimate is available.
  useEffect(() => {
    if (!estimate) return
    let cancelled = false

    fetch(`/api/salary/tax-table?year=${selectedYear}`, { credentials: 'include' })
      .then(async res => {
        if (!res.ok) throw new Error(await res.text())
        return res.json() as Promise<TaxTableResponse>
      })
      .then(data => {
        if (!cancelled) {
          setTaxTable(data)
          setTaxEditorBrackets(data.brackets)
        }
      })
      .catch(err => { if (!cancelled) console.error('Failed to load tax table:', err) })

    return () => { cancelled = true }
  }, [estimate, selectedYear])

  useEffect(() => {
    if (activeTab !== 'year') return
    let cancelled = false

    ;(async () => {
      setYearLoading(true)
      setYearError(null)
      try {
        const res = await fetch(`/api/salary/estimate/year?year=${selectedYear}`, { credentials: 'include' })
        if (!res.ok) throw new Error(await res.text())
        const data = await res.json() as YearEstimateResponse
        if (!cancelled) setYearData(data)
      } catch (err) {
        if (!cancelled) setYearError((err as Error).message)
      } finally {
        if (!cancelled) setYearLoading(false)
      }
    })()

    return () => { cancelled = true }
  }, [activeTab, selectedYear])

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
          effective_from: `${selectedMonth}-01`,
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
      const estimateRes = await fetch(`/api/salary/estimate/month?month=${selectedMonth}`, { credentials: 'include' })
      if (estimateRes.ok) {
        const data = await estimateRes.json() as EstimateResponse
        setEstimate(data)
        setShowConfig(false)
      }
    } catch {
      // Non-fatal: the save succeeded; the page will show stale data until reload.
    }
  }

  const handleConfirm = async (month: string) => {
    setConfirming(month)
    setConfirmError(null)
    try {
      const res = await fetch(`/api/salary/records/${month}/confirm`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const responseText = await res.text().catch(() => '')
        let message = t('errors.failedToConfirm')
        if (responseText.trim()) {
          try {
            const data = JSON.parse(responseText) as { error?: string }
            if (data.error?.trim()) {
              message = data.error
            } else {
              message = responseText.trim()
            }
          } catch {
            message = responseText.trim()
          }
        }
        throw new Error(message)
      }
    } catch (err) {
      setConfirmError(err instanceof Error ? err.message : t('errors.failedToConfirm'))
      setConfirming(null)
      return
    }
    setConfirming(null)
    // Reload year data independently — non-fatal if it fails.
    try {
      const res2 = await fetch(`/api/salary/estimate/year?year=${selectedYear}`, { credentials: 'include' })
      if (res2.ok) {
        const data = await res2.json() as YearEstimateResponse
        setYearData(data)
      }
    } catch {
      // Non-fatal: confirm succeeded; data will refresh on next navigation.
    }
  }

  const handleSaveTaxTable = async () => {
    if (!taxTable) return
    setSavingTax(true)
    setTaxSaveError(null)
    try {
      const res = await fetch('/api/salary/tax-table', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ year: taxTable.year, brackets: taxEditorBrackets }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error((data as { error?: string }).error ?? 'Save failed')
      }
      const updated = await res.json() as TaxTableResponse
      setTaxTable(updated)
      setTaxEditorBrackets(updated.brackets)
      setShowTaxEditor(false)
    } catch (err) {
      setTaxSaveError(err instanceof Error ? err.message : 'Save failed')
    } finally {
      setSavingTax(false)
    }
  }

  const handleResetTaxDefaults = async () => {
    if (!taxTable) return
    setSavingTax(true)
    setTaxSaveError(null)
    try {
      const defaultsRes = await fetch(`/api/salary/tax-table/defaults?year=${taxTable.year}`, { credentials: 'include' })
      if (!defaultsRes.ok) throw new Error('Failed to fetch defaults')
      const defaultsData = await defaultsRes.json() as TaxTableResponse
      const res = await fetch('/api/salary/tax-table', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ year: taxTable.year, brackets: defaultsData.brackets }),
      })
      if (!res.ok) throw new Error('Reset failed')
      const updated = await res.json() as TaxTableResponse
      setTaxTable(updated)
      setTaxEditorBrackets(updated.brackets)
      setShowTaxEditor(false)
    } catch (err) {
      setTaxSaveError(err instanceof Error ? err.message : 'Reset failed')
    } finally {
      setSavingTax(false)
    }
  }

  const handleSyncBudget = async (month: string) => {
    setSyncing(month)
    setSyncErrors(prev => { const n = { ...prev }; delete n[month]; return n })
    try {
      const res = await fetch(`/api/salary/records/${month}/sync-budget`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error((data as { error?: string }).error ?? t('budgetSync.error'))
      }
      const data = await res.json() as { net_income: number }
      setSyncResults(prev => ({
        ...prev,
        [month]: t('budgetSync.synced', { amount: formatCurrency(data.net_income) }),
      }))
    } catch (err) {
      setSyncErrors(prev => ({
        ...prev,
        [month]: err instanceof Error ? err.message : t('budgetSync.error'),
      }))
    } finally {
      setSyncing(null)
    }
  }

  const handleSaveOverride = async () => {
    setSavingOverride(true)
    setOverrideError(null)
    const billable = parseFloat(overrideBillableHours) || 0
    const internal = parseFloat(overrideInternalHours) || 0
    const vacDays = parseInt(overrideVacationDays, 10) || 0
    const sickDays = parseInt(overrideSickDays, 10) || 0
    const gross = parseFloat(overrideGross) || 0
    const net = parseFloat(overrideNet) || 0
    const hoursWorked = billable + internal
    const tax = Math.max(0, gross - net)

    try {
      const res = await fetch(`/api/salary/records/${selectedMonth}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
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
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error((data as { error?: string }).error ?? t('override.saveError'))
      }
    } catch (err) {
      setOverrideError(err instanceof Error ? err.message : t('override.saveError'))
      setSavingOverride(false)
      return
    }
    setSavingOverride(false)
    setShowOverride(false)

    // Reload estimate to reflect the saved actual data.
    try {
      const estimateRes = await fetch(`/api/salary/estimate/month?month=${selectedMonth}`, { credentials: 'include' })
      if (estimateRes.ok) {
        const data = await estimateRes.json() as EstimateResponse
        setEstimate(data)
      }
    } catch {
      // Non-fatal: data refreshes on next navigation.
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

  const noConfig = estimate === null && selectedMonth === currentMonthStr
  const noConfigPastMonth = estimate === null && selectedMonth !== currentMonthStr

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
      {(showConfig || noConfig || noConfigPastMonth) && (
        <div className="bg-gray-800 rounded-xl p-5 space-y-4">
          <h2 className="text-base font-medium text-white">
            {(noConfig || noConfigPastMonth) ? t('noConfig.title') : t('config.title')}
          </h2>
          {(noConfig || noConfigPastMonth) && (
            <p className="text-sm text-gray-400">
              {noConfigPastMonth ? t('noConfig.pastMonth') : t('noConfig.hint')}
            </p>
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

      {/* Tab switcher */}
      {!noConfig && (
        <div className="flex gap-1 bg-gray-800/50 rounded-lg p-1 w-fit">
          <button
            type="button"
            onClick={() => setActiveTab('month')}
            className={`px-4 py-1.5 rounded-md text-sm font-medium transition-colors ${
              activeTab === 'month'
                ? 'bg-gray-700 text-white'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            {t('year.tabs.month')}
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('year')}
            className={`px-4 py-1.5 rounded-md text-sm font-medium transition-colors ${
              activeTab === 'year'
                ? 'bg-gray-700 text-white'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            {t('year.tabs.year')}
          </button>
        </div>
      )}

      {/* Month view */}
      {activeTab === 'month' && estimate && (
        <>
          {/* Hero card — month estimate with prev/next navigation */}
          <div className="bg-gray-800 rounded-xl p-5 space-y-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  onClick={() => setSelectedMonth(prev => addMonth(prev, -1))}
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
                  onClick={() => setSelectedMonth(prev => addMonth(prev, 1))}
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
                      <label className="block text-xs text-gray-400 mb-1">{t('override.billableHours')}</label>
                      <input
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
                      <label className="block text-xs text-gray-400 mb-1">{t('override.internalHours')}</label>
                      <input
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
                      <label className="block text-xs text-gray-400 mb-1">{t('override.vacationDays')}</label>
                      <input
                        type="number"
                        value={overrideVacationDays}
                        onChange={e => setOverrideVacationDays(e.target.value)}
                        className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                        placeholder="0"
                        min="0"
                      />
                    </div>
                    <div>
                      <label className="block text-xs text-gray-400 mb-1">{t('override.sickDays')}</label>
                      <input
                        type="number"
                        value={overrideSickDays}
                        onChange={e => setOverrideSickDays(e.target.value)}
                        className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                        placeholder="0"
                        min="0"
                      />
                    </div>
                    <div>
                      <label className="block text-xs text-gray-400 mb-1">{t('override.actualGross')}</label>
                      <input
                        type="number"
                        value={overrideGross}
                        onChange={e => setOverrideGross(e.target.value)}
                        className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                        placeholder="0"
                        min="0"
                      />
                    </div>
                    <div>
                      <label className="block text-xs text-gray-400 mb-1">{t('override.actualNet')}</label>
                      <input
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
                      onClick={() => { setShowOverride(false); setOverrideError(null) }}
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

          {/* Vacation tracker */}
          {vacation && (
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
          )}

          {/* Tax brackets */}
          {taxTable && (
            <div className="bg-gray-800 rounded-xl p-5 space-y-3">
              <div className="flex items-center justify-between">
                <h2 className="text-base font-medium text-white">
                  {t('tax.title')} — {t('tax.year', { year: taxTable.year })}
                </h2>
                <button
                  type="button"
                  onClick={() => {
                    setShowTaxEditor(v => !v)
                    setTaxEditorBrackets(taxTable.brackets)
                    setTaxSaveError(null)
                  }}
                  className="text-xs text-gray-400 hover:text-white transition-colors"
                >
                  {showTaxEditor ? t('tax.cancel') : t('tax.edit')}
                </button>
              </div>

              {!showTaxEditor && (
                <div className="divide-y divide-gray-700/50">
                  {taxTable.brackets.map((b, i) => (
                    <div key={b.id ?? i} className="flex justify-between items-center py-1.5 text-sm">
                      <span className="text-gray-400">
                        {new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(b.income_from)}
                        {' – '}
                        {b.income_to === 0
                          ? t('tax.unbounded')
                          : new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(b.income_to)}
                      </span>
                      <span className="text-white font-medium tabular-nums">
                        {t('tax.marginalRate', { rate: (b.rate * 100).toFixed(1) })}
                      </span>
                    </div>
                  ))}
                </div>
              )}

              {showTaxEditor && (
                <div className="space-y-3">
                  <div className="grid grid-cols-3 gap-2 text-xs text-gray-400 px-1">
                    <span>{t('tax.from')}</span>
                    <span>{t('tax.to')} (0 = {t('tax.unbounded')})</span>
                    <span>{t('tax.rate')} (0–1)</span>
                  </div>
                  {taxEditorBrackets.map((b, i) => (
                    <div key={i} className="grid grid-cols-3 gap-2">
                      <input
                        type="number"
                        value={b.income_from}
                        onChange={e => {
                          const updated = [...taxEditorBrackets]
                          updated[i] = { ...updated[i], income_from: parseFloat(e.target.value) || 0 }
                          setTaxEditorBrackets(updated)
                        }}
                        className="bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                        min="0"
                      />
                      <input
                        type="number"
                        value={b.income_to}
                        onChange={e => {
                          const updated = [...taxEditorBrackets]
                          updated[i] = { ...updated[i], income_to: parseFloat(e.target.value) || 0 }
                          setTaxEditorBrackets(updated)
                        }}
                        className="bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                        min="0"
                      />
                      <input
                        type="number"
                        value={b.rate}
                        onChange={e => {
                          const updated = [...taxEditorBrackets]
                          updated[i] = { ...updated[i], rate: parseFloat(e.target.value) || 0 }
                          setTaxEditorBrackets(updated)
                        }}
                        className="bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                        min="0"
                        max="1"
                        step="0.001"
                      />
                    </div>
                  ))}
                  {taxSaveError && <p className="text-sm text-red-400">{taxSaveError}</p>}
                  <div className="flex gap-2 flex-wrap">
                    <button
                      type="button"
                      onClick={handleSaveTaxTable}
                      disabled={savingTax}
                      className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-sm rounded-lg transition-colors"
                    >
                      {savingTax ? '...' : t('tax.save')}
                    </button>
                    <button
                      type="button"
                      onClick={handleResetTaxDefaults}
                      disabled={savingTax}
                      className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded-lg transition-colors"
                    >
                      {t('tax.resetDefaults')}
                    </button>
                    <button
                      type="button"
                      onClick={() => { setShowTaxEditor(false); setTaxSaveError(null) }}
                      className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded-lg transition-colors"
                    >
                      {t('tax.cancel')}
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}
        </>
      )}

      {/* Year view */}
      {activeTab === 'year' && (
        <div className="space-y-6">
          {/* Year selector */}
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => setSelectedYear(y => Math.max(2000, y - 1))}
              disabled={selectedYear <= 2000}
              aria-label={t('year.prev')}
              className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-700 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
            >
              <ChevronLeft size={18} />
            </button>
            <span className="text-white font-semibold w-16 text-center">{selectedYear}</span>
            <button
              type="button"
              onClick={() => setSelectedYear(y => y + 1)}
              disabled={selectedYear >= currentYear}
              aria-label={t('year.next')}
              className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-700 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
            >
              <ChevronRight size={18} />
            </button>
          </div>

          {yearLoading && <div className="text-gray-400 py-4">{t('year.title')}…</div>}
          {yearError && <div className="text-red-400">{t('errors.failedToLoad')}: {yearError}</div>}
          {confirmError && <div className="text-red-400 text-sm">{confirmError}</div>}

          {yearData && !yearLoading && (
            <>
              {/* Utilization chart */}
              <div className="bg-gray-800 rounded-xl p-5">
                <h2 className="text-base font-medium text-white mb-4">{t('year.chart.title')}</h2>
                <div role="img" aria-label={t('year.chart.title')}>
                <ResponsiveContainer width="100%" height={180}>
                  <LineChart
                    data={yearData.months.map(mp => ({
                      name: shortMonthLabel(mp.month, locale),
                      utilization: Math.round(mp.utilization_pct),
                    }))}
                    margin={{ top: 4, right: 8, left: -16, bottom: 0 }}
                  >
                    <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                    <XAxis dataKey="name" tick={{ fill: '#9CA3AF', fontSize: 11 }} />
                    <YAxis
                      tick={{ fill: '#9CA3AF', fontSize: 11 }}
                      domain={[0, 120]}
                      tickFormatter={v => `${v}%`}
                    />
                    <Tooltip
                      contentStyle={{ backgroundColor: '#1F2937', border: 'none', borderRadius: '8px' }}
                      labelStyle={{ color: '#F3F4F6' }}
                      itemStyle={{ color: '#60A5FA' }}
                      formatter={(value) => [`${typeof value === 'number' ? value : 0}%`, t('year.chart.utilization')]}
                    />
                    <ReferenceLine y={100} stroke="#6B7280" strokeDasharray="4 2" />
                    <Line
                      type="monotone"
                      dataKey="utilization"
                      stroke="#3B82F6"
                      strokeWidth={2}
                      dot={{ r: 3, fill: '#3B82F6' }}
                      activeDot={{ r: 5 }}
                    />
                  </LineChart>
                </ResponsiveContainer>
                </div>
              </div>

              {/* Year overview table */}
              <div className="bg-gray-800 rounded-xl overflow-hidden">
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-gray-700">
                        <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 whitespace-nowrap">
                          {t('year.table.month')}
                        </th>
                        <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                          {t('year.table.days')}
                        </th>
                        <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                          {t('year.table.utilization')}
                        </th>
                        <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                          {t('year.table.gross')}
                        </th>
                        <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                          {t('year.table.tax')}
                        </th>
                        <th className="px-3 py-3 text-right text-xs font-medium text-gray-400 whitespace-nowrap">
                          {t('year.table.net')}
                        </th>
                        <th className="px-3 py-3 text-center text-xs font-medium text-gray-400 whitespace-nowrap">
                          {t('year.table.status')}
                        </th>
                        <th className="px-3 py-3" />
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-gray-700/50">
                      {yearData.months.map(mp => {
                        const rowClass = mp.is_current
                          ? 'bg-blue-950/30'
                          : mp.is_future
                          ? 'opacity-60'
                          : ''

                        const statusBadge = mp.is_estimate
                          ? mp.is_future
                            ? (
                              <span className="text-xs px-1.5 py-0.5 rounded bg-gray-700 text-gray-400">
                                {t('year.status.projected')}
                              </span>
                            )
                            : (
                              <span className="text-xs px-1.5 py-0.5 rounded bg-yellow-900/60 text-yellow-300">
                                {t('year.status.estimate')}
                              </span>
                            )
                          : (
                            <span className="text-xs px-1.5 py-0.5 rounded bg-green-900/60 text-green-400">
                              {t('year.status.actual')}
                            </span>
                          )

                        // Allow confirming past estimate months (not current, not future, not already confirmed).
                        const canConfirm = mp.is_estimate && !mp.is_future && !mp.is_current
                        // Allow syncing confirmed past months (or any non-future month with net income).
                        const canSync = !mp.is_future && mp.net > 0

                        const utilColor = mp.utilization_pct >= 100
                          ? 'text-green-400'
                          : mp.utilization_pct >= 80
                          ? 'text-yellow-400'
                          : 'text-gray-400'

                        return (
                          <tr
                            key={mp.month}
                            className={`${rowClass} hover:bg-gray-700/30 transition-colors`}
                          >
                            <td className="px-4 py-2.5 text-gray-200 font-medium whitespace-nowrap">
                              {shortMonthLabel(mp.month, locale)}
                            </td>
                            <td className="px-3 py-2.5 text-right text-gray-400 tabular-nums">
                              {mp.working_days}
                            </td>
                            <td className={`px-3 py-2.5 text-right tabular-nums ${utilColor}`}>
                              {mp.utilization_pct.toFixed(0)}%
                            </td>
                            <td className="px-3 py-2.5 text-right text-gray-200 tabular-nums whitespace-nowrap">
                              {formatCurrency(mp.gross)}
                            </td>
                            <td className="px-3 py-2.5 text-right text-red-400 tabular-nums whitespace-nowrap">
                              {formatCurrency(mp.tax)}
                            </td>
                            <td className="px-3 py-2.5 text-right text-green-400 font-medium tabular-nums whitespace-nowrap">
                              {formatCurrency(mp.net)}
                            </td>
                            <td className="px-3 py-2.5 text-center whitespace-nowrap">
                              {statusBadge}
                            </td>
                            <td className="px-3 py-2.5 text-right whitespace-nowrap">
                              <div className="flex gap-1 justify-end">
                                {canConfirm && (
                                  <button
                                    type="button"
                                    onClick={() => handleConfirm(mp.month)}
                                    disabled={confirming === mp.month}
                                    className="text-xs px-2 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 hover:text-white rounded transition-colors disabled:opacity-50"
                                  >
                                    {confirming === mp.month ? t('year.confirming') : t('year.confirm')}
                                  </button>
                                )}
                                {canSync && (
                                  <button
                                    type="button"
                                    onClick={() => handleSyncBudget(mp.month)}
                                    disabled={syncing === mp.month}
                                    title={syncResults[mp.month] ?? t('budgetSync.sync')}
                                    className={`text-xs px-2 py-1 rounded transition-colors disabled:opacity-50 ${
                                      syncResults[mp.month]
                                        ? 'bg-green-900/60 text-green-400'
                                        : syncErrors[mp.month]
                                        ? 'bg-red-900/60 text-red-400 hover:bg-red-900/80'
                                        : 'bg-gray-700 hover:bg-gray-600 text-gray-300 hover:text-white'
                                    }`}
                                  >
                                    {syncing === mp.month
                                      ? t('budgetSync.syncing')
                                      : syncResults[mp.month]
                                      ? '✓'
                                      : syncErrors[mp.month]
                                      ? '!'
                                      : '↔'}
                                  </button>
                                )}
                              </div>
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                    <tfoot>
                      <tr className="border-t border-gray-600">
                        <td className="px-4 py-3 text-sm font-medium text-gray-300">
                          {t('year.totals')}
                        </td>
                        <td />
                        <td />
                        <td className="px-3 py-3 text-right text-sm font-semibold text-gray-200 tabular-nums whitespace-nowrap">
                          {formatCurrency(yearData.totals.gross)}
                        </td>
                        <td className="px-3 py-3 text-right text-sm font-semibold text-red-400 tabular-nums whitespace-nowrap">
                          {formatCurrency(yearData.totals.tax)}
                        </td>
                        <td className="px-3 py-3 text-right text-sm font-bold text-green-400 tabular-nums whitespace-nowrap">
                          {formatCurrency(yearData.totals.net)}
                        </td>
                        <td />
                        <td />
                      </tr>
                    </tfoot>
                  </table>
                </div>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
