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
  onPlan: (id: number) => void
  onReject: (id: number) => void
}

export function SuggestionCard({ suggestion }: SuggestionCardProps) {
  return (
    <article
      data-suggestion-id={suggestion.id}
      className="rounded-lg border border-gray-700 bg-gray-800 p-4 text-sm text-gray-200"
    >
      <h3 className="font-medium text-white break-words">{suggestion.title}</h3>
    </article>
  )
}
