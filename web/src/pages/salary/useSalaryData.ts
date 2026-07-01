import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import type {
  EstimateResponse,
  Tab,
  TrekktabellAssignment,
  TrekktabellParams,
  VacationResponse,
  YearEstimateResponse,
} from './types'

export interface ConfigInput {
  base_salary: number
  hourly_rate: number
  internal_hourly_rate: number
  taxable_benefits: number
  standard_hours: number
  currency: string
}

export interface OverrideInput {
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
}

/**
 * Central data source for the Salary page. Owns every fetch and mutation so the
 * subcomponents share a single source of truth (no duplicated fetch logic).
 * Mutations update the shared fetched data on success and throw on failure so
 * callers can drive their own local saving/error UI state.
 */
export function useSalaryData(selectedMonth: string, selectedYear: number, activeTab: Tab) {
  const { t } = useTranslation('salary')

  // Month estimate
  const [estimate, setEstimate] = useState<EstimateResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  // Force a refetch of the current month's estimate after assignment / import changes.
  const [estimateRefreshToken, setEstimateRefreshToken] = useState(0)

  // Year projections
  const [yearData, setYearData] = useState<YearEstimateResponse | null>(null)
  const [yearLoading, setYearLoading] = useState(false)
  const [yearError, setYearError] = useState<string | null>(null)

  // Vacation
  const [vacation, setVacation] = useState<VacationResponse | null>(null)

  // Trekktabell params
  const [trekktabell, setTrekktabell] = useState<TrekktabellParams | null>(null)

  // Trekktabell assignments (per-month table number selection). Shared because
  // both the initial fetch and the mutations write the same error state.
  const [assignments, setAssignments] = useState<TrekktabellAssignment[]>([])
  const [assignmentsLoading, setAssignmentsLoading] = useState(true)
  const [assignmentsError, setAssignmentsError] = useState<string | null>(null)

  const formatCurrency = (amount: number) => {
    const curr = estimate?.config.currency ?? 'NOK'
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

  useEffect(() => {
    let cancelled = false
    /* eslint-disable react-hooks/set-state-in-effect -- reset before fetch */
    setLoading(true)
    setError(null)
    setEstimate(null)
    /* eslint-enable react-hooks/set-state-in-effect */

    fetch(`/api/salary/estimate/month?month=${selectedMonth}`, { credentials: 'include' })
      .then(async res => {
        if (res.status === 404) return null
        if (!res.ok) throw new Error(await res.text())
        return res.json() as Promise<EstimateResponse>
      })
      .then(data => {
        if (cancelled) return
        setEstimate(data)
      })
      .catch(err => {
        if (!cancelled) setError(err.message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => { cancelled = true }
  }, [selectedMonth, estimateRefreshToken])

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

  // Load trekktabell params when estimate is available.
  useEffect(() => {
    if (!estimate) return
    let cancelled = false

    fetch(`/api/salary/trekktabell?year=${selectedYear}`, { credentials: 'include' })
      .then(async res => {
        if (!res.ok) throw new Error(await res.text())
        return res.json() as Promise<TrekktabellParams>
      })
      .then(data => { if (!cancelled) setTrekktabell(data) })
      .catch(err => { if (!cancelled) console.error('Failed to load trekktabell:', err) })

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

  useEffect(() => {
    let cancelled = false

    fetch('/api/salary/trekktabell-assignments', { credentials: 'include' })
      .then(async res => {
        if (!res.ok) throw new Error(t('errors.failedToLoadAssignments'))
        return res.json() as Promise<{ assignments: TrekktabellAssignment[] }>
      })
      .then(data => {
        if (!cancelled) setAssignments(data.assignments ?? [])
      })
      .catch(err => {
        if (!cancelled) setAssignmentsError(err instanceof Error ? err.message : t('errors.failedToLoadAssignments'))
      })
      .finally(() => {
        if (!cancelled) setAssignmentsLoading(false)
      })

    return () => { cancelled = true }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // --- Mutations -----------------------------------------------------------

  // Save config, then reload the estimate in the background. Throws on save failure.
  const saveConfig = async (values: ConfigInput): Promise<void> => {
    const res = await fetch('/api/salary/config', {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        base_salary: values.base_salary,
        hourly_rate: values.hourly_rate,
        internal_hourly_rate: values.internal_hourly_rate,
        taxable_benefits: values.taxable_benefits,
        standard_hours: values.standard_hours,
        currency: values.currency,
        effective_from: `${selectedMonth}-01`,
      }),
    })
    if (!res.ok) {
      const data = await res.json().catch(() => ({}))
      throw new Error((data as { error?: string }).error ?? t('errors.failedToSave'))
    }

    // Reload estimate in the background — non-fatal if it fails.
    try {
      const estimateRes = await fetch(`/api/salary/estimate/month?month=${selectedMonth}`, { credentials: 'include' })
      if (estimateRes.ok) {
        const data = await estimateRes.json() as EstimateResponse
        setEstimate(data)
      }
    } catch {
      // Non-fatal: the save succeeded; the page will show stale data until reload.
    }
  }

  // Confirm a past estimate month, then reload the year projections. Throws on failure.
  const confirmMonth = async (month: string): Promise<void> => {
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

  // Save trekktabell params. Throws on failure. Returns the updated params.
  const saveTrekktabell = async (editor: TrekktabellParams): Promise<TrekktabellParams> => {
    const res = await fetch('/api/salary/trekktabell', {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(editor),
    })
    if (!res.ok) {
      const data = await res.json().catch(() => ({}))
      throw new Error((data as { error?: string }).error ?? t('errors.failedToSave'))
    }
    const updated = await res.json() as TrekktabellParams
    setTrekktabell(updated)
    return updated
  }

  // Reset trekktabell params to the year's defaults. Throws on failure. Returns the updated params.
  const resetTrekktabellDefaults = async (current: TrekktabellParams): Promise<TrekktabellParams> => {
    const defaultsRes = await fetch(`/api/salary/trekktabell/defaults?year=${current.year}`, { credentials: 'include' })
    if (!defaultsRes.ok) throw new Error(t('errors.failedToFetchDefaults'))
    const defaults = await defaultsRes.json() as TrekktabellParams
    const body = { ...defaults, user_id: current.user_id, id: current.id, year: current.year }
    const res = await fetch('/api/salary/trekktabell', {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok) throw new Error(t('errors.failedToReset'))
    const updated = await res.json() as TrekktabellParams
    setTrekktabell(updated)
    return updated
  }

  // Save a per-month trekktabell assignment. Sets assignmentsError on failure.
  // Returns true on success so the caller can clear its inputs.
  const saveAssignment = async (month: string, tableNumber: string): Promise<boolean> => {
    setAssignmentsError(null)
    try {
      const res = await fetch('/api/salary/trekktabell-assignments', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ effective_from: month, table_number: tableNumber }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error((data as { error?: string }).error ?? t('errors.failedToSaveAssignment'))
      }
      const data = await res.json() as { assignments: TrekktabellAssignment[] }
      setAssignments(data.assignments ?? [])
      // Refresh the current month's estimate so the new assignment takes effect immediately.
      setEstimateRefreshToken(tok => tok + 1)
      return true
    } catch (err) {
      setAssignmentsError(err instanceof Error ? err.message : t('errors.failedToSaveAssignment'))
      return false
    }
  }

  // Delete a per-month trekktabell assignment. Sets assignmentsError on failure.
  const deleteAssignment = async (effectiveFrom: string): Promise<void> => {
    setAssignmentsError(null)
    try {
      const res = await fetch(`/api/salary/trekktabell-assignments/${effectiveFrom}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok && res.status !== 204) {
        const data = await res.json().catch(() => ({}))
        throw new Error((data as { error?: string }).error ?? t('errors.failedToDelete'))
      }
      setAssignments(prev => prev.filter(a => a.effective_from !== effectiveFrom))
      setEstimateRefreshToken(tok => tok + 1)
    } catch (err) {
      setAssignmentsError(err instanceof Error ? err.message : t('errors.failedToDelete'))
    }
  }

  // Import trekktabell data (admin). Throws on failure. Returns import stats.
  const importTrekktabellData = async (
    year: number,
    file: File,
  ): Promise<{ rows: number; tables: number; year: number }> => {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('year', String(year))
    const res = await fetch('/api/salary/trekktabell-data/import', {
      method: 'POST',
      credentials: 'include',
      body: formData,
    })
    if (!res.ok) {
      const data = await res.json().catch(() => ({}))
      throw new Error((data as { error?: string }).error ?? t('errors.failedToImport'))
    }
    const data = await res.json() as { rows: number; tables: number; year: number }
    setEstimateRefreshToken(tok => tok + 1)
    return data
  }

  // Sync a month's net income to the budget. Throws on failure. Returns the synced amount.
  const syncBudget = async (month: string): Promise<{ net_income: number }> => {
    const res = await fetch(`/api/salary/records/${month}/sync-budget`, {
      method: 'POST',
      credentials: 'include',
    })
    if (!res.ok) {
      const data = await res.json().catch(() => ({}))
      throw new Error((data as { error?: string }).error ?? t('budgetSync.error'))
    }
    return await res.json() as { net_income: number }
  }

  // Save a manual override for a past estimate month, then reload the estimate. Throws on failure.
  const saveOverride = async (body: OverrideInput): Promise<void> => {
    const res = await fetch(`/api/salary/records/${selectedMonth}`, {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const data = await res.json().catch(() => ({}))
      throw new Error((data as { error?: string }).error ?? t('override.saveError'))
    }
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

  return {
    estimate,
    loading,
    error,
    yearData,
    yearLoading,
    yearError,
    vacation,
    trekktabell,
    assignments,
    assignmentsLoading,
    assignmentsError,
    setAssignmentsError,
    formatCurrency,
    saveConfig,
    confirmMonth,
    saveTrekktabell,
    resetTrekktabellDefaults,
    saveAssignment,
    deleteAssignment,
    importTrekktabellData,
    syncBudget,
    saveOverride,
  }
}

export type SalaryData = ReturnType<typeof useSalaryData>
