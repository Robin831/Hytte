import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import { useAuth } from '../auth'
import { CollapsibleSection } from '../components/CollapsibleSection'
import { useDebouncedPreferences } from '../hooks/useDebouncedPreferences'
import type { PrefSaveStatus } from '../hooks/useDebouncedPreferences'
import { Skeleton } from '../components/ui/skeleton'
import ProfileSection from './settings/ProfileSection'
import TrainingSection from './settings/TrainingSection'
import NotificationsSection from './settings/NotificationsSection'
import SecuritySection from './settings/SecuritySection'
import IntegrationsSection from './settings/IntegrationsSection'
import PokemonSection from './settings/PokemonSection'
import AIAutomationSection from './settings/AIAutomationSection'
import KioskTokensSection from './settings/KioskTokensSection'

// Maps each preference key to the settings section it belongs to, so the
// debounced batch writer can drive a per-section save status.
const PREF_KEY_SECTIONS: Record<string, string> = {
  theme: 'profile',
  home_location: 'profile',
  weather_location: 'profile',
  recent_locations: 'profile',
  max_hr: 'training',
  threshold_hr: 'training',
  threshold_pace: 'training',
  resting_hr: 'training',
  easy_pace_min: 'training',
  easy_pace_max: 'training',
  zone_boundaries: 'training',
  ai_auto_analyze: 'training',
  stride_custom_prompt: 'training',
  goal_race_name: 'training',
  goal_race_date: 'training',
  goal_race_distance: 'training',
  goal_race_target_time: 'training',
  notifications_enabled: 'notifications',
  notifications_degraded: 'notifications',
  notification_filter_sources: 'notifications',
  notification_filter_events: 'notifications',
  quiet_hours_enabled: 'notifications',
  quiet_hours_start: 'notifications',
  quiet_hours_end: 'notifications',
  quiet_hours_timezone: 'notifications',
  claude_enabled: 'integrations',
  claude_cli_path: 'integrations',
  claude_model: 'integrations',
  pokemon_scan_push_enabled: 'pokemon',
  pokemon_scan_daily_cap: 'pokemon',
  pokemon_scan_auto_discard_hours: 'pokemon',
}

function sectionForPrefKey(key: string): string {
  return PREF_KEY_SECTIONS[key] ?? 'other'
}

// Inline "Saving…" / "Saved" / "Error" indicator rendered next to a section header.
function SectionStatusBadge({
  status,
  texts,
}: {
  status?: PrefSaveStatus
  texts: Record<'saving' | 'saved' | 'error', string>
}) {
  if (!status || status === 'idle') return null
  const styles = { saving: 'text-gray-400', saved: 'text-green-400', error: 'text-red-400' } as const
  return (
    <span className={`text-xs font-normal ${styles[status]}`} role="status" aria-live="polite">
      {texts[status]}
    </span>
  )
}

