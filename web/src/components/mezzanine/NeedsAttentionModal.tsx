import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import {
  RotateCcw,
  CheckCircle,
  XCircle,
  Hammer,
  ExternalLink,
} from 'lucide-react'
import { Dialog, DialogHeader, DialogBody } from '../ui/dialog'
import ConfirmDialog from '../ConfirmDialog'
import type { NeedsAttentionItem, NeedsAttentionAction, NeedsAttentionStatus } from '../../hooks/useNeedsAttention'
import { useBeadActions } from '../../hooks/useBeadActions'
import type { BeadActionType } from '../../hooks/useBeadActions'

interface NeedsAttentionModalProps {
  open: boolean
  onClose: () => void
  items: NeedsAttentionItem[]
  showToast: (message: string, type: 'success' | 'error') => void
  onBeadClick?: (beadId: string) => void
}

interface PendingConfirm {
  action: NeedsAttentionAction
  beadId: string
}

function statusLabel(status: NeedsAttentionStatus, t: TFunction<'forge'>): string {
  switch (status) {
    case 'needs_human': return t('attention.modal.statusNeedsHuman')
    case 'clarification_needed': return t('attention.clarificationNeeded')
    case 'dispatch_failure': return t('attention.modal.statusDispatchFailure')
    case 'ci_exhausted': return t('attention.modal.statusCIExhausted')
    case 'review_exhausted': return t('attention.modal.statusReviewExhausted')
  }
}

function statusColor(status: NeedsAttentionStatus): string {
  switch (status) {
    case 'needs_human': return 'text-orange-400'
    case 'clarification_needed': return 'text-yellow-500'
    case 'dispatch_failure': return 'text-red-400'
    case 'ci_exhausted': return 'text-red-400'
    case 'review_exhausted': return 'text-red-400'
  }
}

function actionIcon(action: NeedsAttentionAction) {
  switch (action) {
    case 'retry': return <RotateCcw size={14} />
    case 'approve': return <CheckCircle size={14} />
    case 'dismiss': return <XCircle size={14} />
    case 'forceSmith': return <Hammer size={14} />
  }
}

function actionStyle(action: NeedsAttentionAction): string {
  switch (action) {
    case 'retry':
      return 'bg-amber-600/20 text-amber-300 border-amber-600/30 hover:bg-amber-600/30'
    case 'approve':
      return 'bg-green-600/20 text-green-300 border-green-600/30 hover:bg-green-600/30'
    case 'dismiss':
      return 'bg-red-600/20 text-red-300 border-red-600/30 hover:bg-red-600/30'
    case 'forceSmith':
      return 'bg-purple-600/20 text-purple-300 border-purple-600/30 hover:bg-purple-600/30'
  }
}

function actionLabel(action: NeedsAttentionAction, t: TFunction<'forge'>): string {
  switch (action) {
    case 'retry': return t('attention.retry')
    case 'approve': return t('attention.approve')
    case 'dismiss': return t('attention.dismiss')
    case 'forceSmith': return t('attention.forceSmith')
  }
}

function confirmTitle(action: NeedsAttentionAction, t: TFunction<'forge'>): string {
  switch (action) {
    case 'retry': return t('attention.retryConfirmTitle')
    case 'approve': return t('attention.approveConfirmTitle')
    case 'dismiss': return t('attention.dismissConfirmTitle')
    case 'forceSmith': return t('attention.forceSmithConfirmTitle')
  }
}

function confirmMessage(action: NeedsAttentionAction, beadId: string, t: TFunction<'forge'>): string {
  switch (action) {
    case 'retry': return t('attention.retryConfirmMessage', { id: beadId })
    case 'approve': return t('attention.approveConfirmMessage', { id: beadId })
    case 'dismiss': return t('attention.dismissConfirmMessage', { id: beadId })
    case 'forceSmith': return t('attention.forceSmithConfirmMessage', { id: beadId })
  }
}

function prUrl(item: NeedsAttentionItem): string | null {
  if (!item.pr) return null
  return item.pr.anvil.includes('/') ? `https://github.com/${item.pr.anvil}/pull/${item.pr.number}` : null
}

