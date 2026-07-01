import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../../auth'
import { formatDate } from '../../utils/formatDate'
import type { SessionInfo } from './types'

function SecuritySection() {
  const { t } = useTranslation(['settings', 'common'])
  const { logout } = useAuth()
  const navigate = useNavigate()
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleteConfirmText, setDeleteConfirmText] = useState('')

  const fetchSessions = useCallback(async () => {
    const res = await fetch('/api/settings/sessions', { credentials: 'include' })
    if (res.ok) {
      const data = await res.json()
      setSessions(data.sessions || [])
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    async function loadSessions() {
      try {
        const res = await fetch('/api/settings/sessions', { credentials: 'include' })
        if (cancelled) return
        if (res.ok) {
          const data = await res.json()
          setSessions(data.sessions || [])
        }
      } catch (err) {
        console.error('Failed to load sessions:', err)
      }
    }
    loadSessions()
    return () => { cancelled = true }
  }, [])

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

  return (
    <>
      {/* Sessions */}
      <p className="text-sm font-medium text-gray-300 mb-3">{t('sessions.heading')}</p>
      <div className="space-y-3 mb-4">
        {sessions.map((session) => (
          <div
            key={session.id}
            className="flex items-center justify-between bg-gray-700/50 rounded-lg px-4 py-3"
          >
            <div>
              <p className="text-sm font-medium">
                {t('sessions.session', { id: session.id })}
                {session.current && (
                  <span className="ml-2 text-xs bg-green-600/20 text-green-400 px-2 py-0.5 rounded-full">
                    {t('sessions.current')}
                  </span>
                )}
              </p>
              <p className="text-xs text-gray-400">
                {t('sessions.createdExpires', {
                  created: formatDate(session.created_at),
                  expires: formatDate(session.expires_at),
                })}
              </p>
            </div>
          </div>
        ))}
        {sessions.length === 0 && (
          <p className="text-sm text-gray-400">{t('sessions.noSessions')}</p>
        )}
      </div>
      {sessions.length > 1 && (
        <button
          onClick={signOutEverywhere}
          className="bg-gray-700 hover:bg-gray-600 text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer"
        >
          {t('sessions.signOutEverywhere')}
        </button>
      )}

      {/* Danger Zone */}
      <div className="border-t border-gray-700 pt-4 mt-4">
        <p className="text-sm font-medium text-red-400 mb-3">{t('dangerZone.heading')}</p>
        {!showDeleteConfirm ? (
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">{t('dangerZone.deleteAccount')}</p>
              <p className="text-sm text-gray-400">
                {t('dangerZone.deleteAccountDescription')}
              </p>
            </div>
            <button
              onClick={() => setShowDeleteConfirm(true)}
              className="bg-red-600 hover:bg-red-700 text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer"
            >
              {t('dangerZone.deleteAccount')}
            </button>
          </div>
        ) : (
          <div>
            <p className="text-sm text-gray-300 mb-3">
              {t('dangerZone.deleteIrreversibleBefore')} <span className="font-mono font-bold text-red-400">{t('dangerZone.deleteKeyword')}</span> {t('dangerZone.deleteIrreversibleAfter')}
            </p>
            <input
              type="text"
              value={deleteConfirmText}
              onChange={(e) => setDeleteConfirmText(e.target.value)}
              placeholder={t('dangerZone.deleteTypePlaceholder')}
              aria-label={t('dangerZone.deleteTypePlaceholder')}
              className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white w-full mb-3 focus:outline-none focus:ring-2 focus:ring-red-500"
            />
            <div className="flex gap-3">
              <button
                onClick={deleteAccount}
                disabled={deleteConfirmText !== 'DELETE'}
                className="bg-red-600 hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer"
              >
                {t('dangerZone.deleteConfirmButton')}
              </button>
              <button
                onClick={() => {
                  setShowDeleteConfirm(false)
                  setDeleteConfirmText('')
                }}
                className="bg-gray-700 hover:bg-gray-600 text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer"
              >
                {t('dangerZone.cancel')}
              </button>
            </div>
          </div>
        )}
      </div>
    </>
  )
}

export default SecuritySection
