import { useState } from 'react'
import { ChevronDown, ChevronUp, Plus, X } from 'lucide-react'
import type { TFunction } from 'i18next'
import type { LoanRateChange } from './types'
import { effectiveRate } from './format'

/** Collapsible rate history panel with inline add form. */
export function RateHistoryPanel({ rateChanges, onAdd, onDelete, t }: {
  rateChanges: LoanRateChange[]
  onAdd: (date: string, rate: number) => Promise<void>
  onDelete: (id: number) => Promise<void>
  t: TFunction<'budget'>
}) {
  const [open, setOpen] = useState(false)
  const [newDate, setNewDate] = useState('')
  const [newRate, setNewRate] = useState('')
  const [saving, setSaving] = useState(false)

  async function handleAdd() {
    if (!newDate || !newRate) return
    setSaving(true)
    await onAdd(newDate, Number(newRate) / 100)
    setNewDate('')
    setNewRate('')
    setSaving(false)
  }

  return (
    <div className="mb-4">
      <button
        onClick={() => setOpen(prev => !prev)}
        className="flex items-center gap-1.5 text-sm text-gray-400 hover:text-gray-200 transition-colors"
      >
        {open ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
        {t('loan.rateHistory')}
        {rateChanges.length > 0 && (
          <span className="text-xs text-gray-500">({rateChanges.length})</span>
        )}
      </button>

      {open && (
        <div className="mt-2 space-y-2">
          {rateChanges.map(rc => (
            <div key={rc.id} className="flex items-center gap-3 text-sm">
              <span className="text-gray-400">{rc.effective_date}</span>
              <span className="font-medium">{(rc.annual_rate * 100).toFixed(2)}%</span>
              <span className="text-xs text-gray-500">
                ({t('loan.effectiveShort', { pct: (effectiveRate(rc.annual_rate) * 100).toFixed(2) })})
              </span>
              <button
                onClick={() => void onDelete(rc.id)}
                className="p-1 rounded hover:bg-gray-700 text-gray-500 hover:text-red-400 transition-colors"
                aria-label={t('loan.delete')}
              >
                <X size={12} />
              </button>
            </div>
          ))}

          {rateChanges.length === 0 && (
            <p className="text-xs text-gray-500">{t('loan.noRateChanges')}</p>
          )}

          {/* Add form */}
          <div className="flex items-end gap-2 pt-1">
            <div>
              <label className="block text-xs text-gray-500 mb-0.5">{t('loan.date')}</label>
              <input
                type="date"
                value={newDate}
                onChange={e => setNewDate(e.target.value)}
                className="bg-gray-700 border border-gray-600 rounded px-2 py-1 text-sm"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-0.5">{t('loan.annualRate')}</label>
              <div className="relative">
                <input
                  type="number"
                  min="0"
                  max="100"
                  step="0.01"
                  value={newRate}
                  onChange={e => setNewRate(e.target.value)}
                  placeholder="4.80"
                  className="bg-gray-700 border border-gray-600 rounded px-2 py-1 pr-7 text-sm w-24"
                />
                <span className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 text-sm pointer-events-none">%</span>
              </div>
            </div>
            <button
              onClick={() => void handleAdd()}
              disabled={saving || !newDate || !newRate}
              className="px-3 py-1 bg-blue-600 hover:bg-blue-500 rounded text-sm font-medium transition-colors disabled:opacity-50"
            >
              <Plus size={14} />
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
