import { useCallback, useEffect, useRef, useState } from 'react'

interface UseTabFetchResult {
  loading: boolean
  error: string
  reload: () => void
  invalidate: () => void
}

// Caches per-tab fetched data for the lifetime of the page mount.
//
// - First time `active` becomes true, the fetcher runs and result is delivered
//   to `onSuccess`. Subsequent `active` toggles do not refetch.
// - `reload()` always re-runs the fetcher (also when inactive, so that data
//   stays fresh for cross-tab effects like badges).
// - `invalidate()` marks the data stale so the next time the tab becomes
//   active the fetcher will run again.
// - In-flight requests are cancelled on unmount, tab switch, and reload.
export function useTabFetch<T>(
  active: boolean,
  fetcher: (signal: AbortSignal) => Promise<T>,
  onSuccess: (data: T) => void,
  errorMessage: string,
): UseTabFetchResult {
  const [internalLoading, setInternalLoading] = useState(false)
  const [error, setError] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [forceKey, setForceKey] = useState(0)
  const handledForceKey = useRef(0)
  // Tracks whether the last fetch attempt ended in a non-abort error.
  // While true the guard skips refetching on tab-switch so the error message
  // is preserved until the user explicitly calls reload().
  const failedRef = useRef(false)

  const fetcherRef = useRef(fetcher)
  const onSuccessRef = useRef(onSuccess)
  const errorMessageRef = useRef(errorMessage)
  useEffect(() => {
    fetcherRef.current = fetcher
    onSuccessRef.current = onSuccess
    errorMessageRef.current = errorMessage
  })

  useEffect(() => {
    const isForced = forceKey > handledForceKey.current
    if (!isForced && (!active || loaded || failedRef.current)) {
      // Guarded: reset any in-flight loading flag left over by an aborted fetch
      // so a cached tab doesn't show its skeleton when reopened.
      setInternalLoading(false)
      return
    }
    // Capture forceKey for this invocation. handledForceKey is advanced only
    // after the fetch resolves (success or real error, not abort) so that an
    // abort mid-reload (e.g. due to a tab-switch) keeps the forced reload
    // "pending" and automatically retries on the next active transition.
    const capturedForceKey = forceKey
    failedRef.current = false
    const controller = new AbortController()
    let cancelled = false
    setInternalLoading(true)
    setError('')
    void (async () => {
      try {
        const data = await fetcherRef.current(controller.signal)
        if (cancelled) return
        handledForceKey.current = Math.max(handledForceKey.current, capturedForceKey)
        onSuccessRef.current(data)
        setLoaded(true)
      } catch (err) {
        if (cancelled) return
        if (err instanceof DOMException && err.name === 'AbortError') return
        handledForceKey.current = Math.max(handledForceKey.current, capturedForceKey)
        failedRef.current = true
        setError(errorMessageRef.current)
      } finally {
        if (!cancelled) setInternalLoading(false)
      }
    })()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [active, loaded, forceKey])

  const reload = useCallback(() => {
    setForceKey(k => k + 1)
  }, [])

  const invalidate = useCallback(() => {
    // Also clear failedRef so that a post-mutation invalidate on a previously
    // failed tab correctly retries on the next activation.
    failedRef.current = false
    setLoaded(false)
  }, [])

  // Show loading on first-entry into an active tab (before the effect kicks in)
  // so the skeleton appears immediately, matching the pre-cache behavior.
  const loading = internalLoading || (active && !loaded && !error)

  return { loading, error, reload, invalidate }
}
