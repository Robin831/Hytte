import { useState, useEffect, useCallback, useRef, type AnchorHTMLAttributes, type ReactNode, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  Tag,
  User,
  Calendar,
  ArrowLeft,
  AlertCircle,
  Loader2,
  ArrowRight,
  X,
  Plus,
  Send,
  XCircle,
  ChevronDown,
} from 'lucide-react'
import { Dialog, DialogHeader, DialogBody } from './ui/dialog'
import { formatDateTime } from '../utils/formatDate'
import type { BeadDetail, BeadComment, BeadDependency } from '../types/forge'

interface BeadDetailModalProps {
  open: boolean
  onClose: () => void
  beadId: string | null
}

const priorityColors: Record<number, string> = {
  1: 'bg-red-500/20 text-red-400 border-red-700/30',
  2: 'bg-orange-500/20 text-orange-400 border-orange-700/30',
  3: 'bg-yellow-500/20 text-yellow-400 border-yellow-700/30',
  4: 'bg-gray-500/20 text-gray-400 border-gray-600/30',
}

const statusColors: Record<string, string> = {
  open: 'bg-green-500/20 text-green-400 border-green-700/30',
  'in-progress': 'bg-blue-500/20 text-blue-400 border-blue-700/30',
  in_progress: 'bg-blue-500/20 text-blue-400 border-blue-700/30',
  closed: 'bg-gray-500/20 text-gray-400 border-gray-600/30',
  blocked: 'bg-red-500/20 text-red-400 border-red-700/30',
  deferred: 'bg-purple-500/20 text-purple-400 border-purple-700/30',
  pinned: 'bg-cyan-500/20 text-cyan-400 border-cyan-700/30',
  hooked: 'bg-amber-500/20 text-amber-400 border-amber-700/30',
}

const BEAD_STATUSES = ['open', 'in_progress', 'blocked', 'deferred', 'closed', 'pinned', 'hooked'] as const
const BEAD_PRIORITIES = [1, 2, 3, 4] as const

const SAFE_URL_PROTOCOLS = ['http:', 'https:', 'mailto:', 'tel:'] as const

function getSafeHref(href?: string): string | undefined {
  if (!href) return undefined
  if (href.startsWith('/') || href.startsWith('#')) return href
  try {
    const url = new URL(href, window.location.origin)
    if (SAFE_URL_PROTOCOLS.includes(url.protocol as (typeof SAFE_URL_PROTOCOLS)[number])) {
      return href
    }
  } catch {
    // If parsing fails, treat as unsafe
  }
  return undefined
}

