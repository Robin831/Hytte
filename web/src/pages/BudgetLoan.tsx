import { useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, Home, Plus, Pencil, Trash2, ChevronDown, ChevronUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Loan } from './budget/loan/types'
import { fmt, fmtPct, effectiveRate, localDateString } from './budget/loan/format'
import { LoanForm } from './budget/loan/LoanForm'
import { AmortizationTable } from './budget/loan/AmortizationTable'
import { useLoans } from './budget/loan/useLoans'

// Fallback if the server hasn't started returning ltv_max yet (e.g. during rollout).
// Kept in one place so the list card still renders sensibly without it.
const FALLBACK_LTV_MAX = 0.85

const EMPTY_LOAN: Omit<Loan, 'id'> = {
  name: '',
  principal: 0,
  current_balance: 0,
  annual_rate: 0.048,
  monthly_payment: 0,
  start_date: localDateString(),
  first_payment_date: '',
  term_months: 240,
  payment_day: 1,
  property_value: 0,
  property_name: '',
  notes: '',
}

export default function BudgetLoan() {
  const { t } = useTranslation('budget')
  const { loans, loading, error, createLoan, updateLoan, deleteLoan } = useLoans()
  const [showForm, setShowForm] = useState(false)
  const [editingLoan, setEditingLoan] = useState<Loan | null>(null)
  const [saving, setSaving] = useState(false)
  const [expandedAmortization, setExpandedAmortization] = useState<number | null>(null)
  const [loanVersion, setLoanVersion] = useState(0)

  async function handleCreate(form: Omit<Loan, 'id'>) {
    setSaving(true)
    const ok = await createLoan(form)
    if (ok) setShowForm(false)
    setSaving(false)
  }

  async function handleUpdate(form: Omit<Loan, 'id'>) {
    if (!editingLoan) return
    setSaving(true)
    const ok = await updateLoan(editingLoan.id, form)
    if (ok) {
      setEditingLoan(null)
      setLoanVersion(v => v + 1)
    }
    setSaving(false)
  }

  async function handleDelete(id: number) {
    if (!confirm(t('loan.confirmDelete'))) return
    await deleteLoan(id)
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
          const ltvMax = loan.ltv_max ?? FALLBACK_LTV_MAX
          const ltvOk = ltvPct <= ltvMax

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
                    <div className="text-xs text-gray-500">{t('loan.effectiveShort', { pct: (effectiveRate(loan.annual_rate) * 100).toFixed(2) })}</div>
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
                      <span>{t('loan.ltvMax', { pct: fmtPct(ltvMax) })}</span>
                    </div>
                    <div className="bg-gray-700 rounded-full h-1.5">
                      <div
                        className={`h-1.5 rounded-full transition-all ${ltvOk ? 'bg-green-500' : 'bg-red-500'}`}
                        style={{ width: `${Math.min(ltvMax > 0 ? ltvPct / ltvMax : 0, 1) * 100}%` }}
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
                      first_payment_date: loan.first_payment_date || '',
                      term_months: loan.term_months,
                      payment_day: loan.payment_day || 1,
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
                  <AmortizationTable loanId={loan.id} version={loanVersion} t={t} />
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
