import { Sparkles } from 'lucide-react'
import { isAutoTag, isAITag, displayTag, AUTO_TAG_TOOLTIP, AI_TAG_TOOLTIP } from '../tags'

interface TagBadgeProps {
  tag: string
}

export default function TagBadge({ tag }: TagBadgeProps) {
  const isAuto = isAutoTag(tag)
  const isAI = isAITag(tag)
  return (
    <span
      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs ${
        isAI
          ? 'bg-purple-900/50 text-purple-300 border border-purple-700/50'
          : isAuto
            ? 'bg-blue-900/50 text-blue-300 border border-blue-700/50'
            : 'bg-gray-700 text-gray-400'
      }`}
      title={isAI ? AI_TAG_TOOLTIP : isAuto ? AUTO_TAG_TOOLTIP : undefined}
    >
      {(isAuto || isAI) && <Sparkles size={10} />}
      {displayTag(tag)}
    </span>
  )
}
