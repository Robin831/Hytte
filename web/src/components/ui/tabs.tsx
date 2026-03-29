import React, { createContext, useContext } from 'react'
import { cn } from '../../lib/utils'

type TabsVariant = 'segment' | 'pills'

interface TabsContextValue {
  value: string
  onChange: (value: string) => void
  variant: TabsVariant
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
  return (
    <TabsContext.Provider value={{ value, onChange, variant }}>
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
  const { value: activeValue, onChange, variant } = useTabs()
  const isActive = value === activeValue

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
      aria-controls={`tabpanel-${value}`}
      id={`tab-${value}`}
      onClick={() => onChange(value)}
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
  const { value: activeValue } = useTabs()
  if (value !== activeValue) return null
  return (
    <div
      role="tabpanel"
      id={`tabpanel-${value}`}
      aria-labelledby={`tab-${value}`}
      className={className}
    >
      {children}
    </div>
  )
}

export { Tabs, TabList, TabTrigger, TabPanel }
