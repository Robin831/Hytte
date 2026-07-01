import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TrekktabellParams } from './types'
import type { SalaryData } from './useSalaryData'

interface TrekktabellEditorProps {
  salary: SalaryData
}

/**
 * Trekktabell parameters card: read-only summary plus an inline editor. Owns the
 * editor's draft state and toggle; persistence goes through useSalaryData.
 */
export default function TrekktabellEditor({ salary }: TrekktabellEditorProps) {
  const { t } = useTranslation('salary')
  const { trekktabell, saveTrekktabell, resetTrekktabellDefaults } = salary

  const [showEditor, setShowEditor] = useState(false)
  const [editorTrekktabell, setEditorTrekktabell] = useState<TrekktabellParams | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  if (!trekktabell) return null

  const handleSave = async () => {
    if (!editorTrekktabell) return
    setSaving(true)
    setSaveError(null)
    try {
      const updated = await saveTrekktabell(editorTrekktabell)
      setEditorTrekktabell(updated)
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
    try {
      const updated = await resetTrekktabellDefaults(trekktabell)
      setEditorTrekktabell(updated)
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
            setEditorTrekktabell(trekktabell)
            setSaveError(null)
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

      {showEditor && editorTrekktabell && (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('trekktabell.minstefradragRate')}</label>
              <input
                type="number"
                value={editorTrekktabell.minstefradrag_rate}
                onChange={e => setEditorTrekktabell(p => p && ({ ...p, minstefradrag_rate: parseFloat(e.target.value) || 0 }))}
                className="w-full bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                min="0" max="1" step="0.01"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('trekktabell.minstefradragMin')}</label>
              <input
                type="number"
                value={editorTrekktabell.minstefradrag_min}
                onChange={e => setEditorTrekktabell(p => p && ({ ...p, minstefradrag_min: parseFloat(e.target.value) || 0 }))}
                className="w-full bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                min="0"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('trekktabell.minstefradragMax')}</label>
              <input
                type="number"
                value={editorTrekktabell.minstefradrag_max}
                onChange={e => setEditorTrekktabell(p => p && ({ ...p, minstefradrag_max: parseFloat(e.target.value) || 0 }))}
                className="w-full bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                min="0"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('trekktabell.personfradragLabel')}</label>
              <input
                type="number"
                value={editorTrekktabell.personfradrag}
                onChange={e => setEditorTrekktabell(p => p && ({ ...p, personfradrag: parseFloat(e.target.value) || 0 }))}
                className="w-full bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                min="0"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('trekktabell.alminneligSkattRate')}</label>
              <input
                type="number"
                value={editorTrekktabell.alminnelig_skatt_rate}
                onChange={e => setEditorTrekktabell(p => p && ({ ...p, alminnelig_skatt_rate: parseFloat(e.target.value) || 0 }))}
                className="w-full bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                min="0" max="1" step="0.01"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t('trekktabell.trygdeavgiftRate')}</label>
              <input
                type="number"
                value={editorTrekktabell.trygdeavgift}
                onChange={e => setEditorTrekktabell(p => p && ({ ...p, trygdeavgift: parseFloat(e.target.value) || 0 }))}
                className="w-full bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                min="0" max="1" step="0.001"
              />
            </div>
          </div>

          <div>
            <p className="text-xs text-gray-400 mb-1">{t('trekktabell.trinnskattTiers')}</p>
            <div className="grid grid-cols-2 gap-1 text-xs text-gray-500 px-1 mb-1">
              <span>{t('trekktabell.incomeFromHeader')}</span>
              <span>{t('trekktabell.rate')}</span>
            </div>
            {editorTrekktabell.trinnskatt_tiers.map((tier, i) => (
              <div key={i} className="grid grid-cols-2 gap-2 mb-1">
                <input
                  type="number"
                  value={tier.income_from}
                  onChange={e => {
                    const tiers = [...editorTrekktabell.trinnskatt_tiers]
                    tiers[i] = { ...tiers[i], income_from: parseFloat(e.target.value) || 0 }
                    setEditorTrekktabell(p => p && ({ ...p, trinnskatt_tiers: tiers }))
                  }}
                  className="bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                  min="0"
                />
                <input
                  type="number"
                  value={tier.rate}
                  onChange={e => {
                    const tiers = [...editorTrekktabell.trinnskatt_tiers]
                    tiers[i] = { ...tiers[i], rate: parseFloat(e.target.value) || 0 }
                    setEditorTrekktabell(p => p && ({ ...p, trinnskatt_tiers: tiers }))
                  }}
                  className="bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
                  min="0" max="1" step="0.001"
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
              onClick={() => { setShowEditor(false); setSaveError(null) }}
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
