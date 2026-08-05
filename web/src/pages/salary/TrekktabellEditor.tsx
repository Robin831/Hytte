import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import DecimalField from './DecimalField'
import { collectDecimalErrors, parseRequiredDecimal } from './decimalDraft'
import type { DecimalDraft } from './decimalDraft'
import type { TrekktabellParams } from './types'
import type { SalaryData } from './useSalaryData'
import InlineRetry from './InlineRetry'

interface TrekktabellEditorProps {
  salary: SalaryData
}

/** Compact field styling used throughout this card. */
const FIELD_CLASS = 'w-full bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1'

/**
 * Editor drafts are held as raw text rather than numbers so a partially typed
 * value ("0,") survives a re-render — parsing on every keystroke would drop the
 * trailing separator and move the caret.
 */
interface TrekktabellDraft {
  minstefradrag_rate: string
  minstefradrag_min: string
  minstefradrag_max: string
  personfradrag: string
  alminnelig_skatt_rate: string
  trygdeavgift: string
  trinnskatt_tiers: { income_from: string; rate: string }[]
}

function toDraft(params: TrekktabellParams): TrekktabellDraft {
  return {
    minstefradrag_rate: String(params.minstefradrag_rate),
    minstefradrag_min: String(params.minstefradrag_min),
    minstefradrag_max: String(params.minstefradrag_max),
    personfradrag: String(params.personfradrag),
    alminnelig_skatt_rate: String(params.alminnelig_skatt_rate),
    trygdeavgift: String(params.trygdeavgift),
    trinnskatt_tiers: params.trinnskatt_tiers.map(tier => ({
      income_from: String(tier.income_from),
      rate: String(tier.rate),
    })),
  }
}

/**
 * Trekktabell parameters card: read-only summary plus an inline editor. Owns the
 * editor's draft state and toggle; persistence goes through useSalaryData.
 */
