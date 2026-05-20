import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'

// PICKER_EMOJIS is the fixed grid shown when the user opens the picker. The
// list is intentionally short so the popover stays compact on mobile and we
// don't ship a megabyte emoji search index. Server-side validation accepts
// any single emoji grapheme so users can still react with anything they can
// type via their OS keyboard from the textarea path (future work).
const PICKER_EMOJIS = [
  '👍', '❤️', '🎉', '😂', '😮', '😢',
  '🙏', '🔥', '👏', '👀', '💯', '🚀',
  '😡', '🤔', '🥳', '😴',
]

// Sensible first-paint defaults until the portal node has been measured. The
// real size is read back via getBoundingClientRect in a second layout effect.
const DEFAULT_PICKER_W = 320
const DEFAULT_PICKER_H = 180

// Gap between trigger and popup, and minimum margin from viewport edges.
const TRIGGER_GAP = 8
const VIEWPORT_MARGIN = 8

export type PickerPlacement = 'above' | 'below'
export type PickerAlign = 'left' | 'right'

export interface PickerPosition {
  top: number
  left: number
  placement: PickerPlacement
  align: PickerAlign
}

export interface Viewport {
  w: number
  h: number
}

export interface PickerSize {
  w: number
  h: number
}

// computePickerPosition picks the popover placement around a trigger rect.
// Vertical: prefer above if there's room (>= pickerH + TRIGGER_GAP), else
// below. Horizontal: right-align when the trigger center is in the right half
// of the viewport, else left-align. Final top/left are clamped so the popup
// stays within the viewport (with a small margin), which matters on tiny
// viewports where the picker doesn't really fit either side.
export function computePickerPosition(
  triggerRect: DOMRect,
  viewport: Viewport,
  pickerSize: PickerSize,
): PickerPosition {
  const fitsAbove = triggerRect.top >= pickerSize.h + TRIGGER_GAP
  const placement: PickerPlacement = fitsAbove ? 'above' : 'below'

  const rawTop = placement === 'above'
    ? triggerRect.top - pickerSize.h - TRIGGER_GAP
    : triggerRect.bottom + TRIGGER_GAP

  const triggerCenterX = triggerRect.left + triggerRect.width / 2
  const align: PickerAlign = triggerCenterX >= viewport.w / 2 ? 'right' : 'left'

  const rawLeft = align === 'right'
    ? triggerRect.right - pickerSize.w
    : triggerRect.left

  const maxLeft = Math.max(VIEWPORT_MARGIN, viewport.w - pickerSize.w - VIEWPORT_MARGIN)
  const maxTop = Math.max(VIEWPORT_MARGIN, viewport.h - pickerSize.h - VIEWPORT_MARGIN)
  const left = Math.min(Math.max(rawLeft, VIEWPORT_MARGIN), maxLeft)
  const top = Math.min(Math.max(rawTop, VIEWPORT_MARGIN), maxTop)

  return { top, left, placement, align }
}

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
