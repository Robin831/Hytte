import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { ChevronLeft } from 'lucide-react'
import { formatNumber } from '../utils/formatDate'

// ── Types ────────────────────────────────────────────────────────────────────

interface RegningItem {
  id: number
  description: string
  amount: number
  monthly: number
  split_type: 'percentage' | 'equal' | 'fixed_you' | 'fixed_partner'
  split_pct: number | null
  your_share: number
  partner_share: number
}

interface RegningData {
  expenses: RegningItem[]
  total_your_share: number
  total_partner_share: number
  your_income: number
  partner_income: number
  your_remaining: number
  partner_remaining: number
  income_split_pct: number
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatCurrency(amount: number): string {
  return formatNumber(Math.abs(amount), {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

// ── Component ─────────────────────────────────────────────────────────────────

export default function BudgetRegning() {
  const { t } = useTranslation('budget')

  const [data, setData] = useState<RegningData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const ctrl = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch
    setLoading(true)

    fetch('/api/budget/regning', { credentials: 'include', signal: ctrl.signal })
      .then(async res => {
        if (!res.ok) throw new Error('failed')
        const json = await res.json() as RegningData
        if (!ctrl.signal.aborted) setData(json)
      })
      .catch(err => {
        const isAbortError = err instanceof DOMException && err.name === 'AbortError'
        if (!ctrl.signal.aborted && !isAbortError) setError(t('regning.errors.loadFailed'))
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false)
      })

    return () => ctrl.abort()
  }, [t])

  function splitLabel(item: RegningItem, globalPct: number): string {
    switch (item.split_type) {
      case 'equal':
        return t('regning.splitTypes.equal')
      case 'fixed_you':
        return t('regning.splitTypes.fixed_you')
      case 'fixed_partner':
        return t('regning.splitTypes.fixed_partner')
      default: {
        const pct = item.split_pct ?? globalPct
        return t('regning.splitTypes.percentage', { pct })
      }
    }
  }

  if (loading) {
    return <div className="p-6 text-gray-400 text-sm">{t('loading')}</div>
  }

  return (
    <div className="max-w-2xl mx-auto p-4 space-y-4">
      {/* Header */}
      <div className="flex items-center gap-2">
        <Link to="/budget" className="text-gray-400 hover:text-white p-1">
          <ChevronLeft size={20} />
        </Link>
        <h1 className="text-lg font-semibold flex-1">{t('regning.title')}</h1>
      </div>

      {error && (
        <div className="bg-red-900/40 border border-red-700 text-red-300 text-sm rounded px-3 py-2">
          {error}
        </div>
      )}

      {data && (
        <>
          {/* Summary cards */}
          <div className="grid grid-cols-2 gap-3">
            {/* You */}
            <div className="bg-gray-800 rounded-lg p-4 space-y-2">
              <p className="text-xs text-gray-400 uppercase tracking-wide">
                {t('regning.summary.you')}
              </p>
              <div className="space-y-1">
                <div className="flex justify-between text-sm">
                  <span className="text-gray-400">{t('regning.summary.income')}</span>
                  <span className="text-white">{formatCurrency(data.your_income)}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-gray-400">{t('regning.summary.transfer')}</span>
                  <span className="text-red-300">−{formatCurrency(data.total_your_share)}</span>
                </div>
                <div className="flex justify-between text-sm font-semibold border-t border-gray-700 pt-1">
                  <span className="text-gray-300">{t('regning.summary.remaining')}</span>
                  <span className={data.your_remaining >= 0 ? 'text-green-400' : 'text-red-400'}>
                    {formatCurrency(data.your_remaining)}
                  </span>
                </div>
              </div>
            </div>

            {/* Partner */}
            <div className="bg-gray-800 rounded-lg p-4 space-y-2">
              <p className="text-xs text-gray-400 uppercase tracking-wide">
                {t('regning.summary.partner')}
              </p>
              <div className="space-y-1">
                <div className="flex justify-between text-sm">
                  <span className="text-gray-400">{t('regning.summary.income')}</span>
                  <span className="text-white">{formatCurrency(data.partner_income)}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-gray-400">{t('regning.summary.transfer')}</span>
                  <span className="text-red-300">−{formatCurrency(data.total_partner_share)}</span>
                </div>
                <div className="flex justify-between text-sm font-semibold border-t border-gray-700 pt-1">
                  <span className="text-gray-300">{t('regning.summary.remaining')}</span>
                  <span className={data.partner_remaining >= 0 ? 'text-green-400' : 'text-red-400'}>
                    {formatCurrency(data.partner_remaining)}
                  </span>
                </div>
              </div>
            </div>
          </div>

          {/* Expense list */}
          {data.expenses.length === 0 ? (
            <p className="text-gray-500 text-sm text-center py-6">{t('regning.empty')}</p>
          ) : (
            <div className="bg-gray-800 rounded-lg overflow-hidden">
              {/* Column headers */}
              <div className="grid grid-cols-[1fr_auto_auto_auto] gap-x-3 px-3 py-2 text-xs text-gray-400 uppercase tracking-wide border-b border-gray-700">
                <span>{t('regning.expense')}</span>
                <span>{t('regning.splitType')}</span>
                <span className="text-right">{t('regning.yourShare')}</span>
                <span className="text-right">{t('regning.partnerShare')}</span>
              </div>

              {data.expenses.map(item => (
                <div
                  key={item.id}
                  className="grid grid-cols-[1fr_auto_auto_auto] gap-x-3 px-3 py-2.5 border-b border-gray-700/50 items-center last:border-b-0"
                >
                  <div className="min-w-0">
                    <p className="text-sm text-white truncate">
                      {item.description || t('noDescription')}
                    </p>
                    {Math.abs(item.monthly) !== Math.abs(item.amount) && (
                      <p className="text-xs text-gray-500">
                        {formatCurrency(item.amount)} {t('regning.originalAmount')}
                      </p>
                    )}
                  </div>
                  <span className="text-xs text-gray-400 bg-gray-700 rounded px-1.5 py-0.5 whitespace-nowrap">
                    {splitLabel(item, data.income_split_pct)}
                  </span>
                  <span className="text-sm text-right text-white tabular-nums">
                    {formatCurrency(item.your_share)}
                  </span>
                  <span className="text-sm text-right text-gray-300 tabular-nums">
                    {formatCurrency(item.partner_share)}
                  </span>
                </div>
              ))}

              {/* Totals row */}
              <div className="grid grid-cols-[1fr_auto_auto_auto] gap-x-3 px-3 py-2.5 bg-gray-700/30 border-t border-gray-700">
                <span className="text-sm font-semibold text-gray-300">{t('regning.totals')}</span>
                <span />
                <span className="text-sm font-semibold text-right text-white tabular-nums">
                  {formatCurrency(data.total_your_share)}
                </span>
                <span className="text-sm font-semibold text-right text-gray-300 tabular-nums">
                  {formatCurrency(data.total_partner_share)}
                </span>
              </div>
            </div>
          )}

          {/* Link to manage recurring rules */}
          <div className="pt-2">
            <Link to="/budget/recurring" className="text-sm text-blue-400 hover:text-blue-300">
              {t('subscriptions.manageRecurring')}
            </Link>
          </div>
        </>
      )}
    </div>
  )
}
