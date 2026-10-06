import { useTranslation } from 'react-i18next'
import type { PreferenceSectionProps } from './types'

type OffersSectionProps = Pick<PreferenceSectionProps, 'preferences' | 'saving' | 'savePreference'>

// Opt-in for watchlist push notifications. Off unless the preference is
// exactly 'true', matching how the server-side notify pass reads it.
function OffersSection({ preferences, saving, savePreference }: OffersSectionProps) {
  const { t } = useTranslation(['settings', 'common'])
  const enabled = preferences.offers_notify === 'true'

  return (
    <div className="flex items-center justify-between gap-4">
      <div>
        <p className="font-medium">{t('offers.notifyTitle')}</p>
        <p className="text-sm text-gray-400">{t('offers.notifyDescription')}</p>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={enabled}
        aria-label={enabled ? t('offers.disableNotify') : t('offers.enableNotify')}
        onClick={async () => {
          await savePreference('offers_notify', enabled ? 'false' : 'true')
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
}

export default OffersSection
