import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { GitMerge, Bell, ShieldCheck, ExternalLink } from 'lucide-react'
import type { OpenPR } from '../hooks/useForgeStatus'
import ConfirmDialog from './ConfirmDialog'
import { CollapsiblePanelHeader } from './CollapsiblePanelHeader'
import { usePanelCollapse } from '../hooks/usePanelCollapse'

interface ReadyToMergeCardProps {
  prs: OpenPR[]
  onMerged?: (id: number) => void
  showToast: (message: string, type: 'success' | 'error') => void
}

function isMergeReady(pr: OpenPR): boolean {
  return pr.ci_passing && pr.has_approval && !pr.is_conflicting && !pr.has_unresolved_threads
}

export default function ReadyToMergeCard({ prs, onMerged, showToast }: ReadyToMergeCardProps) {
  const { t } = useTranslation('forge')
  const [merging, setMerging] = useState<Partial<Record<number, boolean>>>({})
  const [bellowing, setBellowing] = useState<Partial<Record<number, boolean>>>({})
  const [approving, setApproving] = useState<Partial<Record<number, boolean>>>({})
  const [confirmMerge, setConfirmMerge] = useState<OpenPR | null>(null)
  const [confirmBellows, setConfirmBellows] = useState<OpenPR | null>(null)
  const [confirmApprove, setConfirmApprove] = useState<OpenPR | null>(null)
  const [isOpen, toggle] = usePanelCollapse('prs')

  async function handleMerge(pr: OpenPR) {
    setConfirmMerge(null)
    setMerging(prev => ({ ...prev, [pr.id]: true }))
    try {
      const res = await fetch(`/api/forge/prs/${pr.id}/merge`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t('readyToMerge.mergeSuccess', { number: pr.number }), 'success')
        onMerged?.(pr.id)
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('readyToMerge.mergeError'), 'error')
    } finally {
      setMerging(prev => ({ ...prev, [pr.id]: false }))
    }
  }

  async function handleBellows(pr: OpenPR) {
    setConfirmBellows(null)
    setBellowing(prev => ({ ...prev, [pr.id]: true }))
    try {
      const res = await fetch(`/api/forge/prs/${pr.id}/bellows`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t('readyToMerge.bellowsSuccess', { number: pr.number }), 'success')
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('readyToMerge.bellowsError'), 'error')
    } finally {
      setBellowing(prev => ({ ...prev, [pr.id]: false }))
    }
  }

  async function handleApprove(pr: OpenPR) {
    setConfirmApprove(null)
    setApproving(prev => ({ ...prev, [pr.id]: true }))
    try {
      const res = await fetch(`/api/forge/prs/${pr.id}/approve`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        showToast(t('readyToMerge.approveSuccess', { number: pr.number }), 'success')
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('readyToMerge.approveError'), 'error')
    } finally {
      setApproving(prev => ({ ...prev, [pr.id]: false }))
    }
  }

  const mergeReadyCount = prs.filter(isMergeReady).length

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
            {prs.length > 0 && (
              <span className="flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-gray-700 text-gray-400 text-xs font-medium">
                {prs.length}
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
      {prs.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('readyToMerge.noOpenPullRequests')}</p>
      ) : (
        <div className="divide-y divide-gray-700/40">
          {prs.map(pr => {
            const ready = isMergeReady(pr)
            const githubUrl = pr.anvil.includes('/') ? `https://github.com/${pr.anvil}/pull/${pr.number}` : null
            return (
              <div key={pr.id} className={`px-5 py-4 flex flex-col gap-2 ${ready ? 'bg-green-900/10' : ''}`}>
                <div className="flex items-start justify-between gap-3">
                  <div className="flex flex-col gap-1 min-w-0">
                    <span className="text-sm text-white truncate">{pr.title}</span>
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-xs text-gray-500">#{pr.number}</span>
                      {pr.ci_passing ? (
                        <span className="text-xs text-green-500">CI ✓</span>
                      ) : (
                        <span className="text-xs text-red-500">CI ✗</span>
                      )}
                      {pr.has_approval ? (
                        <span className="text-xs text-green-500">{t('readyToMerge.approved')}</span>
                      ) : (
                        <span className="text-xs text-gray-500">{t('readyToMerge.pendingReview')}</span>
                      )}
                      {pr.is_conflicting && (
                        <span className="text-xs text-amber-500">{t('readyToMerge.conflict')}</span>
                      )}
                      {pr.bellows_managed && (
                        <span className="text-xs text-indigo-400">{t('readyToMerge.bellowsActive')}</span>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-1.5 shrink-0 flex-wrap justify-end">
                    {githubUrl && (
                      <a
                        href={githubUrl}
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
                        onClick={() => setConfirmBellows(pr)}
                        disabled={!!bellowing[pr.id]}
                        aria-label={t('readyToMerge.bellowsLabel', { number: pr.number })}
                        className="flex items-center gap-1 min-h-[36px] min-w-[36px] px-2 rounded-lg text-xs font-medium transition-colors
                          bg-indigo-600/20 text-indigo-300 border border-indigo-600/30
                          hover:bg-indigo-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
                      >
                        <Bell size={13} />
                        <span className="hidden sm:inline">{t('readyToMerge.bellows')}</span>
                      </button>
                    )}

                    <button
                      type="button"
                      onClick={() => setConfirmApprove(pr)}
                      disabled={!!approving[pr.id]}
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
                        onClick={() => setConfirmMerge(pr)}
                        disabled={!!merging[pr.id]}
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
          })}
        </div>
      )}
      </div>

      <ConfirmDialog
        open={confirmMerge !== null}
        title={t('readyToMerge.mergeConfirmTitle')}
        message={t('readyToMerge.mergeConfirmMessage', { number: confirmMerge?.number ?? 0 })}
        confirmLabel={t('readyToMerge.merge')}
        onConfirm={() => { if (confirmMerge) void handleMerge(confirmMerge) }}
        onCancel={() => setConfirmMerge(null)}
      />

      <ConfirmDialog
        open={confirmBellows !== null}
        title={t('readyToMerge.bellowsConfirmTitle')}
        message={t('readyToMerge.bellowsConfirmMessage', { number: confirmBellows?.number ?? 0 })}
        confirmLabel={t('readyToMerge.bellows')}
        onConfirm={() => { if (confirmBellows) void handleBellows(confirmBellows) }}
        onCancel={() => setConfirmBellows(null)}
      />

      <ConfirmDialog
        open={confirmApprove !== null}
        title={t('readyToMerge.approveConfirmTitle')}
        message={t('readyToMerge.approveConfirmMessage', { number: confirmApprove?.number ?? 0 })}
        confirmLabel={t('readyToMerge.approve')}
        onConfirm={() => { if (confirmApprove) void handleApprove(confirmApprove) }}
        onCancel={() => setConfirmApprove(null)}
      />
    </div>
  )
}
