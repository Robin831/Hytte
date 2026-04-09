import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import { formatNumber } from '../../utils/formatDate'

interface RegningData {
  your_remaining: number
  total_your_share: number
  your_income_due: string
}

interface Account {
  id: number
  name: string
  type: string
  balance: number
  credit_limit: number
  currency?: string
}

function formatCurrency(amount: number, currency = 'NOK'): string {
  return formatNumber(amount, {
    style: 'currency',
    currency,
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

export default function BudgetSnapshotCard() {
  const { t } = useTranslation('today')
  const { user } = useAuth()
  const [regning, setRegning] = useState<RegningData | null>(null)
  const [creditCards, setCreditCards] = useState<Account[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    const signal = controller.signal

    Promise.all([
      fetch('/api/budget/regning', { credentials: 'include', signal }),
      fetch('/api/budget/accounts', { credentials: 'include', signal }),
    ])
      .then(async ([regningRes, accountsRes]) => {
        if (signal.aborted) return
        if (!regningRes.ok) throw new Error('Failed to fetch')
        const regningData = await regningRes.json() as RegningData
        if (signal.aborted) return
        setRegning(regningData)

        if (accountsRes.ok) {
          const accountsData = await accountsRes.json() as { accounts?: Account[] }
          if (signal.aborted) return
          const cards = (accountsData.accounts ?? []).filter(
            (a: Account) => a.type === 'credit'
          )
          setCreditCards(cards)
        }
        setError(false)
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        if (!signal.aborted) setError(true)
      })
      .finally(() => {
        if (!signal.aborted) setLoading(false)
      })

    return () => { controller.abort() }
  }, [user])

  const daysUntilPayday = (() => {
    if (!regning?.your_income_due) return null
    const parts = regning.your_income_due.split('-').map(Number)
    const dueUtc = Date.UTC(parts[0], parts[1] - 1, parts[2])
    const today = new Date()
    const todayUtc = Date.UTC(today.getFullYear(), today.getMonth(), today.getDate())
    const millisecondsPerDay = 1000 * 60 * 60 * 24
    return Math.max(0, Math.ceil((dueUtc - todayUtc) / millisecondsPerDay))
  })()

  return (
    <div className="bg-gray-800 rounded-xl p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xs uppercase tracking-wide text-gray-500">
          {t('budget.title')}
        </h2>
        <Link to="/budget/regning" className="text-xs text-gray-500 hover:text-gray-400" aria-label={t('viewMore')}>
          →
        </Link>
      </div>

      {loading && (
        <div className="space-y-3" role="status" aria-live="polite">
          <span className="sr-only">{t('budget.loading')}</span>
          <div className="h-4 bg-gray-700 rounded animate-pulse w-3/4" />
          <div className="h-4 bg-gray-700 rounded animate-pulse w-1/2" />
          <div className="h-4 bg-gray-700 rounded animate-pulse w-2/3" />
        </div>
      )}

      {error && !loading && (
        <p className="text-red-400 text-sm">{t('unavailable')}</p>
      )}

      {!loading && !error && regning && (
        <div className="space-y-3">
          {/* Remaining after bills */}
          <div className="flex items-center justify-between">
            <span className="text-sm text-gray-400">{t('budget.remaining')}</span>
            <span className={`text-sm font-semibold tabular-nums ${regning.your_remaining >= 0 ? 'text-green-400' : 'text-red-400'}`}>
              {formatCurrency(regning.your_remaining)}
            </span>
          </div>

          {/* Bills total */}
          <div className="flex items-center justify-between">
            <span className="text-sm text-gray-400">{t('budget.bills')}</span>
            <span className="text-sm text-gray-300 tabular-nums">
              {formatCurrency(regning.total_your_share)}
            </span>
          </div>

          {/* Days until payday */}
          {daysUntilPayday !== null && (
            <div className="flex items-center justify-between">
              <span className="text-sm text-gray-400">{t('budget.payday')}</span>
              <span className="text-sm text-gray-300 tabular-nums">
                {daysUntilPayday === 0
                  ? t('budget.paydayToday')
                  : t('budget.paydayDays', { count: daysUntilPayday })}
              </span>
            </div>
          )}

          {/* Credit card balances */}
          {creditCards.map(card => (
            <div key={card.id} className="flex items-center justify-between border-t border-gray-700 pt-2">
              <span className="text-sm text-gray-400 truncate mr-2">{card.name}</span>
              <span className={`text-sm tabular-nums ${card.balance < 0 ? 'text-red-400' : 'text-gray-300'}`}>
                {formatCurrency(card.balance, card.currency)}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
