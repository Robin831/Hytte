import { Sparkles } from 'lucide-react'
import { isAutoTag, displayTag, AUTO_TAG_TOOLTIP } from '../tags'

interface TagBadgeProps {
  tag: string
}

export default function TagBadge({ tag }: TagBadgeProps) {
  const isAuto = isAutoTag(tag)
  return (
    <span
      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs ${
        isAuto
          ? 'bg-blue-900/50 text-blue-300 border border-blue-700/50'
          : 'bg-gray-700 text-gray-400'
      }`}
      title={isAuto ? AUTO_TAG_TOOLTIP : undefined}
    >
      {isAuto && <Sparkles size={10} />}
      {displayTag(tag)}
    </span>
  )
}
