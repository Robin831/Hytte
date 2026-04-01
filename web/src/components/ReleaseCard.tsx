import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Tag, RefreshCw, Rocket, CheckCircle, XCircle, Loader2, ExternalLink } from 'lucide-react'
import { CollapsiblePanelHeader } from './CollapsiblePanelHeader'
import { usePanelCollapse } from '../hooks/usePanelCollapse'
import ConfirmDialog from './ConfirmDialog'

interface FragmentSummary {
  file: string
  category: string
  summary: string
}

interface SuggestResponse {
  current_version: string
  suggested_version: string
  suggested_bump: string
  changelog_preview: FragmentSummary[]
}

interface StepResult {
  step: string
  command: string
  output?: string
  success: boolean
  error?: string
}

interface ReleaseResponse {
  version: string
  tag: string
  success: boolean
  steps: StepResult[]
}

interface ReleaseCardProps {
  showToast: (message: string, type: 'success' | 'error') => void
}

export default function ReleaseCard({ showToast }: ReleaseCardProps) {
  const { t } = useTranslation('forge')
  const [isOpen, toggle] = usePanelCollapse('release')

  const [suggestion, setSuggestion] = useState<SuggestResponse | null>(null)
  const [suggestLoading, setSuggestLoading] = useState(false)
  const [suggestError, setSuggestError] = useState<string | null>(null)

  const [version, setVersion] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [releasing, setReleasing] = useState(false)
  const [releaseResult, setReleaseResult] = useState<ReleaseResponse | null>(null)

  async function fetchSuggestion() {
    setSuggestLoading(true)
    setSuggestError(null)
    try {
      const res = await fetch('/api/forge/release/suggest', { credentials: 'include' })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error((data as { error?: string }).error ?? `HTTP ${res.status}`)
      }
      const data: SuggestResponse = await res.json()
      setSuggestion(data)
      setVersion(data.suggested_version)
      setReleaseResult(null)
    } catch (err) {
      setSuggestError(err instanceof Error ? err.message : t('unknownError'))
    } finally {
      setSuggestLoading(false)
    }
  }

  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      setSuggestLoading(true)
      setSuggestError(null)
      try {
        const res = await fetch('/api/forge/release/suggest', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          throw new Error((data as { error?: string }).error ?? `HTTP ${res.status}`)
        }
        const data: SuggestResponse = await res.json()
        setSuggestion(data)
        setVersion(data.suggested_version)
      } catch (err) {
        if (controller.signal.aborted) return
        setSuggestError(err instanceof Error ? err.message : t('unknownError'))
      } finally {
        if (!controller.signal.aborted) setSuggestLoading(false)
      }
    }
    void load()
    return () => controller.abort()
  }, [t])

  async function handleRelease() {
    setConfirmOpen(false)
    setReleasing(true)
    setReleaseResult(null)
    try {
      const res = await fetch('/api/forge/release', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version }),
      })
      const data: ReleaseResponse = await res.json()
      setReleaseResult(data)
      if (data.success) {
        showToast(t('release.success', { tag: data.tag }), 'success')
      } else {
        const failedStep = data.steps.find(s => !s.success)
        showToast(
          failedStep?.error ?? t('release.failed'),
          'error',
        )
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setReleasing(false)
    }
  }

  const categoryColors: Record<string, string> = {
    Added: 'text-green-400',
    Changed: 'text-blue-400',
    Fixed: 'text-amber-400',
    Removed: 'text-red-400',
    Deprecated: 'text-gray-400',
    Security: 'text-purple-400',
  }

  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <CollapsiblePanelHeader
        isOpen={isOpen}
        toggle={toggle}
        panelId="release-panel"
        icon={<Tag size={18} className="text-emerald-400 shrink-0" />}
        title={t('release.title')}
        trailing={
          suggestion && !suggestLoading ? (
            <span className="text-xs text-gray-500">
              v{suggestion.current_version}
            </span>
          ) : undefined
        }
      />

      <div id="release-panel" hidden={!isOpen}>
        {suggestLoading && !suggestion ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 size={20} className="animate-spin text-gray-400" />
          </div>
        ) : suggestError ? (
          <div className="px-5 py-4">
            <div className="bg-red-900/30 border border-red-700/50 rounded-lg p-3 text-red-300 text-sm flex items-center justify-between">
              <span>{suggestError}</span>
              <button
                type="button"
                onClick={() => void fetchSuggestion()}
                className="ml-3 text-red-400 hover:text-red-300 transition-colors"
                aria-label={t('release.refresh')}
              >
                <RefreshCw size={14} />
              </button>
            </div>
          </div>
        ) : suggestion ? (
          <div className="px-5 py-4 flex flex-col gap-4">
            {/* Version info row */}
            <div className="flex flex-wrap items-center gap-4">
              <div className="flex flex-col gap-0.5">
                <span className="text-xs text-gray-500">{t('release.currentVersion')}</span>
                <span className="text-sm font-mono text-gray-300">v{suggestion.current_version}</span>
              </div>
              <div className="flex flex-col gap-0.5">
                <span className="text-xs text-gray-500">{t('release.suggestedBump')}</span>
                <span className="text-sm font-medium text-emerald-400">{suggestion.suggested_bump}</span>
              </div>
              <button
                type="button"
                onClick={() => void fetchSuggestion()}
                disabled={suggestLoading}
                className="ml-auto text-gray-400 hover:text-gray-300 transition-colors disabled:opacity-50"
                aria-label={t('release.refresh')}
              >
                <RefreshCw size={14} className={suggestLoading ? 'animate-spin' : ''} />
              </button>
            </div>

            {/* Changelog preview */}
            {suggestion.changelog_preview.length > 0 && (
              <div>
                <h4 className="text-xs font-medium text-gray-500 mb-2">{t('release.changelogPreview')}</h4>
                <ul className="space-y-1">
                  {[...suggestion.changelog_preview]
                    .sort((a, b) => a.category.localeCompare(b.category))
                    .map((frag) => (
                    <li key={frag.file} className="text-sm flex items-start gap-2">
                      <span className={`shrink-0 font-medium ${categoryColors[frag.category] ?? 'text-gray-400'}`}>
                        {frag.category}
                      </span>
                      <span className="text-gray-300">{frag.summary}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {/* Version input + release button */}
            <div className="flex items-end gap-3">
              <div className="flex flex-col gap-1">
                <label htmlFor="release-version" className="text-xs text-gray-500">
                  {t('release.versionLabel')}
                </label>
                <input
                  id="release-version"
                  type="text"
                  value={version}
                  onChange={e => setVersion(e.target.value)}
                  disabled={releasing}
                  className="w-36 px-3 py-1.5 rounded-lg text-sm font-mono bg-gray-900 border border-gray-600
                    text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-emerald-500
                    disabled:opacity-50"
                  placeholder="1.2.3"
                />
              </div>
              <button
                type="button"
                onClick={() => setConfirmOpen(true)}
                disabled={releasing || !version.trim()}
                className="flex items-center gap-1.5 min-h-[36px] px-4 rounded-lg text-sm font-medium transition-colors
                  bg-emerald-600 text-white hover:bg-emerald-500
                  disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {releasing ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : (
                  <Rocket size={14} />
                )}
                {t('release.releaseButton')}
              </button>
            </div>

            {/* Release steps result */}
            {releaseResult && (
              <div className={`rounded-lg border p-3 ${
                releaseResult.success
                  ? 'bg-emerald-900/20 border-emerald-700/50'
                  : 'bg-red-900/20 border-red-700/50'
              }`}>
                <div className="flex items-center gap-2 mb-2">
                  {releaseResult.success ? (
                    <CheckCircle size={16} className="text-emerald-400" />
                  ) : (
                    <XCircle size={16} className="text-red-400" />
                  )}
                  <span className={`text-sm font-medium ${
                    releaseResult.success ? 'text-emerald-300' : 'text-red-300'
                  }`}>
                    {releaseResult.success
                      ? t('release.successTitle', { tag: releaseResult.tag })
                      : t('release.failedTitle')}
                  </span>
                </div>
                <ul className="space-y-1">
                  {releaseResult.steps.map((step) => (
                    <li key={step.step} className="flex items-center gap-2 text-xs">
                      {step.success ? (
                        <CheckCircle size={12} className="text-emerald-500 shrink-0" />
                      ) : (
                        <XCircle size={12} className="text-red-500 shrink-0" />
                      )}
                      <span className="text-gray-400 font-mono">{step.step}</span>
                      {step.error && (
                        <span className="text-red-400 truncate">{step.error}</span>
                      )}
                    </li>
                  ))}
                </ul>

                {/* GitHub Actions link after successful push */}
                {releaseResult.success && (
                  <a
                    href="https://github.com/Robin831/Hytte/actions"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1.5 mt-3 text-xs text-emerald-400 hover:text-emerald-300 transition-colors"
                  >
                    <ExternalLink size={12} />
                    {t('release.viewActions')}
                  </a>
                )}
              </div>
            )}
          </div>
        ) : null}
      </div>

      <ConfirmDialog
        open={confirmOpen}
        title={t('release.confirmTitle')}
        message={t('release.confirmMessage', { version: `v${version}` })}
        confirmLabel={t('release.releaseButton')}
        onConfirm={() => void handleRelease()}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  )
}
