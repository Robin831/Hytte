import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { formatDate } from '../../utils/formatDate'

export type SuggestionStatus = 'pending' | 'planned' | 'rejected' | 'bead_created'

export type SuggestionSource = 'claude' | 'user'

export type SuggestionType = 'addition' | 'bugfix' | 'improvement' | 'refactor' | 'new_page'

export type SuggestionSize = 's' | 'm' | 'l'

export interface Suggestion {
  id: number
  user_id: number
  generated_at: string
  page_slug: string
  source: SuggestionSource
  type: SuggestionType
  size: SuggestionSize
  title: string
  body: string
  status: SuggestionStatus
  feedback?: string
  plan?: string
  bead_id?: string
  rejected_at?: string | null
  planned_at?: string | null
  bead_created_at?: string | null
}

export interface SuggestionCardProps {
  suggestion: Suggestion
  actionsSlot?: ReactNode
}

export function typeBadgeClass(type: SuggestionType): string {
  switch (type) {
    case 'addition':
      return 'border-green-500/40 bg-green-500/15 text-green-300'
    case 'bugfix':
      return 'border-red-500/40 bg-red-500/15 text-red-300'
    case 'improvement':
      return 'border-blue-500/40 bg-blue-500/15 text-blue-300'
    case 'refactor':
      return 'border-amber-500/40 bg-amber-500/15 text-amber-300'
    case 'new_page':
      return 'border-purple-500/40 bg-purple-500/15 text-purple-300'
  }
}

const COLLAPSED_BODY_LIMIT = 240

export function SuggestionCard({ suggestion, actionsSlot }: SuggestionCardProps) {
  const { t } = useTranslation('common')
  const [expanded, setExpanded] = useState(false)

  const body = suggestion.body ?? ''
  const isLong = body.length > COLLAPSED_BODY_LIMIT
  const visibleBody = isLong && !expanded
    ? `${body.slice(0, COLLAPSED_BODY_LIMIT).trimEnd()}…`
    : body

  const typeLabel = t(`suggestions.card.types.${suggestion.type}`)
  const sizeLabel = t(`suggestions.card.sizes.${suggestion.size}`)
  const sourceLabel = t(`suggestions.card.source.${suggestion.source}`)
  const generatedDate = formatDate(suggestion.generated_at, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })

  return (
    <article
      data-suggestion-id={suggestion.id}
      className="rounded-lg border border-gray-700 bg-gray-800 p-4 text-sm text-gray-200"
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="inline-flex max-w-full items-center rounded-full border border-gray-600 bg-gray-700/60 px-2 py-0.5 text-xs font-medium text-gray-300 break-all">
          {suggestion.page_slug}
        </span>
        <span
          className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${typeBadgeClass(suggestion.type)}`}
        >
          {typeLabel}
        </span>
        <span className="inline-flex items-center rounded-full border border-gray-600 bg-gray-700/60 px-2 py-0.5 text-xs font-semibold text-gray-200">
          {sizeLabel}
        </span>
      </div>

      <h3 className="mt-3 font-medium text-white break-words">{suggestion.title}</h3>

      {body && (
        <div className="mt-2">
          <p className="whitespace-pre-wrap break-words text-sm text-gray-300">
            {visibleBody}
          </p>
          {isLong && (
            <button
              type="button"
              onClick={() => setExpanded(prev => !prev)}
              aria-expanded={expanded}
              className="mt-2 inline-flex items-center gap-1 text-xs font-medium text-blue-300 hover:text-blue-200"
            >
              {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
              <span>
                {expanded ? t('suggestions.card.showLess') : t('suggestions.card.showMore')}
              </span>
            </button>
          )}
        </div>
      )}

      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-400">
        <span>{sourceLabel}</span>
        <span aria-hidden="true">·</span>
        <time dateTime={suggestion.generated_at}>{generatedDate}</time>
      </div>

      {actionsSlot && (
        <div className="mt-3 flex flex-wrap gap-2 border-t border-gray-700/60 pt-3">
          {actionsSlot}
        </div>
      )}
    </article>
  )
}
