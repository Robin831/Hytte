import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import { CollapsibleSection } from '../components/CollapsibleSection'
import { Skeleton } from '../components/ui/skeleton'
import ProfileSection from './settings/ProfileSection'
import TrainingSection from './settings/TrainingSection'
import NotificationsSection from './settings/NotificationsSection'
import SecuritySection from './settings/SecuritySection'
import IntegrationsSection from './settings/IntegrationsSection'
import PokemonSection from './settings/PokemonSection'
import AIAutomationSection from './settings/AIAutomationSection'
import KioskTokensSection from './settings/KioskTokensSection'

function Settings() {
  const { t } = useTranslation(['settings', 'common'])
  const { user, familyStatus, hasFeature } = useAuth()
  const isKidsPlan = Boolean(user?.features?.['kids_stars'])
  const isChild = isKidsPlan && familyStatus?.is_child === true
  const [preferences, setPreferences] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saveToast, setSaveToast] = useState<{ type: 'success' | 'error'; message: string } | null>(null)
  const saveToastTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (saveToastTimer.current) clearTimeout(saveToastTimer.current)
    }
  }, [])

  const showToast = useCallback((type: 'success' | 'error', message: string) => {
    setSaveToast({ type, message })
    if (saveToastTimer.current) clearTimeout(saveToastTimer.current)
    saveToastTimer.current = setTimeout(() => setSaveToast(null), 3000)
  }, [])

  const savePreferences = useCallback(async (prefs: Record<string, string>, toast = false) => {
    setSaving(true)
    try {
      const res = await fetch('/api/settings/preferences', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ preferences: prefs }),
      })
      if (res.ok) {
        const data = await res.json()
        setPreferences(data.preferences || {})
        if (toast) showToast('success', t('training.saveSuccess'))
      } else if (toast) {
        showToast('error', t('training.saveError'))
      }
    } catch {
      if (toast) showToast('error', t('training.saveError'))
    } finally {
      setSaving(false)
    }
  }, [showToast, t])

  const savePreference = useCallback(async (key: string, value: string, toast = false) => {
    await savePreferences({ [key]: value }, toast)
  }, [savePreferences])

  useEffect(() => {
    let cancelled = false
    async function loadData() {
      try {
        const prefsRes = await fetch('/api/settings/preferences', { credentials: 'include' })
        if (cancelled) return
        if (prefsRes.ok) {
          const data = await prefsRes.json()
          setPreferences(data.preferences || {})
        }
      } catch (err) {
        console.error('Failed to load settings data:', err)
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    loadData()
    return () => { cancelled = true }
  }, [])

  if (!user) return null
  if (loading) {
    return (
      <main className="max-w-2xl mx-auto px-4 py-8 space-y-4" role="status" aria-live="polite" aria-busy="true">
        <p className="sr-only">{t('loading')}</p>
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-32 w-full" />
      </main>
    )
  }

  return (
    <main className="max-w-2xl mx-auto px-4 py-8 min-h-screen">
      {saveToast && (
        <div
          role="status"
          aria-live="polite"
          className={`fixed top-4 right-4 z-50 px-4 py-3 rounded-lg text-sm font-medium shadow-lg transition-opacity ${
            saveToast.type === 'success' ? 'bg-green-700 text-white' : 'bg-red-700 text-white'
          }`}
        >
          {saveToast.message}
        </div>
      )}
      <h1 className="text-2xl font-bold mb-8">{t('title')}</h1>

      {/* Profile Section — includes appearance, language, and location */}
      <CollapsibleSection id="profile" title={t('profile.heading')}>
        <ProfileSection preferences={preferences} saving={saving} savePreference={savePreference} />
      </CollapsibleSection>

      {/* Training Section — hidden for child users */}
      {!isChild && (
        <CollapsibleSection id="training" title={t('training.heading')}>
          <TrainingSection
            preferences={preferences}
            saving={saving}
            savePreference={savePreference}
            savePreferences={savePreferences}
          />
        </CollapsibleSection>
      )}

      {/* Notifications Section — hidden for child users */}
      {!isChild && (
        <CollapsibleSection id="notifications" title={t('notifications.heading')}>
          <NotificationsSection
            preferences={preferences}
            saving={saving}
            savePreference={savePreference}
            savePreferences={savePreferences}
          />
        </CollapsibleSection>
      )}

      {/* Security Section — sessions + account deletion */}
      <CollapsibleSection id="security" title={t('security.heading')}>
        <SecuritySection />
      </CollapsibleSection>

      {/* Integrations Section — hidden for child users and non-feature users */}
      {!isChild && (user?.is_admin || hasFeature('infra') || hasFeature('claude_ai')) && (
        <CollapsibleSection id="integrations" title={t('integrations.heading')}>
          <IntegrationsSection preferences={preferences} saving={saving} savePreference={savePreference} />
        </CollapsibleSection>
      )}

      {/* Pokémon — gated by the per-user feature flag */}
      {hasFeature('pokemon') && (
        <CollapsibleSection id="pokemon" title={t('pokemon.heading')}>
          <PokemonSection preferences={preferences} saving={saving} savePreference={savePreference} />
        </CollapsibleSection>
      )}

      {/* AI & Automation — admin only */}
      {user?.is_admin && (
        <CollapsibleSection id="ai-automation" title={t('aiAutomation.heading')}>
          <AIAutomationSection />
        </CollapsibleSection>
      )}

      {/* Kiosk Tokens — admin only */}
      {user?.is_admin && (
        <CollapsibleSection id="kiosk-tokens" title={t('kioskTokens.heading')}>
          <KioskTokensSection />
        </CollapsibleSection>
      )}
    </main>
  )
}

export default Settings
