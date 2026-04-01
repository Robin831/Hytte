import { PanelResizeHandle } from 'react-resizable-panels'
import { GripHorizontal } from 'lucide-react'

interface ResizePanelHandleProps {
  id?: string
  'aria-label'?: string
}

export function ResizePanelHandle({ id, 'aria-label': ariaLabel }: ResizePanelHandleProps) {
  return (
    <PanelResizeHandle
      id={id}
      className="group relative flex items-center justify-center py-1 focus-visible:outline-none"
    >
      <div className="absolute inset-x-0 -inset-y-1 group-hover:bg-blue-500/10 group-data-[resize-handle-active]:bg-blue-500/15 transition-colors rounded" />
      <div className="relative flex items-center justify-center w-12 h-4 rounded-full bg-gray-700/50 group-hover:bg-gray-600 group-data-[resize-handle-active]:bg-blue-600 transition-colors cursor-row-resize" aria-label={ariaLabel}>
        <GripHorizontal size={14} className="text-gray-500 group-hover:text-gray-300 group-data-[resize-handle-active]:text-blue-200 transition-colors" />
      </div>
    </PanelResizeHandle>
  )
}
