import { useState, useEffect, useRef } from 'react'

/**
 * Persists a panel's collapsed/expanded state in localStorage.
 * Key format: forge-dashboard-panel-{panelId}
 *
 * Returns [isOpen, toggle].
 */
export function usePanelCollapse(panelId: string, defaultOpen = true): [boolean, () => void] {
  const key = `forge-dashboard-panel-${panelId}`

  const readStorage = (k: string): boolean | null => {
    try {
      const stored = localStorage.getItem(k)
      if (stored !== null) return stored === 'true'
    } catch {
      // ignore — storage may be unavailable
    }
    return null
  }

  const [isOpen, setIsOpen] = useState<boolean>(() => readStorage(key) ?? defaultOpen)

  // When the key changes, re-read from localStorage.
  useEffect(() => {
    const stored = readStorage(key)
    const newValue = stored ?? defaultOpen
    setIsOpen(newValue)
  }, [key, defaultOpen])

  // Persist to localStorage whenever the value changes.
  const isFirstRender = useRef(true)
  useEffect(() => {
    if (isFirstRender.current) {
      isFirstRender.current = false
      return
    }
    try {
      localStorage.setItem(key, String(isOpen))
    } catch {
      // ignore
    }
  }, [key, isOpen])

  const toggle = () => setIsOpen(prev => !prev)

  return [isOpen, toggle]
}
