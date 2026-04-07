import { useState } from 'react'
import { useTranslation } from 'react-i18next'

export type BeadActionType = 'retry' | 'approve' | 'dismiss' | 'forceSmith'

const actionPath: Record<BeadActionType, string> = {
  retry: 'retry',
  approve: 'approve',
  dismiss: 'dismiss',
  forceSmith: 'force-smith',
}

interface UseBeadActionsOptions {
  showToast: (message: string, type: 'success' | 'error') => void
  onRetried?: (beadId: string) => void
}

/**
 * Shared hook for performing bead actions (retry, approve, dismiss, forceSmith).
 * Handles fetch, error parsing, toast notifications, and per-bead loading state.
 */
export function useBeadActions({ showToast, onRetried }: UseBeadActionsOptions) {
  const { t } = useTranslation('forge')
  const [acting, setActing] = useState<Record<string, boolean>>({})

  async function handleAction(type: BeadActionType, beadId: string) {
    setActing(prev => ({ ...prev, [beadId]: true }))
    try {
      const res = await fetch(
        `/api/forge/beads/${encodeURIComponent(beadId)}/${actionPath[type]}`,
        { method: 'POST', credentials: 'include' }
      )
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t(`attention.${type}Success`, { id: beadId }), 'success')
        if (type === 'retry') onRetried?.(beadId)
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setActing(prev => ({ ...prev, [beadId]: false }))
    }
  }

  return { acting, handleAction }
}
