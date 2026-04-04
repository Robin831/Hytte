import { useState, useEffect, useCallback, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Plus, Trash2, Pencil, X, Check, ChevronLeft, ToggleLeft, ToggleRight } from 'lucide-react'
import { formatNumber } from '../utils/formatDate'

// ── Types ────────────────────────────────────────────────────────────────────

interface Account {
  id: number
  name: string
  currency: string
}

interface Category {
  id: number
  name: string
  is_income: boolean
}

type SplitType = 'percentage' | 'equal' | 'fixed_you' | 'fixed_partner'

interface RecurringRule {
  id: number
  account_id: number
  category_id: number | null
  amount: number
  description: string
  frequency: 'monthly' | 'quarterly' | 'weekly' | 'yearly'
  day_of_month: number
  start_date: string
  end_date: string
  last_generated: string
  active: boolean
  next_due: string
  split_type: SplitType
  split_pct: number | null
}

interface RecurringForm {
  account_id: number
  category_id: number | null
  amount: string
  description: string
  frequency: string
  day_of_month: string
  start_date: string
  end_date: string
  active: boolean
  split_type: SplitType
  split_pct: string
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function formatAmount(amount: number): string {
  return formatNumber(Math.abs(amount), {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

function todayDate(): string {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
}

function blankForm(accounts: Account[]): RecurringForm {
  return {
    account_id: accounts[0]?.id ?? 0,
    category_id: null,
    amount: '',
    description: '',
    frequency: 'monthly',
    day_of_month: '1',
    start_date: todayDate(),
    end_date: '',
    active: true,
    split_type: 'percentage',
    split_pct: '',
  }
}

function ruleToForm(rule: RecurringRule): RecurringForm {
  return {
    account_id: rule.account_id,
    category_id: rule.category_id,
    amount: String(rule.amount),
    description: rule.description,
    frequency: rule.frequency,
    day_of_month: String(rule.day_of_month),
    start_date: rule.start_date,
    end_date: rule.end_date,
    active: rule.active,
    split_type: rule.split_type || 'percentage',
    split_pct: rule.split_pct != null ? String(rule.split_pct) : '',
  }
}

// ── Component ────────────────────────────────────────────────────────────────

export default function BudgetRecurring() {
  const { t } = useTranslation('budget')

  const [rules, setRules] = useState<RecurringRule[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Form state: null = hidden, 0 = new, >0 = editing rule id
  const [editingId, setEditingId] = useState<number | null>(null)
  const [form, setForm] = useState<RecurringForm | null>(null)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [rulesRes, accountsRes, catsRes] = await Promise.all([
        fetch('/api/budget/recurring', { credentials: 'include' }),
        fetch('/api/budget/accounts', { credentials: 'include' }),
        fetch('/api/budget/categories', { credentials: 'include' }),
      ])
      if (!rulesRes.ok || !accountsRes.ok || !catsRes.ok) {
        throw new Error('load failed')
      }
      const [rulesData, accountsData, catsData] = await Promise.all([
        rulesRes.json(),
        accountsRes.json(),
        catsRes.json(),
      ])
      setRules(rulesData.recurring ?? [])
      setAccounts(accountsData.accounts ?? [])
      setCategories(catsData.categories ?? [])
    } catch {
      setError(t('errors.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch
    load()
  }, [load])

  function openCreate() {
    setEditingId(0)
    setForm(blankForm(accounts))
    setFormError(null)
  }

  function openEdit(rule: RecurringRule) {
    setEditingId(rule.id)
    setForm(ruleToForm(rule))
    setFormError(null)
  }

  function cancelEdit() {
    setEditingId(null)
    setForm(null)
    setFormError(null)
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!form) return
    const amountNum = parseFloat(form.amount.replace(',', '.'))
    if (isNaN(amountNum)) {
      setFormError(t('recurring.errors.invalidAmount', { defaultValue: 'Please enter a valid number' }))
      return
    }
    if (!form.account_id) {
      setFormError(t('errors.noAccounts'))
      return
    }
    setSaving(true)
    setFormError(null)
    try {
      const body: Record<string, unknown> = {
        account_id: form.account_id,
        category_id: form.category_id,
        amount: amountNum,
        description: form.description,
        frequency: form.frequency,
        day_of_month: parseInt(form.day_of_month) || 1,
        start_date: form.start_date,
        end_date: form.end_date || '',
        active: form.active,
        split_type: form.split_type,
        split_pct: form.split_pct ? [parseFloat(form.split_pct)] : [null],
      }
      // The API uses double-pointer for tri-state: wrap in array to send explicit null vs absent.
      // Actually the API expects *float64 inside **float64. Simplify: send the value directly.
      body.split_pct = form.split_pct ? parseFloat(form.split_pct) : null
      const isNew = editingId === 0
      const res = await fetch(
        isNew ? '/api/budget/recurring' : `/api/budget/recurring/${editingId}`,
        {
          method: isNew ? 'POST' : 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        }
      )
      if (!res.ok) {
        throw new Error('save failed')
      }
      cancelEdit()
      await load()
    } catch {
      setFormError(t('recurring.errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: number) {
    try {
      const res = await fetch(`/api/budget/recurring/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('delete failed')
      setRules(prev => prev.filter(r => r.id !== id))
    } catch {
      setError(t('recurring.errors.deleteFailed'))
    }
  }

  async function handleToggleActive(rule: RecurringRule) {
    try {
      const body = {
        account_id: rule.account_id,
        category_id: rule.category_id,
        amount: rule.amount,
        description: rule.description,
        frequency: rule.frequency,
        day_of_month: rule.day_of_month,
        start_date: rule.start_date,
        end_date: rule.end_date,
        active: !rule.active,
        split_type: rule.split_type,
        split_pct: rule.split_pct,
      }
      const res = await fetch(`/api/budget/recurring/${rule.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) throw new Error('toggle failed')
      setRules(prev => prev.map(r => r.id === rule.id ? { ...r, active: !r.active } : r))
    } catch {
      setError(t('recurring.errors.saveFailed'))
    }
  }

  function accountName(id: number): string {
    return accounts.find(a => a.id === id)?.name ?? String(id)
  }

  function categoryName(id: number | null): string {
    if (id == null) return t('noCategory')
    return categories.find(c => c.id === id)?.name ?? String(id)
  }

  // ── Render ──────────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="p-6 text-gray-400">{t('loading')}</div>
    )
  }

  return (
    <div className="p-4 md:p-6 max-w-3xl mx-auto">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Link
          to="/budget"
          className="text-gray-400 hover:text-white transition-colors"
          aria-label={t('import.backToBudget')}
        >
          <ChevronLeft size={20} />
        </Link>
        <h1 className="text-xl font-semibold text-white">{t('recurring.title')}</h1>
        <button
          onClick={openCreate}
          disabled={accounts.length === 0}
          title={accounts.length === 0 ? t('errors.noAccounts') : undefined}
          className="ml-auto flex items-center gap-1.5 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm px-3 py-1.5 rounded-lg transition-colors"
        >
          <Plus size={16} />
          {t('recurring.add')}
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/40 border border-red-700 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* Create / Edit form */}
      {editingId !== null && form !== null && (
        <form
          onSubmit={handleSubmit}
          className="mb-6 p-4 bg-gray-800 rounded-xl border border-gray-700 space-y-3"
        >
          <h2 className="text-sm font-medium text-gray-200">
            {editingId === 0 ? t('recurring.newRule') : t('recurring.editRule')}
          </h2>

          {formError && (
            <p className="text-red-400 text-sm">{formError}</p>
          )}

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {/* Description */}
            <div className="sm:col-span-2">
              <label className="block text-xs text-gray-400 mb-1">{t('recurring.description')}</label>
              <input
                type="text"
                value={form.description}
                onChange={e => setForm({ ...form, description: e.target.value })}
                placeholder={t('recurring.descriptionPlaceholder')}
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              />
            </div>

            {/* Amount */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('quickAdd.amount')}</label>
              <input
                type="number"
                value={form.amount}
                onChange={e => setForm({ ...form, amount: e.target.value })}
                step="any"
                required
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              />
              <p className="text-xs text-gray-500 mt-1">{t('recurring.amountHint')}</p>
            </div>

            {/* Account */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('quickAdd.account')}</label>
              <select
                value={form.account_id}
                onChange={e => setForm({ ...form, account_id: Number(e.target.value) })}
                required
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              >
                {accounts.map(a => (
                  <option key={a.id} value={a.id}>{a.name}</option>
                ))}
              </select>
            </div>

            {/* Category */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('quickAdd.category')}</label>
              <select
                value={form.category_id ?? ''}
                onChange={e => setForm({ ...form, category_id: e.target.value ? Number(e.target.value) : null })}
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              >
                <option value="">{t('quickAdd.noCategory')}</option>
                {categories.map(c => (
                  <option key={c.id} value={c.id}>{c.name}</option>
                ))}
              </select>
            </div>

            {/* Frequency */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('recurring.frequency')}</label>
              <select
                value={form.frequency}
                onChange={e => setForm({ ...form, frequency: e.target.value })}
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              >
                <option value="monthly">{t('recurring.monthly')}</option>
                <option value="quarterly">{t('recurring.quarterly')}</option>
                <option value="weekly">{t('recurring.weekly')}</option>
                <option value="yearly">{t('recurring.yearly')}</option>
              </select>
            </div>

            {/* Day of month (for monthly/yearly) */}
            {(form.frequency === 'monthly' || form.frequency === 'quarterly' || form.frequency === 'yearly') && (
              <div>
                <label className="block text-xs text-gray-400 mb-1">{t('recurring.dayOfMonth')}</label>
                <input
                  type="number"
                  value={form.day_of_month}
                  onChange={e => setForm({ ...form, day_of_month: e.target.value })}
                  min="1"
                  max="31"
                  className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
                />
              </div>
            )}

            {/* Start date */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('recurring.startDate')}</label>
              <input
                type="date"
                value={form.start_date}
                onChange={e => setForm({ ...form, start_date: e.target.value })}
                required
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              />
            </div>

            {/* End date */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('recurring.endDate')}</label>
              <input
                type="date"
                value={form.end_date}
                onChange={e => setForm({ ...form, end_date: e.target.value })}
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              />
            </div>

            {/* Split type */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('regning.splitType')}</label>
              <select
                value={form.split_type}
                onChange={e => setForm({ ...form, split_type: e.target.value as SplitType })}
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              >
                <option value="percentage">{t('regning.splitTypes.percentage', { pct: form.split_pct || '60' })}</option>
                <option value="equal">{t('regning.splitTypes.equal')}</option>
                <option value="fixed_you">{t('regning.splitTypes.fixed_you')}</option>
                <option value="fixed_partner">{t('regning.splitTypes.fixed_partner')}</option>
              </select>
            </div>

            {/* Split percentage (only for percentage type) */}
            {form.split_type === 'percentage' && (
              <div>
                <label className="block text-xs text-gray-400 mb-1">%</label>
                <div className="relative">
                  <input
                    type="number"
                    min="0"
                    max="100"
                    step="1"
                    value={form.split_pct}
                    onChange={e => setForm({ ...form, split_pct: e.target.value })}
                    placeholder="60"
                    className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 pr-8 border border-gray-600 focus:border-indigo-500 focus:outline-none"
                  />
                  <span className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 text-sm pointer-events-none">%</span>
                </div>
                <p className="text-xs text-gray-500 mt-0.5">{t('recurring.splitPctHint', { defaultValue: 'Leave empty for global split' })}</p>
              </div>
            )}

            {/* Active toggle */}
            <div className="sm:col-span-2 flex items-center gap-2">
              <input
                id="recurring-active"
                type="checkbox"
                checked={form.active}
                onChange={e => setForm({ ...form, active: e.target.checked })}
                className="w-4 h-4 rounded"
              />
              <label htmlFor="recurring-active" className="text-sm text-gray-300">
                {t('recurring.active')}
              </label>
            </div>
          </div>

          {/* Form actions */}
          <div className="flex gap-2 pt-1">
            <button
              type="submit"
              disabled={saving}
              className="flex items-center gap-1.5 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors"
            >
              <Check size={14} />
              {saving ? t('quickAdd.saving') : t('recurring.save')}
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              className="flex items-center gap-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm px-3 py-1.5 rounded-lg transition-colors"
            >
              <X size={14} />
              {t('quickAdd.cancel')}
            </button>
          </div>
        </form>
      )}

      {/* Rules list */}
      {rules.length === 0 ? (
        <div className="text-center py-12 text-gray-500 text-sm">
          {t('recurring.empty')}
        </div>
      ) : (
        <ul className="space-y-3">
          {rules.map(rule => (
            <li
              key={rule.id}
              className={`p-4 bg-gray-800 rounded-xl border transition-colors ${
                rule.active ? 'border-gray-700' : 'border-gray-700/50 opacity-60'
              }`}
            >
              <div className="flex items-start gap-3">
                {/* Active toggle */}
                <button
                  onClick={() => handleToggleActive(rule)}
                  className="mt-0.5 text-gray-400 hover:text-indigo-400 transition-colors flex-shrink-0"
                  aria-label={rule.active ? t('recurring.deactivate') : t('recurring.activate')}
                >
                  {rule.active
                    ? <ToggleRight size={22} className="text-indigo-400" />
                    : <ToggleLeft size={22} />
                  }
                </button>

                {/* Info */}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-medium text-white text-sm">
                      {rule.description || t('noDescription')}
                    </span>
                    <span className={`text-sm font-semibold ${rule.amount >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                      {rule.amount < 0 ? '-' : '+'}{formatAmount(rule.amount)}
                    </span>
                  </div>

                  <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-gray-400">
                    <span>{categoryName(rule.category_id)}</span>
                    <span>{accountName(rule.account_id)}</span>
                    <span className="capitalize">{t(`recurring.${rule.frequency}`)}</span>
                    {(rule.frequency === 'monthly' || rule.frequency === 'quarterly' || rule.frequency === 'yearly') && rule.day_of_month > 0 && (
                      <span>{t('recurring.dayLabel', { day: rule.day_of_month })}</span>
                    )}
                    <span className="text-indigo-400">
                      {rule.split_type === 'equal' ? '50/50'
                        : rule.split_type === 'fixed_you' ? t('regning.splitTypes.fixed_you')
                        : rule.split_type === 'fixed_partner' ? t('regning.splitTypes.fixed_partner')
                        : rule.split_pct != null ? `${rule.split_pct}%` : ''}
                    </span>
                  </div>

                  {rule.next_due && (
                    <div className="mt-1 text-xs text-gray-500">
                      {t('recurring.nextDue')}: <span className="text-gray-300">{rule.next_due}</span>
                    </div>
                  )}
                  {rule.last_generated && (
                    <div className="text-xs text-gray-600">
                      {t('recurring.lastGenerated')}: {rule.last_generated}
                    </div>
                  )}
                </div>

                {/* Actions */}
                <div className="flex gap-1 flex-shrink-0">
                  <button
                    onClick={() => openEdit(rule)}
                    className="p-1.5 text-gray-400 hover:text-white rounded transition-colors"
                    aria-label={t('recurring.edit')}
                  >
                    <Pencil size={15} />
                  </button>
                  <button
                    onClick={() => handleDelete(rule.id)}
                    className="p-1.5 text-gray-400 hover:text-red-400 rounded transition-colors"
                    aria-label={t('recurring.delete')}
                  >
                    <Trash2 size={15} />
                  </button>
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
