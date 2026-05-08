import type { KeyboardEvent, ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'

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
  expanded: boolean
  onToggleExpanded: (next: boolean) => void
  actionsSlot?: ReactNode
}

export const NEW_PAGE_SLUG = '__new_page__'

function typeBadgeClass(type: SuggestionType): string {
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

function formatLocalDate(dateStr: string, language: string, options?: Intl.DateTimeFormatOptions): string {
  const d = new Date(dateStr)
  const opts: Intl.DateTimeFormatOptions = { ...options }
  if (language === 'th' || language.startsWith('th-')) {
    opts.calendar = 'gregory'
  }
  return d.toLocaleDateString(language, opts)
}

export function SuggestionCard({
  suggestion,
  expanded,
  onToggleExpanded,
  actionsSlot,
}: SuggestionCardProps) {
  const { t, i18n } = useTranslation('suggestions')

  const body = suggestion.body ?? ''
  const bodyId = `suggestion-body-${suggestion.id}`

  const typeLabel = t(`card.types.${suggestion.type}`)
  const sizeLabel = t(`card.sizes.${suggestion.size}`)
  const sourceLabel = t(`card.source.${suggestion.source}`)
  const generatedDate = formatLocalDate(suggestion.generated_at, i18n.language, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })

  function handleHeaderKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onToggleExpanded(!expanded)
    }
  }

  return (
    <article
      data-suggestion-id={suggestion.id}
      data-testid={`suggestion-card-${suggestion.id}`}
      data-expanded={expanded ? 'true' : 'false'}
      className="rounded-lg border border-gray-700 bg-gray-800 text-sm text-gray-200"
    >
      <div
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        aria-controls={bodyId}
        aria-label={t('card.toggleAria', { title: suggestion.title })}
        onClick={() => onToggleExpanded(!expanded)}
        onKeyDown={handleHeaderKeyDown}
        data-testid={`suggestion-card-header-${suggestion.id}`}
        className="flex w-full flex-wrap items-start gap-2 rounded-lg p-4 text-left cursor-pointer hover:bg-gray-800/60 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/60"
      >
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            {suggestion.page_slug === NEW_PAGE_SLUG ? (
              <span
                data-testid="new-page-chip"
                className="inline-flex max-w-full items-center rounded-full border border-purple-500/40 bg-purple-500/15 px-2 py-0.5 text-xs font-medium text-purple-300 break-all"
              >
                {t('card.newPageChip')}
              </span>
            ) : (
              <span className="inline-flex max-w-full items-center rounded-full border border-gray-600 bg-gray-700/60 px-2 py-0.5 text-xs font-medium text-gray-300 break-all">
                {suggestion.page_slug}
              </span>
            )}
            <span
              className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${typeBadgeClass(suggestion.type)}`}
            >
              {typeLabel}
            </span>
            <span className="inline-flex items-center rounded-full border border-gray-600 bg-gray-700/60 px-2 py-0.5 text-xs font-semibold text-gray-200">
              {sizeLabel}
            </span>
          </div>
          <h3 className="font-medium text-white break-words">{suggestion.title}</h3>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-400">
            <span>{sourceLabel}</span>
            <span aria-hidden="true">·</span>
            <time dateTime={suggestion.generated_at}>{generatedDate}</time>
          </div>
        </div>
        <span
          aria-hidden="true"
          className="ml-auto inline-flex h-11 w-11 shrink-0 items-center justify-center text-gray-400"
        >
          {expanded ? <ChevronDown size={20} /> : <ChevronRight size={20} />}
        </span>
      </div>

      {expanded && (
        <div id={bodyId} className="space-y-3 border-t border-gray-700/60 px-4 py-3">
          {body && (
            <p className="whitespace-pre-wrap break-words text-sm text-gray-300">
              {body}
            </p>
          )}
          {actionsSlot && <div>{actionsSlot}</div>}
        </div>
      )}
    </article>
  )
}
