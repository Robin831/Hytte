import { useState, useEffect, useCallback } from 'react'
import { useAuth } from '../auth'
import { useNavigate } from 'react-router-dom'

const NORWEGIAN_CITIES = [
  'Oslo',
  'Bergen',
  'Trondheim',
  'Stavanger',
  'Tromsø',
  'Kristiansand',
  'Drammen',
  'Fredrikstad',
  'Bodø',
  'Ålesund',
  'Lillehammer',
  'Haugesund',
  'Molde',
  'Narvik',
  'Alta',
]

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
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleteConfirmText, setDeleteConfirmText] = useState('')

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
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    loadData()
    return () => { cancelled = true }
  }, [])

  const savePreference = async (key: string, value: string) => {
    setSaving(true)
    const res = await fetch('/api/settings/preferences', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ preferences: { [key]: value } }),
    })
    if (res.ok) {
      const data = await res.json()
      setPreferences(data.preferences || {})
    }
    setSaving(false)
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
      <main className="flex items-center justify-center" style={{ minHeight: 'calc(100vh - 72px)' }}>
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
    <main className="max-w-2xl mx-auto px-4 py-8" style={{ minHeight: 'calc(100vh - 72px)' }}>
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
            {NORWEGIAN_CITIES.map((city) => (
              <option key={city} value={city}>
                {city}
              </option>
            ))}
          </select>
        </div>
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
