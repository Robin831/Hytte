import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  GitPullRequest,
  ExternalLink,
  ChevronDown,
  ChevronRight,
  CheckCircle,
  XCircle,
  Clock,
  AlertTriangle,
  MessageSquare,
  GitMerge,
  Bell,
  ShieldCheck,
  Wrench,
  RotateCcw,
} from 'lucide-react'
import type { TFunction } from 'i18next'
import { Dialog, DialogHeader, DialogBody } from '../ui/dialog'
import ConfirmDialog from '../ConfirmDialog'
import { useAllPRs } from '../../hooks/useAllPRs'
import type { ExternalPR } from '../../hooks/useAllPRs'
import type { OpenPR } from '../../hooks/useForgeStatus'

interface PRModalProps {
  open: boolean
  onClose: () => void
  showToast?: (message: string, type: 'success' | 'error') => void
  onBeadClick?: (beadId: string) => void
}

type ForgeAction = 'merge' | 'bellows' | 'approve' | 'fixComments' | 'fixCI' | 'fixConflicts' | 'resetCounters' | 'close'
type ExternalAction = 'extApprove' | 'extMerge' | 'extFixComments' | 'extFixCI' | 'extFixConflicts' | 'extBellows' | 'extResetCounters'

interface PendingForgeAction {
  type: ForgeAction
  pr: OpenPR
}

interface PendingExternalAction {
  type: ExternalAction
  pr: ExternalPR
}

function githubUrl(anvil: string, number: number): string | null {
  return anvil.includes('/') ? `https://github.com/${anvil}/pull/${number}` : null
}

function CIBadge({ pr, t }: { pr: OpenPR; t: TFunction<'forge'> }) {
  if (pr.ci_passing) {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-green-500/15 text-green-400 border border-green-500/25">
        <CheckCircle size={12} />
        <span className="hidden sm:inline">{t('prModal.ciPass')}</span>
      </span>
    )
  }
  if (pr.ci_pending) {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-yellow-500/15 text-yellow-400 border border-yellow-500/25">
        <Clock size={12} />
        <span className="hidden sm:inline">{t('prModal.ciPending')}</span>
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-red-500/15 text-red-400 border border-red-500/25">
      <XCircle size={12} />
      <span className="hidden sm:inline">{t('prModal.ciFail')}</span>
    </span>
  )
}

function ReviewBadge({ pr, t }: { pr: OpenPR; t: TFunction<'forge'> }) {
  if (pr.has_approval) {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-green-500/15 text-green-400 border border-green-500/25">
        <CheckCircle size={12} />
        <span className="hidden sm:inline">{t('prModal.reviewApproved')}</span>
      </span>
    )
  }
  if (pr.changes_requested) {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-red-500/15 text-red-400 border border-red-500/25">
        <XCircle size={12} />
        <span className="hidden sm:inline">{t('prModal.reviewChanges')}</span>
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-gray-500/15 text-gray-400 border border-gray-500/25">
      <Clock size={12} />
      <span className="hidden sm:inline">{t('prModal.reviewPending')}</span>
    </span>
  )
}

