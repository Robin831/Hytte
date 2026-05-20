import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { computePickerPosition, type PickerPosition } from './pickerPosition'

const PICKER_EMOJIS = [
  '👍', '❤️', '🎉', '😂', '😮', '😢',
  '🙏', '🔥', '👏', '👀', '💯', '🚀',
  '😡', '🤔', '🥳', '😴',
]

const DEFAULT_PICKER_W = 320
const DEFAULT_PICKER_H = 180

interface ReactionPickerProps {
  onPick: (emoji: string) => void
  onClose: () => void
  triggerRef: RefObject<HTMLElement | null>
}

export default function ReactionPicker({ onPick, onClose, triggerRef }: ReactionPickerProps) {
  const { t } = useTranslation('familyChat')
  const containerRef = useRef<HTMLDivElement>(null)
  const [position, setPosition] = useState<PickerPosition | null>(null)

  // First layout pass: compute a position using default picker dimensions so
  // the popup paints in roughly the right place on the first frame and we
  // don't flash at (0,0). Read the trigger via the parent-supplied ref.
  useLayoutEffect(() => {
    const trigger = triggerRef.current
    if (!trigger) return
    const viewport = { w: window.innerWidth, h: window.innerHeight }
    const pickerSize = { w: DEFAULT_PICKER_W, h: DEFAULT_PICKER_H }
    setPosition(computePickerPosition(trigger.getBoundingClientRect(), viewport, pickerSize))
  }, [triggerRef])

  // Second layout pass: measure the actual rendered popup and recompute the
  // position so the clamping uses real dimensions. This is what catches the
  // case where the popup is shorter/taller than the default and would
  // otherwise have a stale top/left.
  useLayoutEffect(() => {
    const trigger = triggerRef.current
    const popup = containerRef.current
    if (!trigger || !popup) return
    const rect = popup.getBoundingClientRect()
    const viewport = { w: window.innerWidth, h: window.innerHeight }
    const pickerSize = { w: rect.width, h: rect.height }
    const next = computePickerPosition(trigger.getBoundingClientRect(), viewport, pickerSize)
    setPosition(prev => {
      if (prev && prev.top === next.top && prev.left === next.left
        && prev.placement === next.placement && prev.align === next.align) {
        return prev
      }
      return next
    })
  }, [triggerRef])

  useEffect(() => {
    containerRef.current?.focus()
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as Node
      if (containerRef.current && containerRef.current.contains(target)) return
      // Clicks on the trigger itself shouldn't fall through onClose, because
      // the trigger's own click handler is responsible for toggling the
      // picker. Without this guard the picker would close, then immediately
      // re-open on the same click.
      if (triggerRef.current && triggerRef.current.contains(target)) return
      onClose()
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
  }, [onClose, triggerRef])

  if (typeof document === 'undefined') return null

  const style: React.CSSProperties = position
    ? { position: 'fixed', top: position.top, left: position.left }
    // First paint before the layout effect runs — keep the popup off-screen
    // so it can't flash in the wrong place. The layout effect runs
    // synchronously before the browser paints, so this style is almost never
    // observable.
    : { position: 'fixed', top: -9999, left: -9999 }

  const popup = (
    <div
      ref={containerRef}
      role="dialog"
      aria-modal={true}
      aria-label={t('reactions.pickerLabel')}
      tabIndex={-1}
      style={style}
      className="z-[60] bg-gray-800 border border-gray-700 rounded-lg shadow-xl p-3 grid grid-cols-6 gap-2 outline-none"
      data-testid="reaction-picker"
    >
      {PICKER_EMOJIS.map(emoji => (
        <button
          key={emoji}
          type="button"
          onClick={() => onPick(emoji)}
          className="w-11 h-11 flex items-center justify-center text-2xl rounded-md hover:bg-gray-700 cursor-pointer"
          aria-label={emoji}
        >
          {emoji}
        </button>
      ))}
    </div>
  )

  return createPortal(popup, document.body)
}
