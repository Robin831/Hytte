import { useEffect, useState } from 'react'
import { useAuth } from '../../auth'

/** GETs a JSON endpoint with the session cookie; a non-2xx reply rejects. */
export async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, { credentials: 'include', signal })
  if (!res.ok) throw new Error(`${url}: ${res.status}`)
  return res.json() as Promise<T>
}

export interface WidgetData<T> {
  data: T | null
  /** True once the first load settled, successfully or not. */
  loaded: boolean
  failed: boolean
  reload: () => void
}

/**
 * Loads a widget's data once the user is known. `load` must be a stable
 * reference (a module-level function), or the widget refetches every render.
 */
export function useWidgetData<T>(load: (signal: AbortSignal) => Promise<T>): WidgetData<T> {
  const { user } = useAuth()
  const [state, setState] = useState<{ data: T | null; loaded: boolean; failed: boolean }>({
    data: null,
    loaded: false,
    failed: false,
  })
  const [version, setVersion] = useState(0)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    load(controller.signal)
      .then((data) => {
        if (!controller.signal.aborted) setState({ data, loaded: true, failed: false })
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
        // Keep what was already shown: a failed refresh must not blank the widget.
        setState((prev) => ({ data: prev.data, loaded: true, failed: true }))
      })
    return () => controller.abort()
  }, [user, load, version])

  return { ...state, reload: () => setVersion((v) => v + 1) }
}

/** Whole days from `from` to `to`, both YYYY-MM-DD (negative when `to` is earlier). */
export function daysBetween(from: string, to: string): number {
  const utc = (d: string) => {
    const [y, m, day] = d.split('-').map(Number)
    return Date.UTC(y, m - 1, day)
  }
  return Math.round((utc(to) - utc(from)) / 86_400_000)
}
