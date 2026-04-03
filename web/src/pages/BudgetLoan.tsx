import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, Home, Plus, Pencil, Trash2, ChevronDown, ChevronUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'

interface Loan {
  id: number
  name: string
  principal: number
  current_balance: number
  annual_rate: number
  monthly_payment: number
  start_date: string
  term_months: number
  property_value: number
  property_name: string
  notes: string
  ltv_ratio?: number
}

interface AmortizationRow {
  payment_num: number
  date: string
  payment: number
  principal: number
  interest: number
  remaining_balance: number
  rate: number
}

interface AmortizationResponse {
  loan: Loan
  amortization: AmortizationRow[]
  ltv_ratio: number
  ltv_max: number
}

function localDateString(): string {
  const d = new Date()
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

const EMPTY_LOAN: Omit<Loan, 'id'> = {
  name: '',
  principal: 0,
  current_balance: 0,
  annual_rate: 0.048,
  monthly_payment: 0,
  start_date: localDateString(),
  term_months: 240,
  property_value: 0,
  property_name: '',
  notes: '',
}

function fmt(n: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(n)
}

function fmtPct(n: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'percent',
    minimumFractionDigits: 1,
    maximumFractionDigits: 2,
  }).format(n)
}

interface LoanFormProps {
  initial: Omit<Loan, 'id'>
  onSave: (loan: Omit<Loan, 'id'>) => Promise<void>
  onCancel: () => void
  saving: boolean
  t: TFunction
}

