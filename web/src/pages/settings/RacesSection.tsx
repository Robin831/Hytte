import { useTranslation } from 'react-i18next'
import type { PreferenceSectionProps } from './types'

type RacesSectionProps = Pick<PreferenceSectionProps, 'preferences' | 'saving' | 'savePreference'>

const TOGGLES = [
  { key: 'races_notify_deadlines', label: 'deadlines' },
  { key: 'races_notify_changes', label: 'changes' },
] as const

// Push toggles for tracked races. Both are on unless set to exactly 'false',
// matching how the server-side notifier reads them — tracking a race is the opt-in.
function RacesSection({ preferences, saving, savePreference }: RacesSectionProps) {
  const { t } = useTranslation('settings')

  return (
    <div className="space-y-4">
      {TOGGLES.map(({ key, label }) => {
        const enabled = preferences[key] !== 'false'
        return (
          <div key={key} className="flex items-center justify-between gap-4">
            <div>
              <p className="font-medium">{t(`races.${label}Title`)}</p>
              <p className="text-sm text-gray-400">{t(`races.${label}Description`)}</p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={enabled}
              aria-label={t(`races.${label}Title`)}
              onClick={async () => {
                await savePreference(key, enabled ? 'false' : 'true')
              }}
              disabled={saving}
              className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                enabled ? 'bg-blue-600' : 'bg-gray-600'
              }`}
            >
              <span
                className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                  enabled ? 'translate-x-6' : 'translate-x-1'
                }`}
              />
            </button>
          </div>
        )
      })}
      <p className="text-xs text-gray-500">{t('races.pushHint')}</p>
    </div>
  )
}

export default RacesSection
