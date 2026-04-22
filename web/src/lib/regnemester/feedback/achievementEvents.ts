import type { UnlockedAchievement } from '../../../components/math/UnlockedAchievements'

type Handler = (items: UnlockedAchievement[]) => void

const handlers = new Set<Handler>()

// emitAchievementUnlock broadcasts a batch of newly-unlocked achievements to
// any subscribed listeners (typically the AchievementUnlockOverlay). Empty
// batches are ignored so callers can pass unfiltered server payloads.
export function emitAchievementUnlock(items: UnlockedAchievement[] | null | undefined): void {
  if (!items || items.length === 0) return
  // Copy so listeners that mutate their argument don't clobber the caller.
  const snapshot = items.slice()
  for (const h of handlers) {
    try {
      h(snapshot)
    } catch {
      // A broken listener shouldn't take down the celebration — swallow.
    }
  }
}

export function subscribeAchievementUnlock(handler: Handler): () => void {
  handlers.add(handler)
  return () => { handlers.delete(handler) }
}

// Exposed for tests — drops all listeners between cases so a leaking
// subscription can't poison subsequent runs.
export function _resetAchievementHandlers(): void {
  handlers.clear()
}
