import { useTranslation } from 'react-i18next'
import { Plus, Trash2 } from 'lucide-react'
import { Select, type SelectOption } from '../../../components/ui/select'
import { formatMins } from '../dateUtils'
import { normalizePresetIcon } from '../presetIcons'
import type { WorkDeduction, WorkDeductionPreset } from '../types'
import type { DeductionDraft } from '../useDayEntry'

export interface DeductionListProps {
  deductions: WorkDeduction[]
  lunch: boolean
  /** Lunch minutes of the saved day; shown next to the checkbox when positive. */
  lunchMinutes: number
  presets: WorkDeductionPreset[]
  /** Presets in dropdown order (recently used first). */
  sortedPresets: WorkDeductionPreset[]
  selectedPresetId: number | null
  draft: DeductionDraft
  /** A day edit or punch action is in flight; locks the controls. */
  disabled: boolean
  onLunchToggle: () => void
  onPresetToggle: (preset: WorkDeductionPreset) => void
  onPresetSelect: (value: string) => void
  onDraftChange: (patch: Partial<DeductionDraft>) => void
  onAdd: () => void
  onDelete: (deductionID: number) => void
  onManagePresets: () => void
}

/** The deductions section: lunch, preset chips, custom deductions and the add row. */
export default function DeductionList({
  deductions,
  lunch,
  lunchMinutes,
  presets,
  sortedPresets,
  selectedPresetId,
  draft,
  disabled: busy,
  onLunchToggle,
  onPresetToggle,
  onPresetSelect,
  onDraftChange,
  onAdd,
  onDelete,
  onManagePresets,
}: DeductionListProps) {
  const { t } = useTranslation(['workhours', 'common'])

  return (
    <section className="space-y-3">
      <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
        {t('workhours:deductions')}
      </h2>

      {/* Lunch checkbox */}
      <label className="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          checked={lunch}
          onChange={onLunchToggle}
          disabled={busy}
          className="w-4 h-4 rounded accent-blue-500"
        />
        <span className="text-sm text-white">{t('workhours:lunch')}</span>
        {lunchMinutes > 0 && (
          <span className="text-xs text-gray-400 font-mono">({formatMins(lunchMinutes)})</span>
        )}
      </label>

      {/* Preset chips */}
      {presets.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {presets.map(p => {
            const active = deductions.some(d => d.preset_id === p.id)
            return (
              <button
                key={p.id}
                type="button"
                onClick={() => onPresetToggle(p)}
                disabled={busy}
                className={`px-3 py-1 rounded-full text-xs font-medium transition-colors cursor-pointer disabled:opacity-40 ${
                  active
                    ? 'bg-blue-600 text-white'
                    : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
                }`}
              >
                {p.name} ({t('workhours:minutesValue', { count: p.default_minutes })})
              </button>
            )
          })}
        </div>
      )}

      {/* Custom deductions list */}
      {deductions.filter(d => !d.preset_id).length > 0 && (
        <div className="space-y-1">
          {deductions
            .filter(d => !d.preset_id)
            .map(d => (
              <div key={d.id} className="flex items-center gap-2 text-sm bg-gray-800/50 rounded px-2 py-1">
                <span className="flex-1 text-gray-300">{d.name}</span>
                <span className="text-gray-400 text-xs font-mono">{formatMins(d.minutes)}</span>
                <button
                  type="button"
                  onClick={() => onDelete(d.id)}
                  disabled={busy}
                  className="text-gray-500 hover:text-red-400 transition-colors disabled:opacity-40 cursor-pointer"
                  aria-label={t('workhours:removeDeduction')}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
        </div>
      )}

      {/* Add custom deduction */}
      <div className="space-y-2">
        {/* Preset dropdown */}
        {sortedPresets.length > 0 && (
          <div className="flex items-center gap-2">
            <Select
              value={selectedPresetId !== null ? String(selectedPresetId) : ''}
              onChange={onPresetSelect}
              placeholder={t('workhours:presetDropdownPlaceholder')}
              aria-label={t('workhours:presetDropdownPlaceholder')}
              className="flex-1"
              options={sortedPresets.map(p => {
                const icon = normalizePresetIcon(p.icon)
                return {
                  value: String(p.id),
                  label: `${p.name} — ${t('workhours:minutesValue', { count: p.default_minutes })}`,
                  icon: icon || undefined,
                } satisfies SelectOption
              })}
            />
            <button
              type="button"
              onClick={onManagePresets}
              className="text-xs text-gray-400 hover:text-blue-400 transition-colors cursor-pointer whitespace-nowrap"
            >
              {t('workhours:managePresets')}
            </button>
          </div>
        )}
        <div className="flex items-center gap-2 flex-wrap">
          <input
            type="text"
            value={draft.name}
            onChange={e => onDraftChange({ name: e.target.value })}
            placeholder={t('workhours:deductionNamePlaceholder')}
            aria-label={t('workhours:deductionNamePlaceholder')}
            className="flex-1 min-w-32 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none placeholder-gray-500"
          />
          <input
            type="number"
            value={draft.minutes}
            onChange={e => onDraftChange({ minutes: e.target.value })}
            placeholder={t('workhours:minutesShort')}
            min="1"
            className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none placeholder-gray-500"
            aria-label={t('workhours:minutes')}
          />
          <button
            type="button"
            onClick={onAdd}
            disabled={!draft.name.trim() || !draft.minutes || busy}
            className="flex items-center gap-1 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm rounded transition-colors cursor-pointer"
            aria-label={t('workhours:addDeduction')}
          >
            <Plus size={14} />
          </button>
        </div>
      </div>
    </section>
  )
}
