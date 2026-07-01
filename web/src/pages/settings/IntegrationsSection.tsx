import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { Eye, EyeOff } from 'lucide-react'
import { useAuth } from '../../auth'
import { Skeleton } from '../../components/ui/skeleton'
import type { HetznerTokenState, PreferenceSectionProps } from './types'

type IntegrationsSectionProps = Pick<PreferenceSectionProps, 'preferences' | 'saving' | 'savePreference'> & {
  queuePreference: (key: string, value: string) => void
  flushPreferences: () => void
}

function IntegrationsSection({ preferences, saving, savePreference, queuePreference, flushPreferences }: IntegrationsSectionProps) {
  const { t } = useTranslation(['settings', 'common'])
  const { user, hasFeature } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()

  const [hetznerToken, setHetznerToken] = useState<HetznerTokenState | null>(null)
  const [hetznerNewToken, setHetznerNewToken] = useState('')
  const [hetznerShowToken, setHetznerShowToken] = useState(false)
  const [hetznerSaving, setHetznerSaving] = useState(false)
  const [hetznerDeleting, setHetznerDeleting] = useState(false)
  const [hetznerError, setHetznerError] = useState<string | null>(null)
  const [netatmoConnected, setNetatmoConnected] = useState<boolean | null>(
    searchParams.get('netatmo') === 'connected' ? true : null
  )
  const [netatmoDisconnecting, setNetatmoDisconnecting] = useState(false)
  const [netatmoError, setNetatmoError] = useState<string | null>(
    searchParams.get('netatmo') === 'error' ? t('integrations.netatmoConnectFailed') : null
  )
  const [wordfeudConnected, setWordfeudConnected] = useState<boolean | null>(null)
  const [wordfeudConnecting, setWordfeudConnecting] = useState(false)
  const [wordfeudDisconnecting, setWordfeudDisconnecting] = useState(false)
  const [wordfeudError, setWordfeudError] = useState<string | null>(null)
  const [wordfeudEmail, setWordfeudEmail] = useState('')
  const [wordfeudPassword, setWordfeudPassword] = useState('')
  const [claudeTesting, setClaudeTesting] = useState(false)
  const [claudeTestResult, setClaudeTestResult] = useState<{ ok: boolean; message: string } | null>(null)
  const [claudeCliPathDraft, setClaudeCliPathDraft] = useState(preferences.claude_cli_path || '')

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
      if (!res.ok) throw new Error('remove-token-failed')
      await loadHetznerToken()
    } catch {
      setHetznerError(t('integrations.failedRemoveToken'))
    } finally {
      setHetznerDeleting(false)
    }
  }

  // Load Hetzner token status — skip for users without infra access.
  useEffect(() => {
    if (!user?.is_admin && !hasFeature('infra')) return
    const controller = new AbortController()
    async function load() {
      await loadHetznerToken(controller.signal)
    }
    load()
    return () => controller.abort()
  }, [hasFeature, user?.is_admin, loadHetznerToken])

  // Load Netatmo connection status — admin only.
  useEffect(() => {
    if (!user?.is_admin) return
    const controller = new AbortController()
    fetch('/api/netatmo/status', { credentials: 'include', signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`Failed to load netatmo status (${res.status})`)
        return res.json()
      })
      .then((data) => setNetatmoConnected(Boolean(data.connected)))
      .catch((err) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        // Not configured or not available — treat as disconnected.
        setNetatmoConnected(false)
      })
    return () => controller.abort()
  }, [user?.is_admin])

  // Load Wordfeud connection status — admin only.
  useEffect(() => {
    if (!user?.is_admin) return
    const controller = new AbortController()
    fetch('/api/wordfeud/status', { credentials: 'include', signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`Failed to load wordfeud status (${res.status})`)
        return res.json()
      })
      .then((data) => setWordfeudConnected(Boolean(data.connected)))
      .catch((err) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setWordfeudConnected(false)
      })
    return () => controller.abort()
  }, [user?.is_admin])

  // Remove the netatmo query param without adding a history entry.
  // State is initialized from the param above; this just cleans up the URL.
  useEffect(() => {
    if (!searchParams.get('netatmo')) return
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      next.delete('netatmo')
      return next
    }, { replace: true })
  }, [searchParams, setSearchParams, t])

  const handleNetatmoDisconnect = async () => {
    setNetatmoDisconnecting(true)
    setNetatmoError(null)
    try {
      const res = await fetch('/api/netatmo/token', {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('disconnect-failed')
      setNetatmoConnected(false)
    } catch {
      setNetatmoError(t('integrations.netatmoDisconnectFailed'))
    } finally {
      setNetatmoDisconnecting(false)
    }
  }

  const handleWordfeudConnect = async () => {
    setWordfeudConnecting(true)
    setWordfeudError(null)
    try {
      const res = await fetch('/api/wordfeud/connect', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: wordfeudEmail, password: wordfeudPassword }),
      })
      const data = await res.json().catch(() => null)
      if (!res.ok) {
        setWordfeudError(data?.error || t('integrations.wordfeudConnectFailed'))
        return
      }
      setWordfeudConnected(true)
      setWordfeudEmail('')
      setWordfeudPassword('')
    } catch {
      setWordfeudError(t('integrations.wordfeudConnectFailed'))
    } finally {
      setWordfeudConnecting(false)
    }
  }

  const handleWordfeudDisconnect = async () => {
    setWordfeudDisconnecting(true)
    setWordfeudError(null)
    try {
      const res = await fetch('/api/wordfeud/disconnect', {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('disconnect-failed')
      setWordfeudConnected(false)
    } catch {
      setWordfeudError(t('integrations.wordfeudDisconnectFailed'))
    } finally {
      setWordfeudDisconnecting(false)
    }
  }

  return (
    <>
      {/* Hetzner Cloud API Token */}
      <div>
        <div className="flex items-center justify-between mb-2">
          <div>
            <p className="font-medium">{t('integrations.hetznerToken')}</p>
            <p className="text-sm text-gray-400">{t('integrations.hetznerDescription')}</p>
          </div>
        </div>

        {hetznerError && (
          <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
            {hetznerError}
            <button onClick={() => setHetznerError(null)} className="ml-2 underline cursor-pointer" aria-label={t('integrations.dismissErrorAriaLabel')}>{t('integrations.dismiss')}</button>
          </div>
        )}

        {hetznerToken?.configured ? (
          <div className="flex items-center gap-3">
            <span className="text-xs text-gray-400 font-mono">{hetznerToken.masked}</span>
            <button
              onClick={handleDeleteHetznerToken}
              disabled={hetznerDeleting}
              className="text-xs text-red-400 hover:text-red-300 underline cursor-pointer disabled:opacity-50"
              aria-label={t('integrations.hetznerRemoveAriaLabel')}
            >
              {hetznerDeleting ? t('integrations.removing') : t('notifications.remove')}
            </button>
          </div>
        ) : (
          <div className="flex gap-2">
            <div className="relative flex-1">
              <input
                type={hetznerShowToken ? 'text' : 'password'}
                placeholder={t('integrations.hetznerPlaceholder')}
                value={hetznerNewToken}
                onChange={e => setHetznerNewToken(e.target.value)}
                className="w-full px-3 py-2 pr-10 rounded-lg bg-gray-900 border border-gray-600 text-white text-sm focus:outline-none focus:border-blue-500"
                aria-label={t('integrations.hetznerAriaLabel')}
              />
              <button
                type="button"
                onClick={() => setHetznerShowToken(!hetznerShowToken)}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 cursor-pointer"
                aria-label={hetznerShowToken ? t('integrations.hideToken') : t('integrations.showToken')}
              >
                {hetznerShowToken ? <EyeOff size={14} /> : <Eye size={14} />}
              </button>
            </div>
            <button
              onClick={handleSaveHetznerToken}
              disabled={hetznerSaving || !hetznerNewToken.trim()}
              className="px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
            >
              {hetznerSaving ? t('integrations.saving') : t('integrations.save')}
            </button>
          </div>
        )}
      </div>

      {/* Claude AI */}
      <div className="border-t border-gray-700 pt-4 mt-4">
        <div className="flex items-center justify-between mb-3">
          <div>
            <p className="font-medium">{t('integrations.claudeAI')}</p>
            <p className="text-sm text-gray-400">{t('integrations.claudeDescription')}</p>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={preferences.claude_enabled === 'true'}
            onClick={() =>
              savePreference('claude_enabled', preferences.claude_enabled === 'true' ? 'false' : 'true')
            }
            disabled={saving}
            aria-label={preferences.claude_enabled === 'true' ? t('integrations.disableClaude') : t('integrations.enableClaude')}
            className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
              preferences.claude_enabled === 'true' ? 'bg-blue-600' : 'bg-gray-600'
            }`}
          >
            <span
              className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                preferences.claude_enabled === 'true' ? 'translate-x-6' : 'translate-x-1'
              }`}
            />
          </button>
        </div>

        {preferences.claude_enabled === 'true' && (
          <div className="space-y-3">
            <div>
              <label htmlFor="claude-cli-path" className="text-sm text-gray-400 block mb-1">
                {t('integrations.claudeCliPath')}
              </label>
              <input
                id="claude-cli-path"
                type="text"
                value={claudeCliPathDraft}
                onChange={(e) => {
                  setClaudeCliPathDraft(e.target.value)
                  queuePreference('claude_cli_path', e.target.value)
                }}
                onBlur={() => flushPreferences()}
                placeholder="claude"
                disabled={saving}
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <p className="text-xs text-gray-500 mt-1">
                {t('integrations.claudeCliPathHint')}
              </p>
            </div>

            <div>
              <label htmlFor="claude-model" className="text-sm text-gray-400 block mb-1">
                {t('integrations.claudeModel')}
              </label>
              <select
                id="claude-model"
                value={preferences.claude_model || 'claude-sonnet-4-6'}
                onChange={(e) => savePreference('claude_model', e.target.value)}
                disabled={saving}
                className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                <option value="claude-fable-5">Claude Fable 5</option>
                <option value="claude-opus-4-8">Claude Opus 4.8</option>
                <option value="claude-opus-4-7">Claude Opus 4.7</option>
                <option value="claude-sonnet-4-6">Claude Sonnet 4.6</option>
                <option value="claude-haiku-4-5">Claude Haiku 4.5</option>
                <option value="claude-opus-4-6">Claude Opus 4.6</option>
              </select>
            </div>

            <div className="flex items-center gap-3">
              <button
                onClick={async () => {
                  setClaudeTesting(true)
                  setClaudeTestResult(null)
                  try {
                    const res = await fetch('/api/settings/claude-test', {
                      method: 'POST',
                      credentials: 'include',
                    })
                    const data = await res.json().catch(() => null)
                    if (data?.ok) {
                      setClaudeTestResult({ ok: true, message: `Connected — ${data.version}` })
                    } else {
                      setClaudeTestResult({ ok: false, message: data?.error || t('integrations.claudeTestFailed') })
                    }
                  } catch (err) {
                    console.error('Claude test failed:', err)
                    setClaudeTestResult({ ok: false, message: t('integrations.claudeTestFailed') })
                  } finally {
                    setClaudeTesting(false)
                  }
                }}
                disabled={claudeTesting}
                className="bg-gray-700 hover:bg-gray-600 text-sm text-white px-4 py-2 rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {claudeTesting ? t('integrations.claudeTesting') : t('integrations.claudeTestButton')}
              </button>
              {claudeTestResult && (
                <p className={`text-sm ${claudeTestResult.ok ? 'text-green-400' : 'text-red-400'}`}>
                  {claudeTestResult.message}
                </p>
              )}
            </div>
          </div>
        )}
      </div>

      {/* Netatmo weather station — admin only */}
      {user?.is_admin && (
        <div className="border-t border-gray-700 pt-4 mt-4">
          <div className="flex items-center justify-between mb-2">
            <div>
              <p className="font-medium">{t('integrations.netatmo')}</p>
              <p className="text-sm text-gray-400">{t('integrations.netatmoDescription')}</p>
            </div>
          </div>

          {netatmoError && (
            <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
              {netatmoError}
              <button
                onClick={() => setNetatmoError(null)}
                className="ml-2 underline cursor-pointer"
                aria-label={t('integrations.dismissErrorAriaLabel')}
              >
                {t('integrations.dismiss')}
              </button>
            </div>
          )}

          {netatmoConnected === null ? (
            <div role="status" aria-live="polite">
              <span className="sr-only">{t('common:status.checking')}</span>
              <Skeleton className="h-5 w-40" />
            </div>
          ) : netatmoConnected ? (
            <div className="flex items-center gap-3">
              <span className="text-sm text-green-400">{t('integrations.netatmoConnected')}</span>
              <button
                onClick={handleNetatmoDisconnect}
                disabled={netatmoDisconnecting}
                className="text-xs text-red-400 hover:text-red-300 underline cursor-pointer disabled:opacity-50"
                aria-label={t('integrations.netatmoDisconnectAriaLabel')}
              >
                {netatmoDisconnecting ? t('integrations.removing') : t('integrations.netatmoDisconnect')}
              </button>
            </div>
          ) : (
            <a
              href="/api/netatmo/auth/login"
              className="inline-block px-3 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors"
            >
              {t('integrations.netatmoConnect')}
            </a>
          )}
        </div>
      )}

      {/* Wordfeud — admin only */}
      {user?.is_admin && (
        <div className="border-t border-gray-700 pt-4 mt-4">
          <div className="flex items-center justify-between mb-2">
            <div>
              <p className="font-medium">{t('integrations.wordfeud')}</p>
              <p className="text-sm text-gray-400">{t('integrations.wordfeudDescription')}</p>
            </div>
          </div>

          {wordfeudError && (
            <div className="text-sm text-red-400 mb-3 px-3 py-2 bg-red-400/10 rounded border border-red-400/20">
              {wordfeudError}
              <button
                onClick={() => setWordfeudError(null)}
                className="ml-2 underline cursor-pointer"
                aria-label={t('integrations.dismissErrorAriaLabel')}
              >
                {t('integrations.dismiss')}
              </button>
            </div>
          )}

          {wordfeudConnected === null ? (
            <div role="status" aria-live="polite">
              <span className="sr-only">{t('common:status.checking')}</span>
              <Skeleton className="h-5 w-40" />
            </div>
          ) : wordfeudConnected ? (
            <div className="flex items-center gap-3">
              <span className="text-sm text-green-400">{t('integrations.wordfeudConnected')}</span>
              <button
                onClick={handleWordfeudDisconnect}
                disabled={wordfeudDisconnecting}
                className="text-xs text-red-400 hover:text-red-300 underline cursor-pointer disabled:opacity-50"
                aria-label={t('integrations.wordfeudDisconnectAriaLabel')}
              >
                {wordfeudDisconnecting ? t('integrations.removing') : t('integrations.wordfeudDisconnect')}
              </button>
            </div>
          ) : (
            <div className="space-y-3">
              <div>
                <label htmlFor="wordfeud-email" className="text-sm text-gray-400 block mb-1">
                  {t('integrations.wordfeudEmail')}
                </label>
                <input
                  id="wordfeud-email"
                  type="email"
                  value={wordfeudEmail}
                  onChange={(e) => setWordfeudEmail(e.target.value)}
                  placeholder={t('integrations.wordfeudEmailPlaceholder')}
                  disabled={wordfeudConnecting}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
              <div>
                <label htmlFor="wordfeud-password" className="text-sm text-gray-400 block mb-1">
                  {t('integrations.wordfeudPassword')}
                </label>
                <input
                  id="wordfeud-password"
                  type="password"
                  value={wordfeudPassword}
                  onChange={(e) => setWordfeudPassword(e.target.value)}
                  placeholder={t('integrations.wordfeudPasswordPlaceholder')}
                  disabled={wordfeudConnecting}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
              <button
                onClick={handleWordfeudConnect}
                disabled={wordfeudConnecting || !wordfeudEmail || !wordfeudPassword}
                className="bg-blue-600 hover:bg-blue-500 text-white text-sm px-4 py-2 rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {wordfeudConnecting ? t('integrations.wordfeudConnecting') : t('integrations.wordfeudConnect')}
              </button>
            </div>
          )}
        </div>
      )}
    </>
  )
}

export default IntegrationsSection
