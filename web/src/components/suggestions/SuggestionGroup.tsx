import type { KeyboardEvent, ReactNode } from 'react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'

export interface SuggestionGroupProps {
  groupKey: string
  pageTitle: string
  count: number
  expanded: boolean
  onToggle: (next: boolean) => void
  children: ReactNode
}

export function SuggestionGroup({
  groupKey,
  pageTitle,
  count,
  expanded,
  onToggle,
  children,
}: SuggestionGroupProps) {
  const { t } = useTranslation('suggestions')
  const contentId = useId()

  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onToggle(!expanded)
    }
  }

  const countLabel = t('groups.count', { count })

  return (
    <section
      data-testid={`suggestion-group-${groupKey}`}
      data-expanded={expanded ? 'true' : 'false'}
      className="space-y-2"
    >
      <div
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        aria-controls={contentId}
        aria-label={t('groups.toggleAria', { page: pageTitle })}
        onClick={() => onToggle(!expanded)}
        onKeyDown={handleKeyDown}
        data-testid={`suggestion-group-header-${groupKey}`}
        className="flex w-full flex-wrap items-center gap-2 rounded-md border border-gray-800 bg-gray-900/40 px-3 py-2 text-left cursor-pointer hover:bg-gray-900/60 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/60"
      >
        <span
          aria-hidden="true"
          className="inline-flex h-6 w-6 shrink-0 items-center justify-center text-gray-400"
        >
          {expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
        </span>
        <h2 className="min-w-0 flex-1 text-sm font-semibold text-gray-100 break-words">
          {pageTitle}
        </h2>
        <span className="shrink-0 rounded-full border border-gray-700 bg-gray-800 px-2 py-0.5 text-xs font-medium text-gray-300">
          {countLabel}
        </span>
      </div>
      {expanded && (
        <div id={contentId} className="space-y-3 pl-2 sm:pl-4">
          {children}
        </div>
      )}
    </section>
  )
}
