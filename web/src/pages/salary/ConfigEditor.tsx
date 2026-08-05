import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import DecimalField from './DecimalField'
import { collectDecimalErrors, parseOptionalDecimal, parseRequiredDecimal } from './decimalDraft'
import type { SalaryData } from './useSalaryData'

interface ConfigEditorProps {
  salary: SalaryData
  noConfig: boolean
  noConfigPastMonth: boolean
  onClose: () => void
}

type ConfigField =
  | 'baseSalary'
  | 'hourlyRate'
  | 'internalHourlyRate'
  | 'taxableBenefits'
  | 'standardHours'

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
  const [fieldErrors, setFieldErrors] = useState<Partial<Record<ConfigField, string>>>({})

  const handleSave = async () => {
    // Parse every field before touching the network. An unparseable value blocks
    // the save with an inline message instead of silently landing in the config
    // as 0 (or the 7.5 standard-hours default).
    const drafts = {
      baseSalary: parseRequiredDecimal(baseSalary),
      hourlyRate: parseRequiredDecimal(hourlyRate),
      internalHourlyRate: parseOptionalDecimal(internalHourlyRate),
      taxableBenefits: parseOptionalDecimal(taxableBenefits),
      standardHours: parseRequiredDecimal(standardHours),
    }
    const errors = collectDecimalErrors(drafts, {
      required: t('validation.required'),
      invalid: t('validation.invalidNumber'),
    })
    setFieldErrors(errors)
    if (Object.keys(errors).length > 0) return

    setSaving(true)
    setSaveError(null)
    try {
      await saveConfig({
        base_salary: drafts.baseSalary.value,
        hourly_rate: drafts.hourlyRate.value,
        internal_hourly_rate: drafts.internalHourlyRate.value,
        taxable_benefits: drafts.taxableBenefits.value,
        standard_hours: drafts.standardHours.value,
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
        <DecimalField
          id="cfg-base-salary"
          label={t('config.baseSalary')}
          value={baseSalary}
          onChange={setBaseSalary}
          placeholder="0"
          error={fieldErrors.baseSalary}
        />
        <DecimalField
          id="cfg-hourly-rate"
          label={t('config.hourlyRate')}
          value={hourlyRate}
          onChange={setHourlyRate}
          placeholder="0"
          error={fieldErrors.hourlyRate}
        />
        <DecimalField
          id="cfg-internal-rate"
          label={t('config.internalHourlyRate')}
          value={internalHourlyRate}
          onChange={setInternalHourlyRate}
          placeholder="0"
          error={fieldErrors.internalHourlyRate}
        />
        <DecimalField
          id="cfg-taxable-benefits"
          label={t('config.taxableBenefits')}
          value={taxableBenefits}
          onChange={setTaxableBenefits}
          placeholder="0"
          error={fieldErrors.taxableBenefits}
        />
        <DecimalField
          id="cfg-standard-hours"
          label={t('config.standardHours')}
          value={standardHours}
          onChange={setStandardHours}
          placeholder="7.5"
          error={fieldErrors.standardHours}
        />
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
