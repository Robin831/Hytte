import { useState, useEffect, useCallback, type AnchorHTMLAttributes, type ReactNode } from 'react'
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
  closed: 'bg-gray-500/20 text-gray-400 border-gray-600/30',
  blocked: 'bg-red-500/20 text-red-400 border-red-700/30',
}

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

export default function BeadDetailModal({ open, onClose, beadId }: BeadDetailModalProps) {
  const { t } = useTranslation('forge')

  const [bead, setBead] = useState<BeadDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Track navigation history alongside the beadId it belongs to, so that
  // when beadId changes the stack is automatically treated as empty (no effect needed).
  const [historyStack, setHistoryStack] = useState<{ baseId: string | null; items: string[] }>({
    baseId: beadId,
    items: [],
  })

  // Effective history: stale if the beadId the stack was built for no longer matches.
  const history = historyStack.baseId === beadId ? historyStack.items : []
  const currentId = history.length > 0 ? history[history.length - 1] : beadId

  // Fetch bead detail whenever currentId changes
  useEffect(() => {
    if (!open || !currentId) return

    const controller = new AbortController()
    let cancelled = false

    async function fetchBead() {
      setLoading(true)
      setError(null)
      try {
        const res = await fetch(`/api/forge/beads/${encodeURIComponent(currentId!)}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (cancelled) return
        if (!res.ok) {
          if (res.status === 404) {
            setError(t('beadDetail.errors.notFound'))
          } else {
            setError(t('beadDetail.errors.fetchFailed'))
          }
          setBead(null)
          return
        }
        const data: BeadDetail = await res.json()
        if (!cancelled) {
          setBead(data)
        }
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        if (!cancelled) {
          setError(t('beadDetail.errors.fetchFailed'))
          setBead(null)
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }

    fetchBead()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [open, currentId, t])

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
    onClose()
  }, [onClose])

  const displayId = currentId ?? beadId ?? ''
  const canGoBack = history.length > 0

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
              <Badge className={priorityColors[bead.priority] ?? priorityColors[4]}>
                P{bead.priority}
              </Badge>
              <Badge className={statusColors[bead.status] ?? statusColors.open}>
                {bead.status}
              </Badge>
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
              {bead.assignee && (
                <div className="flex items-center gap-1.5">
                  <User size={14} />
                  <span>{t('beadDetail.assignee')}:</span>
                  <span className="text-gray-200">{bead.assignee}</span>
                </div>
              )}
              <div className="flex items-center gap-1.5">
                <span>{t('beadDetail.createdBy')}:</span>
                <span className="text-gray-200">{bead.created_by}</span>
              </div>
            </div>

            {/* Labels */}
            {bead.labels.length > 0 && (
              <div className="flex flex-wrap gap-1.5 mt-2">
                {bead.labels.map(label => (
                  <span
                    key={label}
                    className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs bg-gray-700/60 text-gray-400 border border-gray-600/40"
                  >
                    <Tag size={10} className="shrink-0" />
                    {label}
                  </span>
                ))}
              </div>
            )}

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
            {bead.comments.length > 0 && (
              <Section title={t('beadDetail.comments', { count: bead.comments.length })}>
                <div className="space-y-2 max-h-64 overflow-y-auto">
                  {bead.comments.map(comment => (
                    <CommentItem key={`${comment.created_at}-${comment.author}`} comment={comment} />
                  ))}
                </div>
              </Section>
            )}

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
          </div>
        )}
      </DialogBody>
    </Dialog>
  )
}
