import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import { formatDate } from '../../utils/formatDate'
import LanguageSwitcher from '../../components/LanguageSwitcher'
import type { PreferenceSectionProps } from './types'

type ProfileSectionProps = Pick<PreferenceSectionProps, 'preferences' | 'saving' | 'savePreference'>

function ProfileSection({ preferences, saving, savePreference }: ProfileSectionProps) {
  const { t } = useTranslation(['settings', 'common'])
  const { user } = useAuth()
  const [cityNames, setCityNames] = useState<string[]>([])

  // Fetch available locations from the backend (single source of truth).
  useEffect(() => {
    let cancelled = false
    fetch('/api/weather/locations')
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch locations')
        return r.json()
      })
      .then((data) => {
        if (cancelled) return
        const locs = (data.locations ?? []) as { name: string }[]
        setCityNames(locs.map((l) => l.name).sort())
      })
      .catch(() => {
        // Best-effort: dropdown will be empty until loaded.
      })
    return () => { cancelled = true }
  }, [])

  if (!user) return null

  const memberSince = formatDate(user.created_at, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })

  return (
    <>
      <div className="flex items-center gap-4 mb-4">
        {user.picture ? (
          <img
            src={user.picture}
            alt={user.name}
            className="w-16 h-16 rounded-full border-2 border-gray-600"
            referrerPolicy="no-referrer"
          />
        ) : (
          <div className="w-16 h-16 rounded-full bg-blue-600 flex items-center justify-center text-xl font-medium">
            {user.name.charAt(0).toUpperCase()}
          </div>
        )}
        <div>
          <p className="text-lg font-medium">{user.name}</p>
          <p className="text-sm text-gray-400">{user.email}</p>
        </div>
      </div>
      <p className="text-sm text-gray-500">
        {t('profile.memberSince', { date: memberSince })}
      </p>

      {/* Appearance */}
      <div className="border-t border-gray-700 pt-4 mt-4">
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">{t('appearance.theme')}</p>
            <p className="text-sm text-gray-400">{t('appearance.themeDescription')}</p>
          </div>
          <select
            value={preferences.theme || 'dark'}
            onChange={(e) => savePreference('theme', e.target.value)}
            disabled={saving}
            aria-label={t('appearance.theme')}
            className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="dark">{t('appearance.themeDark')}</option>
            <option value="light" disabled>{t('appearance.themeLight')}</option>
          </select>
        </div>
      </div>

      {/* Language */}
      <div className="border-t border-gray-700 pt-4 mt-4">
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="font-medium">{t('language.displayLanguage')}</p>
            <p className="text-sm text-gray-400">{t('language.displayLanguageDescription')}</p>
          </div>
          <div className="w-52">
            <LanguageSwitcher />
          </div>
        </div>
      </div>

      {/* Location */}
      <div className="border-t border-gray-700 pt-4 mt-4">
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">{t('location.homeCity')}</p>
            <p className="text-sm text-gray-400">{t('location.homeCityDescription')}</p>
          </div>
          <select
            value={preferences.home_location || ''}
            onChange={(e) => savePreference('home_location', e.target.value)}
            disabled={saving}
            aria-label={t('location.homeCity')}
            className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">{t('location.selectCity')}</option>
            {cityNames.map((city) => (
              <option key={city} value={city}>
                {city}
              </option>
            ))}
          </select>
        </div>
      </div>
    </>
  )
}

export default ProfileSection
