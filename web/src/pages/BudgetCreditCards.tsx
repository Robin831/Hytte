import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { formatDate as fmtDate, formatNumber } from '../utils/formatDate'

// ── Types ────────────────────────────────────────────────────────────────────

interface Account {
  id: number
  name: string
  type: string
  currency: string
  balance: number
  icon: string
  credit_limit: number
}

interface CategorySummary {
  category_id: number | null
  category_name: string
  color: string
  is_income: boolean
  total: number
  budget_amount: number
  budget_pct: number
}

interface CreditCardSummary {
  account: Account
  credit_limit: number
  used_amount: number
  remaining: number
  month: string
  expense_total: number
  by_category: CategorySummary[]
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function currentMonth(): string {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
}

function prevMonth(month: string): string {
  const [year, mo] = month.split('-').map(Number)
  const d = new Date(year, mo - 2, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

function nextMonth(month: string): string {
  const [year, mo] = month.split('-').map(Number)
  const d = new Date(year, mo, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

function formatMonth(month: string): string {
  const [year, mo] = month.split('-').map(Number)
  return fmtDate(new Date(year, mo - 1, 1), { year: 'numeric', month: 'long' })
}

function formatCurrency(amount: number, currency = 'NOK'): string {
  return formatNumber(Math.abs(amount), {
    style: 'currency',
    currency,
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

// ── Component ─────────────────────────────────────────────────────────────────

export default function BudgetCreditCards() {
  const { t } = useTranslation('budget')

  const [month, setMonth] = useState(currentMonth)
  const [accounts, setAccounts] = useState<Account[]>([])
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [summary, setSummary] = useState<CreditCardSummary | null>(null)
  const [loadingAccounts, setLoadingAccounts] = useState(true)
  const [loadingSummary, setLoadingSummary] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Load credit card accounts on mount
  useEffect(() => {
    const ctrl = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch
    setLoadingAccounts(true)
    setError(null)
    fetch('/api/budget/accounts', { credentials: 'include', signal: ctrl.signal })
      .then(async r => {
        if (!r.ok) {
          throw new Error(`Failed to load accounts: ${r.status}`)
        }
        return r.json()
      })
      .then(data => {
        setError(null)
        const allAccounts = Array.isArray(data.accounts) ? (data.accounts as Account[]) : []
        const creditAccounts = allAccounts.filter(a => a.type === 'credit')
        setAccounts(creditAccounts)
        setSelectedId(prev => prev === null && creditAccounts.length > 0 ? creditAccounts[0].id : prev)
      })
      .catch(err => {
        if (err.name !== 'AbortError') setError(t('errors.loadFailed'))
      })
      .finally(() => { if (!ctrl.signal.aborted) setLoadingAccounts(false) })
    return () => ctrl.abort()
  }, [t])

  // Load credit card summary when account or month changes
  const loadSummary = useCallback((accountId: number, m: string) => {
    const ctrl = new AbortController()
    setLoadingSummary(true)
    setError(null)
    setSummary(null)
    fetch(`/api/budget/credit/summary?account_id=${accountId}&month=${m}`, {
      credentials: 'include',
      signal: ctrl.signal,
    })
      .then(r => {
        if (!r.ok) throw new Error('failed')
        return r.json()
      })
      .then((data: CreditCardSummary) => setSummary(data))
      .catch(err => {
        if (err.name !== 'AbortError') setError(t('creditCards.errors.loadFailed'))
      })
      .finally(() => { if (!ctrl.signal.aborted) setLoadingSummary(false) })
    return () => ctrl.abort()
  }, [t])

  useEffect(() => {
    if (selectedId !== null) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch
      return loadSummary(selectedId, month)
    }
  }, [selectedId, month, loadSummary])

  const creditAccounts = accounts

  if (loadingAccounts) {
    return (
      <div className="p-6 text-gray-400 text-sm">{t('loading')}</div>
    )
  }

  if (creditAccounts.length === 0) {
    return (
      <div className="max-w-2xl mx-auto p-6">
        <div className="flex items-center gap-2 mb-6">
          <Link to="/budget" className="text-gray-400 hover:text-white">
            <ChevronLeft size={20} />
          </Link>
          <h1 className="text-lg font-semibold">{t('creditCards.title')}</h1>
        </div>
        <p className="text-gray-400 text-sm">{t('creditCards.noCards')}</p>
      </div>
    )
  }

  const selectedAccount = creditAccounts.find(a => a.id === selectedId) ?? creditAccounts[0]
  const usedPct = summary && summary.credit_limit > 0
    ? Math.min(100, (summary.used_amount / summary.credit_limit) * 100)
    : 0
  const usedColor = usedPct >= 90 ? 'bg-red-500' : usedPct >= 70 ? 'bg-yellow-500' : 'bg-blue-500'

  return (
    <div className="max-w-2xl mx-auto p-4 space-y-4">
      {/* Header */}
      <div className="flex items-center gap-2">
        <Link to="/budget" className="text-gray-400 hover:text-white p-1">
          <ChevronLeft size={20} />
        </Link>
        <h1 className="text-lg font-semibold flex-1">{t('creditCards.title')}</h1>
      </div>

      {error && (
        <div className="bg-red-900/40 border border-red-700 text-red-300 text-sm rounded px-3 py-2">{error}</div>
      )}

      {/* Account selector */}
      {creditAccounts.length > 1 && (
        <div className="flex gap-2 flex-wrap">
          {creditAccounts.map(a => (
            <button
              key={a.id}
              type="button"
              onClick={() => setSelectedId(a.id)}
              className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
                a.id === selectedId
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-800 text-gray-300 hover:bg-gray-700'
              }`}
            >
              {a.icon && <span className="mr-1">{a.icon}</span>}
              {a.name}
            </button>
          ))}
        </div>
      )}

      {/* Credit limit overview card */}
      {summary && (
        <div className="bg-gray-800 rounded-lg p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              {selectedAccount.icon && (
                <span className="text-xl">{selectedAccount.icon}</span>
              )}
              <span className="font-semibold text-white">{selectedAccount.name}</span>
            </div>
            <span className="text-xs text-gray-400 uppercase tracking-wide">
              {t('accounts.types.credit')}
            </span>
          </div>

          {/* Usage bar */}
          {summary.credit_limit > 0 && (
            <div className="space-y-1">
              <div className="h-2 bg-gray-700 rounded-full overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all ${usedColor}`}
                  style={{ width: `${usedPct}%` }}
                />
              </div>
              <div className="flex justify-between text-xs text-gray-400">
                <span>{t('creditCards.used')}: {formatCurrency(summary.used_amount, selectedAccount.currency)}</span>
                <span>{t('creditCards.remaining')}: {formatCurrency(Math.max(0, summary.remaining), selectedAccount.currency)}</span>
              </div>
            </div>
          )}

          {/* Stats grid */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-center">
            <div>
              <p className="text-xs text-gray-400 uppercase tracking-wide">{t('creditCards.limit')}</p>
              <p className="text-sm font-semibold text-white">
                {summary.credit_limit > 0
                  ? formatCurrency(summary.credit_limit, selectedAccount.currency)
                  : '—'}
              </p>
            </div>
            <div>
              <p className="text-xs text-gray-400 uppercase tracking-wide">{t('creditCards.used')}</p>
              <p className="text-sm font-semibold text-red-400">
                {formatCurrency(summary.used_amount, selectedAccount.currency)}
              </p>
            </div>
            <div>
              <p className="text-xs text-gray-400 uppercase tracking-wide">{t('creditCards.available')}</p>
              <p className={`text-sm font-semibold ${summary.remaining >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                {formatCurrency(Math.max(0, summary.remaining), selectedAccount.currency)}
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Month navigation */}
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => setMonth(prevMonth(month))}
          className="p-1 rounded hover:bg-gray-700 text-gray-300"
          aria-label={t('nav.prev')}
        >
          <ChevronLeft size={20} />
        </button>
        <span className="flex-1 text-center text-sm font-medium">{formatMonth(month)}</span>
        <button
          type="button"
          onClick={() => setMonth(nextMonth(month))}
          className="p-1 rounded hover:bg-gray-700 text-gray-300"
          aria-label={t('nav.next')}
        >
          <ChevronRight size={20} />
        </button>
      </div>

      {/* Monthly spending */}
      {loadingSummary && (
        <div className="text-gray-400 text-sm">{t('loading')}</div>
      )}

      {summary && !loadingSummary && (
        <div className="space-y-2">
          <div className="flex items-center justify-between text-sm mb-3">
            <span className="text-gray-400">{t('summary.expenses')}</span>
            <span className="font-semibold text-red-400">
              {formatCurrency(summary.expense_total, selectedAccount.currency)}
            </span>
          </div>

          {summary.by_category.length === 0 && (
            <p className="text-gray-500 text-sm text-center py-4">{t('empty')}</p>
          )}

          {summary.by_category
            .filter(c => !c.is_income)
            .map((cat, i) => {
              const pct = cat.budget_pct
              const barColor = pct >= 100 ? 'bg-red-500' : pct >= 80 ? 'bg-yellow-500' : 'bg-blue-500'
              return (
                <div key={cat.category_id ?? `uncategorised-${i}`} className="bg-gray-800 rounded px-3 py-2">
                  <div className="flex items-center gap-2 text-sm">
                    {cat.color && (
                      <span
                        className="inline-block w-2.5 h-2.5 rounded-full flex-shrink-0"
                        style={{ background: cat.color }}
                      />
                    )}
                    <span className="flex-1 text-gray-200">
                      {cat.category_name || t('noCategory')}
                    </span>
                    <span className="font-medium text-red-300">
                      {formatCurrency(Math.abs(cat.total), selectedAccount.currency)}
                    </span>
                    {cat.budget_amount > 0 && (
                      <span className="text-xs text-gray-400">
                        / {formatCurrency(cat.budget_amount, selectedAccount.currency)}
                      </span>
                    )}
                  </div>
                  {cat.budget_amount > 0 && (
                    <div className="mt-1.5 h-1 bg-gray-700 rounded-full overflow-hidden">
                      <div
                        className={`h-full rounded-full ${barColor}`}
                        style={{ width: `${Math.min(100, pct)}%` }}
                      />
                    </div>
                  )}
                </div>
              )
            })}
        </div>
      )}
    </div>
  )
}
