import { useState, type AnchorHTMLAttributes, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Suggestion } from './SuggestionCard'

export interface SuggestionActionsProps {
  suggestion: Suggestion
  onPlanned?: () => void
  onRejected?: () => void
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

export function SuggestionActions({ suggestion, onPlanned, onRejected }: SuggestionActionsProps) {
  if (suggestion.status === 'pending') {
    return <PendingActions suggestion={suggestion} onPlanned={onPlanned} onRejected={onRejected} />
  }
  if (suggestion.status === 'planned') {
    return <PlannedActions suggestion={suggestion} />
  }
  return null
}

function PendingActions({
  suggestion,
  onPlanned,
  onRejected,
}: SuggestionActionsProps) {
  const { t } = useTranslation('common')
  const [feedback, setFeedback] = useState('')
  const [submitting, setSubmitting] = useState<'plan' | 'reject' | null>(null)
  const [error, setError] = useState<string | null>(null)

  const isBusy = submitting !== null
  const feedbackId = `suggestion-feedback-${suggestion.id}`

  async function handleReject() {
    if (isBusy) return
    setSubmitting('reject')
    setError(null)
    try {
      const res = await fetch(`/api/suggestions/${suggestion.id}/reject`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        throw new Error(t('suggestions.errors.rejectFailed'))
      }
      onRejected?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('suggestions.errors.rejectFailed'))
    } finally {
      setSubmitting(null)
    }
  }

  async function handlePlan(e: FormEvent) {
    e.preventDefault()
    if (isBusy) return
    setSubmitting('plan')
    setError(null)
    try {
      const trimmed = feedback.trim()
      const res = await fetch(`/api/suggestions/${suggestion.id}/plan`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(trimmed ? { feedback: trimmed } : {}),
      })
      if (!res.ok) {
        throw new Error(t('suggestions.errors.planFailed'))
      }
      setFeedback('')
      onPlanned?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('suggestions.errors.planFailed'))
    } finally {
      setSubmitting(null)
    }
  }

  return (
    <form onSubmit={handlePlan} className="w-full space-y-2">
      <label htmlFor={feedbackId} className="block text-xs font-medium text-gray-400">
        {t('suggestions.actions.feedbackLabel')}
      </label>
      <textarea
        id={feedbackId}
        value={feedback}
        onChange={e => setFeedback(e.target.value)}
        disabled={isBusy}
        placeholder={t('suggestions.actions.feedbackPlaceholder')}
        rows={3}
        className="w-full rounded-md border border-gray-700 bg-gray-900/60 px-3 py-2 text-sm text-gray-200 placeholder:text-gray-500 focus:outline-none focus:border-blue-500 disabled:opacity-50"
      />
      <p className="text-xs text-gray-500">{t('suggestions.actions.planHint')}</p>
      {error && (
        <p
          role="alert"
          data-testid={`suggestion-${suggestion.id}-action-error`}
          className="text-xs text-red-300"
        >
          {error}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <button
          type="submit"
          disabled={isBusy}
          className="inline-flex items-center gap-2 rounded-lg border border-blue-500/40 bg-blue-500/20 px-3 py-1.5 text-xs font-medium text-blue-300 hover:bg-blue-500/30 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {submitting === 'plan' && (
            <Loader2 size={14} className="animate-spin" aria-hidden={true} />
          )}
          <span>
            {submitting === 'plan'
              ? t('suggestions.actions.planning')
              : t('suggestions.actions.planIt')}
          </span>
        </button>
        <button
          type="button"
          onClick={handleReject}
          disabled={isBusy}
          className="inline-flex items-center gap-2 rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 hover:bg-red-500/20 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {submitting === 'reject' && (
            <Loader2 size={14} className="animate-spin" aria-hidden={true} />
          )}
          <span>
            {submitting === 'reject'
              ? t('suggestions.actions.rejecting')
              : t('suggestions.actions.reject')}
          </span>
        </button>
      </div>
    </form>
  )
}

function PlannedActions({ suggestion }: { suggestion: Suggestion }) {
  const { t } = useTranslation('common')
  const plan = suggestion.plan ?? ''
  const tooltip = t('suggestions.actions.createBeadDisabledTooltip')

  return (
    <div className="w-full space-y-3">
      {plan ? (
        <div
          className="prose prose-invert prose-sm max-w-none rounded-md border border-gray-700/60 bg-gray-900/40 px-3 py-2"
          data-testid={`suggestion-${suggestion.id}-plan`}
        >
          <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
            {plan}
          </ReactMarkdown>
        </div>
      ) : (
        <p className="text-xs italic text-gray-500">
          {t('suggestions.actions.noPlanYet')}
        </p>
      )}
      <button
        type="button"
        disabled
        title={tooltip}
        aria-label={tooltip}
        className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-800/60 px-3 py-1.5 text-xs font-medium text-gray-400 cursor-not-allowed opacity-70"
      >
        {t('suggestions.actions.createBead')}
      </button>
    </div>
  )
}
