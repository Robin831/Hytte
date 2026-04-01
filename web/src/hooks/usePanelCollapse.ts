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

  // Track first render and last key to handle panelId/key changes correctly.
  const isFirstRender = useRef(true)
  const lastKeyRef = useRef(key)

  useEffect(() => {
    const isKeyChanged = lastKeyRef.current !== key

    // On first render or when the key changes, rehydrate from storage for the
    // current key instead of writing the previous state's value to the new key.
    if (isFirstRender.current || isKeyChanged) {
      isFirstRender.current = false
      lastKeyRef.current = key

      try {
        const stored = localStorage.getItem(key)
        if (stored !== null) {
          setIsOpen(stored === 'true')
          return
        }
      } catch {
        // ignore — storage may be unavailable
      }

      // No stored value: fall back to the default for this panel.
      setIsOpen(defaultOpen)
      return
    }

    try {
      localStorage.setItem(key, String(isOpen))
    } catch {
      // ignore
    }
  }, [key, isOpen, defaultOpen])

  const toggle = () => setIsOpen(prev => !prev)

  return [isOpen, toggle]
}
