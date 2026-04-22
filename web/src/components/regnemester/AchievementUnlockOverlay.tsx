import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { ParseKeys } from 'i18next'
import { Award, Sparkles } from 'lucide-react'
import type { UnlockedAchievement } from '../../lib/regnemester/achievements/types'
import { subscribeAchievementUnlock } from '../../lib/regnemester/feedback/achievementEvents'
import { soundEngine } from '../../lib/regnemester/feedback/sound'
import { vibrate } from '../../lib/regnemester/feedback/haptics'
import './achievementUnlockOverlay.css'

// Time the overlay stays on screen before auto-advancing to the next entry
// in the queue. Long enough to read the description, short enough that a
// chain of unlocks doesn't feel like a modal gauntlet.
const AUTO_DISMISS_MS = 4200

// Duration of the exit animation — matches the CSS keyframes so unmount
// lines up with the fade-out visually.
const EXIT_ANIM_MS = 180

// Haptic pattern for the unlock moment: two short taps, then a longer
// one so it feels like a small drum roll. No-op under reduced motion.
const VIBRATE_PATTERN = [40, 50, 40, 50, 80]

const FOCUSABLE_SELECTOR =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export function AchievementUnlockOverlay() {
  const { t, i18n } = useTranslation('regnemester')
  const [queue, setQueue] = useState<UnlockedAchievement[]>([])
  const [exiting, setExiting] = useState(false)
  // Ref-based re-entrancy guard: prevents a rapid double-tap (or tap +
  // auto-advance collision) from slicing the queue twice.
  const exitingRef = useRef(false)
  const cardRef = useRef<HTMLDivElement | null>(null)
  const dismissButtonRef = useRef<HTMLButtonElement | null>(null)
  // Element that held focus before the overlay opened. Captured on the
  // first shown unlock and cleared once the queue fully drains so focus
  // is returned to where the user was before the celebration.
  const previousFocusRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    return subscribeAchievementUnlock(items => {
      setQueue(prev => [...prev, ...items])
    })
  }, [])

  const advance = useCallback(() => {
    if (exitingRef.current) return
    exitingRef.current = true
    setExiting(true)
    window.setTimeout(() => {
      setQueue(prev => prev.slice(1))
      setExiting(false)
      exitingRef.current = false
    }, EXIT_ANIM_MS)
  }, [])

  const current = queue[0] ?? null

  // Mount effect per distinct achievement: fire fanfare + haptics, set up
  // auto-dismiss timer and keyboard dismissal. Cleanup clears both so they
  // don't bleed into the next unlock's lifecycle. `current` is the first
  // item in the queue — it changes reference only when we slice, so this
  // effect restarts once per shown unlock, not on every unrelated render.
  useEffect(() => {
    if (!current) return
    soundEngine.play('fanfare')
    vibrate(VIBRATE_PATTERN)

    // Capture the pre-overlay focus target once per sequence, then move
    // focus into the dialog. aria-modal=true promises a focus trap, so
    // screen-reader and keyboard users expect focus to land here.
    if (previousFocusRef.current === null && typeof document !== 'undefined') {
      const active = document.activeElement as HTMLElement | null
      previousFocusRef.current = active && active !== document.body ? active : null
    }
    dismissButtonRef.current?.focus()

    const autoId = window.setTimeout(() => advance(), AUTO_DISMISS_MS)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' || e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') {
        e.preventDefault()
        advance()
        return
      }
      if (e.key === 'Tab') {
        // Trap focus inside the dialog. With a single focusable control
        // Tab/Shift+Tab keep focus parked on it; if we ever add more
        // controls the cycle still stays inside the card.
        const container = cardRef.current
        if (!container) return
        const focusable = Array.from(
          container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
        )
        if (focusable.length === 0) {
          e.preventDefault()
          return
        }
        const first = focusable[0]
        const last = focusable[focusable.length - 1]
        const active = document.activeElement as HTMLElement | null
        if (e.shiftKey) {
          if (!active || active === first || !container.contains(active)) {
            e.preventDefault()
            last.focus()
          }
        } else {
          if (!active || active === last || !container.contains(active)) {
            e.preventDefault()
            first.focus()
          }
        }
      }
    }
    window.addEventListener('keydown', onKey)

    return () => {
      window.clearTimeout(autoId)
      window.removeEventListener('keydown', onKey)
    }
  }, [current, advance])

  // Restore focus once the queue has fully drained. Kept in its own effect
  // so the save/restore bookkeeping stays separate from the per-unlock
  // setup above.
  useEffect(() => {
    if (current) return
    const prev = previousFocusRef.current
    previousFocusRef.current = null
    if (prev && typeof prev.focus === 'function') {
      prev.focus()
    }
  }, [current])

  if (!current) return null

  const titleKey = `achievements.codes.${current.code}.title` as ParseKeys<'regnemester'>
  const descKey = `achievements.codes.${current.code}.description` as ParseKeys<'regnemester'>
  const tierKey = `achievements.tiers.${current.tier}` as ParseKeys<'regnemester'>
  const title = i18n.exists(`regnemester:${titleKey}`) ? t(titleKey) : current.title
  const description = i18n.exists(`regnemester:${descKey}`) ? t(descKey) : current.description
  const tierLabel = i18n.exists(`regnemester:${tierKey}`) ? t(tierKey) : current.tier

  const remaining = queue.length - 1
  const backdropClass = exiting
    ? 'regnemester-achievement-overlay-backdrop regnemester-achievement-overlay-backdrop-exit'
    : 'regnemester-achievement-overlay-backdrop'
  const cardClass = exiting
    ? 'regnemester-achievement-overlay-card regnemester-achievement-overlay-card-exit'
    : 'regnemester-achievement-overlay-card'

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="regnemester-achievement-overlay-title"
      className={`fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70 backdrop-blur-sm ${backdropClass}`}
      onClick={advance}
      data-testid="regnemester-achievement-overlay"
    >
      <div
        ref={cardRef}
        className={`mx-auto w-full max-w-md rounded-2xl border border-yellow-400/50 bg-gradient-to-br from-yellow-500/20 via-pink-500/15 to-purple-500/20 p-6 text-center shadow-2xl ${cardClass}`}
        onClick={e => e.stopPropagation()}
      >
        <div className="flex justify-center mb-3">
          <div className="regnemester-achievement-overlay-icon rounded-full bg-yellow-400/20 p-4 ring-1 ring-yellow-300/40">
            <Award size={56} className="text-yellow-300" aria-hidden="true" />
          </div>
        </div>
        <div className="mb-1 flex items-center justify-center gap-1 text-xs font-semibold uppercase tracking-widest text-pink-300">
          <Sparkles size={14} aria-hidden="true" />
          <span>{t('achievements.unlockedOverlay.eyebrow', { tier: tierLabel })}</span>
        </div>
        <h2
          id="regnemester-achievement-overlay-title"
          className="mb-2 text-2xl font-bold text-white"
        >
          {title}
        </h2>
        <p className="mb-4 text-sm text-gray-200">{description}</p>
        {remaining > 0 && (
          <div className="mb-3 text-xs text-gray-400" aria-live="polite">
            {t('achievements.unlockedOverlay.queueHint', { count: remaining })}
          </div>
        )}
        <button
          ref={dismissButtonRef}
          type="button"
          onClick={advance}
          className="inline-flex items-center gap-2 rounded-lg bg-white/10 px-4 py-2 text-sm font-medium text-white hover:bg-white/20 active:bg-white/30"
        >
          {t('achievements.unlockedOverlay.dismiss')}
        </button>
      </div>
    </div>
  )
}

export default AchievementUnlockOverlay
