import { useEffect, useState } from 'react'

// useKeyboardInset returns the height (in CSS pixels) currently covered by the
// on-screen keyboard, derived from the VisualViewport API. It is 0 when no
// keyboard is open, while the page is scrolled normally, or when the API is
// unavailable (older browsers, SSR-style environments without window).
//
// The value lets a full-height view shrink so its bottom edge (e.g. a chat
// composer) stays pinned above the keyboard instead of being pushed off-screen.
//
// It composes cleanly with the CSS `interactive-widget=resizes-content`
// viewport hint: on browsers that honour it (Chrome Android) the layout
// viewport already shrinks when the keyboard opens, so `window.innerHeight`
// tracks the keyboard and this inset stays ~0 — no double-counting. On browsers
// that don't (notably iOS Safari) the layout viewport stays full and this inset
// reports the real keyboard height.
export function useKeyboardInset(): number {
  const [inset, setInset] = useState(0)

  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const opts: AddEventListenerOptions = { passive: true }

    const onResize = () => {
      // window.innerHeight is the layout viewport; vv.height + vv.offsetTop is
      // the bottom edge of the visible (keyboard-reduced) viewport. The gap is
      // the keyboard. Clamp at 0 so a transient negative (rounding) never grows
      // the container.
      const next = Math.max(0, window.innerHeight - (vv.height + vv.offsetTop))
      // Ignore sub-pixel jitter so we don't thrash re-renders while scrolling.
      setInset(prev => (Math.abs(prev - next) < 1 ? prev : next))
    }

    vv.addEventListener('resize', onResize, opts)
    vv.addEventListener('scroll', onResize, opts)
    onResize()
    return () => {
      vv.removeEventListener('resize', onResize, opts)
      vv.removeEventListener('scroll', onResize, opts)
    }
  }, [])

  return inset
}