function LoanForm({ initial, onSave, onCancel, saving, t }: LoanFormProps) {
  const [form, setForm] = useState(initial)

  function set<K extends keyof typeof form>(key: K, value: typeof form[K]) {
    setForm(prev => ({ ...prev, [key]: value }))
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    void onSave(form)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-name">
            {t('loan.name')}
          </label>
          <input
            id="loan-name"
            type="text"
            required
            value={form.name}
            onChange={e => set('name', e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
            placeholder={t('loan.namePlaceholder')}
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-start-date">
            {t('loan.startDate')}
          </label>
          <input
            id="loan-start-date"
            type="date"
            required
            value={form.start_date}
            onChange={e => set('start_date', e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-principal">
            {t('loan.principal')}
          </label>
          <input
            id="loan-principal"
            type="number"
            min="0"
            step="1000"
            value={form.principal}
            onChange={e => set('principal', Number(e.target.value))}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-balance">
            {t('loan.currentBalance')}
          </label>
          <input
            id="loan-balance"
            type="number"
            min="0"
            step="1000"
            value={form.current_balance}
            onChange={e => set('current_balance', Number(e.target.value))}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-rate">
            {t('loan.annualRate')}
          </label>
          <input
            id="loan-rate"
            type="number"
            min="0"
            max="1"
            step="0.001"
            value={form.annual_rate}
            onChange={e => set('annual_rate', Number(e.target.value))}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
            placeholder="0.048"
          />
          <p className="text-xs text-gray-500 mt-0.5">
            {t('loan.annualRateHint', { pct: (form.annual_rate * 100).toFixed(2) })}
          </p>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-payment">
            {t('loan.monthlyPayment')}
          </label>
          <input
            id="loan-payment"
            type="number"
            min="0"
            step="100"
            value={form.monthly_payment}
            onChange={e => set('monthly_payment', Number(e.target.value))}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
            placeholder="0"
          />
          <p className="text-xs text-gray-500 mt-0.5">{t('loan.monthlyPaymentHint')}</p>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-term">
            {t('loan.termMonths')}
          </label>
          <input
            id="loan-term"
            type="number"
            min="0"
            step="12"
            value={form.term_months}
            onChange={e => set('term_months', Number(e.target.value))}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-prop-value">
            {t('loan.propertyValue')}
          </label>
          <input
            id="loan-prop-value"
            type="number"
            min="0"
            step="10000"
            value={form.property_value}
            onChange={e => set('property_value', Number(e.target.value))}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-prop-name">
            {t('loan.propertyName')}
          </label>
          <input
            id="loan-prop-name"
            type="text"
            value={form.property_name}
            onChange={e => set('property_name', e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
          />
        </div>
        <div className="sm:col-span-2">
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-notes">
            {t('loan.notes')}
          </label>
          <textarea
            id="loan-notes"
            value={form.notes}
            onChange={e => set('notes', e.target.value)}
            rows={2}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm resize-none"
          />
        </div>
      </div>

      <div className="flex gap-3 justify-end">
        <button
          type="button"
          onClick={onCancel}
          className="px-4 py-2 rounded-lg bg-gray-700 hover:bg-gray-600 text-sm transition-colors"
        >
          {t('quickAdd.cancel')}
        </button>
        <button
          type="submit"
          disabled={saving}
          className="px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-sm font-medium transition-colors disabled:opacity-50"
        >
          {saving ? t('quickAdd.saving') : t('loan.save')}
        </button>
      </div>
    </form>
  )
}

interface AmortizationTableProps {
  loanId: number
  t: TFunction
}

function AmortizationTable({ loanId, t }: AmortizationTableProps) {
  const [data, setData] = useState<AmortizationResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showAll, setShowAll] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    setLoading(true)
    fetch(`/api/budget/loans/${loanId}/amortization?rows=360`, { credentials: 'include', signal: controller.signal })
      .then(r => {
        if (!r.ok) throw new Error('fetch failed')
        return r.json() as Promise<AmortizationResponse>
      })
      .then(setData)
      .catch(err => {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(t('loan.errors.loadFailed'))
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [loanId, t])

  if (loading) return <p className="text-gray-400 text-sm py-4">{t('loading')}</p>
  if (error) return <p className="text-red-400 text-sm py-4">{error}</p>
  if (!data || data.amortization.length === 0) {
    return <p className="text-gray-500 text-sm py-4">{t('loan.noAmortization')}</p>
  }

  const ltvPct = data.ltv_ratio
  const ltvOk = ltvPct <= data.ltv_max
  const rows = showAll ? data.amortization : data.amortization.slice(0, 24)

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
            {rows.map(row => (
              <tr key={row.payment_num} className="text-gray-300 hover:bg-gray-700/30">
                <td className="py-1.5 pr-3 text-gray-500">{row.payment_num}</td>
                <td className="py-1.5 pr-3">{row.date}</td>
                <td className="py-1.5 pr-3 text-right">{fmt(row.payment)}</td>
                <td className="py-1.5 pr-3 text-right text-blue-400">{fmt(row.principal)}</td>
                <td className="py-1.5 pr-3 text-right text-red-400">{fmt(row.interest)}</td>
                <td className="py-1.5 pr-3 text-right">{fmt(row.remaining_balance)}</td>
                <td className="py-1.5 text-right text-gray-400">{(row.rate * 100).toFixed(2)}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {data.amortization.length > 24 && (
        <button
          onClick={() => setShowAll(prev => !prev)}
          className="mt-3 flex items-center gap-1 text-sm text-blue-400 hover:text-blue-300"
        >
          {showAll ? (
            <><ChevronUp size={14} />{t('loan.showLess')}</>
          ) : (
            <><ChevronDown size={14} />{t('loan.showAll', { count: data.amortization.length })}</>
          )}
        </button>
      )}
    </div>
  )
}

export default function BudgetLoan() {
  const { t } = useTranslation('budget')
  const [loans, setLoans] = useState<Loan[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showForm, setShowForm] = useState(false)
  const [editingLoan, setEditingLoan] = useState<Loan | null>(null)
  const [saving, setSaving] = useState(false)
  const [expandedAmortization, setExpandedAmortization] = useState<number | null>(null)

  const fetchLoans = useCallback(async () => {
    try {
      const r = await fetch('/api/budget/loans', { credentials: 'include' })
      if (!r.ok) throw new Error('fetch failed')
      const d = await r.json() as { loans: Loan[] }
      setLoans(d.loans)
    } catch {
      setError(t('loan.errors.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    void fetchLoans()
  }, [fetchLoans])

  async function handleCreate(form: Omit<Loan, 'id'>) {
    setSaving(true)
    try {
      const r = await fetch('/api/budget/loans', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      })
      if (!r.ok) throw new Error('create failed')
      setShowForm(false)
      await fetchLoans()
    } catch {
      setError(t('loan.errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  async function handleUpdate(form: Omit<Loan, 'id'>) {
    if (!editingLoan) return
    setSaving(true)
    try {
      const r = await fetch(`/api/budget/loans/${editingLoan.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      })
      if (!r.ok) throw new Error('update failed')
      setEditingLoan(null)
      await fetchLoans()
    } catch {
      setError(t('loan.errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: number) {
    if (!confirm(t('loan.confirmDelete'))) return
    try {
      const r = await fetch(`/api/budget/loans/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!r.ok) throw new Error('delete failed')
      await fetchLoans()
    } catch {
      setError(t('loan.errors.deleteFailed'))
    }
  }

  return (
    <div className="max-w-4xl mx-auto p-4 md:p-6">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Link
          to="/budget"
          className="p-2 rounded-lg bg-gray-800 hover:bg-gray-700 transition-colors"
          aria-label={t('charts.back')}
        >
          <ArrowLeft size={18} />
        </Link>
        <Home size={22} className="text-blue-400" />
        <h1 className="text-xl font-bold">{t('loan.title')}</h1>
        <button
          onClick={() => { setShowForm(true); setEditingLoan(null) }}
          className="ml-auto flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium transition-colors"
        >
          <Plus size={16} />
          {t('loan.addLoan')}
        </button>
      </div>

      {error && (
        <p className="text-red-400 mb-4 text-sm">{error}</p>
      )}

      {/* Add form */}
      {showForm && !editingLoan && (
        <div className="bg-gray-800 rounded-xl p-6 mb-6">
          <h2 className="font-semibold mb-4">{t('loan.addLoan')}</h2>
          <LoanForm
            initial={EMPTY_LOAN}
            onSave={handleCreate}
            onCancel={() => setShowForm(false)}
            saving={saving}
            t={t}
          />
        </div>
      )}

      {loading && (
        <p className="text-gray-400 text-center py-12">{t('loading')}</p>
      )}

      {!loading && loans.length === 0 && !showForm && (
        <p className="text-gray-500 text-center py-12">{t('loan.noLoans')}</p>
      )}

      {/* Loan cards */}
      <div className="space-y-4">
        {loans.map(loan => {
          const isEditing = editingLoan?.id === loan.id
          const isAmortOpen = expandedAmortization === loan.id
          const ltvPct = loan.ltv_ratio ?? 0
          const ltvOk = ltvPct <= 0.85

          return (
            <div key={loan.id} className="bg-gray-800 rounded-xl overflow-hidden">
              {/* Loan header */}
              <div className="p-5">
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <h3 className="font-semibold text-lg">{loan.name}</h3>
                    {loan.property_name && (
                      <p className="text-sm text-gray-400">{loan.property_name}</p>
                    )}
                  </div>
                  <div className="flex gap-2">
                    <button
                      onClick={() => {
                        setEditingLoan(loan)
                        setShowForm(false)
                      }}
                      className="p-1.5 rounded-lg hover:bg-gray-700 transition-colors text-gray-400 hover:text-white"
                      aria-label={t('loan.editLoan')}
                    >
                      <Pencil size={15} />
                    </button>
                    <button
                      onClick={() => void handleDelete(loan.id)}
                      className="p-1.5 rounded-lg hover:bg-gray-700 transition-colors text-gray-400 hover:text-red-400"
                      aria-label={t('loan.delete')}
                    >
                      <Trash2 size={15} />
                    </button>
                  </div>
                </div>

                {/* Key stats */}
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mt-4">
                  <div>
                    <div className="text-xs text-gray-400">{t('loan.currentBalance')}</div>
                    <div className="font-semibold">{fmt(loan.current_balance)}</div>
                  </div>
                  <div>
                    <div className="text-xs text-gray-400">{t('loan.annualRate')}</div>
                    <div className="font-semibold">{(loan.annual_rate * 100).toFixed(2)}%</div>
                  </div>
                  <div>
                    <div className="text-xs text-gray-400">{t('loan.monthlyPayment')}</div>
                    <div className="font-semibold">{fmt(loan.monthly_payment)}</div>
                  </div>
                  {loan.property_value > 0 && (
                    <div>
                      <div className="text-xs text-gray-400">{t('loan.ltv')}</div>
                      <div className={`font-semibold ${ltvOk ? 'text-green-400' : 'text-red-400'}`}>
                        {fmtPct(ltvPct)}
                      </div>
                    </div>
                  )}
                </div>

                {/* LTV bar */}
                {loan.property_value > 0 && (
                  <div className="mt-3">
                    <div className="flex justify-between text-xs text-gray-500 mb-1">
                      <span>{t('loan.ltv')}</span>
                      <span>{t('loan.ltvMax', { pct: '85%' })}</span>
                    </div>
                    <div className="bg-gray-700 rounded-full h-1.5">
                      <div
                        className={`h-1.5 rounded-full transition-all ${ltvOk ? 'bg-green-500' : 'bg-red-500'}`}
                        style={{ width: `${Math.min(ltvPct / 0.85, 1) * 100}%` }}
                      />
                    </div>
                  </div>
                )}

                {loan.notes && (
                  <p className="mt-3 text-sm text-gray-400">{loan.notes}</p>
                )}

                {/* Toggle amortization */}
                <button
                  onClick={() => setExpandedAmortization(isAmortOpen ? null : loan.id)}
                  className="mt-4 flex items-center gap-1.5 text-sm text-blue-400 hover:text-blue-300 transition-colors"
                >
                  {isAmortOpen ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
                  {t('loan.amortization')}
                </button>
              </div>

              {/* Edit form */}
              {isEditing && (
                <div className="border-t border-gray-700 p-5">
                  <h3 className="font-semibold mb-4">{t('loan.editLoan')}</h3>
                  <LoanForm
                    initial={{
                      name: loan.name,
                      principal: loan.principal,
                      current_balance: loan.current_balance,
                      annual_rate: loan.annual_rate,
                      monthly_payment: loan.monthly_payment,
                      start_date: loan.start_date,
                      term_months: loan.term_months,
                      property_value: loan.property_value,
                      property_name: loan.property_name,
                      notes: loan.notes,
                    }}
                    onSave={handleUpdate}
                    onCancel={() => setEditingLoan(null)}
                    saving={saving}
                    t={t}
                  />
                </div>
              )}

              {/* Amortization table */}
              {isAmortOpen && (
                <div className="border-t border-gray-700 p-5">
                  <AmortizationTable loanId={loan.id} t={t} />
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