export default function TrekktabellEditor({ salary }: TrekktabellEditorProps) {
  const { t } = useTranslation('salary')
  const { trekktabell, trekktabellError, retryTrekktabell, saveTrekktabell, resetTrekktabellDefaults } = salary

  const [showEditor, setShowEditor] = useState(false)
  const [draft, setDraft] = useState<TrekktabellDraft | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string | undefined>>({})

  // A failed fetch is explained inline; only a genuinely absent (not yet loaded)
  // trekktabell renders nothing.
  if (trekktabellError) {
    return <InlineRetry message={t('errors.failedToLoadTrekktabell')} onRetry={retryTrekktabell} />
  }

  if (!trekktabell) return null

  const updateDraft = (patch: Partial<TrekktabellDraft>) =>
    setDraft(prev => prev && { ...prev, ...patch })

  const updateTier = (index: number, patch: Partial<{ income_from: string; rate: string }>) =>
    setDraft(prev => prev && {
      ...prev,
      trinnskatt_tiers: prev.trinnskatt_tiers.map((tier, i) => i === index ? { ...tier, ...patch } : tier),
    })

  const handleSave = async () => {
    if (!draft) return

    // Parse the whole draft first — a field that does not parse blocks the save
    // with an inline message rather than being written as 0.
    const scalars = {
      minstefradrag_rate: parseRequiredDecimal(draft.minstefradrag_rate),
      minstefradrag_min: parseRequiredDecimal(draft.minstefradrag_min),
      minstefradrag_max: parseRequiredDecimal(draft.minstefradrag_max),
      personfradrag: parseRequiredDecimal(draft.personfradrag),
      alminnelig_skatt_rate: parseRequiredDecimal(draft.alminnelig_skatt_rate),
      trygdeavgift: parseRequiredDecimal(draft.trygdeavgift),
    }
    const tierDrafts: Record<string, DecimalDraft> = {}
    draft.trinnskatt_tiers.forEach((tier, i) => {
      tierDrafts[`tier-${i}-income_from`] = parseRequiredDecimal(tier.income_from)
      tierDrafts[`tier-${i}-rate`] = parseRequiredDecimal(tier.rate)
    })

    const messages = { required: t('validation.required'), invalid: t('validation.invalidNumber') }
    const errors = {
      ...collectDecimalErrors(scalars, messages),
      ...collectDecimalErrors(tierDrafts, messages),
    }
    setFieldErrors(errors)
    if (Object.keys(errors).length > 0) return

    setSaving(true)
    setSaveError(null)
    try {
      const updated = await saveTrekktabell({
        ...trekktabell,
        minstefradrag_rate: scalars.minstefradrag_rate.value,
        minstefradrag_min: scalars.minstefradrag_min.value,
        minstefradrag_max: scalars.minstefradrag_max.value,
        personfradrag: scalars.personfradrag.value,
        alminnelig_skatt_rate: scalars.alminnelig_skatt_rate.value,
        trygdeavgift: scalars.trygdeavgift.value,
        trinnskatt_tiers: draft.trinnskatt_tiers.map((_, i) => ({
          income_from: tierDrafts[`tier-${i}-income_from`].value,
          rate: tierDrafts[`tier-${i}-rate`].value,
        })),
      })
      setDraft(toDraft(updated))
      setShowEditor(false)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t('errors.failedToSave'))
    } finally {
      setSaving(false)
    }
  }

  const handleResetDefaults = async () => {
    setSaving(true)
    setSaveError(null)
    setFieldErrors({})
    try {
      const updated = await resetTrekktabellDefaults(trekktabell)
      setDraft(toDraft(updated))
      setShowEditor(false)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t('errors.failedToReset'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="bg-gray-800 rounded-xl p-5 space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-medium text-white">
          {t('trekktabell.title')} — {t('trekktabell.year', { year: trekktabell.year })}
        </h2>
        <button
          type="button"
          onClick={() => {
            setShowEditor(v => !v)
            setDraft(toDraft(trekktabell))
            setSaveError(null)
            setFieldErrors({})
          }}
          className="text-xs text-gray-400 hover:text-white transition-colors"
        >
          {showEditor ? t('trekktabell.cancel') : t('trekktabell.edit')}
        </button>
      </div>

      {!showEditor && (
        <div className="divide-y divide-gray-700/50 text-sm">
          <div className="flex justify-between items-center py-1.5">
            <span className="text-gray-400">{t('trekktabell.minstefradrag')}</span>
            <span className="text-white tabular-nums">
              {(trekktabell.minstefradrag_rate * 100).toFixed(0)}%,{' '}
              {new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(trekktabell.minstefradrag_min)}
              {' – '}
              {new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(trekktabell.minstefradrag_max)}
            </span>
          </div>
          <div className="flex justify-between items-center py-1.5">
            <span className="text-gray-400">{t('trekktabell.personfradrag')}</span>
            <span className="text-white tabular-nums">
              {new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(trekktabell.personfradrag)}
            </span>
          </div>
          <div className="flex justify-between items-center py-1.5">
            <span className="text-gray-400">{t('trekktabell.alminneligSkatt')}</span>
            <span className="text-white tabular-nums">
              {(trekktabell.alminnelig_skatt_rate * 100).toFixed(0)}%
            </span>
          </div>
          <div className="flex justify-between items-center py-1.5">
            <span className="text-gray-400">{t('trekktabell.trygdeavgift')}</span>
            <span className="text-white tabular-nums">
              {(trekktabell.trygdeavgift * 100).toFixed(1)}%
            </span>
          </div>
          {trekktabell.trinnskatt_tiers.length > 0 && (
            <div className="pt-1.5">
              <p className="text-gray-400 text-xs mb-1">{t('trekktabell.trinnskatt')}</p>
              {trekktabell.trinnskatt_tiers.map((tier, i) => (
                <div key={i} className="flex justify-between items-center py-0.5 text-xs">
                  <span className="text-gray-500">
                    {t('trekktabell.trinnskattFrom', {
                      from: new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(tier.income_from),
                    })}
                  </span>
                  <span className="text-gray-300 tabular-nums">
                    {(tier.rate * 100).toFixed(1)}%
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {showEditor && draft && (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3 text-sm">
            <DecimalField
              id="tt-minstefradrag-rate"
              label={t('trekktabell.minstefradragRate')}
              value={draft.minstefradrag_rate}
              onChange={v => updateDraft({ minstefradrag_rate: v })}
              error={fieldErrors.minstefradrag_rate}
              inputClassName={FIELD_CLASS}
            />
            <DecimalField
              id="tt-minstefradrag-min"
              label={t('trekktabell.minstefradragMin')}
              value={draft.minstefradrag_min}
              onChange={v => updateDraft({ minstefradrag_min: v })}
              error={fieldErrors.minstefradrag_min}
              inputClassName={FIELD_CLASS}
            />
            <DecimalField
              id="tt-minstefradrag-max"
              label={t('trekktabell.minstefradragMax')}
              value={draft.minstefradrag_max}
              onChange={v => updateDraft({ minstefradrag_max: v })}
              error={fieldErrors.minstefradrag_max}
              inputClassName={FIELD_CLASS}
            />
            <DecimalField
              id="tt-personfradrag"
              label={t('trekktabell.personfradragLabel')}
              value={draft.personfradrag}
              onChange={v => updateDraft({ personfradrag: v })}
              error={fieldErrors.personfradrag}
              inputClassName={FIELD_CLASS}
            />
            <DecimalField
              id="tt-alminnelig-skatt-rate"
              label={t('trekktabell.alminneligSkattRate')}
              value={draft.alminnelig_skatt_rate}
              onChange={v => updateDraft({ alminnelig_skatt_rate: v })}
              error={fieldErrors.alminnelig_skatt_rate}
              inputClassName={FIELD_CLASS}
            />
            <DecimalField
              id="tt-trygdeavgift"
              label={t('trekktabell.trygdeavgiftRate')}
              value={draft.trygdeavgift}
              onChange={v => updateDraft({ trygdeavgift: v })}
              error={fieldErrors.trygdeavgift}
              inputClassName={FIELD_CLASS}
            />
          </div>

          <div>
            <p className="text-xs text-gray-400 mb-1">{t('trekktabell.trinnskattTiers')}</p>
            <div className="grid grid-cols-2 gap-1 text-xs text-gray-500 px-1 mb-1">
              <span>{t('trekktabell.incomeFromHeader')}</span>
              <span>{t('trekktabell.rate')}</span>
            </div>
            {draft.trinnskatt_tiers.map((tier, i) => (
              <div key={i} className="grid grid-cols-2 gap-2 mb-1">
                <DecimalField
                  id={`tt-tier-${i}-income-from`}
                  ariaLabel={t('trekktabell.tierIncomeFromAria', { n: i + 1 })}
                  value={tier.income_from}
                  onChange={v => updateTier(i, { income_from: v })}
                  error={fieldErrors[`tier-${i}-income_from`]}
                  inputClassName={FIELD_CLASS}
                />
                <DecimalField
                  id={`tt-tier-${i}-rate`}
                  ariaLabel={t('trekktabell.tierRateAria', { n: i + 1 })}
                  value={tier.rate}
                  onChange={v => updateTier(i, { rate: v })}
                  error={fieldErrors[`tier-${i}-rate`]}
                  inputClassName={FIELD_CLASS}
                />
              </div>
            ))}
          </div>

          {saveError && <p className="text-sm text-red-400">{saveError}</p>}
          <div className="flex gap-2 flex-wrap">
            <button
              type="button"
              onClick={handleSave}
              disabled={saving}
              className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-sm rounded-lg transition-colors"
            >
              {saving ? '...' : t('trekktabell.save')}
            </button>
            <button
              type="button"
              onClick={handleResetDefaults}
              disabled={saving}
              className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded-lg transition-colors"
            >
              {t('trekktabell.resetDefaults')}
            </button>
            <button
              type="button"
              onClick={() => { setShowEditor(false); setSaveError(null); setFieldErrors({}) }}
              className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded-lg transition-colors"
            >
              {t('trekktabell.cancel')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
