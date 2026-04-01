import { useSyncExternalStore, useCallback } from 'react'

function getStorageSnapshot(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function subscribeToStorage(callback: () => void): () => void {
  window.addEventListener('storage', callback)
  return () => window.removeEventListener('storage', callback)
}

/**
 * Persists a panel's collapsed/expanded state in localStorage.
 * Key format: forge-dashboard-panel-{panelId}
 *
 * Returns [isOpen, toggle].
 */
export function usePanelCollapse(panelId: string, defaultOpen = true): [boolean, () => void] {
  const key = `forge-dashboard-panel-${panelId}`

  const getSnapshot = useCallback(() => getStorageSnapshot(key), [key])
  const storedValue = useSyncExternalStore(subscribeToStorage, getSnapshot)
  const isOpen = storedValue !== null ? storedValue === 'true' : defaultOpen

  const toggle = useCallback(() => {
    const newValue = !isOpen
    try {
      localStorage.setItem(key, String(newValue))
    } catch {
      // ignore
    }
    // Trigger re-render by dispatching a storage event manually for same-window updates
    window.dispatchEvent(new StorageEvent('storage', { key }))
  }, [key, isOpen])

  return [isOpen, toggle]
}
