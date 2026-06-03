import { useCallback, useEffect, useState, type Dispatch, type SetStateAction } from 'react'
import { formatNumber } from '../../utils/formatDate'

// Shared helpers for the Budget sub-pages. Centralizes the NOK/number
// formatting and the GET + AbortController + loading/error lifecycle that the
// pages otherwise copy-paste, so locale/format behavior lives in one place and
// future budget pages can reuse it. For today's local date, use the shared
// `toLocalDateString` from `../../utils/formatDate`.

// Formats a signed amount in the given currency using the active locale.
// Callers without a per-account context fall back to 'NOK'; the negative sign
// (or parentheses, depending on locale) is rendered by Intl.NumberFormat —
// red/green tinting belongs on the surrounding cell, not in here.
export function formatNOK(amount: number, currency?: string): string {
  return formatNumber(amount, {
    style: 'currency',
    currency: currency ?? 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

// Symbol-free number formatter for chart axes, tooltips, and plain amounts.
// Uses the `undefined` locale to respect the browser's settings and defaults to
// whole numbers; pass `options` to override (e.g. two fraction digits for the
// CSV import preview).
export function formatBudgetNumber(n: number, options?: Intl.NumberFormatOptions): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0, ...options }).format(n)
}

export interface BudgetResource<T> {
  data: T | null
  setData: Dispatch<SetStateAction<T | null>>
  loading: boolean
  error: string | null
  reload: () => void
}

// GET a JSON resource with the standard budget lifecycle: sets `loading` true
// and clears `error` on start, stores the parsed body on success, swallows
// AbortError, surfaces `errorMessage` on real failures, and aborts the in-flight
// request on unmount or when `url`/`errorMessage` change. `reload()` forces a
// refetch; `setData` is exposed for optimistic local updates.
export function useBudgetResource<T>(url: string, errorMessage: string): BudgetResource<T> {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reloadKey, setReloadKey] = useState(0)

  const reload = useCallback(() => setReloadKey(k => k + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    setLoading(true)
    setError(null)
    fetch(url, { credentials: 'include', signal: controller.signal })
      .then(r => {
        if (!r.ok) throw new Error('fetch failed')
        return r.json() as Promise<T>
      })
      .then(setData)
      .catch(err => {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(errorMessage)
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [url, errorMessage, reloadKey])

  return { data, setData, loading, error, reload }
}
