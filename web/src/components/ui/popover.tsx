import React, { useEffect, useRef } from 'react'
import { cn } from '../../lib/utils'

interface PopoverProps {
  open: boolean
  onClose: () => void
  anchor: React.ReactNode
  children: React.ReactNode
  className?: string
  align?: 'start' | 'end' | 'center'
  side?: 'top' | 'bottom'
}

function Popover({
  open,
  onClose,
  anchor,
  children,
  className,
  align = 'start',
  side = 'bottom',
}: PopoverProps) {
  const containerRef = useRef<HTMLDivElement>(null)

  // Close on click outside
  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onClose()
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open, onClose])

  // Close on Escape
  useEffect(() => {
    if (!open) return
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [open, onClose])

  return (
    <div ref={containerRef} className="relative">
      {anchor}
      {open && (
        <div
          role="presentation"
          className={cn(
            'absolute z-50 bg-gray-800 border border-gray-700 rounded-lg shadow-xl',
            side === 'top' ? 'bottom-full mb-1' : 'top-full mt-1',
            align === 'start'
              ? 'left-0'
              : align === 'end'
                ? 'right-0'
                : 'left-1/2 -translate-x-1/2',
            className
          )}
        >
          {children}
        </div>
      )}
    </div>
  )
}

export { Popover }
