import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { ParseKeys } from 'i18next'
import { Award, Sparkles } from 'lucide-react'
import type { UnlockedAchievement } from '../math/UnlockedAchievements'
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

export function AchievementUnlockOverlay() {
  const { t, i18n } = useTranslation('regnemester')
  const [queue, setQueue] = useState<UnlockedAchievement[]>([])
  const [exiting, setExiting] = useState(false)
  // Ref-based re-entrancy guard: prevents a rapid double-tap (or tap +
  // auto-advance collision) from slicing the queue twice.
  const exitingRef = useRef(false)

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

    const autoId = window.setTimeout(() => advance(), AUTO_DISMISS_MS)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        advance()
      }
    }
    window.addEventListener('keydown', onKey)

    return () => {
      window.clearTimeout(autoId)
      window.removeEventListener('keydown', onKey)
    }
  }, [current, advance])

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
