import { useState } from 'react'
import type { TFunction } from 'i18next'
import type { Loan } from './types'
import { effectiveRate } from './format'
import { CurrencyInput } from './CurrencyInput'

interface LoanFormProps {
  initial: Omit<Loan, 'id'>
  onSave: (loan: Omit<Loan, 'id'>) => Promise<void>
  onCancel: () => void
  saving: boolean
  t: TFunction<'budget'>
}

export function LoanForm({ initial, onSave, onCancel, saving, t }: LoanFormProps) {
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
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-first-payment">
            {t('loan.firstPaymentDate')}
          </label>
          <input
            id="loan-first-payment"
            type="date"
            value={form.first_payment_date}
            onChange={e => set('first_payment_date', e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
          />
          <p className="text-xs text-gray-500 mt-0.5">{t('loan.firstPaymentDateHint')}</p>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-principal">
            {t('loan.principal')}
          </label>
          <CurrencyInput id="loan-principal" value={form.principal} onChange={v => set('principal', v)} />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-balance">
            {t('loan.currentBalance')}
          </label>
          <CurrencyInput id="loan-balance" value={form.current_balance} onChange={v => set('current_balance', v)} />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-rate">
            {t('loan.annualRate')}
          </label>
          <div className="relative">
            <input
              id="loan-rate"
              type="number"
              min="0"
              max="100"
              step="0.01"
              value={parseFloat((form.annual_rate * 100).toFixed(4))}
              onChange={e => set('annual_rate', Number(e.target.value) / 100)}
              className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 pr-8 text-sm"
              placeholder="4.80"
            />
            <span className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 text-sm pointer-events-none">%</span>
          </div>
          <p className="text-xs text-gray-500 mt-0.5">
            {t('loan.effectiveRateHint', { pct: (effectiveRate(form.annual_rate) * 100).toFixed(2) })}
          </p>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-payment">
            {t('loan.monthlyPayment')}
          </label>
          <CurrencyInput id="loan-payment" value={form.monthly_payment} onChange={v => set('monthly_payment', v)} step="100" placeholder="0" />
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
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-pay-day">
            {t('loan.paymentDay')}
          </label>
          <input
            id="loan-pay-day"
            type="number"
            min="1"
            max="28"
            value={form.payment_day}
            onChange={e => set('payment_day', Number(e.target.value))}
            className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
          />
          <p className="text-xs text-gray-500 mt-0.5">{t('loan.paymentDayHint')}</p>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor="loan-prop-value">
            {t('loan.propertyValue')}
          </label>
          <CurrencyInput id="loan-prop-value" value={form.property_value} onChange={v => set('property_value', v)} step="10000" />
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
