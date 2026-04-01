import { useSyncExternalStore, useCallback } from 'react'

const PANEL_CHANGE_EVENT = 'forge-panel-collapse-change'

function getStorageSnapshot(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function subscribeToStorage(key: string, callback: () => void): () => void {
  if (typeof window === 'undefined') return () => {}

  function handleStorage(e: StorageEvent) {
    if (e.key === null || e.key === key) callback()
  }
  function handleCustom(e: Event) {
    if ((e as CustomEvent<string>).detail === key) callback()
  }

  window.addEventListener('storage', handleStorage)
  window.addEventListener(PANEL_CHANGE_EVENT, handleCustom)
  return () => {
    window.removeEventListener('storage', handleStorage)
    window.removeEventListener(PANEL_CHANGE_EVENT, handleCustom)
  }
}

function getServerSnapshot(): string | null {
  return null
}

/**
 * Persists a panel's collapsed/expanded state in localStorage.
 * Key format: forge-dashboard-panel-{panelId}
 *
 * Returns [isOpen, toggle].
 */
export function usePanelCollapse(panelId: string, defaultOpen = true): [boolean, () => void] {
  const key = `forge-dashboard-panel-${panelId}`

  const subscribe = useCallback(
    (callback: () => void) => subscribeToStorage(key, callback),
    [key],
  )
  const getSnapshot = useCallback(() => getStorageSnapshot(key), [key])
  const storedValue = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)
  const isOpen = storedValue !== null ? storedValue === 'true' : defaultOpen

  const toggle = useCallback(() => {
    const newValue = !isOpen
    try {
      localStorage.setItem(key, String(newValue))
    } catch {
      // ignore
    }
    // Notify same-window subscribers via a custom event
    if (typeof window !== 'undefined') {
      window.dispatchEvent(new CustomEvent(PANEL_CHANGE_EVENT, { detail: key }))
    }
  }, [key, isOpen])

  return [isOpen, toggle]
}