export default function NeedsAttentionModal({ open, onClose, items, showToast, onBeadClick }: NeedsAttentionModalProps) {
  const { t } = useTranslation('forge')
  const { acting, handleAction } = useBeadActions({ showToast })
  const [pendingConfirm, setPendingConfirm] = useState<PendingConfirm | null>(null)
  const [prevOpen, setPrevOpen] = useState(open)

  // Clear any pending confirmation when the modal closes so the ConfirmDialog
  // doesn't remain visible on its own after ESC/backdrop/X dismissal.
  if (prevOpen !== open) {
    setPrevOpen(open)
    if (!open) setPendingConfirm(null)
  }

  function onAction(action: NeedsAttentionAction, beadId: string) {
    if (action === 'dismiss') {
      setPendingConfirm({ action, beadId })
    } else {
      void handleAction(action as BeadActionType, beadId)
    }
  }

  function handleBeadClick(beadId: string) {
    onClose()
    onBeadClick?.(beadId)
  }

  const titleId = useId()

  return (
    <>
      <Dialog open={open && pendingConfirm === null} onClose={onClose} maxWidth="max-w-lg" aria-labelledby={titleId}>
        <DialogHeader id={titleId} title={t('attention.title')} onClose={onClose} />
        <DialogBody className="p-0">
          {items.length === 0 ? (
            <p className="px-6 py-8 text-sm text-gray-500 text-center">{t('attention.empty')}</p>
          ) : (
            <div className="divide-y divide-gray-700/40">
              {items.map(item => {
                const isActing = !!acting[item.beadId]
                const url = prUrl(item)

                return (
                  <div key={item.beadId} className="px-5 py-4 flex flex-col gap-2.5">
                    <div className="flex items-start justify-between gap-2">
                      <div className="flex flex-col gap-0.5 min-w-0">
                        <button
                          type="button"
                          onClick={() => handleBeadClick(item.beadId)}
                          className="text-sm font-mono text-amber-400 hover:text-amber-300 hover:underline truncate transition-colors text-left"
                        >
                          {item.beadId}
                        </button>
                        <span className="text-xs text-gray-500">
                          {item.anvil} · {t('attention.retryCount', { count: item.retryCount })}
                        </span>
                      </div>
                      {url && (
                        <a
                          href={url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-gray-400 hover:text-white transition-colors shrink-0"
                          aria-label={t('attention.viewPR', { number: item.pr!.number })}
                        >
                          <ExternalLink size={14} />
                        </a>
                      )}
                    </div>

                    <div className="flex flex-wrap gap-1.5">
                      {item.statuses.map(s => (
                        <span key={s} className={`text-xs px-2 py-0.5 rounded-full bg-gray-700/50 ${statusColor(s)}`}>
                          {statusLabel(s, t)}
                        </span>
                      ))}
                    </div>

                    {item.lastError && (
                      <p className="text-xs text-red-400 bg-red-900/20 rounded px-2.5 py-1.5 break-words line-clamp-2">
                        {item.lastError}
                      </p>
                    )}

                    <div className="flex flex-wrap gap-1.5">
                      {item.actions.map(action => (
                        <button
                          key={action}
                          type="button"
                          onClick={() => onAction(action, item.beadId)}
                          disabled={isActing}
                          aria-label={actionLabel(action, t)}
                          title={actionLabel(action, t)}
                          className={`flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-medium border transition-colors
                            disabled:opacity-50 disabled:cursor-not-allowed ${actionStyle(action)}`}
                        >
                          {actionIcon(action)}
                          <span>{actionLabel(action, t)}</span>
                        </button>
                      ))}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </DialogBody>
      </Dialog>

      <ConfirmDialog
        open={pendingConfirm !== null}
        title={pendingConfirm ? confirmTitle(pendingConfirm.action, t) : ''}
        message={pendingConfirm ? confirmMessage(pendingConfirm.action, pendingConfirm.beadId, t) : ''}
        confirmLabel={pendingConfirm ? actionLabel(pendingConfirm.action, t) : ''}
        destructive={pendingConfirm?.action === 'dismiss'}
        onConfirm={() => {
          if (pendingConfirm) void handleAction(pendingConfirm.action as BeadActionType, pendingConfirm.beadId)
          setPendingConfirm(null)
        }}
        onCancel={() => setPendingConfirm(null)}
      />
    </>
  )
}
