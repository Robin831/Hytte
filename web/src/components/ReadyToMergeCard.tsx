import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { GitMerge, Bell, ShieldCheck, ExternalLink, MessageSquare, ChevronDown, ChevronRight, RotateCcw, Wrench } from 'lucide-react'
import type { OpenPR } from '../hooks/useForgeStatus'
import type { ExternalPR } from '../hooks/useAllPRs'
import ConfirmDialog from './ConfirmDialog'
import { CollapsiblePanelHeader } from './CollapsiblePanelHeader'
import { usePanelCollapse } from '../hooks/usePanelCollapse'

interface ReadyToMergeCardProps {
  forgePRs: OpenPR[]
  externalPRs: ExternalPR[]
  onMerged?: (pr: { repo: string; number: number }) => void
  showToast: (message: string, type: 'success' | 'error') => void
  onBeadClick?: (beadId: string) => void
}

function isMergeReady(pr: OpenPR): boolean {
  return pr.ci_passing && pr.has_approval && !pr.is_conflicting && !pr.has_unresolved_threads
}

type ForgeAction = 'merge' | 'bellows' | 'approve' | 'fixComments' | 'fixCI' | 'fixConflicts' | 'resetCounters'
type ExternalAction = 'extApprove' | 'extMerge' | 'extFixComments' | 'extFixCI' | 'extFixConflicts' | 'extBellows' | 'extResetCounters'

interface PendingForgeAction {
  type: ForgeAction
  pr: OpenPR
}

interface PendingExternalAction {
  type: ExternalAction
  pr: ExternalPR
}

