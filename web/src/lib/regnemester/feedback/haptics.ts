export function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

export function vibrate(pattern: number | number[]): void {
  if (typeof navigator === 'undefined') return
  const nav = navigator as Navigator & { vibrate?: (pattern: number | number[]) => boolean }
  if (typeof nav.vibrate !== 'function') return
  if (prefersReducedMotion()) return
  try {
    nav.vibrate(pattern)
  } catch {
    // Some browsers throw if the pattern is malformed or the page is hidden.
  }
}

export function vibrateCorrect(): void {
  vibrate(20)
}

export function vibrateWrong(): void {
  vibrate([30, 40, 30])
}
