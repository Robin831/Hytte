import { useEffect } from 'react'

/**
 * Keeps the device screen awake for as long as the component is mounted by
 * holding a {@link https://developer.mozilla.org/docs/Web/API/Screen_Wake_Lock_API
 * Screen Wake Lock}.
 *
 * The lock is auto-released by the browser whenever the tab is backgrounded or
 * the screen briefly turns off, so it is re-acquired on `visibilitychange`
 * whenever the page becomes visible again. The lock is released on unmount.
 *
 * Feature-detects `navigator.wakeLock` and no-ops (with a `console.debug`) on
 * unsupported browsers. Never throws.
 *
 * Note: the lock only holds while this is the foreground tab, and Android
 * battery saver can still override it. For a permanent kiosk, also max out the
 * device's display/screen-timeout setting.
 */
export function useWakeLock(): void {
  useEffect(() => {
    if (!('wakeLock' in navigator)) {
      console.debug('Screen Wake Lock API not supported')
      return
    }

    let wakeLock: WakeLockSentinel | null = null

    async function acquire() {
      try {
        wakeLock = await navigator.wakeLock.request('screen')
      } catch (err) {
        // e.g. lock denied while not visible, or battery saver active
        console.debug('Wake lock request failed', err)
      }
    }

    function handleVisibilityChange() {
      if (document.visibilityState === 'visible') acquire()
    }

    acquire()
    document.addEventListener('visibilitychange', handleVisibilityChange)

    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      wakeLock?.release().catch(() => {})
      wakeLock = null
    }
  }, [])
}
