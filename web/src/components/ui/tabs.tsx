import React, { createContext, useContext, useId, useRef, useEffect } from 'react'
import { cn } from '../../lib/utils'

type TabsVariant = 'segment' | 'pills'

interface TabsContextValue {
  value: string
  onChange: (value: string) => void
  variant: TabsVariant
  instanceId: string
  tabsRef: React.MutableRefObject<string[]>
}

const TabsContext = createContext<TabsContextValue | null>(null)

function useTabs() {
  const ctx = useContext(TabsContext)
  if (!ctx) throw new Error('Tab components must be used within <Tabs>')
  return ctx
}

interface TabsProps {
  value: string
  onChange: (value: string) => void
  variant?: TabsVariant
  children: React.ReactNode
  className?: string
}

function Tabs({ value, onChange, variant = 'pills', children, className }: TabsProps) {
  const instanceId = useId()
  const tabsRef = useRef<string[]>([])
  return (
    <TabsContext.Provider value={{ value, onChange, variant, instanceId, tabsRef }}>
      <div className={className}>{children}</div>
    </TabsContext.Provider>
  )
}

interface TabListProps {
  children: React.ReactNode
  className?: string
  'aria-label'?: string
}

function TabList({ children, className, 'aria-label': ariaLabel }: TabListProps) {
  const { variant } = useTabs()
  return (
    <div
      role="tablist"
      aria-label={ariaLabel}
      className={cn(
        'flex overflow-x-auto mb-6',
        variant === 'segment'
          ? 'gap-1 bg-gray-800 rounded-lg p-1'
          : 'gap-2',
        className
      )}
    >
      {children}
    </div>
  )
}

interface TabTriggerProps {
  value: string
  children: React.ReactNode
  className?: string
}

function TabTrigger({ value, children, className }: TabTriggerProps) {
  const { value: activeValue, onChange, variant, instanceId, tabsRef } = useTabs()
  const isActive = value === activeValue

  useEffect(() => {
    if (!tabsRef.current.includes(value)) {
      tabsRef.current = [...tabsRef.current, value]
    }
    return () => {
      tabsRef.current = tabsRef.current.filter(v => v !== value)
    }
  }, [value, tabsRef])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    const tabs = tabsRef.current
    const idx = tabs.indexOf(value)
    let nextIdx: number | null = null

    if (e.key === 'ArrowRight') {
      nextIdx = (idx + 1) % tabs.length
    } else if (e.key === 'ArrowLeft') {
      nextIdx = (idx - 1 + tabs.length) % tabs.length
    } else if (e.key === 'Home') {
      nextIdx = 0
    } else if (e.key === 'End') {
      nextIdx = tabs.length - 1
    }

    if (nextIdx !== null) {
      e.preventDefault()
      const nextValue = tabs[nextIdx]
      onChange(nextValue)
      document.getElementById(`tab-${instanceId}-${nextValue}`)?.focus()
    }
  }

  const segmentClasses = isActive
    ? 'bg-gray-700 text-white'
    : 'text-gray-400 hover:text-white'

  const pillsClasses = isActive
    ? 'bg-blue-500/20 text-blue-400 border border-blue-500/40'
    : 'bg-gray-800 text-gray-400 border border-gray-700 hover:text-white hover:border-gray-600'

  return (
    <button
      type="button"
      role="tab"
      aria-selected={isActive}
      aria-controls={`tabpanel-${instanceId}-${value}`}
      id={`tab-${instanceId}-${value}`}
      tabIndex={isActive ? 0 : -1}
      onClick={() => onChange(value)}
      onKeyDown={handleKeyDown}
      className={cn(
        'whitespace-nowrap transition-colors cursor-pointer',
        variant === 'segment'
          ? cn('flex-1 py-2 px-3 rounded-md text-sm font-medium', segmentClasses)
          : cn('px-4 py-2 text-sm rounded-lg', pillsClasses),
        className
      )}
    >
      {children}
    </button>
  )
}

interface TabPanelProps {
  value: string
  children: React.ReactNode
  className?: string
}

function TabPanel({ value, children, className }: TabPanelProps) {
  const { value: activeValue, instanceId } = useTabs()
  const isActive = value === activeValue
  return (
    <div
      role="tabpanel"
      id={`tabpanel-${instanceId}-${value}`}
      aria-labelledby={`tab-${instanceId}-${value}`}
      hidden={!isActive}
      className={isActive ? className : undefined}
    >
      {children}
    </div>
  )
}

export { Tabs, TabList, TabTrigger, TabPanel }
