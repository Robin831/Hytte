import { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, XCircle, Clock, AlertTriangle, GitMerge, MessageSquare, MinusCircle } from 'lucide-react'
import MergeConfirmDialog from '../MergeConfirmDialog'
import QueueContextMenu from './QueueContextMenu'

export interface PRStatusInfo {
  prId: number
  prNumber: number
  ciPassing: boolean
  ciPending: boolean
  hasApproval: boolean
  changesRequested: boolean
  isConflicting: boolean
  hasUnresolvedThreads: boolean
  hasPendingReviews: boolean
  bellowsManaged: boolean
}

export interface PipelineBeadInfo {
  beadId: string
  title: string
  status: 'active' | 'done' | 'failed'
  anvil?: string
  pr?: PRStatusInfo
}

interface PipelineBeadCardProps {
  bead: PipelineBeadInfo
  onBeadClick?: (beadId: string) => void
  onMerge?: (prId: number, prNumber: number) => void
  showQueueActions?: boolean
  showToast?: (message: string, type: 'success' | 'error') => void
  onActionComplete?: () => void
  highlighted?: boolean
}

const statusDot: Record<string, string> = {
  active: 'bg-blue-400 animate-pulse',
  done: 'bg-green-400',
  failed: 'bg-red-400',
}

function isMergeReady(pr: PRStatusInfo): boolean {
  return pr.ciPassing && pr.hasApproval && !pr.isConflicting && !pr.hasUnresolvedThreads
}

export default function PipelineBeadCard({ bead, onBeadClick, onMerge, showQueueActions, showToast, onActionComplete, highlighted }: PipelineBeadCardProps) {
  const { t } = useTranslation('forge')
  const [showMergeConfirm, setShowMergeConfirm] = useState(false)

  const pr = bead.pr

  const handleCardKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onBeadClick?.(bead.beadId)
    }
  }, [onBeadClick, bead.beadId])

  return (
    <>
      <div
        role="button"
        tabIndex={0}
        onClick={() => onBeadClick?.(bead.beadId)}
        onKeyDown={handleCardKeyDown}
        aria-label={t('mezzanine.queueSidebar.viewBead', { beadId: bead.beadId })}
        className={`w-full text-left rounded border px-2.5 py-1.5 hover:bg-gray-700/60 hover:border-gray-600/60 transition-colors group cursor-pointer ${
          highlighted
            ? 'border-amber-500/70 bg-amber-900/20 ring-1 ring-amber-500/40'
            : 'border-gray-700/50 bg-gray-800/80'
        }`}
      >
        <div className="flex items-center gap-1.5 min-w-0">
          <span className={`shrink-0 h-1.5 w-1.5 rounded-full ${statusDot[bead.status] ?? statusDot.active}`} aria-hidden="true" />
          <span className="text-[11px] font-mono text-cyan-400 group-hover:text-cyan-300 shrink-0 transition-colors">
            {bead.beadId}
          </span>
          {pr && (
            <span className="text-[10px] text-gray-500 shrink-0">
              #{pr.prNumber}
            </span>
          )}
          {showQueueActions && showToast && (
            <span className="ml-auto shrink-0">
              <QueueContextMenu
                beadId={bead.beadId}
                showToast={showToast}
                onBeadClick={onBeadClick}
                onActionComplete={onActionComplete}
              />
            </span>
          )}
        </div>
        {bead.title && (
          <p className="mt-0.5 text-[11px] text-gray-400 truncate leading-tight">
            {bead.title}
          </p>
        )}

        {pr && (
          <div className="mt-1 flex items-center gap-1.5 flex-wrap">
            {/* CI status badge */}
            {pr.ciPassing ? (
              <span className="inline-flex items-center gap-0.5 text-[10px] text-green-400" role="status" aria-label={t('mezzanine.pipeline.badges.ciPassing')}>
                <CheckCircle2 size={11} aria-hidden="true" />
                <span>CI</span>
              </span>
            ) : pr.ciPending ? (
              <span className="inline-flex items-center gap-0.5 text-[10px] text-yellow-400" role="status" aria-label={t('mezzanine.pipeline.badges.ciPending')}>
                <Clock size={11} aria-hidden="true" />
                <span>CI</span>
              </span>
            ) : (
              <span className="inline-flex items-center gap-0.5 text-[10px] text-red-400" role="status" aria-label={t('mezzanine.pipeline.badges.ciFailing')}>
                <XCircle size={11} aria-hidden="true" />
                <span>CI</span>
              </span>
            )}

            {/* Review state badge */}
            {pr.hasApproval ? (
              <span className="inline-flex items-center gap-0.5 text-[10px] text-green-400" role="status" aria-label={t('mezzanine.pipeline.badges.approved')}>
                <CheckCircle2 size={11} aria-hidden="true" />
                <span>{t('mezzanine.pipeline.badges.approvedShort')}</span>
              </span>
            ) : pr.changesRequested ? (
              <span className="inline-flex items-center gap-0.5 text-[10px] text-red-400" role="status" aria-label={t('mezzanine.pipeline.badges.changesRequested')}>
                <MinusCircle size={11} aria-hidden="true" />
                <span>{t('mezzanine.pipeline.badges.changesRequestedShort')}</span>
              </span>
            ) : pr.hasPendingReviews ? (
              <span className="inline-flex items-center gap-0.5 text-[10px] text-yellow-400" role="status" aria-label={t('mezzanine.pipeline.badges.pendingReview')}>
                <Clock size={11} aria-hidden="true" />
                <span>{t('mezzanine.pipeline.badges.reviewShort')}</span>
              </span>
            ) : (
              <span className="inline-flex items-center gap-0.5 text-[10px] text-gray-500" role="status" aria-label={t('mezzanine.pipeline.badges.noReview')}>
                <Clock size={11} aria-hidden="true" />
                <span>{t('mezzanine.pipeline.badges.reviewShort')}</span>
              </span>
            )}

            {/* Conflict indicator */}
            {pr.isConflicting && (
              <span className="inline-flex items-center gap-0.5 text-[10px] text-red-400" role="status" aria-label={t('mezzanine.pipeline.badges.conflict')}>
                <AlertTriangle size={11} aria-hidden="true" />
              </span>
            )}

            {/* Unresolved threads indicator */}
            {pr.hasUnresolvedThreads && (
              <span className="inline-flex items-center gap-0.5 text-[10px] text-yellow-400" role="status" aria-label={t('mezzanine.pipeline.badges.unresolvedThreads')}>
                <MessageSquare size={11} aria-hidden="true" />
              </span>
            )}

            {/* Merge readiness / button */}
            {isMergeReady(pr) && onMerge && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  setShowMergeConfirm(true)
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') {
                    e.stopPropagation()
                  }
                }}
                aria-label={t('mezzanine.pipeline.badges.mergeLabel', { number: pr.prNumber })}
                className="inline-flex items-center gap-0.5 text-[10px] font-medium text-green-300 bg-green-900/40 hover:bg-green-800/50 px-1.5 py-0.5 rounded transition-colors"
              >
                <GitMerge size={11} aria-hidden="true" />
                <span>{t('mezzanine.pipeline.badges.merge')}</span>
              </button>
            )}
          </div>
        )}
      </div>

      {pr && (
        <MergeConfirmDialog
          open={showMergeConfirm}
          prNumber={pr.prNumber}
          onConfirm={() => {
            setShowMergeConfirm(false)
            onMerge?.(pr.prId, pr.prNumber)
          }}
          onCancel={() => setShowMergeConfirm(false)}
        />
      )}
    </>
  )
}
