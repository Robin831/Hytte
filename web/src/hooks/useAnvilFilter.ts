import { useState, useCallback, useMemo } from 'react'

const STORAGE_KEY = 'mezzanine-anvil-filter'

interface AnvilFilterState {
  hiddenAnvils: Set<string>
  toggleAnvil: (anvil: string) => void
  showAll: () => void
  hideAll: (anvils: string[]) => void
  isVisible: (anvil: string) => boolean
  hasFilter: boolean
}

function loadHidden(): Set<string> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) return new Set(JSON.parse(raw) as string[])
  } catch { /* use defaults */ }
  return new Set()
}

function saveHidden(set: Set<string>) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify([...set]))
}

export function useAnvilFilter(): AnvilFilterState {
  const [hiddenAnvils, setHiddenAnvils] = useState<Set<string>>(loadHidden)

  const toggleAnvil = useCallback((anvil: string) => {
    setHiddenAnvils(prev => {
      const next = new Set(prev)
      if (next.has(anvil)) {
        next.delete(anvil)
      } else {
        next.add(anvil)
      }
      saveHidden(next)
      return next
    })
  }, [])

  const showAll = useCallback(() => {
    setHiddenAnvils(new Set())
    saveHidden(new Set())
  }, [])

  const hideAll = useCallback((anvils: string[]) => {
    const set = new Set(anvils)
    setHiddenAnvils(set)
    saveHidden(set)
  }, [])

  const isVisible = useCallback((anvil: string) => {
    return !hiddenAnvils.has(anvil)
  }, [hiddenAnvils])

  const hasFilter = useMemo(() => hiddenAnvils.size > 0, [hiddenAnvils])

  return { hiddenAnvils, toggleAnvil, showAll, hideAll, isVisible, hasFilter }
}
