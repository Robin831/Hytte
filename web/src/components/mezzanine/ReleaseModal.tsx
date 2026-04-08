import { useState, useEffect, useRef, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { RefreshCw, Rocket, CheckCircle, XCircle, Loader2, ExternalLink } from 'lucide-react'
import { Dialog, DialogHeader, DialogBody } from '../ui/dialog'
import ConfirmDialog from '../ConfirmDialog'

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
  actions_url?: string
}

interface ReleaseModalProps {
  open: boolean
  onClose: () => void
  showToast: (message: string, type: 'success' | 'error') => void
}

const categoryColors: Record<string, string> = {
  Added: 'text-green-400',
  Changed: 'text-blue-400',
  Fixed: 'text-amber-400',
  Removed: 'text-red-400',
  Deprecated: 'text-gray-400',
  Security: 'text-purple-400',
}

export default function ReleaseModal({ open, onClose, showToast }: ReleaseModalProps) {
  const { t } = useTranslation('forge')
  const titleId = useId()

  const [suggestion, setSuggestion] = useState<SuggestResponse | null>(null)
  const [suggestLoading, setSuggestLoading] = useState(false)
  const [suggestError, setSuggestError] = useState<string | null>(null)

  const [version, setVersion] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [releasing, setReleasing] = useState(false)
  const [releaseResult, setReleaseResult] = useState<ReleaseResponse | null>(null)

  const abortRef = useRef<AbortController | null>(null)

  // Fetch suggestion when modal opens
  useEffect(() => {
    if (!open) return

    const controller = new AbortController()
    abortRef.current = controller

    async function fetchSuggestion() {
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
        if (!controller.signal.aborted) {
          setSuggestion(data)
          setVersion(data.suggested_version)
          setReleaseResult(null)
        }
      } catch (err) {
        if (controller.signal.aborted) return
        setSuggestError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!controller.signal.aborted) setSuggestLoading(false)
      }
    }

    void fetchSuggestion()
    return () => controller.abort()
  }, [open])

  function handleRefresh() {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setSuggestLoading(true)
    setSuggestError(null)

    async function fetchSuggestion() {
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
        if (!controller.signal.aborted) {
          setSuggestion(data)
          setVersion(data.suggested_version)
          setReleaseResult(null)
        }
      } catch (err) {
        if (controller.signal.aborted) return
        setSuggestError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!controller.signal.aborted) setSuggestLoading(false)
      }
    }

    void fetchSuggestion()
  }

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
      const raw: unknown = await res.json()
      if (!res.ok || typeof raw !== 'object' || raw === null || !Array.isArray((raw as Record<string, unknown>).steps)) {
        const msg = (raw as Record<string, unknown> | null)?.['error']
        showToast(typeof msg === 'string' ? msg : t('release.failed'), 'error')
        return
      }
      const data = raw as ReleaseResponse
      setReleaseResult(data)
      if (data.success) {
        showToast(t('release.success', { tag: data.tag }), 'success')
      } else {
        const failedStep = data.steps.find(s => !s.success)
        showToast(failedStep?.error ?? t('release.failed'), 'error')
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), 'error')
    } finally {
      setReleasing(false)
    }
  }

  return (
    <>
      <Dialog
        open={open}
        onClose={onClose}
        maxWidth="max-w-lg"
        aria-labelledby={titleId}
      >
        <DialogHeader
          id={titleId}
          title={t('release.title')}
          onClose={onClose}
        />
        <DialogBody>
          {suggestLoading && !suggestion ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 size={20} className="animate-spin text-gray-400" aria-label={t('release.refresh')} />
            </div>
          ) : suggestError ? (
            <div className="bg-red-900/30 border border-red-700/50 rounded-lg p-3 text-red-300 text-sm flex items-center justify-between" role="alert">
              <span>{suggestError}</span>
              <button
                type="button"
                onClick={handleRefresh}
                className="ml-3 text-red-400 hover:text-red-300 transition-colors"
                aria-label={t('release.refresh')}
              >
                <RefreshCw size={14} />
              </button>
            </div>
          ) : suggestion ? (
            <div className="flex flex-col gap-4">
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
                  onClick={handleRefresh}
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
                  <label htmlFor="release-modal-version" className="text-xs text-gray-500">
                    {t('release.versionLabel')}
                  </label>
                  <input
                    id="release-modal-version"
                    type="text"
                    value={version}
                    onChange={e => setVersion(e.target.value)}
                    disabled={releasing}
                    className="w-36 px-3 py-1.5 rounded-lg text-sm font-mono bg-gray-800 border border-gray-600
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

                  {releaseResult.success && releaseResult.actions_url && (
                    <a
                      href={releaseResult.actions_url}
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
        </DialogBody>
      </Dialog>

      <ConfirmDialog
        open={confirmOpen}
        title={t('release.confirmTitle')}
        message={t('release.confirmMessage', { version: `v${version}` })}
        confirmLabel={t('release.releaseButton')}
        onConfirm={() => void handleRelease()}
        onCancel={() => setConfirmOpen(false)}
      />
    </>
  )
}