function Settings() {
  const { t } = useTranslation(['settings', 'common'])
  const { user, familyStatus, hasFeature } = useAuth()
  const isKidsPlan = Boolean(user?.features?.['kids_stars'])
  const isChild = isKidsPlan && familyStatus?.is_child === true
  const [preferences, setPreferences] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [saving, setSaving] = useState(false)
  // Guards against setting state after unmount, and against a stale in-flight
  // load overwriting a newer retry's result.
  const mountedRef = useRef(true)
  const loadSeq = useRef(0)
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

  // Localized strings for the per-section save status badges.
  const statusTexts = useMemo(
    () => ({
      saving: t('saveStatus.saving'),
      saved: t('saveStatus.saved'),
      error: t('saveStatus.error'),
    }),
    [t],
  )

  // Debounced, batched preference writer. Queued edits accumulate and flush as
  // a single request; toggles/selects use saveNow for an immediate write. Both
  // paths drive a per-section Saving… → Saved / Error status.
  const {
    status: prefSectionStatus,
    queuePreference: prefQueue,
    saveNow: prefSaveNow,
    flush: prefFlush,
  } = useDebouncedPreferences({
    sectionForKey: sectionForPrefKey,
    onSaved: (prefs) => setPreferences(prefs),
    onSuccessToast: () => showToast('success', t('training.saveSuccess')),
    onErrorToast: () => showToast('error', t('training.saveError')),
  })
  const savePreferences = async (prefs: Record<string, string>, toast = false) => {
    setSaving(true)
    try {
      await prefSaveNow(prefs, toast)
    } finally {
      setSaving(false)
    }
  }

  const queuePreference = useCallback(
    (key: string, value: string) => {
      setPreferences((prev) => ({ ...prev, [key]: value }))
      prefQueue(key, value)
    },
    [prefQueue],
  )

  const flushPreferences = useCallback(() => {
    void prefFlush()
  }, [prefFlush])

  const savePreference = async (key: string, value: string, toast = false) => {
    await savePreferences({ [key]: value }, toast)
  }

  // Loads the preference map. A non-OK response or a thrown fetch surfaces a
  // visible error panel instead of rendering the sections against an empty map
  // (which would show hardcoded defaults as if they were saved values).
  const loadPreferences = useCallback(async () => {
    const seq = ++loadSeq.current
    setLoadError(false)
    setLoading(true)
    try {
      const prefsRes = await fetch('/api/settings/preferences', { credentials: 'include' })
      if (seq !== loadSeq.current || !mountedRef.current) return
      if (!prefsRes.ok) {
        console.error('Failed to load settings data: HTTP', prefsRes.status)
        setLoadError(true)
        return
      }
      const data = await prefsRes.json()
      if (seq !== loadSeq.current || !mountedRef.current) return
      setPreferences(data.preferences || {})
    } catch (err) {
      if (seq !== loadSeq.current || !mountedRef.current) return
      console.error('Failed to load settings data:', err)
      setLoadError(true)
    } finally {
      if (seq === loadSeq.current && mountedRef.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    mountedRef.current = true
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; mountedRef/loadSeq prevent stale updates
    void loadPreferences()
    return () => {
      mountedRef.current = false
    }
  }, [loadPreferences])

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

  // The preferences never loaded — render a retry panel instead of the sections
  // so no control is editable against unknown state.
  if (loadError) {
    return (
      <main className="max-w-2xl mx-auto px-4 py-8 min-h-screen">
        <h1 className="text-2xl font-bold mb-8">{t('title')}</h1>
        <div
          role="alert"
          className="rounded-lg border border-red-800 bg-red-950/40 p-4 sm:p-6 text-red-100"
        >
          <div className="flex items-start gap-3">
            <AlertTriangle size={20} className="mt-0.5 shrink-0 text-red-400" aria-hidden="true" />
            <div className="min-w-0">
              <h2 className="text-lg font-semibold">{t('loadError.title')}</h2>
              <p className="mt-1 text-sm text-red-200/90">{t('loadError.body')}</p>
              <button
                type="button"
                onClick={() => void loadPreferences()}
                disabled={loading}
                className="mt-4 px-4 py-2 rounded-lg bg-red-700 hover:bg-red-600 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium"
              >
                {t('loadError.retry')}
              </button>
            </div>
          </div>
        </div>
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
      <CollapsibleSection
        id="profile"
        title={
          <span className="inline-flex items-center gap-2">
            {t('profile.heading')}
            <SectionStatusBadge status={prefSectionStatus.profile} texts={statusTexts} />
          </span>
        }
      >
        <ProfileSection preferences={preferences} saving={saving} savePreference={savePreference} />
      </CollapsibleSection>

      {/* Training Section — hidden for child users */}
      {!isChild && (
        <CollapsibleSection
          id="training"
          title={
            <span className="inline-flex items-center gap-2">
              {t('training.heading')}
              <SectionStatusBadge status={prefSectionStatus.training} texts={statusTexts} />
            </span>
          }
        >
          <TrainingSection
            preferences={preferences}
            saving={saving}
            savePreference={savePreference}
            savePreferences={savePreferences}
            queuePreference={queuePreference}
            flushPreferences={flushPreferences}
          />
        </CollapsibleSection>
      )}

      {/* Notifications Section — hidden for child users */}
      {!isChild && (
        <CollapsibleSection
          id="notifications"
          title={
            <span className="inline-flex items-center gap-2">
              {t('notifications.heading')}
              <SectionStatusBadge status={prefSectionStatus.notifications} texts={statusTexts} />
            </span>
          }
        >
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
        <CollapsibleSection
          id="integrations"
          title={
            <span className="inline-flex items-center gap-2">
              {t('integrations.heading')}
              <SectionStatusBadge status={prefSectionStatus.integrations} texts={statusTexts} />
            </span>
          }
        >
          <IntegrationsSection preferences={preferences} saving={saving} savePreference={savePreference} queuePreference={queuePreference} flushPreferences={flushPreferences} />
        </CollapsibleSection>
      )}

      {/* Pokémon — gated by the per-user feature flag */}
      {hasFeature('pokemon') && (
        <CollapsibleSection
          id="pokemon"
          title={
            <span className="inline-flex items-center gap-2">
              {t('pokemon.heading')}
              <SectionStatusBadge status={prefSectionStatus.pokemon} texts={statusTexts} />
            </span>
          }
        >
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
