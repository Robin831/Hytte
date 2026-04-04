import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { ChevronLeft, Pencil, Check, AlertTriangle } from 'lucide-react'
import { formatDate, formatNumber } from '../utils/formatDate'

// ── Types ────────────────────────────────────────────────────────────────────

interface RegningItem {
  id: number
  description: string
  amount: number
  monthly: number
  split_type: 'percentage' | 'equal' | 'fixed_you' | 'fixed_partner'
  split_pct: number | null
  your_share: number
  partner_share: number
  next_due: string
  variable_id: number | null
  variable_name: string
  variable_no_entries: boolean
}

interface RegningData {
  expenses: RegningItem[]
  total_your_share: number
  total_partner_share: number
  your_income: number
  partner_income: number
  your_remaining: number
  partner_remaining: number
  income_split_pct: number
  your_income_day: number
  partner_income_day: number
  your_income_due: string
  partner_income_due: string
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatCurrency(amount: number): string {
  return formatNumber(Math.abs(amount), {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

function formatCurrencySigned(amount: number): string {
  const formatted = formatCurrency(amount)
  return amount < 0 ? `−${formatted}` : formatted
}

// ── Component ─────────────────────────────────────────────────────────────────

export default function BudgetRegning() {
  const { t } = useTranslation('budget')

  const [data, setData] = useState<RegningData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [editingPartnerIncome, setEditingPartnerIncome] = useState(false)
  const [partnerIncomeInput, setPartnerIncomeInput] = useState('')

  const [editingYourIncomeDay, setEditingYourIncomeDay] = useState(false)
  const [yourIncomeDayInput, setYourIncomeDayInput] = useState('')

  const [editingPartnerIncomeDay, setEditingPartnerIncomeDay] = useState(false)
  const [partnerIncomeDayInput, setPartnerIncomeDayInput] = useState('')

  const loadData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/api/budget/regning', { credentials: 'include' })
      if (!res.ok) throw new Error('failed')
      const json = await res.json() as RegningData
      setData(json)
    } catch {
      setError(t('regning.errors.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch
    loadData()
  }, [loadData])

  async function savePreference(key: string, value: string) {
    const res = await fetch('/api/settings/preferences', {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ preferences: { [key]: value } }),
    })
    if (!res.ok) throw new Error('save failed')
  }

  async function savePartnerIncome() {
    const val = parseInt(partnerIncomeInput) || 0
    try {
      await savePreference('partner_income', String(val))
      setEditingPartnerIncome(false)
      await loadData()
    } catch {
      setError(t('regning.errors.saveFailed'))
    }
  }

  async function saveYourIncomeDay() {
    const val = parseInt(yourIncomeDayInput)
    if (!val || val < 1 || val > 31) {
      setError(t('regning.errors.invalidDay'))
      return
    }
    try {
      await savePreference('income_day', String(val))
      setEditingYourIncomeDay(false)
      await loadData()
    } catch {
      setError(t('regning.errors.saveFailed'))
    }
  }

  async function savePartnerIncomeDay() {
    const val = parseInt(partnerIncomeDayInput)
    if (!val || val < 1 || val > 31) {
      setError(t('regning.errors.invalidDay'))
      return
    }
    try {
      await savePreference('partner_income_day', String(val))
      setEditingPartnerIncomeDay(false)
      await loadData()
    } catch {
      setError(t('regning.errors.saveFailed'))
    }
  }

  function splitLabel(item: RegningItem, globalPct: number): string {
    switch (item.split_type) {
      case 'equal':
        return t('regning.splitTypes.equal')
      case 'fixed_you':
        return t('regning.splitTypes.fixed_you')
      case 'fixed_partner':
        return t('regning.splitTypes.fixed_partner')
      default: {
        const pct = item.split_pct ?? globalPct
        return t('regning.splitTypes.percentage', { pct })
      }
    }
  }

  if (loading) {
    return <div className="p-6 text-gray-400 text-sm">{t('loading')}</div>
  }

  return (
    <div className="max-w-2xl mx-auto p-4 space-y-4">
      {/* Header */}
      <div className="flex items-center gap-2">
        <Link to="/budget" className="text-gray-400 hover:text-white p-1" aria-label={t('import.backToBudget')}>
          <ChevronLeft size={20} />
        </Link>
        <h1 className="text-lg font-semibold flex-1">{t('regning.title')}</h1>
      </div>

      {error && (
        <div className="bg-red-900/40 border border-red-700 text-red-300 text-sm rounded px-3 py-2">
          {error}
        </div>
      )}

      {data && (
        <>
          {/* Summary cards */}
          <div className="grid grid-cols-2 gap-3">
            {/* You */}
            <div className="bg-gray-800 rounded-lg p-4 space-y-2">
              <p className="text-xs text-gray-400 uppercase tracking-wide">
                {t('regning.summary.you')}
              </p>
              <div className="space-y-1">
                <div className="flex justify-between text-sm">
                  <span className="text-gray-400">{t('regning.summary.income')}</span>
                  <span className="text-white">{formatCurrency(data.your_income)}</span>
                </div>
                {data.your_income === 0 && (
                  <p className="text-xs text-yellow-500">
                    <Link to="/salary" className="underline hover:text-yellow-400">
                      {t('regning.summary.setSalary', { defaultValue: 'Set up salary →' })}
                    </Link>
                  </p>
                )}
                <div className="flex justify-between text-sm items-center">
                  <span className="text-gray-400">{t('regning.summary.payday')}</span>
                  {editingYourIncomeDay ? (
                    <div className="flex items-center gap-1">
                      <input
                        type="number"
                        value={yourIncomeDayInput}
                        onChange={e => setYourIncomeDayInput(e.target.value)}
                        className="w-16 bg-gray-700 border border-gray-600 rounded px-2 py-0.5 text-sm text-right"
                        min={1}
                        max={31}
                        autoFocus
                        aria-label={t('regning.summary.enterPaydayDay')}
                        onKeyDown={e => { if (e.key === 'Enter') void saveYourIncomeDay(); if (e.key === 'Escape') setEditingYourIncomeDay(false) }}
                      />
                      <button onClick={() => void saveYourIncomeDay()} className="text-green-400 hover:text-green-300" aria-label={t('regning.summary.savePayday')}>
                        <Check size={14} />
                      </button>
                    </div>
                  ) : (
                    <span className="text-white flex items-center gap-1">
                      {data.your_income_due
                        ? formatDate(data.your_income_due + 'T00:00:00', { month: 'short', day: 'numeric' })
                        : t('regning.summary.notSet')}
                      <button
                        onClick={() => { setYourIncomeDayInput(String(data.your_income_day)); setEditingYourIncomeDay(true) }}
                        className="text-gray-500 hover:text-gray-300"
                        aria-label={t('regning.summary.editPayday')}
                      >
                        <Pencil size={12} />
                      </button>
                    </span>
                  )}
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-gray-400">{t('regning.summary.transfer')}</span>
                  <span className="text-red-300">−{formatCurrency(data.total_your_share)}</span>
                </div>
                <div className="flex justify-between text-sm font-semibold border-t border-gray-700 pt-1">
                  <span className="text-gray-300">{t('regning.summary.remaining')}</span>
                  <span className={data.your_remaining >= 0 ? 'text-green-400' : 'text-red-400'}>
                    {formatCurrencySigned(data.your_remaining)}
                  </span>
                </div>
              </div>
            </div>

            {/* Partner */}
            <div className="bg-gray-800 rounded-lg p-4 space-y-2">
              <p className="text-xs text-gray-400 uppercase tracking-wide">
                {t('regning.summary.partner')}
              </p>
              <div className="space-y-1">
                <div className="flex justify-between text-sm items-center">
                  <span className="text-gray-400">{t('regning.summary.income')}</span>
                  {editingPartnerIncome ? (
                    <div className="flex items-center gap-1">
                      <input
                        type="number"
                        value={partnerIncomeInput}
                        onChange={e => setPartnerIncomeInput(e.target.value)}
                        className="w-24 bg-gray-700 border border-gray-600 rounded px-2 py-0.5 text-sm text-right"
                        autoFocus
                        onKeyDown={e => { if (e.key === 'Enter') void savePartnerIncome(); if (e.key === 'Escape') setEditingPartnerIncome(false) }}
                      />
                      <button onClick={() => void savePartnerIncome()} className="text-green-400 hover:text-green-300">
                        <Check size={14} />
                      </button>
                    </div>
                  ) : (
                    <span className="text-white flex items-center gap-1">
                      {formatCurrency(data.partner_income)}
                      <button
                        onClick={() => { setPartnerIncomeInput(String(data.partner_income)); setEditingPartnerIncome(true) }}
                        className="text-gray-500 hover:text-gray-300"
                        aria-label={t('regning.summary.editIncome')}
                      >
                        <Pencil size={12} />
                      </button>
                    </span>
                  )}
                </div>
                <div className="flex justify-between text-sm items-center">
                  <span className="text-gray-400">{t('regning.summary.payday')}</span>
                  {editingPartnerIncomeDay ? (
                    <div className="flex items-center gap-1">
                      <input
                        type="number"
                        value={partnerIncomeDayInput}
                        onChange={e => setPartnerIncomeDayInput(e.target.value)}
                        className="w-16 bg-gray-700 border border-gray-600 rounded px-2 py-0.5 text-sm text-right"
                        min={1}
                        max={31}
                        autoFocus
                        aria-label={t('regning.summary.enterPaydayDay')}
                        onKeyDown={e => { if (e.key === 'Enter') void savePartnerIncomeDay(); if (e.key === 'Escape') setEditingPartnerIncomeDay(false) }}
                      />
                      <button onClick={() => void savePartnerIncomeDay()} className="text-green-400 hover:text-green-300" aria-label={t('regning.summary.savePayday')}>
                        <Check size={14} />
                      </button>
                    </div>
                  ) : (
                    <span className="text-white flex items-center gap-1">
                      {data.partner_income_due
                        ? formatDate(data.partner_income_due + 'T00:00:00', { month: 'short', day: 'numeric' })
                        : t('regning.summary.notSet')}
                      <button
                        onClick={() => { setPartnerIncomeDayInput(String(data.partner_income_day)); setEditingPartnerIncomeDay(true) }}
                        className="text-gray-500 hover:text-gray-300"
                        aria-label={t('regning.summary.editPayday')}
                      >
                        <Pencil size={12} />
                      </button>
                    </span>
                  )}
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-gray-400">{t('regning.summary.transfer')}</span>
                  <span className="text-red-300">−{formatCurrency(data.total_partner_share)}</span>
                </div>
                <div className="flex justify-between text-sm font-semibold border-t border-gray-700 pt-1">
                  <span className="text-gray-300">{t('regning.summary.remaining')}</span>
                  <span className={data.partner_remaining >= 0 ? 'text-green-400' : 'text-red-400'}>
                    {formatCurrencySigned(data.partner_remaining)}
                  </span>
                </div>
              </div>
            </div>
          </div>

          {/* Expense list */}
          {data.expenses.length === 0 ? (
            <p className="text-gray-500 text-sm text-center py-6">{t('regning.empty')}</p>
          ) : (
            <div className="bg-gray-800 rounded-lg overflow-hidden">
              {/* Column headers */}
              <div className="grid grid-cols-[1fr_auto_auto_auto] gap-x-3 px-3 py-2 text-xs text-gray-400 uppercase tracking-wide border-b border-gray-700">
                <span>{t('regning.expense')}</span>
                <span>{t('regning.splitType')}</span>
                <span className="text-right">{t('regning.yourShare')}</span>
                <span className="text-right">{t('regning.partnerShare')}</span>
              </div>

              {data.expenses.map(item => (
                <div
                  key={item.id}
                  className="grid grid-cols-[1fr_auto_auto_auto] gap-x-3 px-3 py-2.5 border-b border-gray-700/50 items-start last:border-b-0"
                >
                  <div className="min-w-0">
                    <p className="text-sm text-white truncate">
                      {item.description || t('noDescription')}
                    </p>
                    {item.variable_id !== null && (
                      <p className="text-xs text-indigo-400">
                        <Link to="/budget/variables" className="hover:text-indigo-300 underline">
                          {item.variable_name || t('regning.variableBill')}
                        </Link>
                        {item.variable_no_entries && (
                          <span className="ml-1.5 inline-flex items-center gap-0.5 text-yellow-500">
                            <AlertTriangle size={12} />
                            {t('regning.variableNoEntries')}
                          </span>
                        )}
                      </p>
                    )}
                    {item.next_due && (
                      <p className="text-xs text-gray-500">
                        {t('regning.nextDue')}: {formatDate(item.next_due + 'T00:00:00', { month: 'short', day: 'numeric' })}
                      </p>
                    )}
                    {item.variable_id === null && Math.abs(item.monthly) !== Math.abs(item.amount) && (
                      <p className="text-xs text-gray-500">
                        {formatCurrency(item.amount)} {t('regning.originalAmount')}
                      </p>
                    )}
                  </div>
                  <span className="text-xs text-gray-400 bg-gray-700 rounded px-1.5 py-0.5 whitespace-nowrap">
                    {splitLabel(item, data.income_split_pct)}
                  </span>
                  <span className="text-sm text-right text-white tabular-nums">
                    {formatCurrency(item.your_share)}
                  </span>
                  <span className="text-sm text-right text-gray-300 tabular-nums">
                    {formatCurrency(item.partner_share)}
                  </span>
                </div>
              ))}

              {/* Totals row */}
              <div className="grid grid-cols-[1fr_auto_auto_auto] gap-x-3 px-3 py-2.5 bg-gray-700/30 border-t border-gray-700">
                <span className="text-sm font-semibold text-gray-300">{t('regning.totals')}</span>
                <span />
                <span className="text-sm font-semibold text-right text-white tabular-nums">
                  {formatCurrency(data.total_your_share)}
                </span>
                <span className="text-sm font-semibold text-right text-gray-300 tabular-nums">
                  {formatCurrency(data.total_partner_share)}
                </span>
              </div>
            </div>
          )}

          {/* Link to manage recurring rules */}
          <div className="pt-2">
            <Link to="/budget/recurring" className="text-sm text-blue-400 hover:text-blue-300">
              {t('subscriptions.manageRecurring')}
            </Link>
          </div>
        </>
      )}
    </div>
  )
}
