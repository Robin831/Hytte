import { useState, useEffect, ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'

interface CollapsibleSectionProps {
  id: string
  title: ReactNode
  children: ReactNode
  className?: string
  defaultExpanded?: boolean
  titleClassName?: string
}

const STORAGE_KEY = 'settings-section-state'

function loadSectionState(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) return JSON.parse(raw)
  } catch {
    // ignore
  }
  return {}
}

function saveSectionState(state: Record<string, boolean>) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // ignore
  }
}

export function CollapsibleSection({
  id,
  title,
  children,
  className = '',
  defaultExpanded = false,
  titleClassName = 'text-lg font-semibold',
}: CollapsibleSectionProps) {
  const [expanded, setExpanded] = useState<boolean>(() => {
    const state = loadSectionState()
    return id in state ? state[id] : defaultExpanded
  })
  useEffect(() => {
    const state = loadSectionState()
    state[id] = expanded
    saveSectionState(state)
  }, [id, expanded])

  return (
    <section
      className={`bg-gray-800 rounded-xl mb-6 overflow-hidden ${className}`}
    >
      <button
        type="button"
        onClick={() => setExpanded((prev) => !prev)}
        className="w-full flex items-center justify-between px-6 py-5 text-left hover:bg-gray-700/40 transition-colors cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-inset"
        aria-expanded={expanded}
        aria-controls={`settings-section-${id}`}
      >
        <span className={titleClassName}>{title}</span>
        <ChevronDown
          size={20}
          className={`text-gray-400 flex-shrink-0 transition-transform duration-200 ${expanded ? 'rotate-180' : ''}`}
          aria-hidden="true"
        />
      </button>
      <div
        id={`settings-section-${id}`}
        hidden={!expanded}
      >
        <div className="px-6 pb-6">
          {children}
        </div>
      </div>
    </section>
  )
}
