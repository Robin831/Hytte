import { useState, useCallback, useRef, useEffect } from 'react'

const STORAGE_KEY = 'mezzanine-panel-layout'

interface PanelLayout {
  /** Height percentage for the upper zone (pipeline + workers), 0-100 */
  upperPct: number
}

const DEFAULT_LAYOUT: PanelLayout = { upperPct: 60 }
const MIN_PCT = 25
const MAX_PCT = 85

function loadLayout(): PanelLayout {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw) as PanelLayout
      if (typeof parsed.upperPct === 'number' && parsed.upperPct >= MIN_PCT && parsed.upperPct <= MAX_PCT) {
        return parsed
      }
    }
  } catch { /* use defaults */ }
  return DEFAULT_LAYOUT
}

function saveLayout(layout: PanelLayout) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(layout))
  } catch {
    // Ignore storage write failures and keep layout persistence best-effort.
  }
}

export function usePanelLayout() {
  const [layout, setLayout] = useState<PanelLayout>(loadLayout)
  const containerRef = useRef<HTMLDivElement>(null)
  // Track cleanup function for active drag so unmount can cancel it
  const cleanupRef = useRef<(() => void) | null>(null)

  // Cancel any active drag on unmount
  useEffect(() => {
    return () => { cleanupRef.current?.() }
  }, [])

  const handlePointerDown = useCallback((e: React.PointerEvent) => {
    e.preventDefault()
    const container = containerRef.current
    if (!container) return

    const startY = e.clientY
    const startPct = layout.upperPct
    const containerRect = container.getBoundingClientRect()
    const containerHeight = containerRect.height

    // Prevent text selection while dragging
    const prevUserSelect = document.body.style.userSelect
    document.body.style.userSelect = 'none'

    function onPointerMove(ev: PointerEvent) {
      const deltaY = ev.clientY - startY
      const deltaPct = (deltaY / containerHeight) * 100
      const newPct = Math.min(MAX_PCT, Math.max(MIN_PCT, startPct + deltaPct))
      setLayout({ upperPct: Math.round(newPct) })
    }

    function cleanup() {
      document.removeEventListener('pointermove', onPointerMove)
      document.removeEventListener('pointerup', cleanup)
      document.removeEventListener('pointercancel', cleanup)
      window.removeEventListener('blur', cleanup)
      document.body.style.userSelect = prevUserSelect
      cleanupRef.current = null
    }

    function onPointerUp() {
      cleanup()
      // Save on release
      setLayout(prev => {
        saveLayout(prev)
        return prev
      })
    }

    cleanupRef.current = cleanup
    document.addEventListener('pointermove', onPointerMove)
    document.addEventListener('pointerup', onPointerUp)
    document.addEventListener('pointercancel', cleanup)
    window.addEventListener('blur', cleanup)
  }, [layout.upperPct])

  const handleKeyboardResize = useCallback((delta: number) => {
    setLayout(prev => {
      const step = 5
      const newPct = Math.min(MAX_PCT, Math.max(MIN_PCT, prev.upperPct + delta * step))
      const next = { upperPct: Math.round(newPct) }
      saveLayout(next)
      return next
    })
  }, [])

  return {
    layout,
    containerRef,
    handlePointerDown,
    handleKeyboardResize,
    minPct: MIN_PCT,
    maxPct: MAX_PCT,
  }
}
