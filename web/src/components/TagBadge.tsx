import { Sparkles } from 'lucide-react'

interface TagBadgeProps {
  tag: string
}

export default function TagBadge({ tag }: TagBadgeProps) {
  const isAuto = tag.startsWith('auto:')
  return (
    <span
      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs ${
        isAuto
          ? 'bg-blue-900/50 text-blue-300 border border-blue-700/50'
          : 'bg-gray-700 text-gray-400'
      }`}
      title={isAuto ? 'Auto-generated from workout structure' : undefined}
    >
      {isAuto && <Sparkles size={10} />}
      {isAuto ? tag.slice(5) : tag}
    </span>
  )
}
