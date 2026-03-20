import { useState, useEffect, useCallback } from 'react'
import { useAuth } from '../auth'
import { useNavigate } from 'react-router-dom'

interface UserFeatureSet {
  user_id: number
  email: string
  name: string
  picture: string
  is_admin: boolean
  features: Record<string, boolean>
}

const FEATURE_LABELS: Record<string, string> = {
  dashboard: 'Dashboard',
  weather: 'Weather',
  calendar: 'Calendar',
  notes: 'Notes',
  links: 'Links',
  training: 'Training',
  lactate: 'Lactate',
  infra: 'Infra',
  webhooks: 'Webhooks',
  claude_ai: 'Claude AI',
}

const FEATURE_ORDER = [
  'dashboard', 'weather', 'calendar', 'notes', 'links',
  'training', 'lactate', 'infra', 'webhooks', 'claude_ai',
]

function Admin() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const [users, setUsers] = useState<UserFeatureSet[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [togglingKey, setTogglingKey] = useState<string | null>(null)

  useEffect(() => {
    if (user && !user.is_admin) {
      navigate('/dashboard', { replace: true })
    }
  }, [user, navigate])

  const fetchUsers = useCallback(async () => {
    try {
      const res = await fetch('/api/admin/users', { credentials: 'include' })
      if (!res.ok) throw new Error('Failed to load users')
      const data = await res.json()
      setUsers(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load users')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchUsers()
  }, [fetchUsers])

  const toggleFeature = async (userId: number, feature: string, enabled: boolean) => {
    const key = `${userId}-${feature}`
    setTogglingKey(key)

    // Optimistic update
    setUsers(prev =>
      prev.map(u =>
        u.user_id === userId
          ? { ...u, features: { ...u.features, [feature]: enabled } }
          : u
      )
    )

    try {
      const res = await fetch(`/api/admin/users/${userId}/features`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ feature, enabled }),
      })
      if (!res.ok) {
        throw new Error('Failed to update feature')
      }
    } catch {
      // Revert on failure
      setUsers(prev =>
        prev.map(u =>
          u.user_id === userId
            ? { ...u, features: { ...u.features, [feature]: !enabled } }
            : u
        )
      )
    } finally {
      setTogglingKey(null)
    }
  }

  if (!user?.is_admin) return null

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 py-8">
      <h1 className="text-2xl font-bold mb-6">Admin — User Management</h1>

      {loading && <p className="text-gray-400">Loading users...</p>}
      {error && <p className="text-red-400">{error}</p>}

      {!loading && !error && (
        <section className="bg-gray-800 rounded-xl overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-700">
                  <th className="text-left px-4 py-3 font-medium text-gray-300">User</th>
                  {FEATURE_ORDER.map(key => (
                    <th key={key} className="px-3 py-3 font-medium text-gray-300 text-center whitespace-nowrap">
                      {FEATURE_LABELS[key]}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {users.map(u => (
                  <tr key={u.user_id} className="border-b border-gray-700/50 last:border-b-0">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-3">
                        {u.picture ? (
                          <img
                            src={u.picture}
                            alt={u.name}
                            className="w-8 h-8 rounded-full shrink-0"
                            referrerPolicy="no-referrer"
                          />
                        ) : (
                          <div className="w-8 h-8 rounded-full bg-blue-600 flex items-center justify-center text-sm font-medium shrink-0">
                            {u.name.charAt(0).toUpperCase()}
                          </div>
                        )}
                        <div className="min-w-0">
                          <p className="font-medium text-white truncate">
                            {u.name}
                            {u.is_admin && (
                              <span className="ml-2 text-xs text-blue-400 font-normal">(admin)</span>
                            )}
                          </p>
                          <p className="text-xs text-gray-500 truncate">{u.email}</p>
                        </div>
                      </div>
                    </td>
                    {FEATURE_ORDER.map(feature => {
                      const enabled = u.features[feature] ?? false
                      const toggling = togglingKey === `${u.user_id}-${feature}`

                      if (u.is_admin) {
                        return (
                          <td key={feature} className="px-3 py-3 text-center">
                            <span className="text-green-400 text-xs font-medium">All</span>
                          </td>
                        )
                      }

                      return (
                        <td key={feature} className="px-3 py-3 text-center">
                          <button
                            type="button"
                            role="switch"
                            aria-checked={enabled}
                            aria-label={`${enabled ? 'Disable' : 'Enable'} ${FEATURE_LABELS[feature]} for ${u.name}`}
                            onClick={() => toggleFeature(u.user_id, feature, !enabled)}
                            disabled={toggling}
                            className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                              enabled ? 'bg-blue-600' : 'bg-gray-600'
                            }`}
                          >
                            <span
                              className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                                enabled ? 'translate-x-6' : 'translate-x-1'
                              }`}
                            />
                          </button>
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  )
}

export default Admin
