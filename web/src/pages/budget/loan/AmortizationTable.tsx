import { useState, useEffect } from 'react'
import { ChevronDown, ChevronUp } from 'lucide-react'
import type { TFunction } from 'i18next'
import type { AmortizationResponse, WhatIfParams } from './types'
import { EMPTY_WHAT_IF, whatIfQuery } from './types'
import { fmt, fmtPct, localDateString } from './format'
import { RateHistoryPanel } from './RateHistoryPanel'
import { WhatIfPanel } from './WhatIfPanel'

const INITIAL_AMORTIZATION_ROWS = 24
const FULL_AMORTIZATION_ROWS = 360
/** Trailing debounce on the what-if inputs so typing doesn't fire a request per keystroke. */
const WHAT_IF_DEBOUNCE_MS = 350

interface AmortizationTableProps {
  loanId: number
  version: number
  t: TFunction<'budget'>
}

export function AmortizationTable({ loanId, version, t }: AmortizationTableProps) {
  const [data, setData] = useState<AmortizationResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showAll, setShowAll] = useState(false)
  const [showPast, setShowPast] = useState(false)
  const [cache, setCache] = useState<{ key: string; rows: number } | null>(null)
  // Request key the backend rejected (400). Kept so the effect doesn't retry the
  // same invalid what-if in a loop; the inline error is derived from it.
  const [failedKey, setFailedKey] = useState<string | null>(null)
  // whatIf is what the user types; appliedWhatIf is its debounced counterpart
  // and is the value that actually reaches the request.
  const [whatIf, setWhatIf] = useState<WhatIfParams>(EMPTY_WHAT_IF)
  const [appliedWhatIf, setAppliedWhatIf] = useState<WhatIfParams>(EMPTY_WHAT_IF)

  useEffect(() => {
    // Nothing to debounce once the committed params match the input, so a settled
    // panel schedules no timer at all.
    if (
      whatIf.extraMonthly === appliedWhatIf.extraMonthly &&
      whatIf.lumpSum === appliedWhatIf.lumpSum &&
      whatIf.lumpSumDate === appliedWhatIf.lumpSumDate
    ) {
      return
    }
    const timer = setTimeout(() => setAppliedWhatIf(whatIf), WHAT_IF_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [whatIf, appliedWhatIf])

  const query = whatIfQuery(appliedWhatIf)
  const requestedRows = showAll ? FULL_AMORTIZATION_ROWS : INITIAL_AMORTIZATION_ROWS
  // The what-if params are part of the key so a baseline and a what-if result
  // never satisfy each other from cache.
  const requestedKey = `${loanId}|${version}|${query}`
  const whatIfError = failedKey === requestedKey ? t('loan.whatIf.errors.invalid') : null

  useEffect(() => {
    // Skip fetch if already-loaded data satisfies the smaller request (e.g. toggling showAll off)
    if (cache && cache.key === requestedKey && cache.rows >= requestedRows) return
    // The backend already rejected these params — don't retry until they change.
    if (failedKey === requestedKey) return

    const controller = new AbortController()
    // Async data fetch; AbortController prevents stale updates on unmount.
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    setLoading(true)
    setError(null)
    const url = `/api/budget/loans/${loanId}/amortization?rows=${requestedRows}${query ? `&${query}` : ''}`
    fetch(url, { credentials: 'include', signal: controller.signal })
      .then(async r => {
        // Invalid what-if input: surface it inline and keep the schedule that is
        // already on screen rather than blanking the table.
        if (r.status === 400) throw new Error('what-if')
        if (!r.ok) throw new Error('fetch failed')
        return (await r.json()) as AmortizationResponse
      })
      .then(response => {
        setData(response)
        setFailedKey(null)
        setCache({ key: requestedKey, rows: requestedRows })
      })
      .catch(err => {
        if (err instanceof Error && err.name === 'AbortError') return
        if (err instanceof Error && err.message === 'what-if') {
          setFailedKey(requestedKey)
          return
        }
        setError(t('loan.errors.loadFailed'))
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [loanId, cache, failedKey, requestedKey, requestedRows, query, t])

  if (loading && !data) return <p className="text-gray-400 text-sm py-4">{t('loading')}</p>
  if (error && !data) return <p className="text-red-400 text-sm py-4">{error}</p>
  if (!data || data.amortization.length === 0) {
    return <p className="text-gray-500 text-sm py-4">{t('loan.noAmortization')}</p>
  }

  const ltvPct = data.ltv_ratio
  const ltvOk = ltvPct <= data.ltv_max
  const allRows = data.amortization
  const rateChanges = data.rate_changes ?? []
  const today = localDateString()
  const pastRows = allRows.filter(r => r.date <= today)
  const futureRows = allRows.filter(r => r.date > today)
  const visibleRows = showPast ? allRows : futureRows

  async function addRateChange(effectiveDate: string, annualRate: number) {
    try {
      const r = await fetch(`/api/budget/loans/${loanId}/rates`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ effective_date: effectiveDate, annual_rate: annualRate }),
      })
      if (!r.ok) throw new Error('create failed')
      // Force reload amortization
      setCache(null)
    } catch {
      setError(t('loan.errors.saveFailed'))
    }
  }

  async function deleteRateChange(rcId: number) {
    try {
      const r = await fetch(`/api/budget/loans/${loanId}/rates/${rcId}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!r.ok) throw new Error('delete failed')
      setCache(null)
    } catch {
      setError(t('loan.errors.deleteFailed'))
    }
  }

  return (
    <div>
      {/* LTV display */}
      {data.loan.property_value > 0 && (
        <div className="mb-4 flex items-center gap-4">
          <span className="text-sm text-gray-400">{t('loan.ltv')}</span>
          <div className="flex-1 bg-gray-700 rounded-full h-2 max-w-xs">
            <div
              className={`h-2 rounded-full transition-all ${ltvOk ? 'bg-green-500' : 'bg-red-500'}`}
              style={{ width: `${Math.min(ltvPct / data.ltv_max, 1) * 100}%` }}
            />
          </div>
          <span className={`text-sm font-medium ${ltvOk ? 'text-green-400' : 'text-red-400'}`}>
            {fmtPct(ltvPct)}
          </span>
          <span className="text-xs text-gray-500">{t('loan.ltvMax', { pct: fmtPct(data.ltv_max) })}</span>
        </div>
      )}

      {error && <p className="mb-3 text-sm text-red-400">{error}</p>}

      {/* Rate history */}
      <RateHistoryPanel
        rateChanges={rateChanges}
        onAdd={addRateChange}
        onDelete={deleteRateChange}
        t={t}
      />

      {/* Extra payment / early payoff what-if */}
      <WhatIfPanel
        loanId={loanId}
        params={whatIf}
        onChange={setWhatIf}
        summary={data.payoff_summary}
        error={whatIfError}
        pending={loading}
        t={t}
      />

      {/* Past payments toggle */}
      {pastRows.length > 0 && (
        <button
          onClick={() => setShowPast(prev => !prev)}
          className="mb-3 flex items-center gap-1.5 text-sm text-gray-400 hover:text-gray-200 transition-colors"
        >
          {showPast ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
          {showPast
            ? t('loan.hidePastPayments')
            : t('loan.showPastPayments', { count: pastRows.length })}
        </button>
      )}

      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-gray-400 border-b border-gray-700">
              <th className="pb-2 pr-3">#</th>
              <th className="pb-2 pr-3">{t('loan.date')}</th>
              <th className="pb-2 pr-3 text-right">{t('loan.payment')}</th>
              <th className="pb-2 pr-3 text-right">{t('loan.principalPart')}</th>
              <th className="pb-2 pr-3 text-right">{t('loan.interestPart')}</th>
              <th className="pb-2 pr-3 text-right">{t('loan.remainingBalance')}</th>
              <th className="pb-2 text-right">{t('loan.rate')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {visibleRows.map(row => (
              <tr key={row.payment_num} className={`hover:bg-gray-700/30 ${row.date <= today ? 'text-gray-500' : 'text-gray-300'}`}>
                <td className="py-1.5 pr-3 text-gray-500">{row.payment_num}</td>
                <td className="py-1.5 pr-3">{row.date}</td>
                <td className="py-1.5 pr-3 text-right">{fmt(row.payment)}</td>
                <td className={`py-1.5 pr-3 text-right ${row.date <= today ? 'text-blue-400/50' : 'text-blue-400'}`}>{fmt(row.principal)}</td>
                <td className={`py-1.5 pr-3 text-right ${row.date <= today ? 'text-red-400/50' : 'text-red-400'}`}>{fmt(row.interest)}</td>
                <td className="py-1.5 pr-3 text-right">{fmt(row.remaining_balance)}</td>
                <td className="py-1.5 text-right text-gray-400">{(row.rate * 100).toFixed(2)}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {(allRows.length >= INITIAL_AMORTIZATION_ROWS || showAll) && (
        <button
          onClick={() => setShowAll(prev => !prev)}
          className="mt-3 flex items-center gap-1 text-sm text-blue-400 hover:text-blue-300"
        >
          {showAll ? (
            <><ChevronUp size={14} />{t('loan.showLess')}</>
          ) : (
            <><ChevronDown size={14} />{t('loan.showAll', { count: allRows.length })}</>
          )}
        </button>
      )}
    </div>
  )
}
