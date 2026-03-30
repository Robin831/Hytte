import { useState, useEffect, useRef } from 'react'
import type { ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'

interface CollapsibleSectionProps {
  id: string
  title: ReactNode
  children: ReactNode
  className?: string
  defaultExpanded?: boolean
  titleClassName?: string
  headingLevel?: 'h2' | 'h3' | 'h4' | 'h5' | 'h6'
}

const STORAGE_KEY = 'settings-section-state'

// Module-level cache: single parse on first access, shared across all instances.
// Prevents repeated JSON.parse/stringify and eliminates stale-read race conditions
// when multiple sections toggle in quick succession.
let stateCache: Record<string, boolean> | null = null

function getSectionState(): Record<string, boolean> {
  if (stateCache !== null) return stateCache
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed: unknown = JSON.parse(raw)
      if (
        parsed !== null &&
        typeof parsed === 'object' &&
        !Array.isArray(parsed) &&
        Object.values(parsed as Record<string, unknown>).every((v) => typeof v === 'boolean')
      ) {
        stateCache = parsed as Record<string, boolean>
        return stateCache
      }
    }
  } catch {
    // ignore
  }
  stateCache = {}
  return stateCache
}

function saveSectionState(id: string, value: boolean) {
  const state = getSectionState()
  state[id] = value
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
  headingLevel: Heading = 'h2',
}: CollapsibleSectionProps) {
  const [expanded, setExpanded] = useState<boolean>(() => {
    const state = getSectionState()
    // Use own-property check to avoid prototype chain surprises
    return Object.prototype.hasOwnProperty.call(state, id) ? state[id] : defaultExpanded
  })

  // Track whether the current value was already in localStorage at mount time,
  // so we only write when the user actually toggles (not on every initial render).
  const isFirstRender = useRef(true)
  useEffect(() => {
    if (isFirstRender.current) {
      isFirstRender.current = false
      return
    }
    saveSectionState(id, expanded)
  }, [id, expanded])

  return (
    <section
      className={`bg-gray-800 rounded-xl mb-6 overflow-hidden ${className}`}
    >
      <Heading>
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
      </Heading>
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
