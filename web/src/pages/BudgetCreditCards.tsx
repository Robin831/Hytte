import { useState, useEffect, useCallback, useRef, type ChangeEvent } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { Link } from 'react-router-dom'
import { ChevronLeft, ChevronRight, Upload, X, Link2, CreditCard, Plus, Trash2, Settings, History } from 'lucide-react'
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

interface Group {
  id: number
  name: string
  sort_order: number
}

interface Transaction {
  id: number
  transaksjonsdato: string
  beskrivelse: string
  belop: number
  belop_i_valuta: number
  is_pending: boolean
  is_innbetaling: boolean
  group_id: number | null
  group_name: string
}

interface PreviewRow {
  line: number
  transaksjonsdato: string
  beskrivelse: string
  belop: number
  belop_i_valuta: number
  is_pending: boolean
  is_innbetaling: boolean
  error?: string
}

interface ImportPreview {
  new_count: number
  pending_resolve_count: number
  skipped_count: number
  rows: PreviewRow[]
}

interface MonthlyHistoryRowData {
  group_id: number | null
  group_name: string
  totals: Record<string, number>
}

interface MonthlyHistory {
  months: string[]
  rows: MonthlyHistoryRowData[]
  month_totals: Record<string, number>
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

function formatAmount(amount: number, currency = 'NOK'): string {
  return formatNumber(amount, {
    style: 'currency',
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

// ── Subcomponents ─────────────────────────────────────────────────────────────

interface GroupSectionProps {
  title: string
  transactions: Transaction[]
  groups: Group[]
  currency: string
  t: TFunction<'budget'>
  onAssign: (txId: number, groupId: number | null) => void
}

function GroupSection({ title, transactions, groups, currency, t, onAssign }: GroupSectionProps) {
  const subtotal = transactions
    .filter(tx => !tx.is_innbetaling)
    .reduce((sum, tx) => sum + Math.abs(tx.belop), 0)

  if (transactions.length === 0) return null

  return (
    <div className="bg-gray-800 rounded-lg overflow-hidden">
      <div className="flex items-center justify-between px-3 py-2 bg-gray-700 border-b border-gray-600">
        <span className="text-sm font-semibold text-gray-200">{title}</span>
        <span className="text-sm font-semibold text-red-400">
          {formatCurrency(subtotal, currency)}
        </span>
      </div>
      <div className="divide-y divide-gray-700/50">
        {transactions.map(tx => (
          <TransactionItem
            key={tx.id}
            tx={tx}
            groups={groups}
            currency={currency}
            t={t}
            onAssign={onAssign}
          />
        ))}
      </div>
    </div>
  )
}

interface TransactionItemProps {
  tx: Transaction
  groups: Group[]
  currency: string
  t: TFunction<'budget'>
  onAssign: (txId: number, groupId: number | null) => void
}

function TransactionItem({ tx, groups, currency, t, onAssign }: TransactionItemProps) {
  const showForeignAmount =
    tx.belop_i_valuta !== 0 &&
    Math.abs(Math.abs(tx.belop_i_valuta) - Math.abs(tx.belop)) > 0.01

  return (
    <div className="flex items-start gap-2 px-3 py-2">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5 flex-wrap">
          <span className={`text-sm truncate ${tx.is_innbetaling ? 'text-green-400' : 'text-gray-200'}`}>
            {tx.beskrivelse}
          </span>
          {tx.is_pending && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-yellow-900/50 text-yellow-300 border border-yellow-700/50 flex-shrink-0">
              {t('creditCards.pending')}
            </span>
          )}
          {tx.is_innbetaling && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-green-900/50 text-green-300 border border-green-700/50 flex-shrink-0">
              {t('creditCards.payment')}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 mt-0.5">
          <span className="text-xs text-gray-500">{tx.transaksjonsdato}</span>
          {showForeignAmount && (
            <span className="text-xs text-gray-500">
              ({formatNumber(tx.belop_i_valuta, { minimumFractionDigits: 2, maximumFractionDigits: 2 })})
            </span>
          )}
        </div>
      </div>
      <div className="flex items-center gap-2 flex-shrink-0">
        <span className={`text-sm font-medium ${tx.is_innbetaling ? 'text-green-400' : tx.belop < 0 ? 'text-red-300' : 'text-gray-300'}`}>
          {formatAmount(tx.belop, currency)}
        </span>
        <select
          aria-label={t('creditCards.moveToGroup')}
          value={tx.group_id ?? ''}
          onChange={e => {
            const val = e.target.value
            onAssign(tx.id, val === '' ? null : Number(val))
          }}
          className="text-xs bg-gray-700 border border-gray-600 rounded px-1.5 py-1 text-gray-300 hover:border-gray-500 focus:outline-none focus:border-blue-500 cursor-pointer"
        >
          <option value="">{t('creditCards.noGroup')}</option>
          {groups.map(g => (
            <option key={g.id} value={g.id}>{g.name}</option>
          ))}
        </select>
      </div>
    </div>
  )
}

// ── Import Preview Modal ───────────────────────────────────────────────────────

interface ImportPreviewModalProps {
  preview: ImportPreview
  currency: string
  confirming: boolean
  error: string | null
  onConfirm: () => void
  onCancel: () => void
}

function ImportPreviewModal({ preview, currency, confirming, error, onConfirm, onCancel }: ImportPreviewModalProps) {
  const { t } = useTranslation('budget')

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onCancel])

  const newRows = preview.rows.filter(r => !r.error)

  return (
    <div
      className="fixed inset-0 z-50 flex items-end sm:items-center justify-center p-0 sm:p-4"
      role="dialog"
      aria-modal="true"
      aria-label={t('creditCards.importPreview.title')}
    >
      <div
        className="absolute inset-0 bg-black/60"
        onClick={onCancel}
        aria-hidden="true"
      />
      <div className="relative z-10 w-full sm:max-w-lg bg-gray-800 border border-gray-700 rounded-t-2xl sm:rounded-xl shadow-2xl flex flex-col max-h-[90vh]">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-700 flex-shrink-0">
          <h2 className="text-base font-semibold text-white">
            {t('creditCards.importPreview.title')}
          </h2>
          <button
            type="button"
            onClick={onCancel}
            className="p-1 rounded hover:bg-gray-700 text-gray-400"
            aria-label={t('creditCards.importPreview.cancel')}
          >
            <X size={18} />
          </button>
        </div>

        {/* Summary */}
        <div className="px-4 py-3 bg-gray-700 border-b border-gray-600 flex-shrink-0 flex gap-4">
          <span className="text-sm text-green-400 font-medium">
            {t('creditCards.importPreview.new', { count: preview.new_count })}
          </span>
          {preview.pending_resolve_count > 0 && (
            <span className="text-sm text-blue-400 font-medium">
              {t('creditCards.importPreview.pendingResolve', { count: preview.pending_resolve_count })}
            </span>
          )}
          {preview.skipped_count > 0 && (
            <span className="text-sm text-gray-400">
              {t('creditCards.importPreview.skipped', { count: preview.skipped_count })}
            </span>
          )}
        </div>

        {/* Transaction list */}
        <div className="overflow-y-auto flex-1 divide-y divide-gray-700/50">
          {newRows.length === 0 && (
            <p className="text-gray-400 text-sm text-center py-6">
              {t('creditCards.importPreview.empty')}
            </p>
          )}
          {newRows.map(row => (
            <div key={row.line} className="flex items-start gap-2 px-4 py-2">
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-1.5 flex-wrap">
                  <span className="text-sm text-gray-200 truncate">{row.beskrivelse}</span>
                  {row.is_pending && (
                    <span className="text-xs bg-yellow-900/50 text-yellow-300 border border-yellow-700/50 px-1.5 py-0.5 rounded flex-shrink-0">
                      {t('creditCards.pending')}
                    </span>
                  )}
                  {row.is_innbetaling && (
                    <span className="text-xs bg-green-900/50 text-green-300 border border-green-700/50 px-1.5 py-0.5 rounded flex-shrink-0">
                      {t('creditCards.payment')}
                    </span>
                  )}
                </div>
                <span className="text-xs text-gray-500">{row.transaksjonsdato}</span>
              </div>
              <span className={`text-sm font-medium flex-shrink-0 ${row.is_innbetaling ? 'text-green-400' : 'text-red-300'}`}>
                {formatAmount(row.belop, currency)}
              </span>
            </div>
          ))}
        </div>

        {/* Error */}
        {error && (
          <div className="px-4 py-2 bg-red-900/30 border-t border-red-700/50 text-red-300 text-sm flex-shrink-0">
            {error}
          </div>
        )}

        {/* Actions */}
        <div className="flex gap-3 p-4 border-t border-gray-700 flex-shrink-0">
          <button
            type="button"
            onClick={onCancel}
            disabled={confirming}
            className="flex-1 px-4 py-2.5 rounded-lg text-sm font-medium text-gray-300 bg-gray-700 hover:bg-gray-600 transition-colors disabled:opacity-50"
          >
            {t('creditCards.importPreview.cancel')}
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={confirming || newRows.length === 0}
            className="flex-1 px-4 py-2.5 rounded-lg text-sm font-medium text-white bg-blue-600 hover:bg-blue-500 transition-colors disabled:opacity-50"
          >
            {confirming
              ? t('creditCards.importing')
              : t('creditCards.importPreview.confirm', { count: newRows.length })}
          </button>
        </div>
      </div>
    </div>
  )
}

// ── Monthly History View ───────────────────────────────────────────────────────

interface MonthlyHistoryViewProps {
  creditCardId: string
  currency: string
  t: TFunction<'budget'>
}

function MonthlyHistoryView({ creditCardId, currency, t }: MonthlyHistoryViewProps) {
  const [history, setHistory] = useState<MonthlyHistory | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const ctrl = new AbortController()
    setLoading(true)
    setError(null)
    fetch(`/api/credit-card/monthly-history?credit_card_id=${encodeURIComponent(creditCardId)}&months=6`, {
      credentials: 'include',
      signal: ctrl.signal,
    })
      .then(r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json()
      })
      .then((data: MonthlyHistory) => setHistory(data))
      .catch(err => {
        const isAbortError = err instanceof DOMException && err.name === 'AbortError'
        if (!isAbortError) setError(t('creditCards.history.loadFailed'))
      })
      .finally(() => { if (!ctrl.signal.aborted) setLoading(false) })
    return () => ctrl.abort()
  }, [creditCardId, t])

  if (loading) return <div className="p-4 text-gray-400 text-sm">{t('loading')}</div>
  if (error) return <div className="bg-red-900/40 border border-red-700 text-red-300 text-sm rounded px-3 py-2">{error}</div>
  if (!history) return null

  const { months, rows, month_totals } = history

  return (
    <div className="overflow-x-auto rounded-lg border border-gray-700">
      <table className="w-full text-sm border-collapse min-w-max">
        <thead>
          <tr className="bg-gray-700 text-gray-300">
            <th className="text-left px-3 py-2 font-semibold sticky left-0 bg-gray-700 z-10 min-w-[120px]">
              {t('creditCards.history.group')}
            </th>
            {months.map(m => (
              <th key={m} className="text-right px-3 py-2 font-semibold whitespace-nowrap">
                {formatMonth(m)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-700/50">
          {rows.map((row, idx) => (
            <tr
              key={row.group_id ?? `unnamed-${idx}`}
              className="hover:bg-gray-700/30 transition-colors"
            >
              <td className="px-3 py-2 text-gray-200 font-medium sticky left-0 bg-gray-800 z-10">
                {row.group_name || t('creditCards.noGroup')}
              </td>
              {months.map(m => {
                const val = row.totals[m] ?? 0
                return (
                  <td key={m} className="px-3 py-2 text-right tabular-nums">
                    {val > 0
                      ? <span className="text-red-300">{formatCurrency(val, currency)}</span>
                      : <span className="text-gray-600">—</span>
                    }
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr className="border-t-2 border-gray-600 bg-gray-700/50 font-semibold">
            <td className="px-3 py-2 text-gray-200 sticky left-0 bg-gray-700/80 z-10">
              {t('creditCards.monthlyTotal')}
            </td>
            {months.map(m => {
              const val = month_totals[m] ?? 0
              return (
                <td key={m} className="px-3 py-2 text-right tabular-nums text-red-400">
                  {val > 0 ? formatCurrency(val, currency) : <span className="text-gray-600">—</span>}
                </td>
              )
            })}
          </tr>
        </tfoot>
      </table>
    </div>
  )
}

// ── Main Component ─────────────────────────────────────────────────────────────

export default function BudgetCreditCards() {
  const { t } = useTranslation('budget')

  const [month, setMonth] = useState(currentMonth)
  const [accounts, setAccounts] = useState<Account[]>([])
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [summary, setSummary] = useState<CreditCardSummary | null>(null)
  const [loadingAccounts, setLoadingAccounts] = useState(true)
  const setLoadingSummary = useCallback((_loading: boolean) => {}, [])
  const [error, setError] = useState<string | null>(null)

  // Transactions & groups
  const [transactions, setTransactions] = useState<Transaction[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [loadingGroups, setLoadingGroups] = useState(true)
  const [loadingTxns, setLoadingTxns] = useState(false)
  const [variableBillName, setVariableBillName] = useState<string | null>(null)
  const [variableBillAmount, setVariableBillAmount] = useState(0)
  const [txnsError, setTxnsError] = useState<string | null>(null)

  // Import state
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [importPreview, setImportPreview] = useState<ImportPreview | null>(null)
  const [importLoading, setImportLoading] = useState(false)
  const [importConfirming, setImportConfirming] = useState(false)
  const [importError, setImportError] = useState<string | null>(null)
  const [importDoneCount, setImportDoneCount] = useState<number | null>(null)

  // Tab state
  const [activeTab, setActiveTab] = useState<'transactions' | 'history'>('transactions')

  // Group management state
  const [showGroupMgmt, setShowGroupMgmt] = useState(false)
  const [newGroupName, setNewGroupName] = useState('')
  const [addingGroup, setAddingGroup] = useState(false)

  // Re-apply rules state
  const reapplyingRef = useRef(false)
  const [reapplying, setReapplying] = useState(false)
  const [reapplyResult, setReapplyResult] = useState<{ count: number } | null>(null)
  const currentSelectedIdRef = useRef(selectedId)
  const currentMonthRef = useRef(month)
  useEffect(() => { currentSelectedIdRef.current = selectedId }, [selectedId])
  useEffect(() => { currentMonthRef.current = month }, [month])
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset result when selection changes
    setReapplyResult(null)
  }, [selectedId, month, showGroupMgmt])

  // Load credit card accounts on mount
  useEffect(() => {
    const ctrl = new AbortController()
    fetch('/api/budget/accounts', { credentials: 'include', signal: ctrl.signal })
      .then(async r => {
        if (!r.ok) throw new Error(`Failed to load accounts: ${r.status}`)
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
        const isAbortError = err instanceof DOMException && err.name === 'AbortError'
        if (!isAbortError) setError(t('errors.loadFailed'))
      })
      .finally(() => { if (!ctrl.signal.aborted) setLoadingAccounts(false) })
    return () => ctrl.abort()
  }, [t])

  // Load groups on mount
  useEffect(() => {
    const ctrl = new AbortController()
    fetch('/api/credit-card/groups', { credentials: 'include', signal: ctrl.signal })
      .then(r => r.ok ? r.json() : Promise.reject(r.status))
      .then((data: Group[]) => setGroups(Array.isArray(data) ? data : []))
      .catch(err => {
        const isAbortError = err instanceof DOMException && err.name === 'AbortError'
        if (!isAbortError) console.error('Failed to load groups:', err)
      })
      .finally(() => { if (!ctrl.signal.aborted) setLoadingGroups(false) })
    return () => ctrl.abort()
  }, [])

  // Load credit card summary when account or month changes
  const loadSummary = useCallback((accountId: number, m: string) => {
    const ctrl = new AbortController()
    setLoadingSummary(true)
    setSummary(null)
    setError(null)
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
        const isAbortError = err instanceof DOMException && err.name === 'AbortError'
        if (!isAbortError) setError(t('creditCards.errors.loadFailed'))
      })
      .finally(() => { if (!ctrl.signal.aborted) setLoadingSummary(false) })
    return () => ctrl.abort()
  }, [t])

  // Load transactions when account or month changes
  const loadTransactions = useCallback((accountId: number, m: string) => {
    const ctrl = new AbortController()
    setLoadingTxns(true)
    setTxnsError(null)
    setTransactions([])
    setVariableBillName(null)
    setVariableBillAmount(0)
    const cardId = String(accountId)
    fetch(`/api/credit-card/transactions?credit_card_id=${encodeURIComponent(cardId)}&month=${m}`, {
      credentials: 'include',
      signal: ctrl.signal,
    })
      .then(r => {
        if (!r.ok) throw new Error('failed')
        return r.json()
      })
      .then(data => {
        setTransactions(Array.isArray(data.transactions) ? data.transactions : [])
        setVariableBillName(data.variable_bill_name || null)
        setVariableBillAmount(data.variable_bill_amount || 0)
      })
      .catch(err => {
        const isAbortError = err instanceof DOMException && err.name === 'AbortError'
        if (!isAbortError) setTxnsError(t('creditCards.errors.loadTransactionsFailed'))
      })
      .finally(() => { if (!ctrl.signal.aborted) setLoadingTxns(false) })
    return () => ctrl.abort()
  }, [t])

  useEffect(() => {
    if (selectedId !== null) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch
      const cleanupSummary = loadSummary(selectedId, month)
      const cleanupTxns = loadTransactions(selectedId, month)
      setImportDoneCount(null)
      return () => {
        cleanupSummary()
        cleanupTxns()
      }
    }
  }, [selectedId, month, loadSummary, loadTransactions])

  // ── Import handlers ────────────────────────────────────────────────────────

  const handleFileChange = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!e.target.files) return
    // Reset input so same file can be re-selected
    e.target.value = ''

    if (!file || selectedId === null) return
    const cardId = String(selectedId)

    setImportLoading(true)
    setImportError(null)
    setImportPreview(null)
    setImportDoneCount(null)

    try {
      const form = new FormData()
      form.append('file', file)
      form.append('credit_card_id', cardId)

      const r = await fetch('/api/credit-card/import/preview', {
        method: 'POST',
        credentials: 'include',
        body: form,
      })
      if (!r.ok) {
        const body = await r.json().catch(() => ({}))
        throw new Error(body.error || `HTTP ${r.status}`)
      }
      const data: ImportPreview = await r.json()
      setImportPreview(data)
    } catch (err) {
      const msg = err instanceof Error ? err.message : ''
      setImportError(t('creditCards.errors.importPreviewFailed') + (msg ? ` (${msg})` : ''))
    } finally {
      setImportLoading(false)
    }
  }

  const handleImportConfirm = async () => {
    if (!importPreview || selectedId === null) return
    const cardId = String(selectedId)
    const newRows = importPreview.rows.filter(r => !r.error)
    if (newRows.length === 0) return

    setImportConfirming(true)
    setImportError(null)

    try {
      const r = await fetch('/api/credit-card/import/confirm', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ credit_card_id: cardId, rows: newRows }),
      })
      if (!r.ok) {
        const body = await r.json().catch(() => ({}))
        throw new Error(body.error || `HTTP ${r.status}`)
      }
      const data = await r.json()
      const count = typeof data.imported === 'number' ? data.imported : newRows.length
      setImportPreview(null)
      setImportDoneCount(count)
      // Reload transactions and summary
      loadTransactions(selectedId, month)
      loadSummary(selectedId, month)
      // Reload groups in case EnsureDefaultGroup ran
      fetch('/api/credit-card/groups', { credentials: 'include' })
        .then(res => res.ok ? res.json() : Promise.reject())
        .then((data: Group[]) => setGroups(Array.isArray(data) ? data : []))
        .catch(() => {})
    } catch (err) {
      const msg = err instanceof Error ? err.message : ''
      setImportError(t('creditCards.errors.importConfirmFailed') + (msg ? ` (${msg})` : ''))
    } finally {
      setImportConfirming(false)
    }
  }

  // ── Group reassignment ─────────────────────────────────────────────────────

  const handleAssignGroup = useCallback(async (txId: number, groupId: number | null) => {
    // Find the transaction to get its description for the merchant rule.
    const tx = transactions.find(t => t.id === txId)

    // Optimistic update
    setTransactions(prev => prev.map(t => {
      if (t.id !== txId) return t
      const group = groups.find(g => g.id === groupId)
      return { ...t, group_id: groupId, group_name: group?.name ?? '' }
    }))

    try {
      const r = await fetch('/api/credit-card/transactions/bulk-assign', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ transaction_ids: [txId], group_id: groupId }),
      })
      if (!r.ok) throw new Error('failed')

      // Auto-create a merchant rule so future imports of this merchant
      // land in the same group. Extract a generic pattern from the
      // description (first part before * or whitespace-heavy suffixes).
      if (tx && groupId) {
        let pattern = tx.beskrivelse
        // Strip "Reservert - " prefix
        pattern = pattern.replace(/^Reservert\s*-\s*/i, '')
        // Use text before * as the pattern (e.g. "Kindle Svcs" from "Kindle Svcs*BD9Q71P22")
        const starIdx = pattern.indexOf('*')
        if (starIdx > 2) pattern = pattern.substring(0, starIdx)
        // Trim trailing whitespace/junk
        pattern = pattern.trim()
        if (pattern.length > 2) {
          // Fire-and-forget — don't block the UI on rule creation.
          fetch('/api/credit-card/rules', {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ merchant_pattern: pattern, group_id: groupId }),
          }).catch(() => {})
        }
      }
    } catch {
      // Revert optimistic update on failure
      setTxnsError(t('creditCards.errors.assignFailed'))
      if (selectedId !== null) loadTransactions(selectedId, month)
    }
  }, [groups, transactions, t, selectedId, month, loadTransactions])

  // ── Render ─────────────────────────────────────────────────────────────────

  if (loadingAccounts) {
    return <div className="p-6 text-gray-400 text-sm">{t('loading')}</div>
  }

  if (accounts.length === 0) {
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

  const selectedAccount = accounts.find(a => a.id === selectedId) ?? accounts[0]
  const usedPct = summary && summary.credit_limit > 0
    ? Math.min(100, (summary.used_amount / summary.credit_limit) * 100)
    : 0
  const usedColor = usedPct >= 90 ? 'bg-red-500' : usedPct >= 70 ? 'bg-yellow-500' : 'bg-blue-500'

  // ── Group management ───────────────────────────────────────────────────────

  async function reloadGroups() {
    try {
      const res = await fetch('/api/credit-card/groups', { credentials: 'include' })
      if (res.ok) {
        const data = await res.json() as Group[]
        setGroups(Array.isArray(data) ? data : [])
      }
    } catch { /* ignore */ }
  }

  async function handleAddGroup() {
    if (!newGroupName.trim()) return
    setAddingGroup(true)
    try {
      const res = await fetch('/api/credit-card/groups', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newGroupName.trim() }),
      })
      if (!res.ok) throw new Error('failed')
      setNewGroupName('')
      await reloadGroups()
    } catch {
      setError(t('creditCards.errors.groupSaveFailed', { defaultValue: 'Failed to save group.' }))
    } finally {
      setAddingGroup(false)
    }
  }

  async function handleDeleteGroup(id: number) {
    try {
      const res = await fetch(`/api/credit-card/groups/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('failed')
      await reloadGroups()
      if (selectedId !== null) loadTransactions(selectedId, month)
    } catch {
      setError(t('creditCards.errors.groupDeleteFailed', { defaultValue: 'Failed to delete group.' }))
    }
  }

  async function handleReapplyRules() {
    if (reapplyingRef.current || selectedId === null) return
    reapplyingRef.current = true
    setReapplying(true)
    setReapplyResult(null)
    setError(null)
    const capturedId = selectedId
    const capturedMonth = month
    try {
      const res = await fetch('/api/credit-card/transactions/reapply-rules', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ credit_card_id: String(capturedId) }),
      })
      if (!res.ok) throw new Error('failed')
      const data = await res.json() as { updated: number }
      if (capturedId === currentSelectedIdRef.current && capturedMonth === currentMonthRef.current) {
        setReapplyResult({ count: data.updated })
        if (data.updated > 0) loadTransactions(capturedId, capturedMonth)
      }
    } catch {
      setError(t('creditCards.errors.reapplyRulesFailed'))
    } finally {
      reapplyingRef.current = false
      setReapplying(false)
    }
  }

  // Build grouped transactions
  const diverseGroup = groups.find(g => g.name === 'Diverse')
  const namedGroups = groups.filter(g => g.name !== 'Diverse').sort((a, b) => a.sort_order - b.sort_order)

  const byGroupId = new Map<number | null, Transaction[]>()
  for (const tx of transactions) {
    const key = tx.group_id
    if (!byGroupId.has(key)) byGroupId.set(key, [])
    byGroupId.get(key)!.push(tx)
  }

  // Diverse catch-all: transactions in Diverse group + unassigned
  const diverseTxns: Transaction[] = [
    ...(diverseGroup ? (byGroupId.get(diverseGroup.id) ?? []) : []),
    ...(byGroupId.get(null) ?? []),
  ]

  const expenseTotal = transactions
    .filter(tx => !tx.is_innbetaling)
    .reduce((sum, tx) => sum + Math.abs(tx.belop), 0)

  // Groups to show in dropdown for reassignment (include Diverse so any
  // persisted group_id always has a matching option in the controlled select)
  const allGroupOptions = [...groups].sort((a, b) => a.sort_order - b.sort_order)

  return (
    <div className="max-w-2xl mx-auto p-4 space-y-4">
      {/* Header */}
      <div className="flex items-center gap-2">
        <Link to="/budget" className="text-gray-400 hover:text-white p-1">
          <ChevronLeft size={20} />
        </Link>
        <h1 className="text-lg font-semibold flex-1">{t('creditCards.title')}</h1>
        <button
          type="button"
          onClick={() => fileInputRef.current?.click()}
          disabled={importLoading}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-blue-600 hover:bg-blue-500 text-white transition-colors disabled:opacity-60"
        >
          <Upload size={15} />
          {importLoading ? t('creditCards.importing') : t('creditCards.import')}
        </button>
        <input
          ref={fileInputRef}
          type="file"
          accept=".csv,text/csv"
          className="hidden"
          onChange={handleFileChange}
          aria-label={t('creditCards.import')}
        />
      </div>

      {/* Group management toggle */}
      <div>
        <button
          onClick={() => setShowGroupMgmt(prev => !prev)}
          className="flex items-center gap-1.5 text-sm text-gray-400 hover:text-white transition-colors"
        >
          <Settings size={14} />
          {t('creditCards.manageGroups', { defaultValue: 'Manage groups' })}
        </button>

        {showGroupMgmt && (
          <div className="mt-2 bg-gray-800 rounded-lg p-3 space-y-2">
            {groups.map(g => (
              <div key={g.id} className="flex items-center justify-between text-sm">
                <span className="text-white">{g.name}</span>
                {g.name !== 'Diverse' && (
                  <button
                    onClick={() => void handleDeleteGroup(g.id)}
                    className="text-gray-500 hover:text-red-400 transition-colors"
                  >
                    <Trash2 size={14} />
                  </button>
                )}
              </div>
            ))}
            <div className="flex items-center gap-2 pt-1">
              <input
                type="text"
                value={newGroupName}
                onChange={e => setNewGroupName(e.target.value)}
                placeholder={t('creditCards.newGroupName', { defaultValue: 'New group name' })}
                className="flex-1 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-sm text-white"
                onKeyDown={e => { if (e.key === 'Enter') void handleAddGroup() }}
              />
              <button
                onClick={() => void handleAddGroup()}
                disabled={addingGroup || !newGroupName.trim()}
                className="px-2 py-1 bg-blue-600 hover:bg-blue-500 rounded text-sm disabled:opacity-50"
              >
                <Plus size={14} />
              </button>
            </div>
            <div className="pt-1 border-t border-gray-700">
              <button
                onClick={() => void handleReapplyRules()}
                disabled={reapplying || selectedId === null}
                className="text-sm text-blue-400 hover:text-blue-300 transition-colors disabled:opacity-50"
              >
                {reapplying ? t('creditCards.reapplyingRules') : t('creditCards.reapplyRules')}
              </button>
              {reapplyResult !== null && (
                <span className="ml-3 text-xs text-gray-400">
                  {reapplyResult.count === 0
                    ? t('creditCards.reapplyNone')
                    : t('creditCards.reapplyDone', { count: reapplyResult.count })}
                </span>
              )}
            </div>
          </div>
        )}
      </div>

      {error && (
        <div className="bg-red-900/40 border border-red-700 text-red-300 text-sm rounded px-3 py-2">{error}</div>
      )}

      {importError && !importPreview && (
        <div className="bg-red-900/40 border border-red-700 text-red-300 text-sm rounded px-3 py-2">{importError}</div>
      )}

      {importDoneCount !== null && (
        <div className="bg-green-900/40 border border-green-700 text-green-300 text-sm rounded px-3 py-2">
          {t('creditCards.importDone', { count: importDoneCount })}
        </div>
      )}

      {/* Account selector */}
      {accounts.length > 1 && (
        <div className="flex gap-2 flex-wrap">
          {accounts.map(a => (
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

      {/* Tab selector */}
      <div className="flex gap-1 bg-gray-800 rounded-lg p-1 self-start">
        <button
          type="button"
          onClick={() => setActiveTab('transactions')}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium transition-colors ${
            activeTab === 'transactions'
              ? 'bg-blue-600 text-white'
              : 'text-gray-400 hover:text-white'
          }`}
        >
          <CreditCard size={14} />
          {t('creditCards.tabs.transactions')}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('history')}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium transition-colors ${
            activeTab === 'history'
              ? 'bg-blue-600 text-white'
              : 'text-gray-400 hover:text-white'
          }`}
        >
          <History size={14} />
          {t('creditCards.tabs.history')}
        </button>
      </div>

      {/* Credit limit overview card */}
      {summary && (
        <div className="bg-gray-800 rounded-lg p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              {selectedAccount.icon
                ? <span className="text-xl">{selectedAccount.icon}</span>
                : <CreditCard size={20} className="text-gray-400" />
              }
              <span className="font-semibold text-white">{selectedAccount.name}</span>
            </div>
            <span className="text-xs text-gray-400 uppercase tracking-wide">
              {t('accounts.types.credit')}
            </span>
          </div>

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

      {/* Transactions tab */}
      {activeTab === 'transactions' && (
        <>
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

          {/* Variable bill sync badge */}
          {variableBillName && (
            <div className="flex items-center gap-2 px-3 py-2 bg-blue-900/30 border border-blue-700/50 rounded-lg text-sm text-blue-300">
              <Link2 size={14} className="flex-shrink-0" />
              <span>
                {t('creditCards.variableBill', {
                  name: variableBillName,
                  amount: formatCurrency(variableBillAmount, selectedAccount.currency),
                })}
              </span>
            </div>
          )}

          {/* Transactions */}
          {loadingTxns && (
            <div className="text-gray-400 text-sm">{t('loading')}</div>
          )}

          {txnsError && !loadingTxns && (
            <div className="bg-red-900/40 border border-red-700 text-red-300 text-sm rounded px-3 py-2">{txnsError}</div>
          )}

          {!loadingTxns && !txnsError && (
            <div className="space-y-3">
              {transactions.length === 0 && (
                <p className="text-gray-500 text-sm text-center py-4">{t('creditCards.noTransactions')}</p>
              )}

              {transactions.length > 0 && loadingGroups && (
                <div className="text-gray-400 text-sm">{t('loading')}</div>
              )}

              {transactions.length > 0 && !loadingGroups && (
                <>
                  {/* Named groups */}
                  {namedGroups.map(g => {
                    const txns = byGroupId.get(g.id) ?? []
                    return (
                      <GroupSection
                        key={g.id}
                        title={g.name}
                        transactions={txns}
                        groups={allGroupOptions}
                        currency={selectedAccount.currency}
                        t={t}
                        onAssign={handleAssignGroup}
                      />
                    )
                  })}

                  {/* Diverse catch-all */}
                  {diverseTxns.length > 0 && (
                    <GroupSection
                      title={t('creditCards.diverse')}
                      transactions={diverseTxns}
                      groups={allGroupOptions}
                      currency={selectedAccount.currency}
                      t={t}
                      onAssign={handleAssignGroup}
                    />
                  )}

                  {/* Monthly total */}
                  <div className="flex items-center justify-between px-3 py-2 border-t border-gray-700">
                    <span className="text-sm font-semibold text-gray-300">{t('creditCards.monthlyTotal')}</span>
                    <span className="text-sm font-semibold text-red-400">
                      {formatCurrency(expenseTotal, selectedAccount.currency)}
                    </span>
                  </div>
                </>
              )}
            </div>
          )}
        </>
      )}

      {/* History tab */}
      {activeTab === 'history' && (
        <MonthlyHistoryView
          creditCardId={String(selectedAccount.id)}
          currency={selectedAccount.currency}
          t={t}
        />
      )}

      {/* Import preview modal */}
      {importPreview && (
        <ImportPreviewModal
          preview={importPreview}
          currency={selectedAccount.currency}
          confirming={importConfirming}
          error={importError}
          onConfirm={handleImportConfirm}
          onCancel={() => {
            setImportPreview(null)
            setImportError(null)
          }}
        />
      )}
    </div>
  )
}
