import { useState, useRef, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  AlertTriangle,
  RotateCcw,
  MoreVertical,
  CheckCircle,
  XCircle,
  Hammer,
  Square,
  ExternalLink,
  FileText,
} from 'lucide-react'
import type { StuckBead, WorkerInfo, OpenPR } from '../hooks/useForgeStatus'
import ConfirmDialog from './ConfirmDialog'
import WorkerLogModal from './WorkerLogModal'
import { CollapsiblePanelHeader } from './CollapsiblePanelHeader'
import { usePanelCollapse } from '../hooks/usePanelCollapse'

interface NeedsAttentionCardProps {
  stuck: StuckBead[]
  workers: WorkerInfo[]
  openPrs: OpenPR[]
  onRetried?: (beadId: string) => void
  showToast: (message: string, type: 'success' | 'error') => void
  onBeadClick?: (beadId: string) => void
}

type ActionType = 'retry' | 'approve' | 'dismiss' | 'forceSmith' | 'kill'

interface PendingAction {
  type: ActionType
  bead: StuckBead
}

export default function NeedsAttentionCard({ stuck, workers, openPrs, onRetried, showToast, onBeadClick }: NeedsAttentionCardProps) {
  const { t } = useTranslation('forge')
  const [acting, setActing] = useState<Record<string, boolean>>({})
  const [confirmAction, setConfirmAction] = useState<PendingAction | null>(null)
  const [openMenuId, setOpenMenuId] = useState<string | null>(null)
  const [logBead, setLogBead] = useState<StuckBead | null>(null)
  const [isOpen, toggle] = usePanelCollapse('needs-attention')
  const menuRefs = useRef<Record<string, HTMLDivElement | null>>({})
  const currentIds = useMemo(() => new Set(stuck.map(b => b.bead_id)), [stuck])

  // Prune stale refs when the bead list changes
  useEffect(() => {
    for (const id of Object.keys(menuRefs.current)) {
      if (!currentIds.has(id)) delete menuRefs.current[id]
    }
  }, [currentIds])

  useEffect(() => {
    if (!openMenuId) return
    // Guard: if the ref is missing the bead has disappeared — clear state immediately
    if (!menuRefs.current[openMenuId]) {
      setOpenMenuId(null)
      return
    }
    function handleClickOutside(e: MouseEvent) {
      const menuEl = menuRefs.current[openMenuId!]
      if (!menuEl || !menuEl.contains(e.target as Node)) {
        setOpenMenuId(null)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [openMenuId])

  const activeWorkerByBeadId = useMemo(() => {
    const m = new Map<string, WorkerInfo>()
    for (const w of workers) {
      if ((w.status === 'pending' || w.status === 'running') && !m.has(w.bead_id)) {
        m.set(w.bead_id, w)
      }
    }
    return m
  }, [workers])

  const anyWorkerByBeadId = useMemo(() => {
    const m = new Map<string, WorkerInfo>()
    for (const w of workers) {
      if (!m.has(w.bead_id)) m.set(w.bead_id, w)
    }
    return m
  }, [workers])

  const prByBeadId = useMemo(() => {
    const m = new Map<string, OpenPR>()
    for (const pr of openPrs) {
      if (!m.has(pr.bead_id)) m.set(pr.bead_id, pr)
    }
    return m
  }, [openPrs])

  async function handleAction(action: PendingAction) {
    setConfirmAction(null)
    const beadId = action.bead.bead_id
    setActing(prev => ({ ...prev, [beadId]: true }))
    try {
      let url: string
      switch (action.type) {
        case 'retry':
          url = `/api/forge/beads/${encodeURIComponent(beadId)}/retry`
          break
        case 'approve':
          url = `/api/forge/beads/${encodeURIComponent(beadId)}/approve`
          break
        case 'dismiss':
          url = `/api/forge/beads/${encodeURIComponent(beadId)}/dismiss`
          break
        case 'forceSmith':
          url = `/api/forge/beads/${encodeURIComponent(beadId)}/force-smith`
          break
        case 'kill': {
          const worker = activeWorkerByBeadId.get(beadId)
          if (!worker) {
            showToast(t('attention.noWorkerFound'), 'error')
            return
          }
          url = `/api/forge/workers/${encodeURIComponent(worker.id)}/kill`
          break
        }
      }
      const res = await fetch(url, { method: 'POST', credentials: 'include' })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        showToast((data as { error?: string }).error ?? `HTTP ${res.status}`, 'error')
      } else {
        const key = `attention.${action.type}Success` as const
        showToast(t(key, { id: beadId }), 'success')
        if (action.type === 'retry') onRetried?.(beadId)
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('unknownError'), 'error')
    } finally {
      setActing(prev => ({ ...prev, [beadId]: false }))
    }
  }

  function confirmTitle(type: ActionType): string {
    switch (type) {
      case 'retry': return t('attention.retryConfirmTitle')
      case 'approve': return t('attention.approveConfirmTitle')
      case 'dismiss': return t('attention.dismissConfirmTitle')
      case 'forceSmith': return t('attention.forceSmithConfirmTitle')
      case 'kill': return t('workers.killConfirmTitle')
    }
  }

  function confirmMessage(type: ActionType, beadId: string): string {
    switch (type) {
      case 'retry': return t('attention.retryConfirmMessage', { id: beadId })
      case 'approve': return t('attention.approveConfirmMessage', { id: beadId })
      case 'dismiss': return t('attention.dismissConfirmMessage', { id: beadId })
      case 'forceSmith': return t('attention.forceSmithConfirmMessage', { id: beadId })
      case 'kill': return t('workers.killConfirmMessage', { id: beadId })
    }
  }

  function confirmLabel(type: ActionType): string {
    switch (type) {
      case 'retry': return t('attention.retry')
      case 'approve': return t('attention.approve')
      case 'dismiss': return t('attention.dismiss')
      case 'forceSmith': return t('attention.forceSmith')
      case 'kill': return t('workers.kill')
    }
  }

  function isDestructive(type: ActionType): boolean {
    return type === 'kill' || type === 'dismiss'
  }

  function prUrl(pr: OpenPR): string | null {
    return pr.anvil.includes('/') ? `https://github.com/${pr.anvil}/pull/${pr.number}` : null
  }

  return (
    <div id="needs-attention" className="bg-gray-800 rounded-xl border border-amber-600/30 overflow-hidden">
      <CollapsiblePanelHeader
        isOpen={isOpen}
        toggle={toggle}
        panelId="needs-attention-panel"
        icon={<AlertTriangle size={18} className={stuck.length > 0 ? 'text-amber-400 shrink-0' : 'text-gray-500 shrink-0'} />}
        title={t('attention.title')}
        trailing={
          stuck.length > 0 ? (
            <span className="flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-amber-500/20 text-amber-400 text-xs font-medium">
              {stuck.length}
            </span>
          ) : undefined
        }
      />

      <div id="needs-attention-panel" hidden={!isOpen}>
      {stuck.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('attention.empty')}</p>
      ) : (
        <div className="divide-y divide-gray-700/40">
          {stuck.map(bead => {
            const anyWorker = anyWorkerByBeadId.get(bead.bead_id)
            const pr = prByBeadId.get(bead.bead_id)
            const menuOpen = openMenuId === bead.bead_id

            return (
              <div key={bead.bead_id} className="px-5 py-4 flex flex-col gap-3 min-h-[44px]">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex flex-col gap-0.5 min-w-0">
                    <button
                      type="button"
                      onClick={() => onBeadClick?.(bead.bead_id)}
                      className="text-sm font-mono text-amber-400 hover:text-amber-300 hover:underline truncate transition-colors text-left"
                    >
                      {bead.bead_id}
                    </button>
                    <span className="text-xs text-gray-500">
                      {bead.anvil} · {t('attention.retryCount', { count: bead.retry_count })}
                      {bead.clarification_needed && (
                        <span className="ml-2 text-yellow-500">{t('attention.clarificationNeeded')}</span>
                      )}
                    </span>
                  </div>

                  <div className="flex items-center gap-1.5 shrink-0">
                    {/* Primary retry button */}
                    <button
                      type="button"
                      onClick={() => setConfirmAction({ type: 'retry', bead })}
                      disabled={!!acting[bead.bead_id]}
                      aria-label={t('attention.retryLabel', { id: bead.bead_id })}
                      className="flex items-center gap-1.5 min-h-[44px] min-w-[44px] px-3 rounded-lg text-sm font-medium transition-colors
                        bg-amber-600/20 text-amber-300 border border-amber-600/30
                        hover:bg-amber-600/30 disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                      <RotateCcw size={14} className={acting[bead.bead_id] ? 'animate-spin' : ''} />
                      <span className="hidden sm:inline">{t('attention.retry')}</span>
                    </button>

                    {/* Action menu */}
                    <div
                      className="relative"
                      ref={el => { menuRefs.current[bead.bead_id] = el }}
                    >
                      <button
                        type="button"
                        onClick={() => setOpenMenuId(menuOpen ? null : bead.bead_id)}
                        aria-label={t('attention.actionsLabel', { id: bead.bead_id })}
                        aria-expanded={menuOpen}
                        aria-haspopup="true"
                        className="flex items-center justify-center min-h-[44px] min-w-[44px] rounded-lg text-sm font-medium transition-colors
                          bg-gray-700 text-gray-300 border border-gray-600
                          hover:bg-gray-600"
                      >
                        <MoreVertical size={16} />
                      </button>

                      {menuOpen && (
                        <div
                          className="absolute right-0 top-full mt-1 z-30 w-56 rounded-lg bg-gray-800 border border-gray-600 shadow-xl py-1 overflow-hidden"
                        >
                          <button
                            type="button"
                            onClick={() => { setOpenMenuId(null); setConfirmAction({ type: 'approve', bead }) }}
                            className="w-full flex items-center gap-2.5 px-4 py-2.5 text-sm text-gray-300 hover:bg-gray-700 transition-colors text-left"
                          >
                            <CheckCircle size={15} className="text-green-400 shrink-0" />
                            {t('attention.approve')}
                          </button>

                          <button
                            type="button"
                            onClick={() => { setOpenMenuId(null); setConfirmAction({ type: 'forceSmith', bead }) }}
                            className="w-full flex items-center gap-2.5 px-4 py-2.5 text-sm text-gray-300 hover:bg-gray-700 transition-colors text-left"
                          >
                            <Hammer size={15} className="text-amber-400 shrink-0" />
                            {t('attention.forceSmith')}
                          </button>

                          <button
                            type="button"
                            onClick={() => { setOpenMenuId(null); setConfirmAction({ type: 'dismiss', bead }) }}
                            className="w-full flex items-center gap-2.5 px-4 py-2.5 text-sm text-red-400 hover:bg-gray-700 transition-colors text-left"
                          >
                            <XCircle size={15} className="shrink-0" />
                            {t('attention.dismiss')}
                          </button>

                          <button
                            type="button"
                            onClick={() => { setOpenMenuId(null); setConfirmAction({ type: 'kill', bead }) }}
                            className="w-full flex items-center gap-2.5 px-4 py-2.5 text-sm text-red-400 hover:bg-gray-700 transition-colors text-left"
                          >
                            <Square size={15} className="shrink-0" />
                            {t('attention.killWorker')}
                          </button>

                          {pr && prUrl(pr) && (
                            <a
                              href={prUrl(pr)!}
                              target="_blank"
                              rel="noopener noreferrer"
                              onClick={() => setOpenMenuId(null)}
                              className="w-full flex items-center gap-2.5 px-4 py-2.5 text-sm text-gray-300 hover:bg-gray-700 transition-colors text-left"
                            >
                              <ExternalLink size={15} className="text-purple-400 shrink-0" />
                              {t('attention.viewPR', { number: pr.number })}
                            </a>
                          )}

                          {anyWorker && (
                            <button
                              type="button"
                              onClick={() => { setOpenMenuId(null); setLogBead(bead) }}
                              className="w-full flex items-center gap-2.5 px-4 py-2.5 text-sm text-gray-300 hover:bg-gray-700 transition-colors text-left"
                            >
                              <FileText size={15} className="text-cyan-400 shrink-0" />
                              {t('attention.viewLogs')}
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  </div>
                </div>

                {bead.last_error && (
                  <p className="text-xs text-red-400 bg-red-900/20 rounded px-3 py-2 break-words">
                    {bead.last_error}
                  </p>
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
        message={confirmAction ? confirmMessage(confirmAction.type, confirmAction.bead.bead_id) : ''}
        confirmLabel={confirmAction ? confirmLabel(confirmAction.type) : ''}
        destructive={confirmAction ? isDestructive(confirmAction.type) : false}
        onConfirm={() => { if (confirmAction) void handleAction(confirmAction) }}
        onCancel={() => setConfirmAction(null)}
      />

      <WorkerLogModal
        open={logBead !== null}
        onClose={() => setLogBead(null)}
        workerId={logBead ? (anyWorkerByBeadId.get(logBead.bead_id)?.id ?? null) : null}
        beadId={logBead?.bead_id ?? ''}
      />
    </div>
  )
}
