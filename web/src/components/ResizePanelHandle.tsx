import { useState } from 'react'
import { GripHorizontal } from 'lucide-react'

interface ResizePanelHandleProps {
  id?: string
  'aria-label'?: string
  onMouseDown?: (e: React.MouseEvent) => void
}

export function ResizePanelHandle({ id, 'aria-label': ariaLabel, onMouseDown }: ResizePanelHandleProps) {
  const [active, setActive] = useState(false)

  function handleMouseDown(e: React.MouseEvent) {
    setActive(true)
    const onUp = () => {
      setActive(false)
      document.removeEventListener('mouseup', onUp)
    }
    document.addEventListener('mouseup', onUp)
    onMouseDown?.(e)
  }

  return (
    <div
      id={id}
      role="separator"
      aria-orientation="horizontal"
      aria-label={ariaLabel}
      tabIndex={0}
      className="group relative flex items-center justify-center py-1 focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none flex-shrink-0"
      onMouseDown={handleMouseDown}
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
