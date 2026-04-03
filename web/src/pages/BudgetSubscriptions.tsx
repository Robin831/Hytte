import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { ChevronLeft } from 'lucide-react'
import { formatNumber } from '../utils/formatDate'

// ── Types ────────────────────────────────────────────────────────────────────

interface Account {
  id: number
  name: string
  currency: string
}

interface RecurringRule {
  id: number
  account_id: number
  category_id: number | null
  amount: number
  description: string
  frequency: 'monthly' | 'weekly' | 'yearly'
  day_of_month: number
  start_date: string
  end_date: string
  active: boolean
  next_due: string
}

interface SubscriptionRow {
  rule: RecurringRule
  monthlyCost: number
  currency: string
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function toMonthlyCost(amount: number, frequency: string): number {
  switch (frequency) {
    case 'weekly':
      return (amount * 52) / 12
    case 'yearly':
      return amount / 12
    default:
      return amount
  }
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

export default function BudgetSubscriptions() {
  const { t } = useTranslation('budget')

  const [rules, setRules] = useState<RecurringRule[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showInactive, setShowInactive] = useState(false)

  useEffect(() => {
    const ctrl = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch
    setLoading(true)

    Promise.all([
      fetch('/api/budget/recurring', { credentials: 'include', signal: ctrl.signal }),
      fetch('/api/budget/accounts', { credentials: 'include', signal: ctrl.signal }),
    ])
      .then(async ([rRes, aRes]) => {
        if (!rRes.ok || !aRes.ok) throw new Error('failed')
        const [rData, aData] = await Promise.all([rRes.json(), aRes.json()])
        setRules(rData.recurring as RecurringRule[])
        setAccounts(aData.accounts as Account[])
      })
      .catch(err => {
        if (err.name !== 'AbortError') setError(t('errors.loadFailed'))
      })
      .finally(() => setLoading(false))

    return () => ctrl.abort()
  }, [t])

  const accountMap = new Map(accounts.map(a => [a.id, a]))

  const rows: SubscriptionRow[] = rules
    .filter(r => showInactive || r.active)
    .filter(r => r.amount < 0) // only expenses
    .map(r => ({
      rule: r,
      monthlyCost: Math.abs(toMonthlyCost(r.amount, r.frequency)),
      currency: accountMap.get(r.account_id)?.currency ?? 'NOK',
    }))
    .sort((a, b) => b.monthlyCost - a.monthlyCost)

  const totalMonthly = rows
    .filter(row => row.rule.active)
    .reduce((sum, row) => sum + row.monthlyCost, 0)

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
        <h1 className="text-lg font-semibold flex-1">{t('subscriptions.title')}</h1>
      </div>

      {error && (
        <div className="bg-red-900/40 border border-red-700 text-red-300 text-sm rounded px-3 py-2">{error}</div>
      )}

      {/* Total card */}
      <div className="bg-gray-800 rounded-lg p-4">
        <p className="text-xs text-gray-400 uppercase tracking-wide mb-1">{t('subscriptions.totalMonthly')}</p>
        <p className="text-2xl font-bold text-white">{formatCurrency(totalMonthly)}</p>
        <p className="text-xs text-gray-500 mt-1">
          {t('subscriptions.totalYearly')}: {formatCurrency(totalMonthly * 12)}
        </p>
      </div>

      {/* Toggle inactive */}
      <div className="flex items-center gap-2 text-sm">
        <label className="flex items-center gap-2 cursor-pointer text-gray-400">
          <input
            type="checkbox"
            checked={showInactive}
            onChange={e => setShowInactive(e.target.checked)}
            className="rounded"
            aria-label={t('subscriptions.showInactive')}
          />
          {t('subscriptions.showInactive')}
        </label>
      </div>

      {/* Subscription list */}
      {rows.length === 0 ? (
        <p className="text-gray-500 text-sm text-center py-6">{t('subscriptions.empty')}</p>
      ) : (
        <div className="space-y-1">
          {rows.map(({ rule, monthlyCost, currency }) => {
            const acct = accountMap.get(rule.account_id)
            return (
              <div
                key={rule.id}
                className={`bg-gray-800 rounded px-3 py-2.5 flex items-center gap-3 ${
                  !rule.active ? 'opacity-50' : ''
                }`}
              >
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-white truncate">
                    {rule.description || t('noDescription')}
                  </p>
                  <div className="flex items-center gap-2 text-xs text-gray-400 mt-0.5">
                    {acct && <span>{acct.name}</span>}
                    <span>·</span>
                    <span>{t(`recurring.${rule.frequency}`)}</span>
                    {!rule.active && (
                      <>
                        <span>·</span>
                        <span className="text-yellow-500">{t('recurring.inactive')}</span>
                      </>
                    )}
                  </div>
                </div>
                <div className="text-right">
                  <p className="text-sm font-semibold text-red-300">
                    {formatCurrency(monthlyCost, currency)}
                    <span className="text-xs text-gray-400 font-normal ml-1">/{t('subscriptions.month')}</span>
                  </p>
                  {rule.frequency !== 'monthly' && (
                    <p className="text-xs text-gray-500">
                      {formatCurrency(Math.abs(rule.amount), currency)} / {t(`recurring.${rule.frequency}`).toLowerCase()}
                    </p>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* Link to manage recurring */}
      <div className="pt-2">
        <Link
          to="/budget/recurring"
          className="text-sm text-blue-400 hover:text-blue-300"
        >
          {t('subscriptions.manageRecurring')}
        </Link>
      </div>
    </div>
  )
}
