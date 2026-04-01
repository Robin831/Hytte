import { useState, useEffect, useRef } from 'react'

/**
 * Persists a panel's collapsed/expanded state in localStorage.
 * Key format: forge-dashboard-panel-{panelId}
 *
 * Returns [isOpen, toggle].
 */
export function usePanelCollapse(panelId: string, defaultOpen = true): [boolean, () => void] {
  const key = `forge-dashboard-panel-${panelId}`

  const [isOpen, setIsOpen] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(key)
      if (stored !== null) return stored === 'true'
    } catch {
      // ignore — storage may be unavailable
    }
    return defaultOpen
  })

  // Only write on changes after mount to avoid redundant writes on initial render.
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
