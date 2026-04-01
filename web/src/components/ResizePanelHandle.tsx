import { useState, useRef, useEffect } from 'react'
import { GripHorizontal } from 'lucide-react'

interface ResizePanelHandleProps {
  id?: string
  'aria-label'?: string
  onPointerDown?: (e: React.PointerEvent) => void
  /** Called with +1 (expand lower/right panel) or -1 (expand upper/left panel) on keyboard arrow keys. */
  onKeyboardResize?: (delta: number) => void
  /** Current panel size as a percentage (for aria-valuenow). */
  value?: number
  /** Minimum allowed size as a percentage (for aria-valuemin). */
  min?: number
  /** Maximum allowed size as a percentage (for aria-valuemax). */
  max?: number
}

export function ResizePanelHandle({ id, 'aria-label': ariaLabel, onPointerDown, onKeyboardResize, value, min, max }: ResizePanelHandleProps) {
  const [active, setActive] = useState(false)
  const cleanupRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    return () => {
      cleanupRef.current?.()
      cleanupRef.current = null
    }
  }, [])

  function handlePointerDown(e: React.PointerEvent) {
    setActive(true)

    const deactivate = () => {
      setActive(false)
      document.removeEventListener('pointerup', deactivate)
      document.removeEventListener('pointercancel', deactivate)
      window.removeEventListener('blur', deactivate)
      cleanupRef.current = null
    }

    cleanupRef.current = deactivate
    document.addEventListener('pointerup', deactivate)
    document.addEventListener('pointercancel', deactivate)
    window.addEventListener('blur', deactivate)
    onPointerDown?.(e)
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (!onKeyboardResize) return
    if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
      e.preventDefault()
      onKeyboardResize(-1)
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
      e.preventDefault()
      onKeyboardResize(1)
    }
  }

  return (
    <div
      id={id}
      aria-label={ariaLabel}
      role="separator"
      aria-orientation="horizontal"
      aria-valuenow={value}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      className="group relative flex items-center justify-center py-1 flex-shrink-0 touch-none focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
      onPointerDown={handlePointerDown}
      onKeyDown={handleKeyDown}
    >
      <div className={`absolute inset-x-0 -inset-y-1 transition-colors rounded ${active ? 'bg-blue-500/15' : 'group-hover:bg-blue-500/10'}`} />
      <div
        className={`relative flex items-center justify-center w-12 h-4 rounded-full transition-colors cursor-row-resize ${active ? 'bg-blue-600' : 'bg-gray-700/50 group-hover:bg-gray-600'}`}
      >
        <GripHorizontal size={14} className={`transition-colors ${active ? 'text-blue-200' : 'text-gray-500 group-hover:text-gray-300'}`} />
      </div>
    </div>
  )
}
