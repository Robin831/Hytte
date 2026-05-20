// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createRef } from 'react'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import ReactionPicker from './ReactionPicker'
import { computePickerPosition } from './pickerPosition'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

function rect(left: number, top: number, width = 24, height = 24): DOMRect {
  return {
    left,
    top,
    width,
    height,
    right: left + width,
    bottom: top + height,
    x: left,
    y: top,
    toJSON: () => ({}),
  } as DOMRect
}

describe('computePickerPosition', () => {
  const viewport = { w: 800, h: 600 }
  const pickerSize = { w: 320, h: 180 }

  it('places below + left-aligned when trigger is near the top-left', () => {
    const pos = computePickerPosition(rect(20, 20), viewport, pickerSize)
    expect(pos.placement).toBe('below')
    expect(pos.align).toBe('left')
    expect(pos.top).toBe(20 + 24 + 8)
    expect(pos.left).toBe(20)
  })

  it('places below + right-aligned when trigger is near the top-right', () => {
    const pos = computePickerPosition(rect(760, 20), viewport, pickerSize)
    expect(pos.placement).toBe('below')
    expect(pos.align).toBe('right')
    expect(pos.top).toBe(20 + 24 + 8)
    // right-aligned: left = triggerRect.right - pickerW, clamped to maxLeft.
    const expectedLeft = Math.min(760 + 24 - 320, viewport.w - pickerSize.w - 8)
    expect(pos.left).toBe(expectedLeft)
  })

  it('places above + left-aligned when trigger is near the bottom-left', () => {
    const pos = computePickerPosition(rect(20, 560), viewport, pickerSize)
    expect(pos.placement).toBe('above')
    expect(pos.align).toBe('left')
    expect(pos.top).toBe(560 - 180 - 8)
    expect(pos.left).toBe(20)
  })

  it('places above + right-aligned when trigger is near the bottom-right', () => {
    const pos = computePickerPosition(rect(760, 560), viewport, pickerSize)
    expect(pos.placement).toBe('above')
    expect(pos.align).toBe('right')
    expect(pos.top).toBe(560 - 180 - 8)
    const expectedLeft = Math.min(760 + 24 - 320, viewport.w - pickerSize.w - 8)
    expect(pos.left).toBe(expectedLeft)
  })

  it('falls back to below + clamps when the picker fits nowhere', () => {
    const tinyViewport = { w: 200, h: 200 }
    const bigPicker = { w: 320, h: 180 }
    // Trigger near the top with no room above; picker is also wider than
    // viewport so left clamps to the left margin.
    const pos = computePickerPosition(rect(10, 10), tinyViewport, bigPicker)
    expect(pos.placement).toBe('below')
    // Top clamped to the maxTop = max(8, h - pickerH - 8) = max(8, 200-180-8)=12.
    // raw top is 10+24+8 = 42, clamped down to 12.
    expect(pos.top).toBe(12)
    // Left clamped to the left margin (8) because picker is wider than viewport.
    expect(pos.left).toBe(8)
  })
})

describe('ReactionPicker', () => {
  let trigger: HTMLButtonElement

  beforeEach(() => {
    trigger = document.createElement('button')
    trigger.getBoundingClientRect = () => rect(100, 400, 24, 24)
    document.body.appendChild(trigger)
    // happy-dom doesn't always set window dimensions for layout — pin them
    // explicitly so computePickerPosition reads stable values.
    Object.defineProperty(window, 'innerWidth', { value: 800, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: 600, configurable: true })
  })

  afterEach(() => {
    cleanup()
    trigger.remove()
  })

  it('renders the picker into document.body via a portal with the new grid classes', () => {
    const ref = createRef<HTMLElement | null>()
    ;(ref as { current: HTMLElement | null }).current = trigger

    const { container } = render(
      <ReactionPicker onPick={() => {}} onClose={() => {}} triggerRef={ref} />,
    )

    // The portal target is document.body, NOT the React render container.
    const inContainer = container.querySelector('[data-testid="reaction-picker"]')
    expect(inContainer).toBeNull()

    const picker = screen.getByTestId('reaction-picker')
    expect(picker).toBeInTheDocument()
    expect(picker.className).toContain('grid-cols-6')
    expect(picker.className).toContain('gap-2')
    expect(picker.className).toContain('p-3')

    const cells = picker.querySelectorAll('button')
    expect(cells.length).toBe(16)
    cells.forEach(cell => {
      expect(cell.className).toContain('w-11')
      expect(cell.className).toContain('h-11')
      expect(cell.className).toContain('text-2xl')
    })
  })

  it('positions itself via fixed coordinates derived from the trigger rect', () => {
    const ref = createRef<HTMLElement | null>()
    ;(ref as { current: HTMLElement | null }).current = trigger

    render(<ReactionPicker onPick={() => {}} onClose={() => {}} triggerRef={ref} />)

    const picker = screen.getByTestId('reaction-picker')
    expect(picker.style.position).toBe('fixed')
    // Trigger sits at top=400 with plenty of room above (>= 180 + 16), so the
    // picker should flip above.
    expect(picker.style.top).not.toBe('')
    expect(picker.style.left).not.toBe('')
  })

  it('invokes onPick with the chosen emoji', () => {
    const onPick = vi.fn()
    const ref = createRef<HTMLElement | null>()
    ;(ref as { current: HTMLElement | null }).current = trigger

    render(<ReactionPicker onPick={onPick} onClose={() => {}} triggerRef={ref} />)

    const picker = screen.getByTestId('reaction-picker')
    const firstCell = picker.querySelector('button')!
    fireEvent.click(firstCell)
    expect(onPick).toHaveBeenCalledTimes(1)
    expect(typeof onPick.mock.calls[0][0]).toBe('string')
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    const ref = createRef<HTMLElement | null>()
    ;(ref as { current: HTMLElement | null }).current = trigger

    render(<ReactionPicker onPick={() => {}} onClose={onClose} triggerRef={ref} />)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes when clicking outside the picker and outside the trigger', () => {
    const onClose = vi.fn()
    const ref = createRef<HTMLElement | null>()
    ;(ref as { current: HTMLElement | null }).current = trigger

    render(<ReactionPicker onPick={() => {}} onClose={onClose} triggerRef={ref} />)

    const outside = document.createElement('div')
    document.body.appendChild(outside)
    fireEvent.mouseDown(outside)
    expect(onClose).toHaveBeenCalledTimes(1)
    outside.remove()
  })

  it('does NOT close when clicking the trigger element (toggling is the trigger\'s job)', () => {
    const onClose = vi.fn()
    const ref = createRef<HTMLElement | null>()
    ;(ref as { current: HTMLElement | null }).current = trigger

    render(<ReactionPicker onPick={() => {}} onClose={onClose} triggerRef={ref} />)

    fireEvent.mouseDown(trigger)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('does not close when clicking inside the picker itself', () => {
    const onClose = vi.fn()
    const ref = createRef<HTMLElement | null>()
    ;(ref as { current: HTMLElement | null }).current = trigger

    render(<ReactionPicker onPick={() => {}} onClose={onClose} triggerRef={ref} />)

    const picker = screen.getByTestId('reaction-picker')
    fireEvent.mouseDown(picker)
    expect(onClose).not.toHaveBeenCalled()
  })
})
