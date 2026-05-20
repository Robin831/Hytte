import { useTranslation } from 'react-i18next'
import type { ReactionMap } from './api'

interface ReactionChipsProps {
  reactions: ReactionMap | undefined
  onToggle: (emoji: string, currentlyMine: boolean) => void
}

export default function ReactionChips({ reactions, onToggle }: ReactionChipsProps) {
  const { t } = useTranslation('familyChat')
  if (!reactions) return null
  // Sort emoji keys for stable display order across renders. The backend
  // returns the map keyed by emoji string; iteration order over a JS Map
  // mirrors insertion order, which is non-deterministic across reloads.
  const emojis = Object.keys(reactions).sort()
  if (emojis.length === 0) return null
  return (
    <div className="flex flex-wrap gap-1 mt-1" data-testid="reaction-chips">
      {emojis.map(emoji => {
        const bucket = reactions[emoji]
        if (!bucket || bucket.count <= 0) return null
        const mine = !!bucket.me
        return (
          <button
            key={emoji}
            type="button"
            onClick={() => onToggle(emoji, mine)}
            aria-label={mine ? t('reactions.remove', { emoji }) : t('reactions.add', { emoji })}
            aria-pressed={mine}
            className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full text-xs cursor-pointer border ${
              mine
                ? 'bg-blue-500/20 border-blue-400/60 text-blue-100'
                : 'bg-gray-700/60 border-gray-600 text-gray-200 hover:bg-gray-700'
            }`}
            data-testid={`reaction-chip-${emoji}`}
          >
            <span aria-hidden="true">{emoji}</span>
            <span>{bucket.count}</span>
          </button>
        )
      })}
    </div>
  )
}