export default function ReadyToMergeCard({ forgePRs, externalPRs, onMerged, showToast, onBeadClick }: ReadyToMergeCardProps) {
  const { t } = useTranslation('forge')
  const [acting, setActing] = useState<Partial<Record<string, boolean>>>({})
  const [confirmAction, setConfirmAction] = useState<PendingForgeAction | null>(null)
  const [confirmExtAction, setConfirmExtAction] = useState<PendingExternalAction | null>(null)
  const [isOpen, toggle] = usePanelCollapse('prs')
  const [collapsedAnvils, setCollapsedAnvils] = useState<Record<string, boolean>>({})

  // Group by anvil
  const anvilGroups = useMemo(() => {
    const groups = new Map<string, { forge: OpenPR[]; external: ExternalPR[] }>()

    for (const pr of forgePRs) {
      const anvil = pr.anvil
      if (!groups.has(anvil)) groups.set(anvil, { forge: [], external: [] })
      groups.get(anvil)!.forge.push(pr)
    }

    for (const pr of externalPRs) {
      const anvil = pr.anvil
      if (!groups.has(anvil)) groups.set(anvil, { forge: [], external: [] })
      groups.get(anvil)!.external.push(pr)
    }

    // Sort anvils alphabetically for stable order
    return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b))
  }, [forgePRs, externalPRs])

  const totalCount = forgePRs.length + externalPRs.length
  const mergeReadyCount = forgePRs.filter(isMergeReady).length

  function toggleAnvil(anvil: string) {
    setCollapsedAnvils(prev => ({ ...prev, [anvil]: !prev[anvil] }))
  }

  async function handleAction(action: PendingForgeAction) {
    setConfirmAction(null)
    const key = `${action.type}-${action.pr.id}`
    setActing(prev => ({ ...prev, [key]: true }))
    try {
      let url: string
      switch (action.type) {
        case 'merge':
          url = `/api/forge/prs/${action.pr.id}/merge`
          break
        case 'bellows':
          url = `/api/forge/prs/${action.pr.id}/bellows`
          break
        case 'approve':
          url = `/api/forge/prs/${action.pr.id}/approve`
          break
        case 'fixComments':
          url = `/api/forge/prs/${action.pr.id}/fix-comments`
          break
        case 'fixCI':
          url = `/api/forge/prs/${action.pr.id}/fix-ci`
          break
        case 'fixConflicts':
          url = `/api/forge/prs/${action.pr.id}/fix-conflicts`
          break
        case 'resetCounters':
          url = `/api/forge/prs/${action.pr.id}/reset-counters`
          break
        default: {
          const _exhaustive: never = action.type
          throw new Error(`Unknown action: ${_exhaustive}`)
        }
      }
      const res = await fetch(url, { method: 'POST', credentials: 'include' })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        const successKey = `readyToMerge.${action.type}Success` as const
        showToast(t(successKey, { number: action.pr.number }), 'success')
        if (action.type === 'merge') onMerged?.({ repo: action.pr.anvil, number: action.pr.number })
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
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
      const endpoint = endpointMap[action.type]
      const res = await fetch(endpoint, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repo: action.pr.anvil, number: action.pr.number }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
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
        showToast(t(successKeyMap[action.type], { number: action.pr.number }), 'success')
        if (action.type === 'extMerge') onMerged?.({ repo: action.pr.anvil, number: action.pr.number })
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setActing(prev => ({ ...prev, [key]: false }))
    }
  }

  function confirmTitle(type: ForgeAction): string {
    switch (type) {
      case 'merge': return t('readyToMerge.mergeConfirmTitle')
      case 'bellows': return t('readyToMerge.bellowsConfirmTitle')
      case 'approve': return t('readyToMerge.approveConfirmTitle')
      case 'fixComments': return t('readyToMerge.fixCommentsConfirmTitle')
      case 'fixCI': return t('readyToMerge.fixCIConfirmTitle')
      case 'fixConflicts': return t('readyToMerge.fixConflictsConfirmTitle')
      case 'resetCounters': return t('readyToMerge.resetCountersConfirmTitle')
      default: { const _exhaustive: never = type; return _exhaustive }
    }
  }

  function confirmMessage(type: ForgeAction, pr: OpenPR): string {
    switch (type) {
      case 'merge': return t('readyToMerge.mergeConfirmMessage', { number: pr.number })
      case 'bellows': return t('readyToMerge.bellowsConfirmMessage', { number: pr.number })
      case 'approve': return t('readyToMerge.approveConfirmMessage', { number: pr.number })
      case 'fixComments': return t('readyToMerge.fixCommentsConfirmMessage', { number: pr.number })
      case 'fixCI': return t('readyToMerge.fixCIConfirmMessage', { number: pr.number })
      case 'fixConflicts': return t('readyToMerge.fixConflictsConfirmMessage', { number: pr.number })
      case 'resetCounters': return t('readyToMerge.resetCountersConfirmMessage', { number: pr.number })
      default: { const _exhaustive: never = type; return _exhaustive }
    }
  }

  function confirmLabel(type: ForgeAction): string {
    switch (type) {
      case 'merge': return t('readyToMerge.merge')
      case 'bellows': return t('readyToMerge.bellows')
      case 'approve': return t('readyToMerge.approve')
      case 'fixComments': return t('readyToMerge.fixComments')
      case 'fixCI': return t('readyToMerge.fixCI')
      case 'fixConflicts': return t('readyToMerge.fixConflicts')
      case 'resetCounters': return t('readyToMerge.resetCounters')
      default: { const _exhaustive: never = type; return _exhaustive }
    }
  }

  function extConfirmTitle(type: ExternalAction): string {
    switch (type) {
      case 'extApprove': return t('readyToMerge.extApproveConfirmTitle')
      case 'extMerge': return t('readyToMerge.extMergeConfirmTitle')
      case 'extFixComments': return t('readyToMerge.extFixCommentsConfirmTitle')
      case 'extFixCI': return t('readyToMerge.extFixCIConfirmTitle')
      case 'extFixConflicts': return t('readyToMerge.extFixConflictsConfirmTitle')
      case 'extBellows': return t('readyToMerge.extBellowsConfirmTitle')
      case 'extResetCounters': return t('readyToMerge.extResetCountersConfirmTitle')
      default: { const _exhaustive: never = type; return _exhaustive }
    }
  }

  function extConfirmMessage(type: ExternalAction, pr: ExternalPR): string {
    switch (type) {
      case 'extApprove': return t('readyToMerge.extApproveConfirmMessage', { number: pr.number })
      case 'extMerge': return t('readyToMerge.extMergeConfirmMessage', { number: pr.number })
      case 'extFixComments': return t('readyToMerge.extFixCommentsConfirmMessage', { number: pr.number })
      case 'extFixCI': return t('readyToMerge.extFixCIConfirmMessage', { number: pr.number })
      case 'extFixConflicts': return t('readyToMerge.extFixConflictsConfirmMessage', { number: pr.number })
      case 'extBellows': return t('readyToMerge.extBellowsConfirmMessage', { number: pr.number })
      case 'extResetCounters': return t('readyToMerge.extResetCountersConfirmMessage', { number: pr.number })
      default: { const _exhaustive: never = type; return _exhaustive }
    }
  }

  function extConfirmLabel(type: ExternalAction): string {
    switch (type) {
      case 'extApprove': return t('readyToMerge.approve')
      case 'extMerge': return t('readyToMerge.merge')
      case 'extFixComments': return t('readyToMerge.fixComments')
      case 'extFixCI': return t('readyToMerge.fixCI')
      case 'extFixConflicts': return t('readyToMerge.fixConflicts')
      case 'extBellows': return t('readyToMerge.bellows')
      case 'extResetCounters': return t('readyToMerge.resetCounters')
      default: { const _exhaustive: never = type; return _exhaustive }
    }
  }

  function githubUrl(anvil: string, number: number): string | null {
    return anvil.includes('/') ? `https://github.com/${anvil}/pull/${number}` : null
  }

  function renderForgePR(pr: OpenPR) {
    const ready = isMergeReady(pr)
    const url = githubUrl(pr.anvil, pr.number)
    return (
      <div key={`forge-${pr.id}`} className={`px-5 py-3 flex flex-col gap-2 ${ready ? 'bg-green-900/10' : ''}`}>
        <div className="flex items-start justify-between gap-3">
          <div className="flex flex-col gap-1 min-w-0">
            <span className="text-sm text-white truncate">{pr.title}</span>
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-xs text-gray-500">#{pr.number}</span>
              {pr.bead_id && (
                <button
                  type="button"
                  onClick={() => onBeadClick?.(pr.bead_id)}
                  className="text-xs font-mono text-cyan-400 hover:text-cyan-300 hover:underline transition-colors"
                >
                  {pr.bead_id}
                </button>
              )}
              {pr.ci_passing ? (
                <span className="text-xs text-green-500">CI &#10003;</span>
              ) : (
                <span className="text-xs text-red-500">CI &#10007;</span>
              )}
              {pr.has_approval ? (
                <span className="text-xs text-green-500">{t('readyToMerge.approved')}</span>
              ) : (
                <span className="text-xs text-gray-500">{t('readyToMerge.pendingReview')}</span>
              )}
              {pr.is_conflicting && (
                <span className="text-xs text-amber-500">{t('readyToMerge.conflict')}</span>
              )}
              {pr.has_unresolved_threads && (
                <span className="text-xs text-amber-500">{t('readyToMerge.unresolvedThreads')}</span>
              )}
              {pr.bellows_managed && (
                <span className="text-xs text-indigo-400">{t('readyToMerge.bellowsActive')}</span>
              )}
            </div>
          </div>

          <div className="flex items-center gap-1.5 shrink-0 flex-wrap justify-end">
            {url && (
              <a
                href={url}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={t('readyToMerge.viewOnGitHub')}
                className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                  bg-gray-700 text-gray-400 border border-gray-600 hover:bg-gray-600 hover:text-gray-200"
              >
                <ExternalLink size={13} />
              </a>
            )}

            {!pr.bellows_managed && (
              <button
                type="button"
                onClick={() => setConfirmAction({ type: 'bellows', pr })}
                disabled={!!acting[`bellows-${pr.id}`]}
                aria-label={t('readyToMerge.bellowsLabel', { number: pr.number })}
                className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                  bg-indigo-600/20 text-indigo-300 border border-indigo-600/30
                  hover:bg-indigo-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <Bell size={13} />
                <span className="hidden sm:inline">{t('readyToMerge.bellows')}</span>
              </button>
            )}

            {!pr.ci_passing && (
              <button
                type="button"
                onClick={() => setConfirmAction({ type: 'fixCI', pr })}
                disabled={!!acting[`fixCI-${pr.id}`]}
                aria-label={t('readyToMerge.fixCILabel', { number: pr.number })}
                className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
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
                className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                  bg-amber-600/20 text-amber-300 border border-amber-600/30
                  hover:bg-amber-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <RotateCcw size={13} />
                <span className="hidden sm:inline">{t('readyToMerge.fixConflicts')}</span>
              </button>
            )}

            <button
              type="button"
              onClick={() => setConfirmAction({ type: 'fixComments', pr })}
              disabled={!!acting[`fixComments-${pr.id}`]}
              aria-label={t('readyToMerge.fixCommentsLabel', { number: pr.number })}
              className={`flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                disabled:opacity-50 disabled:cursor-not-allowed ${pr.has_unresolved_threads
                  ? 'bg-cyan-600/20 text-cyan-300 border border-cyan-600/30 hover:bg-cyan-600/30'
                  : 'bg-gray-700/50 text-gray-400 border border-gray-600/30 hover:bg-gray-600/50 hover:text-gray-200'}`}
            >
              <MessageSquare size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.fixComments')}</span>
            </button>

            {(pr.ci_fix_count > 0 || pr.review_fix_count > 0) && (
              <button
                type="button"
                onClick={() => setConfirmAction({ type: 'resetCounters', pr })}
                disabled={!!acting[`resetCounters-${pr.id}`]}
                aria-label={t('readyToMerge.resetCountersLabel', { number: pr.number })}
                className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
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
              className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                bg-purple-600/20 text-purple-300 border border-purple-600/30
                hover:bg-purple-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <ShieldCheck size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.approve')}</span>
            </button>

            {ready && (
              <button
                type="button"
                onClick={() => setConfirmAction({ type: 'merge', pr })}
                disabled={!!acting[`merge-${pr.id}`]}
                aria-label={t('readyToMerge.mergeLabel', { number: pr.number })}
                className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                  bg-green-600/20 text-green-300 border border-green-600/30
                  hover:bg-green-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <GitMerge size={13} />
                <span className="hidden sm:inline">{t('readyToMerge.merge')}</span>
              </button>
            )}
          </div>
        </div>
      </div>
    )
  }

  function renderExternalPR(pr: ExternalPR) {
    return (
      <div key={`ext-${pr.anvil}-${pr.number}`} className="px-5 py-3 flex flex-col gap-2">
        <div className="flex items-start justify-between gap-3">
          <div className="flex flex-col gap-1 min-w-0">
            <span className="text-sm text-white truncate">{pr.title}</span>
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-xs text-gray-500">#{pr.number}</span>
              <span className="text-xs text-gray-500">{pr.author}</span>
              <span className="text-xs text-gray-600">{pr.branch}</span>
              {pr.is_draft && (
                <span className="text-xs text-gray-500">{t('readyToMerge.draft')}</span>
              )}
            </div>
          </div>

          <div className="flex items-center gap-1.5 shrink-0 flex-wrap justify-end">
            {pr.url && (
              <a
                href={pr.url}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={t('readyToMerge.viewOnGitHub')}
                className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                  bg-gray-700 text-gray-400 border border-gray-600 hover:bg-gray-600 hover:text-gray-200"
              >
                <ExternalLink size={13} />
              </a>
            )}

            <button
              type="button"
              onClick={() => setConfirmExtAction({ type: 'extBellows', pr })}
              disabled={!!acting[`extBellows-${pr.anvil}-${pr.number}`]}
              aria-label={t('readyToMerge.bellowsLabel', { number: pr.number })}
              className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                bg-indigo-600/20 text-indigo-300 border border-indigo-600/30
                hover:bg-indigo-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Bell size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.bellows')}</span>
            </button>

            <button
              type="button"
              onClick={() => setConfirmExtAction({ type: 'extFixCI', pr })}
              disabled={!!acting[`extFixCI-${pr.anvil}-${pr.number}`]}
              aria-label={t('readyToMerge.fixCILabel', { number: pr.number })}
              className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                bg-red-600/20 text-red-300 border border-red-600/30
                hover:bg-red-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Wrench size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.fixCI')}</span>
            </button>

            <button
              type="button"
              onClick={() => setConfirmExtAction({ type: 'extFixConflicts', pr })}
              disabled={!!acting[`extFixConflicts-${pr.anvil}-${pr.number}`]}
              aria-label={t('readyToMerge.fixConflictsLabel', { number: pr.number })}
              className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                bg-amber-600/20 text-amber-300 border border-amber-600/30
                hover:bg-amber-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <RotateCcw size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.fixConflicts')}</span>
            </button>

            <button
              type="button"
              onClick={() => setConfirmExtAction({ type: 'extFixComments', pr })}
              disabled={!!acting[`extFixComments-${pr.anvil}-${pr.number}`]}
              aria-label={t('readyToMerge.fixCommentsLabel', { number: pr.number })}
              className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                bg-gray-700/50 text-gray-400 border border-gray-600/30
                hover:bg-gray-600/50 hover:text-gray-200 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <MessageSquare size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.fixComments')}</span>
            </button>

            <button
              type="button"
              onClick={() => setConfirmExtAction({ type: 'extResetCounters', pr })}
              disabled={!!acting[`extResetCounters-${pr.anvil}-${pr.number}`]}
              aria-label={t('readyToMerge.resetCountersLabel', { number: pr.number })}
              className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                bg-orange-600/20 text-orange-300 border border-orange-600/30
                hover:bg-orange-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <RotateCcw size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.resetCounters')}</span>
            </button>

            <button
              type="button"
              onClick={() => setConfirmExtAction({ type: 'extApprove', pr })}
              disabled={!!acting[`extApprove-${pr.anvil}-${pr.number}`]}
              aria-label={t('readyToMerge.extApproveLabel', { number: pr.number })}
              className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                bg-purple-600/20 text-purple-300 border border-purple-600/30
                hover:bg-purple-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <ShieldCheck size={13} />
              <span className="hidden sm:inline">{t('readyToMerge.approve')}</span>
            </button>

            {!pr.is_draft && (
              <button
                type="button"
                onClick={() => setConfirmExtAction({ type: 'extMerge', pr })}
                disabled={!!acting[`extMerge-${pr.anvil}-${pr.number}`]}
                aria-label={t('readyToMerge.extMergeLabel', { number: pr.number })}
                className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                  bg-green-600/20 text-green-300 border border-green-600/30
                  hover:bg-green-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <GitMerge size={13} />
                <span className="hidden sm:inline">{t('readyToMerge.merge')}</span>
              </button>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div id="ready-to-merge" className="bg-gray-800 rounded-xl border border-gray-700/50 overflow-hidden">
      <CollapsiblePanelHeader
        isOpen={isOpen}
        toggle={toggle}
        panelId="ready-to-merge-panel"
        icon={<GitMerge size={18} className={mergeReadyCount > 0 ? 'text-green-400 shrink-0' : 'text-gray-500 shrink-0'} />}
        title={t('readyToMerge.title')}
        trailing={
          <>
            {totalCount > 0 && (
              <span className="flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-gray-700 text-gray-400 text-xs font-medium">
                {totalCount}
              </span>
            )}
            {mergeReadyCount > 0 && (
              <span className="flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-green-500/20 text-green-400 text-xs font-medium">
                {t('readyToMerge.readyCount', { total: mergeReadyCount })}
              </span>
            )}
          </>
        }
      />

      <div id="ready-to-merge-panel" hidden={!isOpen}>
        {totalCount === 0 ? (
          <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('readyToMerge.noOpenPullRequests')}</p>
        ) : (
          <div>
            {anvilGroups.map(([anvil, group]) => {
              const anvilCollapsed = !!collapsedAnvils[anvil]
              const anvilTotal = group.forge.length + group.external.length
              const repoName = anvil.includes('/') ? anvil.split('/')[1] : anvil

              return (
                <div key={anvil} className="border-t border-gray-700/40 first:border-t-0">
                  {/* Anvil header */}
                  <button
                    type="button"
                    onClick={() => toggleAnvil(anvil)}
                    className="w-full flex items-center gap-2 px-5 py-2.5 text-left hover:bg-gray-700/30 transition-colors"
                    aria-expanded={!anvilCollapsed}
                  >
                    {anvilCollapsed ? (
                      <ChevronRight size={14} className="text-gray-500 shrink-0" />
                    ) : (
                      <ChevronDown size={14} className="text-gray-500 shrink-0" />
                    )}
                    <span className="text-xs font-medium text-gray-300">{repoName}</span>
                    <span className="text-xs text-gray-600">{anvil}</span>
                    <span className="ml-auto flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-gray-700 text-gray-500 text-xs font-medium">
                      {anvilTotal}
                    </span>
                  </button>

                  {!anvilCollapsed && (
                    <div>
                      {/* Forge PRs */}
                      {group.forge.length > 0 && (
                        <div>
                          {group.external.length > 0 && (
                            <div className="px-5 py-1.5">
                              <span className="text-[10px] font-semibold uppercase tracking-wider text-amber-500/70">
                                {t('readyToMerge.forgeSection')}
                              </span>
                            </div>
                          )}
                          <div className="divide-y divide-gray-700/30">
                            {group.forge.map(renderForgePR)}
                          </div>
                        </div>
                      )}

                      {/* External PRs */}
                      {group.external.length > 0 && (
                        <div>
                          {group.forge.length > 0 && (
                            <div className="px-5 py-1.5">
                              <span className="text-[10px] font-semibold uppercase tracking-wider text-gray-500">
                                {t('readyToMerge.externalSection')}
                              </span>
                            </div>
                          )}
                          <div className="divide-y divide-gray-700/30">
                            {group.external.map(renderExternalPR)}
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={confirmAction !== null}
        title={confirmAction ? confirmTitle(confirmAction.type) : ''}
        message={confirmAction ? confirmMessage(confirmAction.type, confirmAction.pr) : ''}
        confirmLabel={confirmAction ? confirmLabel(confirmAction.type) : ''}
        onConfirm={() => { if (confirmAction) void handleAction(confirmAction) }}
        onCancel={() => setConfirmAction(null)}
      />

      <ConfirmDialog
        open={confirmExtAction !== null}
        title={confirmExtAction ? extConfirmTitle(confirmExtAction.type) : ''}
        message={confirmExtAction ? extConfirmMessage(confirmExtAction.type, confirmExtAction.pr) : ''}
        confirmLabel={confirmExtAction ? extConfirmLabel(confirmExtAction.type) : ''}
        onConfirm={() => { if (confirmExtAction) void handleExtAction(confirmExtAction) }}
        onCancel={() => setConfirmExtAction(null)}
      />
    </div>
  )
}
