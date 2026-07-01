import { useTranslation } from 'react-i18next'
import type { PreferenceSectionProps } from './types'

type PokemonSectionProps = Pick<PreferenceSectionProps, 'preferences' | 'saving' | 'savePreference'>

function PokemonSection({ preferences, saving, savePreference }: PokemonSectionProps) {
  const { t } = useTranslation(['settings', 'common'])

  return (
    <div className="flex items-center justify-between">
      <div>
        <p className="font-medium">{t('pokemon.scanPushTitle')}</p>
        <p className="text-sm text-gray-400">{t('pokemon.scanPushDescription')}</p>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={preferences.pokemon_scan_push_enabled !== 'false'}
        aria-label={
          preferences.pokemon_scan_push_enabled !== 'false'
            ? t('pokemon.disableScanPush')
            : t('pokemon.enableScanPush')
        }
        onClick={async () => {
          const next =
            preferences.pokemon_scan_push_enabled === 'false' ? 'true' : 'false'
          await savePreference('pokemon_scan_push_enabled', next)
        }}
        disabled={saving}
        className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
          preferences.pokemon_scan_push_enabled !== 'false' ? 'bg-blue-600' : 'bg-gray-600'
        }`}
      >
        <span
          className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
            preferences.pokemon_scan_push_enabled !== 'false'
              ? 'translate-x-6'
              : 'translate-x-1'
          }`}
        />
      </button>
    </div>
  )
}

export default PokemonSection
