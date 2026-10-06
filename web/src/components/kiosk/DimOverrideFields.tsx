import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import type { DimMode, DimOverride } from './dimOverride'

interface Props {
  value: DimOverride
  onChange: (value: DimOverride) => void
  disabled?: boolean
}

const MODES: DimMode[] = ['auto', 'on', 'off']

// Form controls for a kiosk token's night-mode override, shared by the create
// dialog and the edit dialog in TokenManager.
export default function DimOverrideFields({ value, onChange, disabled }: Props) {
  const { t } = useTranslation('settings')
  const modeId = useId()
  const startId = useId()
  const endId = useId()
  const hintId = useId()

  const inputClass =
    'w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500 [color-scheme:dark] disabled:opacity-50'

  return (
    <fieldset className="space-y-3" disabled={disabled}>
      <legend className="block text-sm font-medium text-gray-300 mb-1">
        {t('kioskTokens.dim.heading')}
      </legend>
      <div>
        <label className="block text-xs text-gray-400 mb-1" htmlFor={modeId}>
          {t('kioskTokens.dim.modeLabel')}
        </label>
        <select
          id={modeId}
          value={value.mode}
          onChange={(e) => onChange({ ...value, mode: e.target.value as DimMode })}
          className={inputClass}
        >
          {MODES.map((mode) => (
            <option key={mode} value={mode}>
              {t(`kioskTokens.dim.mode.${mode}`)}
            </option>
          ))}
        </select>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor={startId}>
            {t('kioskTokens.dim.startLabel')}
          </label>
          <input
            id={startId}
            type="time"
            value={value.start}
            onChange={(e) => onChange({ ...value, start: e.target.value })}
            placeholder={t('kioskTokens.dim.timePlaceholder')}
            aria-describedby={hintId}
            className={inputClass}
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1" htmlFor={endId}>
            {t('kioskTokens.dim.endLabel')}
          </label>
          <input
            id={endId}
            type="time"
            value={value.end}
            onChange={(e) => onChange({ ...value, end: e.target.value })}
            placeholder={t('kioskTokens.dim.timePlaceholder')}
            aria-describedby={hintId}
            className={inputClass}
          />
        </div>
      </div>
      <p id={hintId} className="text-xs text-gray-500">{t('kioskTokens.dim.hint')}</p>
    </fieldset>
  )
}
