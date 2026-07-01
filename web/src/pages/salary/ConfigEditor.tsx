import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { SalaryData } from './useSalaryData'

interface ConfigEditorProps {
  salary: SalaryData
  noConfig: boolean
  noConfigPastMonth: boolean
  onClose: () => void
}

/**
 * Salary config editor panel. Owns its own form state (seeded from the current
 * config) and delegates persistence to the shared useSalaryData hook.
 */
export default function ConfigEditor({ salary, noConfig, noConfigPastMonth, onClose }: ConfigEditorProps) {
  const { t } = useTranslation('salary')
  const { estimate, saveConfig } = salary

  const [baseSalary, setBaseSalary] = useState(() => estimate ? String(estimate.config.base_salary) : '')
  const [hourlyRate, setHourlyRate] = useState(() => estimate ? String(estimate.config.hourly_rate) : '')
  const [internalHourlyRate, setInternalHourlyRate] = useState(() => estimate ? String(estimate.config.internal_hourly_rate ?? 0) : '0')
  const [taxableBenefits, setTaxableBenefits] = useState(() => estimate ? String(estimate.config.taxable_benefits ?? 0) : '0')
  const [standardHours, setStandardHours] = useState(() => estimate ? String(estimate.config.standard_hours) : '7.5')
  const [currency, setCurrency] = useState(() => estimate?.config.currency ?? 'NOK')

  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const handleSave = async () => {
    setSaving(true)
    setSaveError(null)
    try {
      await saveConfig({
        base_salary: parseFloat(baseSalary) || 0,
        hourly_rate: parseFloat(hourlyRate) || 0,
        internal_hourly_rate: parseFloat(internalHourlyRate) || 0,
        taxable_benefits: parseFloat(taxableBenefits) || 0,
        standard_hours: isNaN(parseFloat(standardHours)) ? 7.5 : parseFloat(standardHours),
        currency: currency || 'NOK',
      })
      onClose()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t('errors.failedToSave'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="bg-gray-800 rounded-xl p-5 space-y-4">
      <h2 className="text-base font-medium text-white">
        {(noConfig || noConfigPastMonth) ? t('noConfig.title') : t('config.title')}
      </h2>
      {(noConfig || noConfigPastMonth) && (
        <p className="text-sm text-gray-400">
          {noConfigPastMonth ? t('noConfig.pastMonth') : t('noConfig.hint')}
        </p>
      )}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div>
          <label htmlFor="cfg-base-salary" className="block text-xs text-gray-400 mb-1">{t('config.baseSalary')}</label>
          <input
            id="cfg-base-salary"
            type="number"
            value={baseSalary}
            onChange={e => setBaseSalary(e.target.value)}
            className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
            placeholder="0"
            min="0"
          />
        </div>
        <div>
          <label htmlFor="cfg-hourly-rate" className="block text-xs text-gray-400 mb-1">{t('config.hourlyRate')}</label>
          <input
            id="cfg-hourly-rate"
            type="number"
            value={hourlyRate}
            onChange={e => setHourlyRate(e.target.value)}
            className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
            placeholder="0"
            min="0"
          />
        </div>
        <div>
          <label htmlFor="cfg-internal-rate" className="block text-xs text-gray-400 mb-1">{t('config.internalHourlyRate')}</label>
          <input
            id="cfg-internal-rate"
            type="number"
            value={internalHourlyRate}
            onChange={e => setInternalHourlyRate(e.target.value)}
            className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
            placeholder="0"
            min="0"
          />
        </div>
        <div>
          <label htmlFor="cfg-taxable-benefits" className="block text-xs text-gray-400 mb-1">{t('config.taxableBenefits')}</label>
          <input
            id="cfg-taxable-benefits"
            type="number"
            value={taxableBenefits}
            onChange={e => setTaxableBenefits(e.target.value)}
            className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
            placeholder="0"
            min="0"
          />
        </div>
        <div>
          <label htmlFor="cfg-standard-hours" className="block text-xs text-gray-400 mb-1">{t('config.standardHours')}</label>
          <input
            id="cfg-standard-hours"
            type="number"
            value={standardHours}
            onChange={e => setStandardHours(e.target.value)}
            className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
            placeholder="7.5"
            min="0"
            step="0.5"
          />
        </div>
        <div>
          <label htmlFor="cfg-currency" className="block text-xs text-gray-400 mb-1">{t('config.currency')}</label>
          <input
            id="cfg-currency"
            type="text"
            value={currency}
            onChange={e => setCurrency(e.target.value.toUpperCase())}
            className="w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
            placeholder="NOK"
            maxLength={3}
          />
        </div>
      </div>
      {saveError && <p className="text-sm text-red-400">{saveError}</p>}
      <div className="flex gap-3">
        <button
          onClick={handleSave}
          disabled={saving}
          className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-sm rounded-lg transition-colors"
        >
          {saving ? '...' : t('config.save')}
        </button>
        {!noConfig && (
          <button
            onClick={onClose}
            className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-white text-sm rounded-lg transition-colors"
          >
            {t('config.cancel')}
          </button>
        )}
      </div>
    </div>
  )
}
