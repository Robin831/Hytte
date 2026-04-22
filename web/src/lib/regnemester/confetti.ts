import confetti from 'canvas-confetti'
import { prefersReducedMotion } from './feedback/haptics'
import './confetti.css'

export type Intensity = 'small' | 'medium' | 'large' | 'full'

export type Palette = 'default' | 'golden' | 'rainbow'

const PALETTES: Record<Palette, string[]> = {
  default: ['#60a5fa', '#34d399', '#f472b6', '#fbbf24', '#a78bfa'],
  golden: ['#fde047', '#facc15', '#eab308', '#ca8a04', '#fef9c3'],
  rainbow: ['#ef4444', '#f97316', '#eab308', '#22c55e', '#3b82f6', '#8b5cf6', '#ec4899'],
}

const INTENSITY_CONFIG: Record<Intensity, { particleCount: number; spread: number; startVelocity: number; scalar: number }> = {
  small: { particleCount: 30, spread: 55, startVelocity: 30, scalar: 0.7 },
  medium: { particleCount: 80, spread: 70, startVelocity: 40, scalar: 1 },
  large: { particleCount: 160, spread: 90, startVelocity: 50, scalar: 1.1 },
  full: { particleCount: 260, spread: 120, startVelocity: 55, scalar: 1.2 },
}

// burst fires a confetti volley. When the user prefers reduced motion, this
// is a no-op — the confetti module also honours the media query internally,
// but we short-circuit for clarity.
export function burst(intensity: Intensity = 'medium', palette: Palette = 'default'): void {
  if (prefersReducedMotion()) return
  const cfg = INTENSITY_CONFIG[intensity]
  const colors = PALETTES[palette]

  if (intensity === 'full') {
    // Fire from both lower corners so the full-screen burst actually covers
    // the width of the viewport rather than just blooming out of the centre.
    void confetti({
      particleCount: Math.round(cfg.particleCount / 2),
      angle: 60,
      spread: cfg.spread,
      startVelocity: cfg.startVelocity,
      scalar: cfg.scalar,
      origin: { x: 0, y: 0.7 },
      colors,
      disableForReducedMotion: true,
    })
    void confetti({
      particleCount: Math.round(cfg.particleCount / 2),
      angle: 120,
      spread: cfg.spread,
      startVelocity: cfg.startVelocity,
      scalar: cfg.scalar,
      origin: { x: 1, y: 0.7 },
      colors,
      disableForReducedMotion: true,
    })
    return
  }

  void confetti({
    particleCount: cfg.particleCount,
    spread: cfg.spread,
    startVelocity: cfg.startVelocity,
    scalar: cfg.scalar,
    origin: { y: 0.65 },
    colors,
    disableForReducedMotion: true,
  })
}

const SHAKE_CLASS = 'regnemester-screen-shake'
const SHAKE_DURATION_MS = 600

// Guard against overlapping shakes on the same root — we read and restore
// the timer per root rather than globally so two animated roots don't fight.
const shakeTimers = new WeakMap<HTMLElement, number>()

// screenShake applies a brief shake animation to the document body (or a
// caller-supplied root element). No-op under prefers-reduced-motion.
export function screenShake(root: HTMLElement | null = typeof document !== 'undefined' ? document.body : null): void {
  if (!root) return
  if (prefersReducedMotion()) return

  const existing = shakeTimers.get(root)
  if (existing !== undefined) {
    window.clearTimeout(existing)
    root.classList.remove(SHAKE_CLASS)
    // Force reflow so re-adding the class restarts the animation.
    void root.offsetWidth
  }

  root.classList.add(SHAKE_CLASS)
  const id = window.setTimeout(() => {
    root.classList.remove(SHAKE_CLASS)
    shakeTimers.delete(root)
  }, SHAKE_DURATION_MS)
  shakeTimers.set(root, id)
}

// Intensity for a Blitz run based on final score. Thresholds roughly match
// the "first bloody good run" (~60), "solid" (~120), and "top of the board"
// (~200+) brackets that live scoring tends to produce.
export function blitzIntensityForScore(score: number): Intensity {
  if (score >= 200) return 'large'
  if (score >= 120) return 'medium'
  return 'small'
}
