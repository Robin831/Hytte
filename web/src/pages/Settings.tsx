import { useState, useEffect, useCallback } from 'react'
import { useAuth } from '../auth'
import { useNavigate } from 'react-router-dom'
import {
  isPushSupported,
  isPushSubscribed,
  subscribeToPush,
  unsubscribeFromPush,
} from '../push'

interface SessionInfo {
  id: string
  created_at: string
  expires_at: string
  current: boolean
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

  const fetchSessions = useCallback(async () => {
    const res = await fetch('/api/settings/sessions')
    if (res.ok) {
      const data = await res.json()
      setSessions(data.sessions || [])
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    async function loadData() {
      try {
        const [prefsRes, sessionsRes] = await Promise.all([
          fetch('/api/settings/preferences'),
          fetch('/api/settings/sessions'),
        ])
        if (cancelled) return
        if (prefsRes.ok) {
          const data = await prefsRes.json()
          setPreferences(data.preferences || {})
        }
        if (sessionsRes.ok) {
          const data = await sessionsRes.json()
          setSessions(data.sessions || [])
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

  // Check push subscription status on mount.
  useEffect(() => {
    let cancelled = false
    if (pushSupported) {
      isPushSubscribed()
        .then((subscribed) => {
          if (!cancelled) setPushSubscribed(subscribed)
        })
        .catch((err) => {
          console.error('Failed to check push subscription status:', err)
        })
    }
    return () => { cancelled = true }
  }, [pushSupported])

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

  const savePreference = async (key: string, value: string) => {
    setSaving(true)
    try {
      const res = await fetch('/api/settings/preferences', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ preferences: { [key]: value } }),
      })
      if (res.ok) {
        const data = await res.json()
        setPreferences(data.preferences || {})
      }
    } finally {
      setSaving(false)
    }
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
    }
  }

  const signOutEverywhere = async () => {
    const res = await fetch('/api/settings/sessions/revoke-others', { method: 'POST' })
    if (res.ok) {
      await fetchSessions()
    }
  }

  const deleteAccount = async () => {
    const res = await fetch('/api/settings/account', { method: 'DELETE' })
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
