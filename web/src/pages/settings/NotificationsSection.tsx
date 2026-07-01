import { useState, useEffect, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { formatDate } from '../../utils/formatDate'
import { TimePicker } from '../../components/ui/time-picker'
import {
  isPushSupported,
  subscribeToPush,
  unsubscribeFromPush,
  getActivePushSubscription,
  isPushSubscribed,
  getCurrentPushEndpoint,
} from '../../push'
import type { PushDevice, EventTypeInfo, PreferenceSectionProps } from './types'

type NotificationsSectionProps = PreferenceSectionProps

function NotificationsSection({ preferences, saving, savePreference, savePreferences }: NotificationsSectionProps) {
  const { t } = useTranslation(['settings', 'common'])
  const [pushSupported] = useState(() => isPushSupported())
  const [pushSubscribed, setPushSubscribed] = useState(false)
  const [pushToggling, setPushToggling] = useState(false)
  const [browserPermission, setBrowserPermission] = useState<NotificationPermission>(
    'Notification' in window ? Notification.permission : 'default'
  )
  const [pushDevices, setPushDevices] = useState<PushDevice[]>([])
  const [currentEndpoint, setCurrentEndpoint] = useState<string | null>(null)
  const [removingDevice, setRemovingDevice] = useState<number | null>(null)
  const [deviceError, setDeviceError] = useState<string | null>(null)
  const [testSending, setTestSending] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null)
  const [eventTypes, setEventTypes] = useState<EventTypeInfo[]>([])

  // Keep a ref to preferences so async toggle callbacks always read fresh state,
  // avoiding stale-closure bugs when multiple toggles fire in quick succession.
  const preferencesRef = useRef(preferences)
  useEffect(() => {
    preferencesRef.current = preferences
  })

  const fetchPushDevices = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/push/subscriptions', { credentials: 'include', signal })
      if (res.ok) {
        const data = await res.json()
        setPushDevices(data.subscriptions || [])
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      console.error('Failed to fetch push devices:', err)
    }
  }, [])

  // Load event types (authenticated, single source of truth in backend).
  useEffect(() => {
    let cancelled = false
    fetch('/api/settings/event-types', { credentials: 'include' })
      .then((res) => {
        if (!res.ok || cancelled) return
        return res.json()
      })
      .then((data) => {
        if (data && !cancelled) setEventTypes(data.event_types || [])
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [])

  // Check push subscription status and load devices.
  // Device list is fetched regardless of push support so users on unsupported
  // browsers can still view and remove existing server-side subscriptions.
  useEffect(() => {
    let cancelled = false
    const abortController = new AbortController()

    async function loadPushState() {
      // Always fetch the server-side subscription list.
      await fetchPushDevices(abortController.signal)

      // Local subscription state is only available when push is supported.
      if (pushSupported) {
        try {
          const subscription = await getActivePushSubscription()
          if (cancelled) return
          setPushSubscribed(subscription !== null)
          setCurrentEndpoint(subscription?.endpoint ?? null)
        } catch (err) {
          console.error('Failed to check push subscription status:', err)
        }
      }
    }

    loadPushState()
    return () => { cancelled = true; abortController.abort() }
  }, [pushSupported, fetchPushDevices])

  const togglePushNotifications = async () => {
    setPushToggling(true)
    try {
      if (pushSubscribed) {
        const ok = await unsubscribeFromPush()
        if (ok) {
          setPushSubscribed(false)
          await savePreference('notifications_enabled', 'false')
        }
      } else {
        const ok = await subscribeToPush()
        if (ok) {
          setPushSubscribed(true)
          if ('Notification' in window) {
            setBrowserPermission(Notification.permission)
          }
          await savePreference('notifications_enabled', 'true')
          await savePreference('notifications_degraded', 'false')
        } else {
          // Subscribe failed — reconcile UI with actual subscription state
          // to avoid showing the toggle in a state that doesn't match reality.
          const actual = await isPushSubscribed()
          setPushSubscribed(actual)
          if ('Notification' in window) {
            setBrowserPermission(Notification.permission)
          }
        }
      }
    } finally {
      setPushToggling(false)
      await fetchPushDevices()
      const endpoint = await getCurrentPushEndpoint()
      setCurrentEndpoint(endpoint)
    }
  }

  const removeDevice = async (device: PushDevice) => {
    setRemovingDevice(device.id)
    setDeviceError(null)
    try {
      const res = await fetch(`/api/push/subscriptions/${device.id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (res.ok) {
        await fetchPushDevices()
        // If we just removed the current device's subscription, update local state
        if (device.endpoint === currentEndpoint) {
          setPushSubscribed(false)
          setCurrentEndpoint(null)
          // Best-effort: unsubscribe locally so the browser stops expecting pushes.
          // This is separate from the server delete — a failure here is non-fatal.
          try {
            const registration = await navigator.serviceWorker?.getRegistration()
            const sub = await registration?.pushManager?.getSubscription()
            if (sub) await sub.unsubscribe()
          } catch (localErr) {
            console.warn('Local push unsubscribe failed (server-side removal succeeded):', localErr)
          }
        }
      } else {
        const data = await res.json().catch(() => null)
        setDeviceError(data?.error || t('notifications.failedRemoveDevice'))
      }
    } catch (err) {
      console.error('Failed to remove device:', err)
      setDeviceError(t('notifications.failedRemoveDevice'))
    } finally {
      setRemovingDevice(null)
    }
  }

  const sendTestNotification = async () => {
    setTestSending(true)
    setTestResult(null)
    try {
      const res = await fetch('/api/push/test', {
        method: 'POST',
        credentials: 'include',
      })
      const data = await res.json().catch(() => null)
      if (res.ok) {
        setTestResult({ ok: true, message: data?.devices_sent != null ? t('notifications.testSentDevices', { count: data.devices_sent }) : t('notifications.testSent') })
      } else {
        setTestResult({ ok: false, message: data?.error || t('notifications.testFailed') })
      }
    } catch (err) {
      console.error('Failed to send test notification:', err)
      setTestResult({ ok: false, message: t('notifications.testFailed') })
    } finally {
      setTestSending(false)
    }
  }

  return (
    <>
      {!pushSupported ? (
        <p className="text-sm text-gray-400">
          {t('notifications.notSupported')}
        </p>
      ) : (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">{t('notifications.pushNotifications')}</p>
              <p className="text-sm text-gray-400">
                {t('notifications.pushDescription')}
              </p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={pushSubscribed}
              onClick={togglePushNotifications}
              disabled={pushToggling || (browserPermission === 'denied' && !pushSubscribed)}
              aria-label={pushSubscribed ? t('notifications.disablePush') : t('notifications.enablePush')}
              className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                pushSubscribed ? 'bg-blue-600' : 'bg-gray-600'
              }`}
            >
              <span
                className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                  pushSubscribed ? 'translate-x-6' : 'translate-x-1'
                }`}
              />
            </button>
          </div>

          {/* Status display */}
          <div className="text-sm">
            {browserPermission === 'denied' && (
              <p className="text-red-400">
                {t('notifications.permissionDenied')}
              </p>
            )}
            {browserPermission === 'granted' && pushSubscribed && (
              <p className="text-green-400">
                {t('notifications.permissionGrantedActive')}
              </p>
            )}
            {browserPermission === 'granted' && !pushSubscribed && (
              <p className="text-gray-400">
                {t('notifications.permissionGrantedInactive')}
              </p>
            )}
            {browserPermission === 'default' && !pushSubscribed && (
              <p className="text-gray-400">
                {t('notifications.permissionDefault')}
              </p>
            )}
            {preferences.notifications_degraded === 'true' && (
              <p className="text-amber-400 mt-2">
                {t('notifications.degraded')}
              </p>
            )}
          </div>

          {/* Test notification */}
          {pushSubscribed && (
            <div className="flex items-center gap-3">
              <button
                onClick={sendTestNotification}
                disabled={testSending}
                className="bg-gray-700 hover:bg-gray-600 text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {testSending ? t('notifications.sending') : t('notifications.sendTest')}
              </button>
              {testResult && (
                <p className={`text-sm ${testResult.ok ? 'text-green-400' : 'text-red-400'}`}>
                  {testResult.message}
                </p>
              )}
            </div>
          )}

          {/* Notification Filters */}
          {(() => {
            const parseFilters = (raw: string | undefined): Record<string, boolean> => {
              try { return JSON.parse(raw || '{}') } catch { return {} }
            }
            const sourceFilters = parseFilters(preferences.notification_filter_sources)
            const eventFilters = parseFilters(preferences.notification_filter_events)

            const sources: { key: 'github' | 'forge' | 'generic'; label: string; desc: string }[] = [
              { key: 'github', label: t('notifications.sourceGithub'), desc: t('notifications.sourceGithubDesc') },
              { key: 'forge', label: t('notifications.sourceForge'), desc: t('notifications.sourceForgeDesc') },
              { key: 'generic', label: t('notifications.sourceGeneric'), desc: t('notifications.sourceGenericDesc') },
            ]
            // Event types are fetched from /api/settings/event-types (authenticated, single source of truth in backend).

            const Toggle = ({ enabled, label, onToggle }: { enabled: boolean; label: string; onToggle: () => Promise<void> }) => (
              <button
                type="button"
                role="switch"
                aria-checked={enabled}
                aria-label={enabled ? t('notifications.disableSource', { source: label }) : t('notifications.enableSource', { source: label })}
                onClick={onToggle}
                disabled={saving}
                className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                  enabled ? 'bg-blue-600' : 'bg-gray-600'
                }`}
              >
                <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${enabled ? 'translate-x-6' : 'translate-x-1'}`} />
              </button>
            )

            return (
              <div className="border-t border-gray-700 pt-4">
                <p className="font-medium mb-1">{t('notifications.filters')}</p>
                <p className="text-sm text-gray-400 mb-3">
                  {t('notifications.filtersDescription')}
                </p>

                {/* Source toggles */}
                <div className="space-y-2 mb-4">
                  <p className="text-sm text-gray-300 font-medium">{t('notifications.sources')}</p>
                  {sources.map(({ key, label, desc }) => (
                    <div key={key} className="flex items-center justify-between pl-2">
                      <div>
                        <p className="text-sm">{label}</p>
                        <p className="text-xs text-gray-500">{desc}</p>
                      </div>
                      <Toggle
                        enabled={sourceFilters[key] !== false}
                        label={label}
                        onToggle={async () => {
                          const fresh = parseFilters(preferencesRef.current.notification_filter_sources)
                          await savePreference('notification_filter_sources', JSON.stringify({ ...fresh, [key]: fresh[key] === false }))
                        }}
                      />
                    </div>
                  ))}
                </div>

                {/* Event type toggles — shown when GitHub or Forge source is enabled */}
                {(sourceFilters['github'] !== false || sourceFilters['forge'] !== false) && (
                  <div className="space-y-2">
                    <p className="text-sm text-gray-300 font-medium">{t('notifications.eventTypes')}</p>
                    {eventTypes.map(({ key, label, description }) => (
                      <div key={key} className="flex items-center justify-between pl-2">
                        <div>
                          <p className="text-sm">{label}</p>
                          <p className="text-xs text-gray-500">{description}</p>
                        </div>
                        <Toggle
                          enabled={eventFilters[key] !== false}
                          label={label}
                          onToggle={async () => {
                            const fresh = parseFilters(preferencesRef.current.notification_filter_events)
                            await savePreference('notification_filter_events', JSON.stringify({ ...fresh, [key]: fresh[key] === false }))
                          }}
                        />
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )
          })()}

          {/* Quiet Hours */}
          <div className="border-t border-gray-700 pt-4">
            <div className="flex items-center justify-between mb-3">
              <div>
                <p className="font-medium">{t('notifications.quietHours')}</p>
                <p className="text-sm text-gray-400">
                  {t('notifications.quietHoursDescription')}
                </p>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={preferences.quiet_hours_enabled === 'true'}
                onClick={async () => {
                  if (preferences.quiet_hours_enabled === 'true') {
                    await savePreference('quiet_hours_enabled', 'false')
                  } else {
                    // When enabling, set defaults for start/end/timezone if not already set.
                    const prefs: Record<string, string> = { quiet_hours_enabled: 'true' }
                    if (!preferences.quiet_hours_start) prefs.quiet_hours_start = '22:00'
                    if (!preferences.quiet_hours_end) prefs.quiet_hours_end = '07:00'
                    if (!preferences.quiet_hours_timezone) {
                      prefs.quiet_hours_timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
                    }
                    await savePreferences(prefs)
                  }
                }}
                disabled={saving}
                aria-label={
                  preferences.quiet_hours_enabled === 'true'
                    ? t('notifications.disableQuietHours')
                    : t('notifications.enableQuietHours')
                }
                className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                  preferences.quiet_hours_enabled === 'true' ? 'bg-blue-600' : 'bg-gray-600'
                }`}
              >
                <span
                  className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                    preferences.quiet_hours_enabled === 'true' ? 'translate-x-6' : 'translate-x-1'
                  }`}
                />
              </button>
            </div>

            {preferences.quiet_hours_enabled === 'true' && (
              <div className="space-y-3 pl-0">
                <div className="flex items-center gap-3">
                  <span className="text-sm text-gray-400 w-12">
                    {t('notifications.quietHoursFrom')}
                  </span>
                  <TimePicker
                    value={preferences.quiet_hours_start || '22:00'}
                    onChange={(v: string) => savePreference('quiet_hours_start', v)}
                    disabled={saving}
                    aria-label={t('notifications.quietHoursFrom')}
                  />
                  <span className="text-sm text-gray-400 w-8">
                    {t('notifications.quietHoursTo')}
                  </span>
                  <TimePicker
                    value={preferences.quiet_hours_end || '07:00'}
                    onChange={(v: string) => savePreference('quiet_hours_end', v)}
                    disabled={saving}
                    aria-label={t('notifications.quietHoursTo')}
                  />
                </div>
                <div className="flex items-center gap-3">
                  <span className="text-sm text-gray-400 w-12">{t('notifications.quietHoursZone')}</span>
                  <p className="text-sm text-gray-300">
                    {preferences.quiet_hours_timezone ||
                      Intl.DateTimeFormat().resolvedOptions().timeZone}
                  </p>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Active Devices — shown regardless of push support so users can remove
          server-side subscriptions even from browsers without Push API support. */}
      {pushDevices.length > 0 && (
        <div className={pushSupported ? 'mt-4' : 'mt-4'}>
          <p className="font-medium mb-2">{t('notifications.activeDevices')}</p>
          {deviceError && (
            <p className="text-sm text-red-400 mb-2">{deviceError}</p>
          )}
          <div className="space-y-2">
            {pushDevices.map((device) => {
              const isCurrent = device.endpoint === currentEndpoint
              let label: string
              try {
                label = new URL(device.endpoint).hostname
              } catch {
                label = t('notifications.unknownService')
              }
              return (
                <div
                  key={device.id}
                  className="flex items-center justify-between bg-gray-700/50 rounded-lg px-4 py-3"
                >
                  <div>
                    <p className="text-sm font-medium">
                      {label}
                      {isCurrent && (
                        <span className="ml-2 text-xs bg-green-600/20 text-green-400 px-2 py-0.5 rounded-full">
                          {t('notifications.thisDevice')}
                        </span>
                      )}
                    </p>
                    <p className="text-xs text-gray-400">
                      {(() => {
                        const d = device.created_at ? new Date(device.created_at) : null
                        return d && !isNaN(d.getTime())
                          ? t('notifications.registeredOn', { date: formatDate(d, { year: 'numeric', month: 'short', day: 'numeric' }) })
                          : t('notifications.registrationUnknown')
                      })()}
                    </p>
                  </div>
                  <button
                    onClick={() => removeDevice(device)}
                    disabled={removingDevice === device.id}
                    className="text-sm text-red-400 hover:text-red-300 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
                    aria-label={t('notifications.removeDevice', { label })}
                  >
                    {removingDevice === device.id ? t('notifications.removing') : t('notifications.remove')}
                  </button>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </>
  )
}

export default NotificationsSection
