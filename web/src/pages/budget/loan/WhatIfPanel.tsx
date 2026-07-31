import { useState } from 'react'
import { AlertTriangle, ChevronDown, ChevronUp, RotateCcw } from 'lucide-react'
import type { TFunction } from 'i18next'
import type { PayoffSummary, WhatIfParams } from './types'
import { EMPTY_WHAT_IF, lumpSumNeedsDate, whatIfQuery } from './types'
import { fmt } from './format'

/**
 * Collapsible "extra payment / early payoff" what-if panel. The inputs are
 * controlled by the parent, which debounces them into the amortization request;
 * this component only renders them plus the resulting payoff summary.
 */
export function WhatIfPanel({ loanId, params, onChange, summary, error, pending, onRetry, t }: {
  loanId: number
  params: WhatIfParams
  onChange: (params: WhatIfParams) => void
  summary?: PayoffSummary | null
  error?: string | null
  pending?: boolean
  onRetry?: () => void
  t: TFunction<'budget'>
}) {
  const hasInput =
    params.extraMonthly !== '' || params.lumpSum !== '' || params.lumpSumDate !== ''
  // A lump sum without a date cannot be applied; warn instead of dropping it.
  const missingLumpSumDate = lumpSumNeedsDate(params)
  // The dot only means "this is affecting the schedule", i.e. something actually
  // reaches the request — a half-filled lump sum gets a warning marker instead.
  const isApplied = whatIfQuery(params) !== ''
  const [open, setOpen] = useState(false)

  function set(patch: Partial<WhatIfParams>) {
    onChange({ ...params, ...patch })
  }

  const inputClass = 'w-full bg-gray-700 border border-gray-600 rounded px-2 py-1.5 text-sm'
  const labelClass = 'block text-xs text-gray-500 mb-0.5'

  return (
    <div className="mb-4">
      <button
        onClick={() => setOpen(prev => !prev)}
        className="flex items-center gap-1.5 text-sm text-gray-400 hover:text-gray-200 transition-colors"
      >
        {open ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
        {t('loan.whatIf.title')}
        {missingLumpSumDate ? (
          <AlertTriangle size={14} className="text-amber-400" aria-label={t('loan.whatIf.errors.lumpSumDateRequired')} />
        ) : (
          isApplied && <span className="w-1.5 h-1.5 rounded-full bg-blue-400" />
        )}
      </button>

      {open && (
        <div className="mt-2 space-y-3">
          <p className="text-xs text-gray-500">{t('loan.whatIf.hint')}</p>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
            <div>
              <label className={labelClass} htmlFor={`whatif-extra-${loanId}`}>
                {t('loan.whatIf.extraMonthly')}
              </label>
              <input
                id={`whatif-extra-${loanId}`}
                type="number"
                min="0"
                step="500"
                inputMode="decimal"
                value={params.extraMonthly}
                onChange={e => set({ extraMonthly: e.target.value })}
                placeholder="0"
                className={inputClass}
              />
            </div>
            <div>
              <label className={labelClass} htmlFor={`whatif-lump-${loanId}`}>
                {t('loan.whatIf.lumpSum')}
              </label>
              <input
                id={`whatif-lump-${loanId}`}
                type="number"
                min="0"
                step="10000"
                inputMode="decimal"
                value={params.lumpSum}
                onChange={e => set({ lumpSum: e.target.value })}
                placeholder="0"
                className={inputClass}
              />
            </div>
            <div>
              <label className={labelClass} htmlFor={`whatif-lump-date-${loanId}`}>
                {t('loan.whatIf.lumpSumDate')}
              </label>
              <input
                id={`whatif-lump-date-${loanId}`}
                type="date"
                value={params.lumpSumDate}
                onChange={e => set({ lumpSumDate: e.target.value })}
                className={inputClass}
              />
            </div>
          </div>

          {hasInput && (
            <button
              onClick={() => onChange(EMPTY_WHAT_IF)}
              className="flex items-center gap-1 text-xs text-gray-400 hover:text-gray-200 transition-colors"
            >
              <RotateCcw size={12} />
              {t('loan.whatIf.reset')}
            </button>
          )}

          {missingLumpSumDate && (
            <p className="flex items-start gap-1.5 text-xs text-amber-400">
              <AlertTriangle size={14} className="shrink-0 mt-px" />
              <span>{t('loan.whatIf.errors.lumpSumDateRequired')}</span>
            </p>
          )}

          {error && (
            <div className="flex flex-wrap items-center gap-2">
              <p className="text-sm text-red-400">{error}</p>
              {onRetry && (
                <button
                  onClick={onRetry}
                  className="flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300 transition-colors"
                >
                  <RotateCcw size={12} />
                  {t('loan.whatIf.retry')}
                </button>
              )}
            </div>
          )}

          {pending && !error && (
            <p className="text-xs text-gray-500">{t('loan.whatIf.calculating')}</p>
          )}

          {summary && !error && (
            <div className="rounded-lg border border-gray-700 bg-gray-800/50 p-3">
              <dl className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
                <div>
                  <dt className="text-xs text-gray-500">{t('loan.whatIf.originalPayoff')}</dt>
                  <dd className="text-gray-300 break-words">{summary.original_payoff_date}</dd>
                </div>
                <div>
                  <dt className="text-xs text-gray-500">{t('loan.whatIf.newPayoff')}</dt>
                  <dd className="text-green-400 font-medium break-words">{summary.new_payoff_date}</dd>
                </div>
                <div>
                  <dt className="text-xs text-gray-500">{t('loan.whatIf.monthsSaved')}</dt>
                  <dd className="text-green-400 font-medium break-words">{summary.months_saved}</dd>
                </div>
                <div>
                  <dt className="text-xs text-gray-500">{t('loan.whatIf.interestSaved')}</dt>
                  <dd className="text-green-400 font-medium break-words">{fmt(summary.interest_saved)}</dd>
                </div>
              </dl>
              <p className="mt-2 text-xs text-gray-500">
                {t('loan.whatIf.interestCompare', {
                  original: fmt(summary.original_total_interest),
                  updated: fmt(summary.new_total_interest),
                })}
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
