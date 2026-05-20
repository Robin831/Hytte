import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

// PICKER_EMOJIS is the fixed grid shown when the user opens the picker. The
// list is intentionally short so the popover stays compact on mobile and we
// don't ship a megabyte emoji search index. Server-side validation accepts
// any single emoji grapheme so users can still react with anything they can
// type via their OS keyboard from the textarea path (future work).
const PICKER_EMOJIS = [
  '👍', '❤️', '🎉', '😂',
  '😮', '😢', '🙏', '🔥',
  '👏', '👀', '💯', '🚀',
  '😡', '🤔', '🥳', '😴',
]

interface ReactionPickerProps {
  onPick: (emoji: string) => void
  onClose: () => void
}

export default function ReactionPicker({ onPick, onClose }: ReactionPickerProps) {
  const { t } = useTranslation('familyChat')
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    containerRef.current?.focus()
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onClose()
      }
    }
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('mousedown', handleClickOutside)
    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
      document.removeEventListener('keydown', handleKey)
    }
  }, [onClose])

  return (
    <div
      ref={containerRef}
      role="dialog"
      aria-modal={true}
      aria-label={t('reactions.pickerLabel')}
      tabIndex={-1}
      className="absolute z-30 bottom-full mb-2 right-0 bg-gray-800 border border-gray-700 rounded-lg shadow-lg p-2 grid grid-cols-4 gap-1 outline-none"
      data-testid="reaction-picker"
    >
      {PICKER_EMOJIS.map(emoji => (
        <button
          key={emoji}
          type="button"
          onClick={() => onPick(emoji)}
          className="w-9 h-9 flex items-center justify-center text-xl rounded-md hover:bg-gray-700 cursor-pointer"
          aria-label={emoji}
        >
          {emoji}
        </button>
      ))}
    </div>
  )
}
