import { useState, useEffect, useCallback, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight, Plus, Trash2, X, Pencil, Check } from 'lucide-react'
import { formatDate as fmtDate, formatNumber } from '../utils/formatDate'

// ── Types ────────────────────────────────────────────────────────────────────

interface Account {
  id: number
  name: string
  type: string
  currency: string
  balance: number
  icon: string
}

interface Category {
  id: number
  name: string
  group_name: string
  icon: string
  color: string
  is_income: boolean
}

interface Transaction {
  id: number
  account_id: number
  category_id: number | null
  amount: number
  description: string
  date: string
  tags: string[]
  is_transfer: boolean
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

interface MonthlySummary {
  month: string
  income_total: number
  expense_total: number
  net: number
  income_split: number
  by_category: CategorySummary[]
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function formatMonth(month: string): string {
  const [year, mo] = month.split('-').map(Number)
  return fmtDate(new Date(year, mo - 1, 1), { year: 'numeric', month: 'long' })
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

function currentMonth(): string {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
}

function todayDate(): string {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
}

function formatAmount(amount: number): string {
  return formatNumber(Math.abs(amount), {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

function formatTxDate(date: string): string {
  const [y, m, d] = date.split('-').map(Number)
  return fmtDate(new Date(y, m - 1, d), { day: 'numeric', month: 'short' })
}

// Returns Tailwind color classes based on budget usage percentage.
function progressColor(pct: number): string {
  if (pct >= 100) return 'bg-red-500'
  if (pct >= 80) return 'bg-yellow-500'
  return 'bg-green-500'
}

// ── Quick-add row ────────────────────────────────────────────────────────────

interface QuickAddRowProps {
  accounts: Account[]
  categories: Category[]
  onAdd: (t: Omit<Transaction, 'id'>) => Promise<void>
  onCancel: () => void
}

function QuickAddRow({ accounts, categories, onAdd, onCancel }: QuickAddRowProps) {
  const { t } = useTranslation('budget')
  const [description, setDescription] = useState('')
  const [amount, setAmount] = useState('')
  const [date, setDate] = useState(todayDate())
  const [accountId, setAccountId] = useState(accounts[0]?.id ?? 0)
  const [categoryId, setCategoryId] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    const parsed = parseFloat(amount.replace(',', '.'))
    if (!parsed || !accountId || !date) return
    setSaving(true)
    setSaveError(null)
    try {
      await onAdd({
        account_id: accountId,
        category_id: categoryId,
        amount: parsed,
        description,
        date,
        tags: [],
        is_transfer: false,
      })
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t('errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
    {saveError && (
      <div className="px-4 py-2 bg-red-900/40 text-red-300 text-xs border-t border-red-800">
        {saveError}
      </div>
    )}
    <form
      onSubmit={handleSubmit}
      className="grid grid-cols-[1fr_auto_auto_auto_auto_auto] gap-2 px-4 py-2 bg-gray-800 border-t border-gray-700 items-center text-sm"
    >
      <input
        className="bg-gray-700 text-white rounded px-2 py-1 outline-none focus:ring-1 focus:ring-blue-500 min-w-0"
        placeholder={t('quickAdd.description')}
        value={description}
        onChange={e => setDescription(e.target.value)}
        autoFocus
      />
      <input
        className="bg-gray-700 text-white rounded px-2 py-1 outline-none focus:ring-1 focus:ring-blue-500 w-28 text-right"
        placeholder={t('quickAdd.amount')}
        value={amount}
        onChange={e => setAmount(e.target.value)}
        aria-label={t('quickAdd.amount')}
      />
      <input
        type="date"
        className="bg-gray-700 text-white rounded px-2 py-1 outline-none focus:ring-1 focus:ring-blue-500 w-36"
        value={date}
        onChange={e => setDate(e.target.value)}
        aria-label={t('quickAdd.date')}
      />
      <select
        className="bg-gray-700 text-white rounded px-2 py-1 outline-none focus:ring-1 focus:ring-blue-500"
        value={categoryId ?? ''}
        onChange={e => setCategoryId(e.target.value ? Number(e.target.value) : null)}
        aria-label={t('quickAdd.category')}
      >
        <option value="">{t('quickAdd.noCategory')}</option>
        {categories.map(c => (
          <option key={c.id} value={c.id}>
            {c.icon} {c.name}
          </option>
        ))}
      </select>
      <select
        className="bg-gray-700 text-white rounded px-2 py-1 outline-none focus:ring-1 focus:ring-blue-500"
        value={accountId}
        onChange={e => setAccountId(Number(e.target.value))}
        aria-label={t('quickAdd.account')}
      >
        {accounts.map(a => (
          <option key={a.id} value={a.id}>
            {a.icon} {a.name}
          </option>
        ))}
      </select>
      <div className="flex gap-1">
        <button
          type="submit"
          disabled={saving}
          className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded px-3 py-1 text-sm"
        >
          {saving ? t('quickAdd.saving') : t('quickAdd.add')}
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="text-gray-400 hover:text-white rounded p-1"
          aria-label={t('quickAdd.cancel')}
        >
          <X size={16} />
        </button>
      </div>
    </form>
    </>
  )
}

// ── Category row with progress bar and inline budget editing ─────────────────

interface CategoryRowProps {
  cs: CategorySummary
  month: string
  onLimitSaved: () => void
}

function CategoryRow({ cs, month, onLimitSaved }: CategoryRowProps) {
  const { t } = useTranslation('budget')
  const [editing, setEditing] = useState(false)
  const [limitInput, setLimitInput] = useState(
    cs.budget_amount > 0 ? String(cs.budget_amount) : ''
  )
  const [prevBudgetAmount, setPrevBudgetAmount] = useState(cs.budget_amount)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  if (!editing && cs.budget_amount !== prevBudgetAmount) {
    setPrevBudgetAmount(cs.budget_amount)
    setLimitInput(cs.budget_amount > 0 ? String(cs.budget_amount) : '')
  }

  const handleSaveLimit = async () => {
    if (cs.category_id == null) return
    const raw = limitInput.trim()
    const amount = raw === '' ? 0 : parseFloat(raw.replace(',', '.'))
    if (isNaN(amount) || amount < 0) {
      setSaveError(t('errors.invalidAmount'))
      return
    }
    setSaving(true)
    setSaveError(null)
    try {
      const res = await fetch('/api/budget/limits', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          month,
          limits: [{ category_id: cs.category_id, amount }],
        }),
      })
      if (!res.ok) throw new Error(t('errors.saveFailed'))
      setEditing(false)
      onLimitSaved()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t('errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  const pct = Math.min(cs.budget_pct, 100)
  const hasBudget = cs.budget_amount > 0
  const spent = Math.abs(cs.total)
  const remaining = hasBudget ? cs.budget_amount - spent : null

  return (
    <li className="space-y-1 group">
      {saveError && (
        <div className="text-xs text-red-400 px-0 pb-0.5">{saveError}</div>
      )}
      {/* Top row: color dot, name, edit button, amount/budget */}
      <div className="flex items-center gap-2 text-sm">
        <span
          className="w-2.5 h-2.5 rounded-full shrink-0"
          style={{ backgroundColor: cs.color || '#6b7280' }}
        />
        <span className="flex-1 text-gray-200 truncate">
          {cs.category_name || t('noCategory')}
        </span>
        {cs.category_id != null && (
          editing ? (
            <div className="flex items-center gap-1">
              <input
                className="bg-gray-700 text-white rounded px-2 py-0.5 text-xs w-24 text-right outline-none focus:ring-1 focus:ring-blue-500"
                value={limitInput}
                onChange={e => setLimitInput(e.target.value)}
                placeholder={t('limits.placeholder')}
                aria-label={t('limits.setLimit')}
                autoFocus
                onKeyDown={e => {
                  if (e.key === 'Enter') void handleSaveLimit()
                  if (e.key === 'Escape') setEditing(false)
                }}
              />
              <button
                onClick={() => void handleSaveLimit()}
                disabled={saving}
                className="text-green-400 hover:text-green-300 disabled:opacity-50 p-0.5"
                aria-label={t('limits.save')}
              >
                <Check size={14} />
              </button>
              <button
                onClick={() => setEditing(false)}
                className="text-gray-400 hover:text-white p-0.5"
                aria-label={t('quickAdd.cancel')}
              >
                <X size={14} />
              </button>
            </div>
          ) : (
            <button
              onClick={() => setEditing(true)}
              className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100 text-gray-500 hover:text-blue-400 p-0.5 transition-opacity"
              aria-label={t('limits.setLimit')}
            >
              <Pencil size={12} />
            </button>
          )
        )}
        <div className="text-right shrink-0">
          <span
            className={`tabular-nums font-medium ${cs.is_income ? 'text-green-400' : cs.total < 0 ? 'text-red-400' : 'text-gray-300'}`}
          >
            {cs.total >= 0 ? '+' : '-'}{formatAmount(cs.total)}
          </span>
          {hasBudget && (
            <span className="text-gray-500 text-xs ml-1">
              / {formatAmount(cs.budget_amount)}
            </span>
          )}
        </div>
      </div>

      {/* Progress bar — only for expense categories with a budget set */}
      {hasBudget && !cs.is_income && (
        <div className="pl-5">
          <div className="h-1.5 bg-gray-700 rounded-full overflow-hidden">
            <div
              className={`h-full rounded-full transition-all ${progressColor(cs.budget_pct)}`}
              style={{ width: `${pct}%` }}
            />
          </div>
          <div className="flex justify-between text-xs text-gray-500 mt-0.5">
            <span>{Math.round(cs.budget_pct)}%</span>
            {remaining !== null && (
              <span className={remaining < 0 ? 'text-red-400' : 'text-gray-400'}>
                {remaining >= 0
                  ? t('limits.remaining', { amount: formatAmount(remaining) })
                  : t('limits.over', { amount: formatAmount(Math.abs(remaining)) })}
              </span>
            )}
          </div>
        </div>
      )}
    </li>
  )
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function BudgetPage() {
  const { t } = useTranslation('budget')
  const [month, setMonth] = useState(currentMonth())
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [transactions, setTransactions] = useState<Transaction[]>([])
  const [summary, setSummary] = useState<MonthlySummary | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showQuickAdd, setShowQuickAdd] = useState(false)
  const [deletingId, setDeletingId] = useState<number | null>(null)

  const loadData = useCallback(async (m: string, signal?: AbortSignal) => {
    setLoading(true)
    setError(null)
    try {
      const [acctRes, catRes, txnRes, sumRes] = await Promise.all([
        fetch('/api/budget/accounts', { credentials: 'include', signal }),
        fetch('/api/budget/categories', { credentials: 'include', signal }),
        fetch(`/api/budget/transactions?month=${m}`, { credentials: 'include', signal }),
        fetch(`/api/budget/summary?month=${m}`, { credentials: 'include', signal }),
      ])
      if (!acctRes.ok || !catRes.ok || !txnRes.ok || !sumRes.ok) {
        throw new Error(t('errors.loadFailed'))
      }
      const [acctData, catData, txnData, sumData] = await Promise.all([
        acctRes.json(),
        catRes.json(),
        txnRes.json(),
        sumRes.json(),
      ])
      setAccounts(acctData.accounts ?? [])
      setCategories(catData.categories ?? [])
      setTransactions(txnData.transactions ?? [])
      setSummary(sumData)
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('errors.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    void loadData(month, controller.signal)
    return () => { controller.abort() }
  }, [month, loadData])

  const handleAddTransaction = async (txn: Omit<Transaction, 'id'>) => {
    setError(null)
    const res = await fetch('/api/budget/transactions', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(txn),
    })
    if (!res.ok) throw new Error(t('errors.saveFailed'))
    setShowQuickAdd(false)
    await loadData(month)
  }

  const handleDelete = async (id: number) => {
    setDeletingId(id)
    try {
      const res = await fetch(`/api/budget/transactions/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(t('errors.deleteFailed'))
      setTransactions(prev => prev.filter(t => t.id !== id))
      await loadData(month)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.deleteFailed'))
    } finally {
      setDeletingId(null)
    }
  }

  const catById = new Map(categories.map(c => [c.id, c]))
  const acctById = new Map(accounts.map(a => [a.id, a]))

  // Running balance: sort oldest→newest and accumulate from 0, so each
  // transaction's balance reflects the sum of all transactions up to and including it.
  const getTransactionDateTime = (date: string) => {
    const [year, month, day] = date.split('-').map(Number)
    return Date.UTC(year, month - 1, day)
  }
  const transactionsOldestFirst = [...transactions].sort((a, b) => {
    const dateDiff = getTransactionDateTime(a.date) - getTransactionDateTime(b.date)
    return dateDiff !== 0 ? dateDiff : a.id - b.id
  })
  let runningBalance = 0
  const balanceByTransactionId = new Map<number, number>()
  for (const txn of transactionsOldestFirst) {
    runningBalance += txn.amount
    balanceByTransactionId.set(txn.id, runningBalance)
  }
  const withBalance = transactions.map(txn => ({
    txn,
    balance: balanceByTransactionId.get(txn.id) ?? 0,
  }))

  // Income vs expenses bar widths
  const incomeExpenseTotal = summary
    ? summary.income_total + summary.expense_total
    : 0
  const incomeBarPct = incomeExpenseTotal > 0 && summary
    ? (summary.income_total / incomeExpenseTotal) * 100
    : 0

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      {/* Header */}
      <div className="sticky top-0 z-10 bg-gray-900 border-b border-gray-800 px-4 py-3 flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t('title')}</h1>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setMonth(prevMonth(month))}
            className="p-1 rounded hover:bg-gray-700 text-gray-300"
            aria-label={t('nav.prev')}
          >
            <ChevronLeft size={20} />
          </button>
          <span className="text-sm font-medium min-w-36 text-center">
            {formatMonth(month)}
          </span>
          <button
            onClick={() => setMonth(nextMonth(month))}
            className="p-1 rounded hover:bg-gray-700 text-gray-300"
            aria-label={t('nav.next')}
          >
            <ChevronRight size={20} />
          </button>
        </div>
        <button
          onClick={() => setShowQuickAdd(v => !v)}
          className="flex items-center gap-1 bg-blue-600 hover:bg-blue-700 text-white rounded px-3 py-1 text-sm"
        >
          <Plus size={16} />
          {t('actions.add')}
        </button>
      </div>

      {/* Summary bar */}
      {summary && (
        <div className="px-4 py-4 bg-gray-800 border-b border-gray-700 space-y-3">
          {/* Income vs Expenses visual bar */}
          {incomeExpenseTotal > 0 && (
            <div>
              <div className="h-2 bg-red-500/60 rounded-full overflow-hidden">
                <div
                  className="h-full bg-green-500/80 rounded-full transition-all"
                  style={{ width: `${incomeBarPct}%` }}
                />
              </div>
              <div className="flex justify-between text-xs text-gray-500 mt-1">
                <span className="text-green-400">{t('summary.income')}: {formatAmount(summary.income_total)}</span>
                <span className="text-red-400">{t('summary.expenses')}: {formatAmount(summary.expense_total)}</span>
              </div>
            </div>
          )}
          {/* Net + income split */}
          <div className="grid grid-cols-2 gap-4">
            <div className="text-center">
              <p className="text-xs text-gray-400 uppercase tracking-wide">{t('summary.net')}</p>
              <p className={`text-lg font-semibold ${summary.net >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                {summary.net < 0 ? '-' : ''}{formatAmount(summary.net)}
              </p>
            </div>
            <div className="text-center">
              <p className="text-xs text-gray-400 uppercase tracking-wide">
                {t('summary.incomeSplit', { pct: summary.income_split })}
              </p>
            </div>
          </div>
        </div>
      )}

      {error && (
        <div className="px-4 py-3 bg-red-900/40 text-red-300 text-sm border-b border-red-800">
          {error}
        </div>
      )}

      {/* Quick-add row */}
      {showQuickAdd && accounts.length > 0 && (
        <QuickAddRow
          accounts={accounts}
          categories={categories}
          onAdd={handleAddTransaction}
          onCancel={() => setShowQuickAdd(false)}
        />
      )}
      {showQuickAdd && accounts.length === 0 && (
        <div className="px-4 py-3 bg-yellow-900/40 text-yellow-300 text-sm border-b border-yellow-800">
          {t('errors.noAccounts')}
        </div>
      )}

      {/* Category breakdown with progress bars */}
      {summary && summary.by_category.length > 0 && (
        <div className="px-4 py-4 border-b border-gray-800">
          <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide mb-3">
            {t('summary.byCategory')}
          </h2>
          <p className="text-xs text-gray-500 mb-3">{t('limits.hint')}</p>
          <ul className="space-y-3">
            {summary.by_category.map((cs) => (
              <CategoryRow
                key={cs.category_id ?? 'uncategorized'}
                cs={cs}
                month={month}
                onLimitSaved={() => void loadData(month)}
              />
            ))}
          </ul>
        </div>
      )}

      {/* Transaction list */}
      {loading ? (
        <div className="flex items-center justify-center py-16 text-gray-400">
          {t('loading')}
        </div>
      ) : transactions.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-gray-400 gap-3">
          <p>{t('empty')}</p>
          <button
            onClick={() => setShowQuickAdd(true)}
            className="flex items-center gap-1 text-blue-400 hover:text-blue-300 text-sm"
          >
            <Plus size={16} />
            {t('actions.add')}
          </button>
        </div>
      ) : (
        <>
          <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wide px-4 pt-4 pb-2">
            {t('summary.transactions')}
          </h2>
          <ul className="divide-y divide-gray-800">
            {withBalance.map(({ txn, balance }) => {
              const cat = txn.category_id != null ? catById.get(txn.category_id) : undefined
              const acct = acctById.get(txn.account_id)
              const isIncome = txn.amount > 0

              return (
                <li key={txn.id} className="flex items-center gap-3 px-4 py-3 hover:bg-gray-800/60 group">
                  {/* Date */}
                  <span className="w-14 text-xs text-gray-400 shrink-0">
                    {formatTxDate(txn.date)}
                  </span>

                  {/* Description + category badge */}
                  <div className="flex-1 min-w-0">
                    <p className="text-sm truncate">{txn.description || t('noDescription')}</p>
                    <div className="flex items-center gap-2 mt-0.5">
                      {cat && (
                        <span
                          className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded-full"
                          style={{
                            backgroundColor: (cat.color || '#6b7280') + '30',
                            color: cat.color || '#9ca3af',
                            border: `1px solid ${(cat.color || '#6b7280')}60`,
                          }}
                        >
                          {cat.icon} {cat.name}
                        </span>
                      )}
                      {acct && (
                        <span className="text-xs text-gray-500">
                          {acct.icon} {acct.name}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Amount */}
                  <div className="text-right shrink-0">
                    <p
                      className={`text-sm font-medium tabular-nums ${isIncome ? 'text-green-400' : 'text-red-400'}`}
                    >
                      {isIncome ? '+' : '-'}{formatAmount(txn.amount)}
                    </p>
                    <p className="text-xs text-gray-500 tabular-nums">
                      {t('summary.remaining')}: {balance < 0 ? '-' : ''}{formatAmount(balance)}
                    </p>
                  </div>

                  {/* Delete button */}
                  <button
                    onClick={() => handleDelete(txn.id)}
                    disabled={deletingId === txn.id}
                    className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100 text-gray-500 hover:text-red-400 focus-visible:text-red-400 transition-opacity disabled:opacity-30 p-1"
                    aria-label={t('actions.delete')}
                  >
                    <Trash2 size={16} />
                  </button>
                </li>
              )
            })}
          </ul>
        </>
      )}
    </div>
  )
}
