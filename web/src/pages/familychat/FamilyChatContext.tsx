import { createContext, useCallback, useContext, useMemo, useState } from 'react'
import type { ReactNode } from 'react'

interface FamilyChatContextValue {
  // refreshConversations triggers a refetch of the conversation list. Any
  // mutation (create, rename, leave, reorder-on-new-message) can call it
  // without the parent threading a data-loading concern down as a prop.
  refreshConversations: () => void
  // refreshSignal increments on every refreshConversations() call. Consumers
  // add it to a fetch effect's dependency array to refetch when it changes.
  refreshSignal: number
}

const FamilyChatContext = createContext<FamilyChatContextValue | null>(null)

export function FamilyChatProvider({ children }: { children: ReactNode }) {
  const [refreshSignal, setRefreshSignal] = useState(0)

  const refreshConversations = useCallback(() => {
    setRefreshSignal(v => v + 1)
  }, [])

  const value = useMemo(
    () => ({ refreshConversations, refreshSignal }),
    [refreshConversations, refreshSignal],
  )

  return <FamilyChatContext.Provider value={value}>{children}</FamilyChatContext.Provider>
}

export function useFamilyChat(): FamilyChatContextValue {
  const ctx = useContext(FamilyChatContext)
  if (ctx === null) {
    throw new Error('useFamilyChat must be used within a FamilyChatProvider')
  }
  return ctx
}
