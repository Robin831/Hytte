import { useState, useEffect, useCallback, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Plus, Trash2, Pencil, X, ArrowLeftRight, ChevronLeft } from 'lucide-react'
import { formatNumber } from '../utils/formatDate'

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

interface AccountForm {
  name: string
  type: string
  currency: string
  icon: string
  balance: string
  credit_limit: string
}

interface TransferForm {
  from_id: number
  to_id: number
  amount: string
  description: string
  date: string
}

// ── Helpers ──────────────────────────────────────────────────────────────────

const ACCOUNT_TYPES = ['checking', 'savings', 'credit', 'cash'] as const

function todayDate(): string {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
}

function blankForm(): AccountForm {
  return { name: '', type: 'checking', currency: 'NOK', icon: '🏦', balance: '0', credit_limit: '0' }
}

function accountToForm(a: Account): AccountForm {
  return { name: a.name, type: a.type, currency: a.currency, icon: a.icon, balance: String(a.balance), credit_limit: String(a.credit_limit ?? 0) }
}

function formatBalance(amount: number, currency: string): string {
  return formatNumber(amount, {
    style: 'currency',
    currency,
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

// ── Account form ─────────────────────────────────────────────────────────────

interface AccountFormPanelProps {
  form: AccountForm
  onChange: <K extends keyof AccountForm>(key: K, value: AccountForm[K]) => void
  onSubmit: (e: FormEvent) => void
  onCancel: () => void
  saving: boolean
  error: string | null
  isNew: boolean
}

function AccountFormPanel({ form, onChange, onSubmit, onCancel, saving, error, isNew }: AccountFormPanelProps) {
  const { t } = useTranslation('budget')

  return (
    <form onSubmit={onSubmit} className="bg-gray-800 border-b border-gray-700 px-4 py-4 space-y-3">
      <h2 className="text-sm font-semibold text-gray-200">
        {isNew ? t('accounts.newAccount') : t('accounts.edit')}
      </h2>
      {error && (
        <div className="text-xs text-red-400 bg-red-900/30 rounded px-2 py-1">{error}</div>
      )}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div className="col-span-2 sm:col-span-1">
          <label className="block text-xs text-gray-400 mb-1" htmlFor="acct-icon">
            {t('accounts.icon')}
          </label>
          <input
            id="acct-icon"
            className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500 text-center text-xl"
            value={form.icon}
            onChange={e => onChange('icon', e.target.value)}
            maxLength={4}
            aria-label={t('accounts.icon')}
          />
        </div>
        <div className="col-span-2 sm:col-span-1">
          <label className="block text-xs text-gray-400 mb-1" htmlFor="acct-name">
            {t('accounts.name')}
          </label>
          <input
            id="acct-name"
            className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500"
            placeholder={t('accounts.namePlaceholder')}
            value={form.name}
            onChange={e => onChange('name', e.target.value)}
            required
            autoFocus
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="acct-type">
            {t('accounts.type')}
          </label>
          <select
            id="acct-type"
            className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500"
            value={form.type}
            onChange={e => onChange('type', e.target.value)}
          >
            {ACCOUNT_TYPES.map(typ => (
              <option key={typ} value={typ}>
                {t(`accounts.types.${typ}`)}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="acct-balance">
            {isNew ? t('accounts.initialBalance') : t('accounts.balance')}
          </label>
          <input
            id="acct-balance"
            className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500 text-right"
            value={form.balance}
            onChange={e => onChange('balance', e.target.value)}
            aria-label={isNew ? t('accounts.initialBalance') : t('accounts.balance')}
          />
        </div>
        {form.type === 'credit' && (
          <div>
            <label className="block text-xs text-gray-400 mb-1" htmlFor="acct-credit-limit">
              {t('accounts.creditLimit')}
            </label>
            <input
              id="acct-credit-limit"
              type="number"
              min="0"
              step="1000"
              className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500 text-right"
              value={form.credit_limit}
              onChange={e => onChange('credit_limit', e.target.value)}
              aria-label={t('accounts.creditLimit')}
            />
          </div>
        )}
      </div>
      <div className="flex gap-2 justify-end">
        <button
          type="button"
          onClick={onCancel}
          className="text-gray-400 hover:text-white text-sm px-3 py-1.5 rounded"
        >
          {t('quickAdd.cancel')}
        </button>
        <button
          type="submit"
          disabled={saving}
          className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded"
        >
          {saving ? t('quickAdd.saving') : t('accounts.save')}
        </button>
      </div>
    </form>
  )
}

// ── Transfer form ─────────────────────────────────────────────────────────────

interface TransferFormPanelProps {
  accounts: Account[]
  form: TransferForm
  onChange: <K extends keyof TransferForm>(key: K, value: TransferForm[K]) => void
  onSubmit: (e: FormEvent) => void
  onCancel: () => void
  saving: boolean
  error: string | null
}

function TransferFormPanel({ accounts, form, onChange, onSubmit, onCancel, saving, error }: TransferFormPanelProps) {
  const { t } = useTranslation('budget')

  return (
    <form onSubmit={onSubmit} className="bg-gray-800 border-b border-gray-700 px-4 py-4 space-y-3">
      <h2 className="text-sm font-semibold text-gray-200">{t('accounts.transferTitle')}</h2>
      {error && (
        <div className="text-xs text-red-400 bg-red-900/30 rounded px-2 py-1">{error}</div>
      )}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="transfer-from">
            {t('accounts.transferFrom')}
          </label>
          <select
            id="transfer-from"
            className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500"
            value={form.from_id}
            onChange={e => onChange('from_id', Number(e.target.value))}
          >
            {accounts.map(a => (
              <option key={a.id} value={a.id}>{a.icon} {a.name}</option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="transfer-to">
            {t('accounts.transferTo')}
          </label>
          <select
            id="transfer-to"
            className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500"
            value={form.to_id}
            onChange={e => onChange('to_id', Number(e.target.value))}
          >
            {accounts.map(a => (
              <option key={a.id} value={a.id}>{a.icon} {a.name}</option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="transfer-amount">
            {t('accounts.transferAmount')}
          </label>
          <input
            id="transfer-amount"
            className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500 text-right"
            placeholder="0"
            value={form.amount}
            onChange={e => onChange('amount', e.target.value)}
            required
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="transfer-date">
            {t('quickAdd.date')}
          </label>
          <input
            id="transfer-date"
            type="date"
            className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500"
            value={form.date}
            onChange={e => onChange('date', e.target.value)}
            required
          />
        </div>
        <div className="col-span-2 sm:col-span-4">
          <label className="block text-xs text-gray-400 mb-1" htmlFor="transfer-desc">
            {t('quickAdd.description')}
          </label>
          <input
            id="transfer-desc"
            className="w-full bg-gray-700 text-white rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-blue-500"
            placeholder={t('accounts.transferDescPlaceholder')}
            value={form.description}
            onChange={e => onChange('description', e.target.value)}
          />
        </div>
      </div>
      <div className="flex gap-2 justify-end">
        <button
          type="button"
          onClick={onCancel}
          className="text-gray-400 hover:text-white text-sm px-3 py-1.5 rounded"
        >
          {t('quickAdd.cancel')}
        </button>
        <button
          type="submit"
          disabled={saving}
          className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded"
        >
          {saving ? t('quickAdd.saving') : t('accounts.transfer')}
        </button>
      </div>
    </form>
  )
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function BudgetAccounts() {
  const { t } = useTranslation('budget')
  const [accounts, setAccounts] = useState<Account[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Account form state: null = hidden, 0 = create, >0 = edit that id
  const [editingId, setEditingId] = useState<number | null>(null)
  const [form, setForm] = useState<AccountForm>(blankForm())
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  // Delete state
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null)

  // Transfer form
  const [showTransfer, setShowTransfer] = useState(false)
  const [transferForm, setTransferForm] = useState<TransferForm>({
    from_id: 0,
    to_id: 0,
    amount: '',
    description: '',
    date: todayDate(),
  })
  const [transferSaving, setTransferSaving] = useState(false)
  const [transferError, setTransferError] = useState<string | null>(null)

  const loadAccounts = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/api/budget/accounts', { credentials: 'include', signal })
      if (!res.ok) throw new Error(t('accounts.errors.loadFailed'))
      const data = await res.json() as { accounts: Account[] }
      setAccounts((data.accounts ?? []).slice().sort((a, b) => a.id - b.id))
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('accounts.errors.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    void loadAccounts(controller.signal)
    return () => { controller.abort() }
  }, [loadAccounts])

  function setFormField<K extends keyof AccountForm>(key: K, value: AccountForm[K]) {
    setForm(prev => ({ ...prev, [key]: value }))
  }

  function setTransferField<K extends keyof TransferForm>(key: K, value: TransferForm[K]) {
    setTransferForm(prev => ({ ...prev, [key]: value }))
  }

  function openCreate() {
    setShowTransfer(false)
    setEditingId(0)
    setForm(blankForm())
    setFormError(null)
  }

  function openEdit(a: Account) {
    setShowTransfer(false)
    setEditingId(a.id)
    setForm(accountToForm(a))
    setFormError(null)
  }

  function closeForm() {
    setEditingId(null)
    setFormError(null)
  }

  function openTransfer() {
    setEditingId(null)
    setTransferError(null)
    setTransferForm(prev => ({
      ...prev,
      from_id: accounts.length >= 1 ? accounts[0].id : 0,
      to_id: accounts.length >= 2 ? accounts[1].id : accounts.length === 1 ? accounts[0].id : 0,
    }))
    setShowTransfer(true)
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!form.name.trim()) {
      setFormError(t('accounts.errors.nameRequired'))
      return
    }
    const balance = parseFloat(form.balance.replace(',', '.')) || 0
    const parsedCreditLimit = parseFloat(form.credit_limit.replace(',', '.'))
    const creditLimit = Number.isFinite(parsedCreditLimit) && parsedCreditLimit >= 0 ? parsedCreditLimit : 0
    const payload = {
      name: form.name.trim(),
      type: form.type,
      currency: form.currency || 'NOK',
      icon: form.icon || '🏦',
      balance,
      credit_limit: form.type === 'credit' ? creditLimit : 0,
    }
    setSaving(true)
    setFormError(null)
    try {
      const isNew = editingId === 0
      const url = isNew ? '/api/budget/accounts' : `/api/budget/accounts/${editingId}`
      const res = await fetch(url, {
        method: isNew ? 'POST' : 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      if (!res.ok) {
        const body = await res.json() as { error?: string }
        throw new Error(body.error ?? t('accounts.errors.saveFailed'))
      }
      closeForm()
      await loadAccounts()
    } catch (err) {
      setFormError(err instanceof Error ? err.message : t('accounts.errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: number) {
    setDeletingId(id)
    setError(null)
    try {
      const res = await fetch(`/api/budget/accounts/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(t('accounts.errors.deleteFailed'))
      setConfirmDeleteId(null)
      await loadAccounts()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('accounts.errors.deleteFailed'))
    } finally {
      setDeletingId(null)
    }
  }

  async function handleTransfer(e: FormEvent) {
    e.preventDefault()
    if (transferForm.from_id === transferForm.to_id) {
      setTransferError(t('accounts.transferSame'))
      return
    }
    const amount = parseFloat(transferForm.amount.replace(',', '.'))
    if (!amount || amount <= 0) {
      setTransferError(t('errors.invalidAmount'))
      return
    }
    setTransferSaving(true)
    setTransferError(null)
    try {
      const desc = transferForm.description.trim() || t('accounts.transferDefaultDesc')
      const transferRes = await fetch('/api/budget/transfers', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          from_account_id: transferForm.from_id,
          to_account_id: transferForm.to_id,
          amount,
          description: desc,
          date: transferForm.date,
        }),
      })
      if (!transferRes.ok) throw new Error(t('accounts.errors.transferFailed'))
      setShowTransfer(false)
      setTransferForm(prev => ({ ...prev, amount: '', description: '' }))
      await loadAccounts()
    } catch (err) {
      setTransferError(err instanceof Error ? err.message : t('accounts.errors.transferFailed'))
    } finally {
      setTransferSaving(false)
    }
  }

  const typeLabel = (typ: string) => t(`accounts.types.${typ}` as never)

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      {/* Header */}
      <div className="sticky top-0 z-10 bg-gray-900 border-b border-gray-800 px-4 py-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Link
            to="/budget"
            className="p-1 rounded hover:bg-gray-700 text-gray-400 hover:text-white transition-colors"
            aria-label={t('import.backToBudget')}
          >
            <ChevronLeft size={20} />
          </Link>
          <h1 className="text-xl font-semibold">{t('accounts.title')}</h1>
        </div>
        <div className="flex items-center gap-2">
          {accounts.length >= 2 && (
            <button
              onClick={() => showTransfer ? setShowTransfer(false) : openTransfer()}
              aria-pressed={showTransfer}
              className="flex items-center gap-1 bg-gray-700 hover:bg-gray-600 text-white rounded px-3 py-1 text-sm"
            >
              <ArrowLeftRight size={14} />
              {t('accounts.transfer')}
            </button>
          )}
          <button
            onClick={() => editingId === 0 ? closeForm() : openCreate()}
            className="flex items-center gap-1 bg-blue-600 hover:bg-blue-700 text-white rounded px-3 py-1 text-sm"
          >
            {editingId === 0 ? <X size={14} /> : <Plus size={14} />}
            {editingId === 0 ? t('quickAdd.cancel') : t('accounts.add')}
          </button>
        </div>
      </div>

      {/* Account creation / edit form */}
      {editingId !== null && (
        <AccountFormPanel
          form={form}
          onChange={setFormField}
          onSubmit={e => { void handleSubmit(e) }}
          onCancel={closeForm}
          saving={saving}
          error={formError}
          isNew={editingId === 0}
        />
      )}

      {/* Transfer form */}
      {showTransfer && (
        <TransferFormPanel
          accounts={accounts}
          form={transferForm}
          onChange={setTransferField}
          onSubmit={e => { void handleTransfer(e) }}
          onCancel={() => setShowTransfer(false)}
          saving={transferSaving}
          error={transferError}
        />
      )}

      {error && (
        <div className="px-4 py-3 bg-red-900/40 text-red-300 text-sm border-b border-red-800">
          {error}
        </div>
      )}

      {/* Account list */}
      {loading ? (
        <div className="flex items-center justify-center py-16 text-gray-400">
          {t('loading')}
        </div>
      ) : accounts.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-gray-400 gap-3">
          <p>{t('accounts.empty')}</p>
          <button
            onClick={openCreate}
            className="flex items-center gap-1 text-blue-400 hover:text-blue-300 text-sm"
          >
            <Plus size={16} />
            {t('accounts.add')}
          </button>
        </div>
      ) : (
        <ul className="divide-y divide-gray-800">
          {accounts.map(a => (
            <li key={a.id} className="px-4 py-4 hover:bg-gray-800/50 group">
              {/* Confirm delete */}
              {confirmDeleteId === a.id ? (
                <div className="flex items-center justify-between gap-4">
                  <p className="text-sm text-gray-300">{t('accounts.confirmDelete')}</p>
                  <div className="flex gap-2 shrink-0">
                    <button
                      onClick={() => setConfirmDeleteId(null)}
                      className="text-gray-400 hover:text-white text-sm px-2 py-1 rounded"
                    >
                      {t('quickAdd.cancel')}
                    </button>
                    <button
                      onClick={() => { void handleDelete(a.id) }}
                      disabled={deletingId === a.id}
                      className="bg-red-700 hover:bg-red-600 disabled:opacity-50 text-white text-sm px-3 py-1 rounded"
                    >
                      {t('accounts.delete')}
                    </button>
                  </div>
                </div>
              ) : (
                <div className="flex items-center gap-3">
                  {/* Icon */}
                  <span className="text-2xl w-10 text-center shrink-0">{a.icon || '🏦'}</span>

                  {/* Name + type */}
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-gray-100 truncate">{a.name}</p>
                    <p className="text-xs text-gray-500 capitalize">{typeLabel(a.type)}</p>
                  </div>

                  {/* Balance */}
                  <div className="text-right shrink-0">
                    <p className={`text-sm font-semibold tabular-nums ${a.balance < 0 ? 'text-red-400' : 'text-gray-100'}`}>
                      {formatBalance(a.balance, a.currency)}
                    </p>
                    {a.type === 'credit' && a.credit_limit > 0 ? (
                      <p className="text-xs text-gray-500">
                        {t('accounts.creditLimitOf', { limit: formatBalance(a.credit_limit, a.currency) })}
                      </p>
                    ) : (
                      <p className="text-xs text-gray-500">{a.currency}</p>
                    )}
                  </div>

                  {/* Actions */}
                  <div className="flex gap-1 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity shrink-0">
                    <button
                      onClick={() => openEdit(a)}
                      className="text-gray-500 hover:text-blue-400 p-1 rounded"
                      aria-label={t('accounts.edit')}
                    >
                      <Pencil size={16} />
                    </button>
                    <button
                      onClick={() => setConfirmDeleteId(a.id)}
                      className="text-gray-500 hover:text-red-400 p-1 rounded"
                      aria-label={t('accounts.delete')}
                    >
                      <Trash2 size={16} />
                    </button>
                  </div>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