const markdownComponents = {
  a: ({ children, href, ...props }: AnchorHTMLAttributes<HTMLAnchorElement>) => {
    const safeHref = getSafeHref(typeof href === 'string' ? href : undefined)
    if (!safeHref) {
      return <span>{children}</span>
    }
    return (
      <a {...props} href={safeHref} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  },
}

function Badge({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${className ?? 'bg-gray-700/60 text-gray-400 border-gray-600/40'}`}>
      {children}
    </span>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="mt-4">
      <h3 className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-2">{title}</h3>
      {children}
    </div>
  )
}

function MarkdownContent({ content }: { content: string }) {
  return (
    <div className="prose prose-invert prose-sm max-w-none">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
        {content}
      </ReactMarkdown>
    </div>
  )
}

function CommentItem({ comment }: { comment: BeadComment }) {
  return (
    <div className="border border-gray-700/50 rounded-lg p-3 bg-gray-800/40">
      <div className="flex items-center gap-2 mb-2">
        <User size={14} className="text-gray-500" />
        <span className="text-sm font-medium text-gray-300">{comment.author}</span>
        <span className="text-xs text-gray-500">{formatDateTime(comment.created_at, { dateStyle: 'medium', timeStyle: 'short' })}</span>
      </div>
      <MarkdownContent content={comment.body} />
    </div>
  )
}

function DependencyItem({
  dep,
  direction,
  onClick,
  t,
}: {
  dep: BeadDependency
  direction: 'dependency' | 'dependent'
  onClick: (id: string) => void
  t: TFunction<'forge'>
}) {
  const statusCls = statusColors[dep.status] ?? statusColors.open
  return (
    <div className="flex items-center gap-2 py-1.5 border-b border-gray-700/30 last:border-0">
      {direction === 'dependency' ? (
        <ArrowLeft size={14} className="text-amber-400 shrink-0" />
      ) : (
        <ArrowRight size={14} className="text-blue-400 shrink-0" />
      )}
      <button
        type="button"
        onClick={() => onClick(dep.id)}
        className="text-xs font-mono text-cyan-400 hover:text-cyan-300 hover:underline transition-colors"
        aria-label={t('beadDetail.viewBead') + ' ' + dep.id}
      >
        {dep.id}
      </button>
      <span className="text-xs text-gray-300 truncate">{dep.title}</span>
      <Badge className={statusCls}>{dep.status}</Badge>
      {dep.dependency_type && (
        <span className="text-xs text-gray-500 italic">{dep.dependency_type}</span>
      )}
    </div>
  )
}

async function mutate(url: string, method: string, body?: unknown): Promise<boolean> {
  try {
    const res = await fetch(url, {
      method,
      credentials: 'include',
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    })
    return res.ok
  } catch {
    return false
  }
}

export default function BeadDetailModal({ open, onClose, beadId }: BeadDetailModalProps) {
  const { t } = useTranslation('forge')

  const [bead, setBead] = useState<BeadDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [historyStack, setHistoryStack] = useState<{ baseId: string | null; items: string[] }>({
    baseId: beadId,
    items: [],
  })

  // Action state
  const [mutating, setMutating] = useState(false)
  const [commentText, setCommentText] = useState('')
  const [newLabel, setNewLabel] = useState('')
  const [assigneeValue, setAssigneeValue] = useState('')
  const [showCloseForm, setShowCloseForm] = useState(false)
  const [closeReason, setCloseReason] = useState('')

  const history = historyStack.baseId === beadId ? historyStack.items : []
  const currentId = history.length > 0 ? history[history.length - 1] : beadId

  // Sync assignee value when bead data changes
  useEffect(() => {
    setAssigneeValue(bead?.assignee ?? '')
  }, [bead?.assignee])

  const fetchBead = useCallback(async (id: string, signal?: AbortSignal) => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`/api/forge/beads/${encodeURIComponent(id)}`, {
        credentials: 'include',
        signal,
      })
      if (signal?.aborted) return
      if (!res.ok) {
        setError(res.status === 404 ? t('beadDetail.errors.notFound') : t('beadDetail.errors.fetchFailed'))
        setBead(null)
        return
      }
      const data: BeadDetail = await res.json()
      if (!signal?.aborted) {
        setBead(data)
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      if (!signal?.aborted) {
        setError(t('beadDetail.errors.fetchFailed'))
        setBead(null)
      }
    } finally {
      if (!signal?.aborted) {
        setLoading(false)
      }
    }
  }, [t])

  useEffect(() => {
    if (!open || !currentId) return
    const controller = new AbortController()
    fetchBead(currentId, controller.signal)
    return () => controller.abort()
  }, [open, currentId, fetchBead])

  const refreshControllerRef = useRef<AbortController | null>(null)

  const refreshBead = useCallback(async () => {
    if (!currentId) return
    refreshControllerRef.current?.abort()
    const controller = new AbortController()
    refreshControllerRef.current = controller
    await fetchBead(currentId, controller.signal)
  }, [currentId, fetchBead])

  const doMutation = useCallback(async (url: string, method: string, body?: unknown): Promise<boolean> => {
    setMutating(true)
    try {
      const ok = await mutate(url, method, body)
      if (ok) await refreshBead()
      return ok
    } finally {
      setMutating(false)
    }
  }, [refreshBead])

  const handlePriorityChange = useCallback(async (priority: number) => {
    if (!currentId) return
    await doMutation(`/api/forge/beads/${encodeURIComponent(currentId)}/priority`, 'PUT', { priority })
  }, [currentId, doMutation])

  const handleStatusChange = useCallback(async (status: string) => {
    if (!currentId) return
    await doMutation(`/api/forge/beads/${encodeURIComponent(currentId)}/status`, 'PUT', { status })
  }, [currentId, doMutation])

  const handleAssigneeChange = useCallback(async (assignee: string) => {
    if (!currentId) return
    await doMutation(`/api/forge/beads/${encodeURIComponent(currentId)}/assignee`, 'PUT', { assignee: assignee || null })
  }, [currentId, doMutation])

  const handleAddLabel = useCallback(async (label: string) => {
    if (!currentId || !label.trim()) return
    const ok = await doMutation(`/api/forge/beads/${encodeURIComponent(currentId)}/labels`, 'POST', { label: label.trim() })
    if (ok) setNewLabel('')
  }, [currentId, doMutation])

  const handleRemoveLabel = useCallback(async (label: string) => {
    if (!currentId) return
    await doMutation(`/api/forge/beads/${encodeURIComponent(currentId)}/labels/${encodeURIComponent(label)}`, 'DELETE')
  }, [currentId, doMutation])

  const handleAddComment = useCallback(async (e: FormEvent) => {
    e.preventDefault()
    if (!currentId || !commentText.trim()) return
    const ok = await doMutation(`/api/forge/beads/${encodeURIComponent(currentId)}/comment`, 'POST', { body: commentText.trim() })
    if (ok) setCommentText('')
  }, [currentId, commentText, doMutation])

  const handleCloseBead = useCallback(async (e: FormEvent) => {
    e.preventDefault()
    if (!currentId || !closeReason.trim()) return
    const ok = await doMutation(`/api/forge/beads/${encodeURIComponent(currentId)}/close`, 'POST', { reason: closeReason.trim() })
    if (ok) {
      setCloseReason('')
      setShowCloseForm(false)
    }
  }, [currentId, closeReason, doMutation])

  const navigateToBead = useCallback((id: string) => {
    setHistoryStack(prev => ({
      baseId: beadId,
      items: [...(prev.baseId === beadId ? prev.items : []), id],
    }))
  }, [beadId])

  const navigateBack = useCallback(() => {
    setHistoryStack(prev => ({ ...prev, items: prev.items.slice(0, -1) }))
  }, [])

  const handleClose = useCallback(() => {
    setHistoryStack({ baseId: null, items: [] })
    setBead(null)
    setError(null)
    setCommentText('')
    setNewLabel('')
    setAssigneeValue('')
    setShowCloseForm(false)
    setCloseReason('')
    onClose()
  }, [onClose])

  const displayId = currentId ?? beadId ?? ''
  const canGoBack = history.length > 0
  const hasForgeReady = bead?.labels.includes('forgeReady') ?? false
  const isClosed = bead?.status === 'closed'

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      maxWidth="max-w-2xl"
      aria-labelledby="bead-detail-title"
    >
      <DialogHeader
        id="bead-detail-title"
        title={loading ? t('beadDetail.loading') : (bead?.title ?? displayId)}
        onClose={handleClose}
      />
      <DialogBody className="min-h-[200px]">
        {canGoBack && (
          <button
            type="button"
            onClick={navigateBack}
            aria-label={t('beadDetail.back')}
            className="flex items-center gap-1.5 text-sm text-gray-400 hover:text-white transition-colors mb-3"
          >
            <ArrowLeft size={14} />
            {t('beadDetail.back')}
          </button>
        )}

        {loading && (
          <div className="flex items-center justify-center py-12">
            <Loader2 size={24} className="animate-spin text-gray-400" />
          </div>
        )}

        {error && !loading && (
          <div className="flex items-center gap-2 text-red-400 py-8 justify-center">
            <AlertCircle size={18} />
            <span className="text-sm">{error}</span>
          </div>
        )}

        {bead && !loading && (
          <div className="space-y-1">
            {/* Meta row: ID, type, priority, status */}
            <div className="flex flex-wrap items-center gap-2 mb-3">
              <span className="text-xs font-mono text-cyan-400">{bead.id}</span>
              <Badge>{bead.issue_type}</Badge>

              {/* Priority dropdown */}
              <div className="relative inline-flex">
                <select
                  value={bead.priority}
                  onChange={(e) => handlePriorityChange(Number(e.target.value))}
                  disabled={mutating}
                  aria-label={t('beadDetail.actions.priorityLabel')}
                  className={`appearance-none cursor-pointer pr-5 pl-2 py-0.5 rounded text-xs font-medium border ${priorityColors[bead.priority] ?? priorityColors[4]} bg-transparent disabled:opacity-50`}
                >
                  {BEAD_PRIORITIES.map(p => (
                    <option key={p} value={p} className="bg-gray-800 text-gray-200">P{p}</option>
                  ))}
                </select>
                <ChevronDown size={10} className="absolute right-1 top-1/2 -translate-y-1/2 pointer-events-none text-gray-400" />
              </div>

              {/* Status dropdown */}
              <div className="relative inline-flex">
                <select
                  value={bead.status}
                  onChange={(e) => handleStatusChange(e.target.value)}
                  disabled={mutating}
                  aria-label={t('beadDetail.actions.statusLabel')}
                  className={`appearance-none cursor-pointer pr-5 pl-2 py-0.5 rounded text-xs font-medium border ${statusColors[bead.status] ?? statusColors.open} bg-transparent disabled:opacity-50`}
                >
                  {BEAD_STATUSES.map(s => (
                    <option key={s} value={s} className="bg-gray-800 text-gray-200">{t(`beadDetail.actions.statuses.${s}`)}</option>
                  ))}
                </select>
                <ChevronDown size={10} className="absolute right-1 top-1/2 -translate-y-1/2 pointer-events-none text-gray-400" />
              </div>

              {bead.close_reason && (
                <span className="text-xs text-gray-500 italic">{bead.close_reason}</span>
              )}
            </div>

            {/* Owner / Assignee */}
            <div className="flex flex-wrap gap-4 text-sm text-gray-400">
              <div className="flex items-center gap-1.5">
                <User size={14} />
                <span>{t('beadDetail.owner')}:</span>
                <span className="text-gray-200">{bead.owner}</span>
              </div>
              <div className="flex items-center gap-1.5">
                <User size={14} />
                <span>{t('beadDetail.assignee')}:</span>
                <input
                  type="text"
                  value={assigneeValue}
                  onChange={(e) => setAssigneeValue(e.target.value)}
                  onBlur={() => {
                    const val = assigneeValue.trim()
                    if (val !== (bead.assignee ?? '')) {
                      handleAssigneeChange(val)
                    }
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.currentTarget.blur()
                    }
                  }}
                  disabled={mutating}
                  aria-label={t('beadDetail.actions.assigneeLabel')}
                  placeholder={t('beadDetail.actions.assigneePlaceholder')}
                  className="bg-transparent border-b border-gray-600 text-gray-200 text-sm px-1 py-0 w-28 focus:outline-none focus:border-cyan-500 disabled:opacity-50 placeholder:text-gray-600"
                />
              </div>
              <div className="flex items-center gap-1.5">
                <span>{t('beadDetail.createdBy')}:</span>
                <span className="text-gray-200">{bead.created_by}</span>
              </div>
            </div>

            {/* Labels */}
            <div className="flex flex-wrap items-center gap-1.5 mt-2">
              {bead.labels.map(label => (
                <span
                  key={label}
                  className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs bg-gray-700/60 text-gray-400 border border-gray-600/40 group"
                >
                  <Tag size={10} className="shrink-0" />
                  {label}
                  <button
                    type="button"
                    onClick={() => handleRemoveLabel(label)}
                    disabled={mutating}
                    aria-label={t('beadDetail.actions.removeLabelAria', { label })}
                    className="ml-0.5 text-gray-500 hover:text-red-400 transition-colors disabled:opacity-50"
                  >
                    <X size={10} />
                  </button>
                </span>
              ))}
              {/* Add label input */}
              <div className="inline-flex items-center gap-1">
                <input
                  type="text"
                  value={newLabel}
                  onChange={(e) => setNewLabel(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      handleAddLabel(newLabel)
                    }
                  }}
                  disabled={mutating}
                  placeholder={t('beadDetail.actions.addLabelPlaceholder')}
                  aria-label={t('beadDetail.actions.addLabelAria')}
                  className="bg-transparent border-b border-gray-600 text-gray-300 text-xs px-1 py-0.5 w-24 focus:outline-none focus:border-cyan-500 disabled:opacity-50 placeholder:text-gray-600"
                />
                <button
                  type="button"
                  onClick={() => handleAddLabel(newLabel)}
                  disabled={mutating || !newLabel.trim()}
                  aria-label={t('beadDetail.actions.addLabelAria')}
                  className="text-gray-500 hover:text-cyan-400 transition-colors disabled:opacity-50"
                >
                  <Plus size={12} />
                </button>
              </div>
              {/* forgeReady quick toggle */}
              {!hasForgeReady && (
                <button
                  type="button"
                  onClick={() => handleAddLabel('forgeReady')}
                  disabled={mutating}
                  aria-label={t('beadDetail.actions.addForgeReady')}
                  className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs border border-dashed border-green-700/40 text-green-500 hover:bg-green-500/10 transition-colors disabled:opacity-50"
                >
                  <Plus size={10} />
                  forgeReady
                </button>
              )}
            </div>

            {/* Description */}
            {bead.description && (
              <Section title={t('beadDetail.description')}>
                <MarkdownContent content={bead.description} />
              </Section>
            )}

            {/* Notes */}
            {bead.notes && (
              <Section title={t('beadDetail.notes')}>
                <MarkdownContent content={bead.notes} />
              </Section>
            )}

            {/* Design */}
            {bead.design && (
              <Section title={t('beadDetail.design')}>
                <MarkdownContent content={bead.design} />
              </Section>
            )}

            {/* Acceptance Criteria */}
            {bead.acceptance_criteria && (
              <Section title={t('beadDetail.acceptanceCriteria')}>
                <MarkdownContent content={bead.acceptance_criteria} />
              </Section>
            )}

            {/* Dependencies */}
            {bead.dependencies.length > 0 && (
              <Section title={t('beadDetail.dependsOn')}>
                <div className="space-y-0">
                  {bead.dependencies.map(dep => (
                    <DependencyItem
                      key={dep.id}
                      dep={dep}
                      direction="dependency"
                      onClick={navigateToBead}
                      t={t}
                    />
                  ))}
                </div>
              </Section>
            )}

            {/* Dependents */}
            {bead.dependents.length > 0 && (
              <Section title={t('beadDetail.blocks')}>
                <div className="space-y-0">
                  {bead.dependents.map(dep => (
                    <DependencyItem
                      key={dep.id}
                      dep={dep}
                      direction="dependent"
                      onClick={navigateToBead}
                      t={t}
                    />
                  ))}
                </div>
              </Section>
            )}

            {/* Comments */}
            <Section title={t('beadDetail.comments', { count: bead.comments.length })}>
              {bead.comments.length > 0 && (
                <div className="space-y-2 max-h-64 overflow-y-auto mb-3">
                  {bead.comments.map(comment => (
                    <CommentItem key={`${comment.created_at}-${comment.author}`} comment={comment} />
                  ))}
                </div>
              )}
              {/* Add comment form */}
              <form onSubmit={handleAddComment} className="flex gap-2">
                <input
                  type="text"
                  value={commentText}
                  onChange={(e) => setCommentText(e.target.value)}
                  disabled={mutating}
                  placeholder={t('beadDetail.actions.commentPlaceholder')}
                  aria-label={t('beadDetail.actions.commentLabel')}
                  className="flex-1 bg-gray-800/60 border border-gray-700/50 rounded px-3 py-1.5 text-sm text-gray-200 focus:outline-none focus:border-cyan-500 disabled:opacity-50 placeholder:text-gray-600"
                />
                <button
                  type="submit"
                  disabled={mutating || !commentText.trim()}
                  aria-label={t('beadDetail.actions.commentSubmit')}
                  className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded transition-colors disabled:opacity-50 flex items-center gap-1.5"
                >
                  <Send size={14} />
                </button>
              </form>
            </Section>

            {/* Timestamps */}
            <div className="flex flex-wrap gap-4 mt-4 pt-3 border-t border-gray-700/50 text-xs text-gray-500">
              <div className="flex items-center gap-1.5">
                <Calendar size={12} />
                <span>{t('beadDetail.created')}:</span>
                <span>{formatDateTime(bead.created_at, { dateStyle: 'medium', timeStyle: 'short' })}</span>
              </div>
              <div className="flex items-center gap-1.5">
                <Calendar size={12} />
                <span>{t('beadDetail.updated')}:</span>
                <span>{formatDateTime(bead.updated_at, { dateStyle: 'medium', timeStyle: 'short' })}</span>
              </div>
              {bead.closed_at && (
                <div className="flex items-center gap-1.5">
                  <Calendar size={12} />
                  <span>{t('beadDetail.closed')}:</span>
                  <span>{formatDateTime(bead.closed_at, { dateStyle: 'medium', timeStyle: 'short' })}</span>
                </div>
              )}
            </div>

            {/* Close bead action */}
            {!isClosed && (
              <div className="mt-4 pt-3 border-t border-gray-700/50">
                {!showCloseForm ? (
                  <button
                    type="button"
                    onClick={() => setShowCloseForm(true)}
                    disabled={mutating}
                    className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-red-400 border border-red-700/40 rounded hover:bg-red-500/10 transition-colors disabled:opacity-50"
                  >
                    <XCircle size={14} />
                    {t('beadDetail.actions.closeBead')}
                  </button>
                ) : (
                  <form onSubmit={handleCloseBead} className="space-y-2">
                    <label className="block text-sm text-gray-400">{t('beadDetail.actions.closeReasonLabel')}</label>
                    <input
                      type="text"
                      value={closeReason}
                      onChange={(e) => setCloseReason(e.target.value)}
                      disabled={mutating}
                      placeholder={t('beadDetail.actions.closeReasonPlaceholder')}
                      aria-label={t('beadDetail.actions.closeReasonLabel')}
                      className="w-full bg-gray-800/60 border border-gray-700/50 rounded px-3 py-1.5 text-sm text-gray-200 focus:outline-none focus:border-red-500 disabled:opacity-50 placeholder:text-gray-600"
                      autoFocus
                    />
                    <div className="flex gap-2">
                      <button
                        type="submit"
                        disabled={mutating || !closeReason.trim()}
                        className="px-3 py-1.5 bg-red-700 hover:bg-red-600 text-white text-sm rounded transition-colors disabled:opacity-50"
                      >
                        {t('beadDetail.actions.confirmClose')}
                      </button>
                      <button
                        type="button"
                        onClick={() => { setShowCloseForm(false); setCloseReason('') }}
                        disabled={mutating}
                        className="px-3 py-1.5 text-sm text-gray-400 hover:text-white transition-colors disabled:opacity-50"
                      >
                        {t('beadDetail.actions.cancel')}
                      </button>
                    </div>
                  </form>
                )}
              </div>
            )}
          </div>
        )}
      </DialogBody>
    </Dialog>
  )
}
