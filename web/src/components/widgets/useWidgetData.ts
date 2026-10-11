import { useEffect, useState } from 'react'
import { useAuth } from '../../auth'

/** GETs a JSON endpoint with the session cookie; a non-2xx reply rejects. */
export async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, { credentials: 'include', signal })
  if (!res.ok) throw new Error(`${url}: ${res.status}`)
  return res.json() as Promise<T>
}

/**
 * A named piece of dashboard data. Several widgets can read the same source
 * (a trips widget and the "next up" list, say) and share one request.
 */
export interface Source<T> {
  key: string
  load: () => Promise<T>
}

export const source = <T,>(key: string, load: () => Promise<T>): Source<T> => ({ key, load })

/** How long a loaded source is reused before the next reader refetches it. */
const FRESH_MS = 30_000

const cache = new Map<string, { promise: Promise<unknown>; at: number }>()
const listeners = new Set<(key: string) => void>()

function read<T>(src: Source<T>): Promise<T> {
  const hit = cache.get(src.key)
  if (hit && Date.now() - hit.at < FRESH_MS) return hit.promise as Promise<T>
  const promise = src.load()
  cache.set(src.key, { promise, at: Date.now() })
  // A failure is not worth remembering: the next reader should try again.
  promise.catch(() => {
    if (cache.get(src.key)?.promise === promise) cache.delete(src.key)
  })
  return promise
}

/** Drops a source's cached value and makes everyone showing it load it again. */
export function invalidate(key: string) {
  cache.delete(key)
  listeners.forEach((listener) => listener(key))
}

/** Forgets everything cached (between tests). */
export function resetSources() {
  cache.clear()
}

export interface WidgetData<T> {
  data: T | null
  /** True once the first load settled, successfully or not. */
  loaded: boolean
  failed: boolean
  reload: () => void
}

/**
 * Reads a source once the user is known. With `enabled` false (the user lacks
 * the feature) nothing is fetched and the result is an empty, settled one.
 */
export function useWidgetData<T>(src: Source<T>, enabled = true): WidgetData<T> {
  const { user } = useAuth()
  const [state, setState] = useState<{ data: T | null; loaded: boolean; failed: boolean }>({
    data: null,
    loaded: false,
    failed: false,
  })
  const [version, setVersion] = useState(0)

  useEffect(() => {
    const onInvalidate = (key: string) => {
      if (key === src.key) setVersion((v) => v + 1)
    }
    listeners.add(onInvalidate)
    return () => {
      listeners.delete(onInvalidate)
    }
  }, [src])

  useEffect(() => {
    if (!user || !enabled) return
    let stale = false
    read(src)
      .then((data) => {
        if (!stale) setState({ data, loaded: true, failed: false })
      })
      .catch(() => {
        // Keep what was already shown: a failed refresh must not blank the widget.
        if (!stale) setState((prev) => ({ data: prev.data, loaded: true, failed: true }))
      })
    return () => {
      stale = true
    }
  }, [user, enabled, src, version])

  const reload = () => invalidate(src.key)
  if (!enabled) return { data: null, loaded: true, failed: false, reload }
  return { ...state, reload }
}

/** Whole days from `from` to `to`, both YYYY-MM-DD (negative when `to` is earlier). */
export function daysBetween(from: string, to: string): number {
  const utc = (d: string) => {
    const [y, m, day] = d.split('-').map(Number)
    return Date.UTC(y, m - 1, day)
  }
  return Math.round((utc(to) - utc(from)) / 86_400_000)
}
