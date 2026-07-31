import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Download, Loader2 } from 'lucide-react'
import { useAuth } from '../../auth'
import { formatDate } from '../../utils/formatDate'
import { timeAgo } from '../../utils/timeAgo'
import { UNKNOWN_DEVICE_LABEL, type SessionInfo } from './types'

/** Local-time YYYY-MM-DD, used as the export filename fallback. */
function localDateStamp(): string {
  const now = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

/** Pulls the filename out of a Content-Disposition header, if present. */
function filenameFromDisposition(header: string | null): string {
  const match = header?.match(/filename="?([^";]+)"?/)
  return match?.[1] ?? `hytte-export-${localDateStamp()}.json`
}

function SecuritySection() {
  const { t } = useTranslation(['settings', 'common'])
  const { t: tCommon } = useTranslation('common')
  const { logout } = useAuth()
  const navigate = useNavigate()
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [sessionsLoaded, setSessionsLoaded] = useState(false)
  const [revokeConfirmId, setRevokeConfirmId] = useState<string | null>(null)
  const [revokingId, setRevokingId] = useState<string | null>(null)
  const [revokeError, setRevokeError] = useState<string | null>(null)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleteConfirmText, setDeleteConfirmText] = useState('')
  const [isExporting, setIsExporting] = useState(false)
  const [exportError, setExportError] = useState<string | null>(null)
  const [exportDone, setExportDone] = useState(false)

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
      } finally {
        if (!cancelled) setSessionsLoaded(true)
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

  const revokeSession = async (id: string) => {
    setRevokingId(id)
    setRevokeError(null)
    try {
      const res = await fetch(`/api/settings/sessions/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        throw new Error(`revoke failed with status ${res.status}`)
      }
      setRevokeConfirmId(null)
      await fetchSessions()
    } catch (err) {
      console.error('Failed to revoke session:', err)
      setRevokeError(t('sessions.revokeError'))
    } finally {
      setRevokingId(null)
    }
  }

  const downloadExport = async () => {
    setIsExporting(true)
    setExportError(null)
    setExportDone(false)
    let objectUrl: string | null = null
    try {
      const res = await fetch('/api/settings/export', { credentials: 'include' })
      if (!res.ok) {
        throw new Error(`export request failed with status ${res.status}`)
      }
      const filename = filenameFromDisposition(res.headers.get('Content-Disposition'))
      const blob = await res.blob()
      objectUrl = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = objectUrl
      link.download = filename
      document.body.appendChild(link)
      link.click()
      link.remove()
      setExportDone(true)
    } catch (err) {
      console.error('Failed to export data:', err)
      setExportError(t('dataExport.error'))
    } finally {
      if (objectUrl) {
        // Defer revocation — revoking synchronously after click() can abort
        // the download in some browsers.
        const url = objectUrl
        setTimeout(() => URL.revokeObjectURL(url), 1000)
      }
      setIsExporting(false)
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
            className="flex flex-col gap-2 bg-gray-700/50 rounded-lg px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
          >
            <div className="min-w-0">
              <p className="text-sm font-medium">
                {session.device_label && session.device_label !== UNKNOWN_DEVICE_LABEL
                  ? session.device_label
                  : t('sessions.unknownDevice')}
                {session.current && (
                  <span className="ml-2 text-xs bg-green-600/20 text-green-400 px-2 py-0.5 rounded-full">
                    {t('sessions.current')}
                  </span>
                )}
              </p>
              <p className="text-xs text-gray-400">
                {session.last_seen_at
                  ? t('sessions.lastActive', { time: timeAgo(session.last_seen_at, tCommon) })
                  : t('sessions.lastActiveUnknown')}
              </p>
              <p className="text-xs text-gray-500">
                {t('sessions.session', { id: session.id })} —{' '}
                {t('sessions.createdExpires', {
                  created: formatDate(session.created_at),
                  expires: formatDate(session.expires_at),
                })}
              </p>
            </div>
            {!session.current && (
              revokeConfirmId === session.id ? (
                <div className="flex flex-col gap-2 sm:items-end shrink-0">
                  <p className="text-xs text-gray-300">{t('sessions.revokeConfirmPrompt')}</p>
                  <div className="flex gap-2">
                    <button
                      onClick={() => revokeSession(session.id)}
                      disabled={revokingId === session.id}
                      className="bg-red-600 hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed text-xs text-white px-3 py-1.5 rounded-lg transition-colors cursor-pointer"
                    >
                      {t('sessions.revokeConfirm')}
                    </button>
                    <button
                      onClick={() => {
                        setRevokeConfirmId(null)
                        setRevokeError(null)
                      }}
                      className="bg-gray-700 hover:bg-gray-600 text-xs text-white px-3 py-1.5 rounded-lg transition-colors cursor-pointer"
                    >
                      {t('sessions.revokeCancel')}
                    </button>
                  </div>
                </div>
              ) : (
                <button
                  onClick={() => {
                    setRevokeConfirmId(session.id)
                    setRevokeError(null)
                  }}
                  className="bg-gray-700 hover:bg-gray-600 text-xs text-white px-3 py-1.5 rounded-lg transition-colors cursor-pointer shrink-0 self-start sm:self-auto"
                >
                  {t('sessions.revoke')}
                </button>
              )
            )}
          </div>
        ))}
        {revokeError && (
          <p className="text-sm text-red-400" role="alert">{revokeError}</p>
        )}
        {sessionsLoaded && sessions.length === 0 && (
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

      {/* Data export — a safe off-ramp, kept above the destructive actions. */}
      <div className="border-t border-gray-700 pt-4 mt-4">
        <p className="text-sm font-medium text-gray-300 mb-3">{t('dataExport.heading')}</p>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-sm text-gray-400">{t('dataExport.description')}</p>
          <button
            onClick={downloadExport}
            disabled={isExporting}
            aria-busy={isExporting}
            className="flex items-center justify-center gap-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer shrink-0"
          >
            {isExporting ? (
              <Loader2 size={16} className="animate-spin" />
            ) : (
              <Download size={16} />
            )}
            {isExporting ? t('dataExport.inProgress') : t('dataExport.button')}
          </button>
        </div>
        {exportError && (
          <p className="text-sm text-red-400 mt-2" role="alert">{exportError}</p>
        )}
        {exportDone && !exportError && (
          <p className="text-sm text-green-400 mt-2">{t('dataExport.success')}</p>
        )}
      </div>

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
