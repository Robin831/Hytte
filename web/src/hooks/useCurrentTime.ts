import { useState, useEffect } from 'react'

/**
 * Returns a live Date that updates on each wall-clock minute boundary
 * (aligned to `:00` seconds), pausing while the tab is hidden.
 *
 * Unlike {@link useNow} (which ticks every second), this hook re-renders only
 * once per minute, so it suits surfaces that show minute-granularity time
 * without incurring per-second re-renders.
 *
 * Both `visibilitychange` (returning to a backgrounded tab) and `focus`
 * (covers laptop wake/resume, where visibility may not change) trigger an
 * immediate recompute and re-align the next-minute timeout, so the displayed
 * time is never left stale after sleep/resume.
 */
export function useCurrentTime(): Date {
  const [now, setNow] = useState<Date>(() => new Date())

  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout> | null = null
    let intervalId: ReturnType<typeof setInterval> | null = null

    const tick = () => setNow(new Date())

    function start() {
      stop()
      const current = new Date()
      const msUntilNextMinute =
        (60 - current.getSeconds()) * 1000 - current.getMilliseconds()
      timeoutId = setTimeout(() => {
        tick()
        intervalId = setInterval(tick, 60_000)
      }, msUntilNextMinute)
    }
    function stop() {
      if (timeoutId !== null) {
        clearTimeout(timeoutId)
        timeoutId = null
      }
      if (intervalId !== null) {
        clearInterval(intervalId)
        intervalId = null
      }
    }
    // Recompute the time immediately and re-align the next-minute timeout.
    // Skipped while hidden so the hook stays paused per its visibility contract.
    function resync() {
      if (document.hidden) return
      tick()
      start()
    }
    function handleVisibility() {
      if (document.hidden) stop()
      else resync()
    }

    if (!document.hidden) start()
    document.addEventListener('visibilitychange', handleVisibility)
    window.addEventListener('focus', resync)
    return () => {
      stop()
      document.removeEventListener('visibilitychange', handleVisibility)
      window.removeEventListener('focus', resync)
    }
  }, [])

  return now
}