export default function PRModal({ open, onClose, showToast, onBeadClick }: PRModalProps) {
  const { t } = useTranslation('forge')
  const titleId = useId()
  const { data, loading, error, refetch } = useAllPRs(open)
  const [acting, setActing] = useState<Partial<Record<string, boolean>>>({})
  const [confirmAction, setConfirmAction] = useState<PendingForgeAction | null>(null)
  const [confirmExtAction, setConfirmExtAction] = useState<PendingExternalAction | null>(null)
  const [collapsedAnvils, setCollapsedAnvils] = useState<Record<string, boolean>>({})

  function handleClose() {
    setConfirmAction(null)
    setConfirmExtAction(null)
    onClose()
  }

  const anvilGroups = useMemo(() => {
    if (!data) return []
    const groups = new Map<string, { forge: OpenPR[]; external: ExternalPR[] }>()

    for (const pr of data.forge_prs) {
      if (!groups.has(pr.anvil)) groups.set(pr.anvil, { forge: [], external: [] })
      groups.get(pr.anvil)!.forge.push(pr)
    }
    for (const pr of data.external_prs) {
      if (!groups.has(pr.anvil)) groups.set(pr.anvil, { forge: [], external: [] })
      groups.get(pr.anvil)!.external.push(pr)
    }

    return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b))
  }, [data])

  const totalCount = data ? data.forge_prs.length + data.external_prs.length : 0

  function toggleAnvil(anvil: string) {
    setCollapsedAnvils(prev => ({ ...prev, [anvil]: !prev[anvil] }))
  }

  function handleBeadClick(beadId: string) {
    onClose()
    onBeadClick?.(beadId)
  }

  async function handleForgeAction(action: PendingForgeAction) {
    setConfirmAction(null)
    const key = `${action.type}-${action.pr.id}`
    setActing(prev => ({ ...prev, [key]: true }))
    try {
      const urlMap: Record<ForgeAction, string> = {
        merge: `/api/forge/prs/${action.pr.id}/merge`,
        bellows: `/api/forge/prs/${action.pr.id}/bellows`,
        approve: `/api/forge/prs/${action.pr.id}/approve`,
        fixComments: `/api/forge/prs/${action.pr.id}/fix-comments`,
        fixCI: `/api/forge/prs/${action.pr.id}/fix-ci`,
        fixConflicts: `/api/forge/prs/${action.pr.id}/fix-conflicts`,
        resetCounters: `/api/forge/prs/${action.pr.id}/reset-counters`,
        close: `/api/forge/prs/${action.pr.id}/close`,
      }
      const res = await fetch(urlMap[action.type], { method: 'POST', credentials: 'include' })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        showToast?.((body as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        const successKey = `readyToMerge.${action.type}Success` as const
        showToast?.(t(successKey, { number: action.pr.number }), 'success')
        refetch()
      }
    } catch (err) {
      showToast?.(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setActing(prev => ({ ...prev, [key]: false }))
    }
  }

  async function handleExtAction(action: PendingExternalAction) {
    setConfirmExtAction(null)
    const key = `${action.type}-${action.pr.anvil}-${action.pr.number}`
    setActing(prev => ({ ...prev, [key]: true }))
    try {
      const endpointMap: Record<ExternalAction, string> = {
        extApprove: '/api/forge/ext-prs/approve',
        extMerge: '/api/forge/ext-prs/merge',
        extFixComments: '/api/forge/ext-prs/fix-comments',
        extFixCI: '/api/forge/ext-prs/fix-ci',
        extFixConflicts: '/api/forge/ext-prs/fix-conflicts',
        extBellows: '/api/forge/ext-prs/bellows',
        extResetCounters: '/api/forge/ext-prs/reset-counters',
      }
      const res = await fetch(endpointMap[action.type], {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repo: action.pr.anvil, number: action.pr.number }),
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        showToast?.((body as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        const successKeyMap: Record<ExternalAction, string> = {
          extApprove: 'readyToMerge.extApproveSuccess',
          extMerge: 'readyToMerge.extMergeSuccess',
          extFixComments: 'readyToMerge.extFixCommentsSuccess',
          extFixCI: 'readyToMerge.extFixCISuccess',
          extFixConflicts: 'readyToMerge.extFixConflictsSuccess',
          extBellows: 'readyToMerge.extBellowsSuccess',
          extResetCounters: 'readyToMerge.extResetCountersSuccess',
        }
        showToast?.(t(successKeyMap[action.type] as never, { number: action.pr.number }), 'success')
        refetch()
      }
    } catch (err) {
      showToast?.(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setActing(prev => ({ ...prev, [key]: false }))
    }
  }

  function forgeConfirmTitle(type: ForgeAction): string {
    const keyMap: Record<ForgeAction, string> = {
      merge: 'readyToMerge.mergeConfirmTitle',
      bellows: 'readyToMerge.bellowsConfirmTitle',
      approve: 'readyToMerge.approveConfirmTitle',
      fixComments: 'readyToMerge.fixCommentsConfirmTitle',
      fixCI: 'readyToMerge.fixCIConfirmTitle',
      fixConflicts: 'readyToMerge.fixConflictsConfirmTitle',
      resetCounters: 'readyToMerge.resetCountersConfirmTitle',
      close: 'readyToMerge.closeConfirmTitle',
    }
    return t(keyMap[type] as never)
  }

  function forgeConfirmMessage(type: ForgeAction, pr: OpenPR): string {
    const keyMap: Record<ForgeAction, string> = {
      merge: 'readyToMerge.mergeConfirmMessage',
      bellows: 'readyToMerge.bellowsConfirmMessage',
      approve: 'readyToMerge.approveConfirmMessage',
      fixComments: 'readyToMerge.fixCommentsConfirmMessage',
      fixCI: 'readyToMerge.fixCIConfirmMessage',
      fixConflicts: 'readyToMerge.fixConflictsConfirmMessage',
      resetCounters: 'readyToMerge.resetCountersConfirmMessage',
      close: 'readyToMerge.closeConfirmMessage',
    }
    return t(keyMap[type] as never, { number: pr.number })
  }

  function forgeConfirmLabel(type: ForgeAction): string {
    const keyMap: Record<ForgeAction, string> = {
      merge: 'readyToMerge.merge',
      bellows: 'readyToMerge.bellows',
      approve: 'readyToMerge.approve',
      fixComments: 'readyToMerge.fixComments',
      fixCI: 'readyToMerge.fixCI',
      fixConflicts: 'readyToMerge.fixConflicts',
      resetCounters: 'readyToMerge.resetCounters',
      close: 'readyToMerge.close',
    }
    return t(keyMap[type] as never)
  }

  function extConfirmTitle(type: ExternalAction): string {
    const keyMap: Record<ExternalAction, string> = {
      extApprove: 'readyToMerge.extApproveConfirmTitle',
      extMerge: 'readyToMerge.extMergeConfirmTitle',
      extFixComments: 'readyToMerge.extFixCommentsConfirmTitle',
      extFixCI: 'readyToMerge.extFixCIConfirmTitle',
      extFixConflicts: 'readyToMerge.extFixConflictsConfirmTitle',
      extBellows: 'readyToMerge.extBellowsConfirmTitle',
      extResetCounters: 'readyToMerge.extResetCountersConfirmTitle',
    }
    return t(keyMap[type] as never)
  }

  function extConfirmMessage(type: ExternalAction, pr: ExternalPR): string {
    const keyMap: Record<ExternalAction, string> = {
      extApprove: 'readyToMerge.extApproveConfirmMessage',
      extMerge: 'readyToMerge.extMergeConfirmMessage',
      extFixComments: 'readyToMerge.extFixCommentsConfirmMessage',
      extFixCI: 'readyToMerge.extFixCIConfirmMessage',
      extFixConflicts: 'readyToMerge.extFixConflictsConfirmMessage',
      extBellows: 'readyToMerge.extBellowsConfirmMessage',
      extResetCounters: 'readyToMerge.extResetCountersConfirmMessage',
    }
    return t(keyMap[type] as never, { number: pr.number })
  }

  function extConfirmLabel(type: ExternalAction): string {
    const keyMap: Record<ExternalAction, string> = {
      extApprove: 'readyToMerge.approve',
      extMerge: 'readyToMerge.merge',
      extFixComments: 'readyToMerge.fixComments',
      extFixCI: 'readyToMerge.fixCI',
      extFixConflicts: 'readyToMerge.fixConflicts',
      extBellows: 'readyToMerge.bellows',
      extResetCounters: 'readyToMerge.resetCounters',
    }
    return t(keyMap[type] as never)
  }

  function isMergeReady(pr: OpenPR): boolean {
    return pr.ci_passing && pr.has_approval && !pr.is_conflicting && !pr.has_unresolved_threads
  }

  function renderForgePR(pr: OpenPR) {
    const ready = isMergeReady(pr)
    const url = githubUrl(pr.anvil, pr.number)

    return (
      <div key={`forge-${pr.id}`} className={`px-4 py-3 flex flex-col gap-2 ${ready ? 'bg-green-900/10' : ''}`}>
        <div className="flex items-start justify-between gap-2">
          <div className="flex flex-col gap-1 min-w-0">
            <div className="flex items-center gap-2 min-w-0">
              <span className="text-xs text-gray-500 shrink-0">#{pr.number}</span>
              <span className="text-sm text-white truncate">{pr.title}</span>
            </div>
            <div className="flex items-center gap-2 flex-wrap">
              {pr.bead_id && (
                <button
                  type="button"
                  onClick={() => handleBeadClick(pr.bead_id)}
                  className="text-xs font-mono text-cyan-400 hover:text-cyan-300 hover:underline transition-colors"
                >
                  {pr.bead_id}
                </button>
              )}
              <span className="text-xs text-gray-600 truncate max-w-[150px] sm:max-w-none">{pr.branch}</span>
            </div>
          </div>
          {url && (
            <a
              href={url}
              target="_blank"
              rel="noopener noreferrer"
              aria-label={t('readyToMerge.viewOnGitHub')}
              className="text-gray-400 hover:text-white transition-colors shrink-0"
            >
              <ExternalLink size={14} />
            </a>
          )}
        </div>

        <div className="flex items-center gap-1.5 flex-wrap">
          <CIBadge pr={pr} t={t} />
          <ReviewBadge pr={pr} t={t} />
          {pr.is_conflicting && (
            <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-400 border border-amber-500/25">
              <AlertTriangle size={12} />
              <span className="hidden sm:inline">{t('prModal.conflict')}</span>
            </span>
          )}
          {pr.has_unresolved_threads && (
            <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-cyan-500/15 text-cyan-400 border border-cyan-500/25">
              <MessageSquare size={12} />
              <span className="hidden sm:inline">{t('prModal.unresolvedThreads')}</span>
            </span>
          )}
          {pr.bellows_managed && (
            <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-indigo-500/15 text-indigo-400 border border-indigo-500/25">
              <Bell size={12} />
              <span className="hidden sm:inline">{t('readyToMerge.bellowsActive')}</span>
            </span>
          )}
        </div>

        <div className="flex items-center gap-1.5 flex-wrap">
          {ready && (
            <button
              type="button"
              onClick={() => setConfirmAction({ type: 'merge', pr })}
              disabled={!!acting[`merge-${pr.id}`]}
              aria-label={t('readyToMerge.mergeLabel', { number: pr.number })}
              className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
                bg-green-600/20 text-green-300 border border-green-600/30
                hover:bg-green-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <GitMerge size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.merge')}</span>
            </button>
          )}
          {!pr.bellows_managed && (
            <button
              type="button"
              onClick={() => setConfirmAction({ type: 'bellows', pr })}
              disabled={!!acting[`bellows-${pr.id}`]}
              aria-label={t('readyToMerge.bellowsLabel', { number: pr.number })}
              className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
                bg-indigo-600/20 text-indigo-300 border border-indigo-600/30
                hover:bg-indigo-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Bell size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.bellows')}</span>
            </button>
          )}
          {!pr.ci_passing && !pr.ci_pending && (
            <button
              type="button"
              onClick={() => setConfirmAction({ type: 'fixCI', pr })}
              disabled={!!acting[`fixCI-${pr.id}`]}
              aria-label={t('readyToMerge.fixCILabel', { number: pr.number })}
              className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
                bg-red-600/20 text-red-300 border border-red-600/30
                hover:bg-red-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Wrench size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.fixCI')}</span>
            </button>
          )}
          {pr.is_conflicting && (
            <button
              type="button"
              onClick={() => setConfirmAction({ type: 'fixConflicts', pr })}
              disabled={!!acting[`fixConflicts-${pr.id}`]}
              aria-label={t('readyToMerge.fixConflictsLabel', { number: pr.number })}
              className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
                bg-amber-600/20 text-amber-300 border border-amber-600/30
                hover:bg-amber-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <RotateCcw size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.fixConflicts')}</span>
            </button>
          )}
          {pr.has_unresolved_threads && (
            <button
              type="button"
              onClick={() => setConfirmAction({ type: 'fixComments', pr })}
              disabled={!!acting[`fixComments-${pr.id}`]}
              aria-label={t('readyToMerge.fixCommentsLabel', { number: pr.number })}
              className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
                bg-cyan-600/20 text-cyan-300 border border-cyan-600/30
                hover:bg-cyan-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <MessageSquare size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.fixComments')}</span>
            </button>
          )}
          {(pr.ci_fix_count > 0 || pr.review_fix_count > 0) && (
            <button
              type="button"
              onClick={() => setConfirmAction({ type: 'resetCounters', pr })}
              disabled={!!acting[`resetCounters-${pr.id}`]}
              aria-label={t('readyToMerge.resetCountersLabel', { number: pr.number })}
              className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
                bg-orange-600/20 text-orange-300 border border-orange-600/30
                hover:bg-orange-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <RotateCcw size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.resetCounters')}</span>
            </button>
          )}
          <button
            type="button"
            onClick={() => setConfirmAction({ type: 'approve', pr })}
            disabled={!!acting[`approve-${pr.id}`]}
            aria-label={t('readyToMerge.approveLabel', { number: pr.number })}
            className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
              bg-purple-600/20 text-purple-300 border border-purple-600/30
              hover:bg-purple-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <ShieldCheck size={13} />
            <span className="hidden sm:inline">{t('readyToMerge.approve')}</span>
          </button>
          <button
            type="button"
            onClick={() => setConfirmAction({ type: 'close', pr })}
            disabled={!!acting[`close-${pr.id}`]}
            aria-label={t('readyToMerge.closeLabel', { number: pr.number })}
            className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
              bg-red-600/20 text-red-300 border border-red-600/30
              hover:bg-red-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <XCircle size={13} />
            <span className="hidden sm:inline">{t('readyToMerge.close')}</span>
          </button>
        </div>
      </div>
    )
  }

  function renderExternalPR(pr: ExternalPR) {
    const url = pr.url
    return (
      <div key={`ext-${pr.anvil}-${pr.number}`} className="px-4 py-3 flex flex-col gap-2">
        <div className="flex items-start justify-between gap-2">
          <div className="flex flex-col gap-1 min-w-0">
            <div className="flex items-center gap-2 min-w-0">
              <span className="text-xs text-gray-500 shrink-0">#{pr.number}</span>
              <span className="text-sm text-white truncate">{pr.title}</span>
            </div>
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-xs text-gray-500">{pr.author}</span>
              <span className="text-xs text-gray-600 truncate max-w-[150px] sm:max-w-none">{pr.branch}</span>
              {pr.is_draft && (
                <span className="inline-flex items-center text-xs px-1.5 py-0.5 rounded bg-gray-500/15 text-gray-400 border border-gray-500/25">
                  {t('readyToMerge.draft')}
                </span>
              )}
            </div>
          </div>
          {url && (
            <a
              href={url}
              target="_blank"
              rel="noopener noreferrer"
              aria-label={t('readyToMerge.viewOnGitHub')}
              className="text-gray-400 hover:text-white transition-colors shrink-0"
            >
              <ExternalLink size={14} />
            </a>
          )}
        </div>

        <div className="flex items-center gap-1.5 flex-wrap">
          <button
            type="button"
            onClick={() => setConfirmExtAction({ type: 'extApprove', pr })}
            disabled={!!acting[`extApprove-${pr.anvil}-${pr.number}`]}
            aria-label={t('readyToMerge.extApproveLabel', { number: pr.number })}
            className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
              bg-purple-600/20 text-purple-300 border border-purple-600/30
              hover:bg-purple-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <ShieldCheck size={13} />
            <span className="hidden sm:inline">{t('readyToMerge.approve')}</span>
          </button>
          <button
            type="button"
            onClick={() => setConfirmExtAction({ type: 'extMerge', pr })}
            disabled={!!acting[`extMerge-${pr.anvil}-${pr.number}`]}
            aria-label={t('readyToMerge.extMergeLabel', { number: pr.number })}
            className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
              bg-green-600/20 text-green-300 border border-green-600/30
              hover:bg-green-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <GitMerge size={13} />
            <span className="hidden sm:inline">{t('readyToMerge.merge')}</span>
          </button>
          <button
            type="button"
            onClick={() => setConfirmExtAction({ type: 'extBellows', pr })}
            disabled={!!acting[`extBellows-${pr.anvil}-${pr.number}`]}
            aria-label={t('readyToMerge.bellowsLabel', { number: pr.number })}
            className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs font-medium transition-colors
              bg-indigo-600/20 text-indigo-300 border border-indigo-600/30
              hover:bg-indigo-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Bell size={13} />
            <span className="hidden sm:inline">{t('readyToMerge.bellows')}</span>
          </button>
        </div>
      </div>
    )
  }

  const hasConfirmDialog = confirmAction !== null || confirmExtAction !== null

  return (
    <>
      <Dialog
        open={open && !hasConfirmDialog}
        onClose={handleClose}
        maxWidth="max-w-2xl"
        aria-labelledby={titleId}
      >
        <DialogHeader
          id={titleId}
          title={totalCount > 0 ? `${t('prModal.title')} (${totalCount})` : t('prModal.title')}
          onClose={handleClose}
        />
        <DialogBody className="p-0">
          {loading && (
            <p className="px-6 py-8 text-sm text-gray-500 text-center">{t('prModal.loading')}</p>
          )}
          {error && (
            <div className="flex flex-col items-center justify-center py-8 gap-3">
              <p className="text-sm text-red-400">{t('prModal.error')}</p>
              <button
                type="button"
                onClick={refetch}
                className="text-xs text-gray-400 hover:text-white transition-colors underline"
              >
                {t('prModal.retry')}
              </button>
            </div>
          )}
          {!loading && !error && totalCount === 0 && (
            <div className="flex flex-col items-center justify-center py-12 text-gray-400">
              <GitPullRequest size={40} className="mb-4 text-gray-600" />
              <p className="text-sm">{t('prModal.empty')}</p>
            </div>
          )}
          {!loading && !error && totalCount > 0 && (
            <div className="divide-y divide-gray-700/40">
              {anvilGroups.map(([anvil, group]) => {
                const anvilCount = group.forge.length + group.external.length
                const collapsed = !!collapsedAnvils[anvil]
                const shortName = anvil.includes('/') ? anvil.split('/')[1] : anvil

                const anvilContentId = `anvil-content-${anvil.replace(/[^a-z0-9]/gi, '-')}`

                return (
                  <div key={anvil}>
                    {anvilGroups.length > 1 && (
                      <button
                        type="button"
                        aria-expanded={!collapsed}
                        aria-controls={anvilContentId}
                        onClick={() => toggleAnvil(anvil)}
                        className="w-full flex items-center gap-2 px-4 py-2.5 text-left hover:bg-gray-800/50 transition-colors"
                      >
                        {collapsed ? <ChevronRight size={14} className="text-gray-500" /> : <ChevronDown size={14} className="text-gray-500" />}
                        <span className="text-sm font-medium text-gray-300">{shortName}</span>
                        <span className="text-xs text-gray-500">({anvilCount})</span>
                      </button>
                    )}

                    {!collapsed && (
                      <div id={anvilContentId} className="divide-y divide-gray-800/50">
                        {group.forge.length > 0 && group.external.length > 0 && (
                          <p className="px-4 py-1.5 text-xs font-medium text-gray-500 uppercase tracking-wider bg-gray-800/30">
                            {t('readyToMerge.forgeSection')}
                          </p>
                        )}
                        {group.forge.map(pr => renderForgePR(pr))}
                        {group.forge.length > 0 && group.external.length > 0 && (
                          <p className="px-4 py-1.5 text-xs font-medium text-gray-500 uppercase tracking-wider bg-gray-800/30">
                            {t('readyToMerge.externalSection')}
                          </p>
                        )}
                        {group.external.map(pr => renderExternalPR(pr))}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </DialogBody>
      </Dialog>

      <ConfirmDialog
        open={open && confirmAction !== null}
        title={confirmAction ? forgeConfirmTitle(confirmAction.type) : ''}
        message={confirmAction ? forgeConfirmMessage(confirmAction.type, confirmAction.pr) : ''}
        confirmLabel={confirmAction ? forgeConfirmLabel(confirmAction.type) : ''}
        destructive={confirmAction?.type === 'close'}
        onConfirm={() => {
          if (confirmAction) void handleForgeAction(confirmAction)
        }}
        onCancel={() => setConfirmAction(null)}
      />

      <ConfirmDialog
        open={open && confirmExtAction !== null}
        title={confirmExtAction ? extConfirmTitle(confirmExtAction.type) : ''}
        message={confirmExtAction ? extConfirmMessage(confirmExtAction.type, confirmExtAction.pr) : ''}
        confirmLabel={confirmExtAction ? extConfirmLabel(confirmExtAction.type) : ''}
        destructive={false}
        onConfirm={() => {
          if (confirmExtAction) void handleExtAction(confirmExtAction)
        }}
        onCancel={() => setConfirmExtAction(null)}
      />
    </>
  )
}
