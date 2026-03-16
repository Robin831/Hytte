import { useState, useEffect, useCallback, useRef } from 'react'
import { useAuth } from '../auth'
import { useNavigate } from 'react-router-dom'
import { Eye, EyeOff } from 'lucide-react'
import {
  isPushSupported,
  subscribeToPush,
  unsubscribeFromPush,
  getActivePushSubscription,
  isPushSubscribed,
  getCurrentPushEndpoint,
} from '../push'

interface HetznerTokenState {
  configured: boolean
  masked: string
}

interface PushDevice {
  id: number
  endpoint: string
  created_at: string
}

interface SessionInfo {
  id: string
  created_at: string
  expires_at: string
  current: boolean
}

interface EventTypeInfo {
  key: string
  label: string
  description: string
}

function Settings() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const [preferences, setPreferences] = useState<Record<string, string>>({})
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [cityNames, setCityNames] = useState<string[]>([])
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleteConfirmText, setDeleteConfirmText] = useState('')
  const [pushSupported] = useState(() => isPushSupported())
  const [pushSubscribed, setPushSubscribed] = useState(false)
  const [pushToggling, setPushToggling] = useState(false)
  const [browserPermission, setBrowserPermission] = useState<NotificationPermission>(
    'Notification' in window ? Notification.permission : 'default'
  )
  const [pushDevices, setPushDevices] = useState<PushDevice[]>([])
  const [currentEndpoint, setCurrentEndpoint] = useState<string | null>(null)
  const [removingDevice, setRemovingDevice] = useState<number | null>(null)
  const [maxHRDraft, setMaxHRDraft] = useState<string>('')
  const [deviceError, setDeviceError] = useState<string | null>(null)
  const [testSending, setTestSending] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null)
  const [eventTypes, setEventTypes] = useState<EventTypeInfo[]>([])
  const [hetznerToken, setHetznerToken] = useState<HetznerTokenState | null>(null)
  const [hetznerNewToken, setHetznerNewToken] = useState('')
  const [hetznerShowToken, setHetznerShowToken] = useState(false)
  const [hetznerSaving, setHetznerSaving] = useState(false)
  const [hetznerDeleting, setHetznerDeleting] = useState(false)
  const [hetznerError, setHetznerError] = useState<string | null>(null)

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

  const fetchSessions = useCallback(async () => {
    const res = await fetch('/api/settings/sessions', { credentials: 'include' })
    if (res.ok) {
      const data = await res.json()
      setSessions(data.sessions || [])
    }
  }, [])

  const loadHetznerToken = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/infra/hetzner/token', { credentials: 'include', signal })
      if (!res.ok) throw new Error(`Failed to load token status (${res.status})`)
      setHetznerToken(await res.json())
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setHetznerError(err instanceof Error ? err.message : 'Failed to load token status')
    }
  }, [])

  const handleSaveHetznerToken = async () => {
    if (!hetznerNewToken.trim()) return
    setHetznerSaving(true)
    setHetznerError(null)
    try {
      const res = await fetch('/api/infra/hetzner/token', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: hetznerNewToken.trim() }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || `Failed (${res.status})`)
      }
      setHetznerNewToken('')
      setHetznerShowToken(false)
      await loadHetznerToken()
    } catch (err) {
      setHetznerError(err instanceof Error ? err.message : 'Failed to save token')
    } finally {
      setHetznerSaving(false)
    }
  }

  const handleDeleteHetznerToken = async () => {
    setHetznerDeleting(true)
    setHetznerError(null)
    try {
      const res = await fetch('/api/infra/hetzner/token', {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('Failed to delete token')
      await loadHetznerToken()
    } catch (err) {
      setHetznerError(err instanceof Error ? err.message : 'Failed to delete token')
    } finally {
      setHetznerDeleting(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    async function loadData() {
      try {
        const [prefsRes, sessionsRes, eventTypesRes] = await Promise.all([
          fetch('/api/settings/preferences', { credentials: 'include' }),
          fetch('/api/settings/sessions', { credentials: 'include' }),
          fetch('/api/settings/event-types', { credentials: 'include' }),
        ])
        if (cancelled) return
        if (prefsRes.ok) {
          const data = await prefsRes.json()
          const prefs = data.preferences || {}
          setPreferences(prefs)
          setMaxHRDraft(prefs.max_hr || '')
        }
        if (sessionsRes.ok) {
          const data = await sessionsRes.json()
          setSessions(data.sessions || [])
        }
        if (eventTypesRes.ok) {
          const data = await eventTypesRes.json()
          setEventTypes(data.event_types || [])
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

  // Load Hetzner token status on mount.
  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      try {
        const res = await fetch('/api/infra/hetzner/token', { credentials: 'include', signal: controller.signal })
        if (!res.ok) throw new Error(`Failed to load token status (${res.status})`)
        setHetznerToken(await res.json())
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setHetznerError(err instanceof Error ? err.message : 'Failed to load token status')
      }
    }
    load()
    return () => controller.abort()
  }, [])

  // Check push subscription status and load devices on mount.
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

  const savePreferences = async (prefs: Record<string, string>) => {
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
      }
    } finally {
      setSaving(false)
    }
  }

  const savePreference = async (key: string, value: string) => {
    await savePreferences({ [key]: value })
  }

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
        setDeviceError(data?.error || 'Failed to remove device')
      }
    } catch (err) {
      console.error('Failed to remove device:', err)
      setDeviceError('Failed to remove device')
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
        setTestResult({ ok: true, message: data?.devices_sent != null ? `Test notification sent to ${data.devices_sent} device(s).` : 'Test notification sent.' })
      } else {
        setTestResult({ ok: false, message: data?.error || 'Failed to send test notification' })
      }
    } catch (err) {
      console.error('Failed to send test notification:', err)
      setTestResult({ ok: false, message: 'Failed to send test notification' })
    } finally {
      setTestSending(false)
    }
  }

  const signOutEverywhere = async () => {
    const res = await fetch('/api/settings/sessions/revoke-others', { method: 'POST', credentials: 'include' })
    if (res.ok) {
      await fetchSessions()
    }
  }

  const deleteAccount = async () => {
    const res = await fetch('/api/settings/account', { method: 'DELETE', credentials: 'include' })
    if (res.ok) {
      await logout()
      navigate('/')
    }
  }

  if (!user) return null
  if (loading) {
    return (
      <main className="flex items-center justify-center min-h-screen">
        <p className="text-gray-400">Loading settings...</p>
      </main>
    )
  }

  const memberSince = new Date(user.created_at).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })

  return (
    <main className="max-w-2xl mx-auto px-4 py-8 min-h-screen">
      <h1 className="text-2xl font-bold mb-8">Settings</h1>

      {/* Profile Section */}
      <section className="bg-gray-800 rounded-xl p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Profile</h2>
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
          Member since {memberSince}. Profile info is managed by your Google account.
        </p>
      </section>

      {/* Appearance Section */}
      <section className="bg-gray-800 rounded-xl p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Appearance</h2>
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">Theme</p>
            <p className="text-sm text-gray-400">Choose your preferred color theme</p>
          </div>
          <select
            value={preferences.theme || 'dark'}
            onChange={(e) => savePreference('theme', e.target.value)}
            disabled={saving}
            className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="dark">Dark</option>
            <option value="light" disabled>Light (coming soon)</option>
          </select>
        </div>
      </section>

      {/* Location Section */}
      <section className="bg-gray-800 rounded-xl p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Location</h2>
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">Home city</p>
            <p className="text-sm text-gray-400">Used for the weather widget</p>
          </div>
          <select
            value={preferences.home_location || ''}
            onChange={(e) => savePreference('home_location', e.target.value)}
            disabled={saving}
            className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">Select a city</option>
            {cityNames.map((city) => (
              <option key={city} value={city}>
                {city}
              </option>
            ))}
          </select>
        </div>
      </section>

      {/* Training Section */}
      <section className="bg-gray-800 rounded-xl p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Training</h2>
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">Max heart rate</p>
            <p className="text-sm text-gray-400">Used for training zone calculations (bpm)</p>
          </div>
          <input
            type="number"
            min="100"
            max="230"
            value={maxHRDraft}
            onChange={(e) => setMaxHRDraft(e.target.value)}
            onBlur={() => {
              if (maxHRDraft === '') {
                savePreference('max_hr', '')
              } else {
                const num = parseInt(maxHRDraft)
                if (num >= 100 && num <= 230) {
                  savePreference('max_hr', maxHRDraft)
                } else {
                  // Revert to last saved value on invalid input
                  setMaxHRDraft(preferences.max_hr || '')
                }
              }
            }}
            placeholder="e.g. 191"
            disabled={saving}
            aria-label="Max heart rate"
            className="w-24 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>
      </section>

      {/* Notifications Section */}
      <section className="bg-gray-800 rounded-xl p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Notifications</h2>
        {!pushSupported ? (
          <p className="text-sm text-gray-400">
            Push notifications are not supported by your browser.
          </p>
        ) : (
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">Push notifications</p>
                <p className="text-sm text-gray-400">
                  Receive notifications about cabin activity
                </p>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={pushSubscribed}
                onClick={togglePushNotifications}
                disabled={pushToggling || (browserPermission === 'denied' && !pushSubscribed)}
                aria-label={pushSubscribed ? 'Disable push notifications' : 'Enable push notifications'}
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
                  Notifications are blocked by your browser. To enable them, update the
                  notification permission in your browser settings for this site.
                </p>
              )}
              {browserPermission === 'granted' && pushSubscribed && (
                <p className="text-green-400">
                  Notifications are active on this device.
                </p>
              )}
              {browserPermission === 'granted' && !pushSubscribed && (
                <p className="text-gray-400">
                  Browser permission granted — toggle on to start receiving notifications.
                </p>
              )}
              {browserPermission === 'default' && !pushSubscribed && (
                <p className="text-gray-400">
                  Your browser will ask for permission when you enable notifications.
                </p>
              )}
              {preferences.notifications_degraded === 'true' && (
                <p className="text-amber-400 mt-2">
                  Your notification subscription may have expired. Try disabling and
                  re-enabling notifications to restore delivery.
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
                  {testSending ? 'Sending...' : 'Send test notification'}
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
                { key: 'github', label: 'GitHub', desc: 'Events from GitHub (push, PR, release, etc.)' },
                { key: 'forge', label: 'The Forge', desc: 'Automated agent notifications (PR created, ready to merge, failures, etc.)' },
                { key: 'generic', label: 'Other webhooks', desc: 'Webhook requests not identified as GitHub or Forge' },
              ]
              // Event types are fetched from /api/settings/event-types (authenticated, single source of truth in backend).

              const Toggle = ({ enabled, label, onToggle }: { enabled: boolean; label: string; onToggle: () => Promise<void> }) => (
                <button
                  type="button"
                  role="switch"
                  aria-checked={enabled}
                  aria-label={`${enabled ? 'Disable' : 'Enable'} ${label} notifications`}
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
                  <p className="font-medium mb-1">Notification filters</p>
                  <p className="text-sm text-gray-400 mb-3">
                    Choose which sources and event types trigger notifications
                  </p>

                  {/* Source toggles */}
                  <div className="space-y-2 mb-4">
                    <p className="text-sm text-gray-300 font-medium">Sources</p>
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
                      <p className="text-sm text-gray-300 font-medium">Event types</p>
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
                  <p className="font-medium">Quiet hours</p>
                  <p className="text-sm text-gray-400">
                    Suppress notifications during scheduled hours
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
                      ? 'Disable quiet hours'
                      : 'Enable quiet hours'
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
                    <label htmlFor="quiet-start" className="text-sm text-gray-400 w-12">
                      From
                    </label>
                    <input
                      id="quiet-start"
                      type="time"
                      value={preferences.quiet_hours_start || '22:00'}
                      onChange={(e) => savePreference('quiet_hours_start', e.target.value)}
                      disabled={saving}
                      className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                    />
                    <label htmlFor="quiet-end" className="text-sm text-gray-400 w-8">
                      To
                    </label>
                    <input
                      id="quiet-end"
                      type="time"
                      value={preferences.quiet_hours_end || '07:00'}
                      onChange={(e) => savePreference('quiet_hours_end', e.target.value)}
                      disabled={saving}
                      className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                    />
                  </div>
                  <div className="flex items-center gap-3">
                    <span className="text-sm text-gray-400 w-12">Zone</span>
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
            <p className="font-medium mb-2">Active devices</p>
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
                  label = 'Unknown service'
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
                            This device
                          </span>
                        )}
                      </p>
                      <p className="text-xs text-gray-400">
                        {(() => {
                          const d = device.created_at ? new Date(device.created_at) : null
                          return d && !isNaN(d.getTime())
                            ? `Registered ${d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })}`
                            : 'Registration date unknown'
                        })()}
                      </p>
                    </div>
                    <button
                      onClick={() => removeDevice(device)}
                      disabled={removingDevice === device.id}
                      className="text-sm text-red-400 hover:text-red-300 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
                      aria-label={`Remove device ${label}`}
                    >
                      {removingDevice === device.id ? 'Removing...' : 'Remove'}
                    </button>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </section>

      {/* Sessions Section */}
      <section className="bg-gray-800 rounded-xl p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Sessions</h2>
        <div className="space-y-3 mb-4">
          {sessions.map((session) => (
            <div
              key={session.id}
              className="flex items-center justify-between bg-gray-700/50 rounded-lg px-4 py-3"
            >
              <div>
                <p className="text-sm font-medium">
                  Session {session.id}
                  {session.current && (
                    <span className="ml-2 text-xs bg-green-600/20 text-green-400 px-2 py-0.5 rounded-full">
                      Current
                    </span>
                  )}
                </p>
                <p className="text-xs text-gray-400">
                  Created {new Date(session.created_at).toLocaleDateString()} — Expires{' '}
                  {new Date(session.expires_at).toLocaleDateString()}
                </p>
              </div>
            </div>
          ))}
          {sessions.length === 0 && (
            <p className="text-sm text-gray-400">No active sessions found.</p>
          )}
        </div>
        {sessions.length > 1 && (
          <button
            onClick={signOutEverywhere}
            className="bg-gray-700 hover:bg-gray-600 text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer"
          >
            Sign out everywhere else
          </button>
        )}
      </section>

      {/* Integrations Section */}
      <section className="bg-gray-800 rounded-xl p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Integrations</h2>

        {/* Hetzner Cloud API Token */}
        <div>
          <div className="flex items-center justify-between mb-2">
            <div>
              <p className="font-medium">Hetzner Cloud API token</p>
              <p className="text-sm text-gray-400">Used by VPS Stats and Bandwidth infra modules</p>
            </div>
          </div>

          {hetznerError && (
            <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
              {hetznerError}
              <button onClick={() => setHetznerError(null)} className="ml-2 underline cursor-pointer" aria-label="Dismiss error">dismiss</button>
            </div>
          )}

          {hetznerToken?.configured ? (
            <div className="flex items-center gap-3">
              <span className="text-xs text-gray-400 font-mono">{hetznerToken.masked}</span>
              <button
                onClick={handleDeleteHetznerToken}
                disabled={hetznerDeleting}
                className="text-xs text-red-400 hover:text-red-300 underline cursor-pointer disabled:opacity-50"
                aria-label="Remove Hetzner API token"
              >
                {hetznerDeleting ? 'Removing...' : 'Remove'}
              </button>
            </div>
          ) : (
            <div className="flex gap-2">
              <div className="relative flex-1">
                <input
                  type={hetznerShowToken ? 'text' : 'password'}
                  placeholder="Hetzner Cloud API token"
                  value={hetznerNewToken}
                  onChange={e => setHetznerNewToken(e.target.value)}
                  className="w-full px-3 py-2 pr-10 rounded-lg bg-gray-900 border border-gray-600 text-white text-sm focus:outline-none focus:border-blue-500"
                  aria-label="Hetzner API token"
                />
                <button
                  type="button"
                  onClick={() => setHetznerShowToken(!hetznerShowToken)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 cursor-pointer"
                  aria-label={hetznerShowToken ? 'Hide token' : 'Show token'}
                >
                  {hetznerShowToken ? <EyeOff size={14} /> : <Eye size={14} />}
                </button>
              </div>
              <button
                onClick={handleSaveHetznerToken}
                disabled={hetznerSaving || !hetznerNewToken.trim()}
                className="px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
              >
                {hetznerSaving ? 'Saving...' : 'Save'}
              </button>
            </div>
          )}
        </div>
      </section>

      {/* Danger Zone */}
      <section className="bg-gray-800 rounded-xl p-6 border border-red-900/50">
        <h2 className="text-lg font-semibold text-red-400 mb-4">Danger Zone</h2>
        {!showDeleteConfirm ? (
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">Delete account</p>
              <p className="text-sm text-gray-400">
                Permanently remove your account and all associated data
              </p>
            </div>
            <button
              onClick={() => setShowDeleteConfirm(true)}
              className="bg-red-600 hover:bg-red-700 text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer"
            >
              Delete account
            </button>
          </div>
        ) : (
          <div>
            <p className="text-sm text-gray-300 mb-3">
              This action is irreversible. Type <span className="font-mono font-bold text-red-400">DELETE</span> to confirm.
            </p>
            <input
              type="text"
              value={deleteConfirmText}
              onChange={(e) => setDeleteConfirmText(e.target.value)}
              placeholder="Type DELETE to confirm"
              className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white w-full mb-3 focus:outline-none focus:ring-2 focus:ring-red-500"
            />
            <div className="flex gap-3">
              <button
                onClick={deleteAccount}
                disabled={deleteConfirmText !== 'DELETE'}
                className="bg-red-600 hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer"
              >
                Permanently delete my account
              </button>
              <button
                onClick={() => {
                  setShowDeleteConfirm(false)
                  setDeleteConfirmText('')
                }}
                className="bg-gray-700 hover:bg-gray-600 text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </section>
    </main>
  )
}

export default Settings
