import { prefersReducedMotion } from './haptics'

const CORRECT_CLASS = 'regnemester-flash-correct'
const WRONG_CLASS = 'regnemester-flash-wrong'
const CORRECT_MS = 150
const WRONG_MS = 250

// Per-element timeout handles so rapid re-triggers don't leave a class stuck
// on the element after the original timeout fires late.
const timers = new WeakMap<HTMLElement, number>()

function flash(el: HTMLElement | null, cls: string, durationMs: number): void {
  if (!el) return
  if (prefersReducedMotion()) return

  const existing = timers.get(el)
  if (existing !== undefined) {
    window.clearTimeout(existing)
    el.classList.remove(cls)
  }

  // Re-adding the class in the same tick doesn't restart the CSS transition
  // in most browsers — force a reflow first so the transition restarts cleanly.
  void el.offsetWidth
  el.classList.add(cls)

  const id = window.setTimeout(() => {
    el.classList.remove(cls)
    timers.delete(el)
  }, durationMs)
  timers.set(el, id)
}

export function flashCorrect(el: HTMLElement | null): void {
  flash(el, CORRECT_CLASS, CORRECT_MS)
}

export function flashWrong(el: HTMLElement | null): void {
  flash(el, WRONG_CLASS, WRONG_MS)
}
