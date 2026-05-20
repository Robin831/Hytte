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

const TRIGGER_GAP = 8
const VIEWPORT_MARGIN = 8

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
