import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

export interface FragmentSummary {
  file: string
  category: string
  summary: string
}

export interface SuggestResponse {
  current_version: string
  suggested_version: string
  suggested_bump: string
  changelog_preview: FragmentSummary[]
}

export interface StepResult {
  step: string
  command: string
  output?: string
  success: boolean
  error?: string
}

export interface ReleaseResponse {
  version: string
  tag: string
  success: boolean
  steps: StepResult[]
  actions_url?: string
}

export const CATEGORY_COLORS: Record<string, string> = {
  Added: 'text-green-400',
  Changed: 'text-blue-400',
  Fixed: 'text-amber-400',
  Removed: 'text-red-400',
  Deprecated: 'text-gray-400',
  Security: 'text-purple-400',
}

export function useReleaseFlow(showToast: (message: string, type: 'success' | 'error') => void) {
  const { t } = useTranslation('forge')

  const [suggestion, setSuggestion] = useState<SuggestResponse | null>(null)
  const [suggestLoading, setSuggestLoading] = useState(false)
  const [suggestError, setSuggestError] = useState<string | null>(null)

  const [version, setVersion] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [releasing, setReleasing] = useState(false)
  const [releaseResult, setReleaseResult] = useState<ReleaseResponse | null>(null)

  const abortRef = useRef<AbortController | null>(null)

  // Abort in-flight request on unmount
  useEffect(() => {
    return () => {
      abortRef.current?.abort()
    }
  }, [])

  async function fetchSuggestion(controller: AbortController) {
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
      if (abortRef.current === controller) {
        abortRef.current = null
      }
      if (!controller.signal.aborted) {
        setSuggestLoading(false)
      }
    }
  }

  function startFetch(): () => void {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    void fetchSuggestion(controller)
    return () => controller.abort()
  }

  function handleRefresh() {
    startFetch()
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

  return {
    suggestion,
    suggestLoading,
    suggestError,
    version,
    setVersion,
    confirmOpen,
    setConfirmOpen,
    releasing,
    releaseResult,
    startFetch,
    handleRefresh,
    handleRelease,
  }
}
