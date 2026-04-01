import type { ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'

interface CollapsiblePanelHeaderProps {
  isOpen: boolean
  toggle: () => void
  panelId: string
  icon: ReactNode
  title: ReactNode
  /** Optional content rendered between the title and the chevron (badges, counts, etc.) */
  trailing?: ReactNode
}

export function CollapsiblePanelHeader({
  isOpen,
  toggle,
  panelId,
  icon,
  title,
  trailing,
}: CollapsiblePanelHeaderProps) {
  return (
    <button
      type="button"
      onClick={toggle}
      className={`w-full flex items-center gap-2 px-5 py-4 text-left hover:bg-gray-700/30 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-inset ${isOpen ? 'border-b border-gray-700/50' : ''}`}
      aria-expanded={isOpen}
      aria-controls={panelId}
    >
      {icon}
      <span className="text-sm font-medium text-gray-300">{title}</span>
      <span className="ml-auto flex items-center gap-2">
        {trailing}
        <ChevronDown
          size={16}
          className={`shrink-0 text-gray-400 transition-transform duration-200 ${isOpen ? 'rotate-180' : ''}`}
          aria-hidden="true"
        />
      </span>
    </button>
  )
}
