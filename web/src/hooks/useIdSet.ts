import { useCallback, useState } from 'react'

/**
 * A Set of numeric ids kept immutably (a fresh Set on each update) so React
 * re-renders fire, with stable add/remove callbacks.
 */
export function useIdSet() {
  const [ids, setIds] = useState<Set<number>>(() => new Set())
  const add = useCallback((id: number) => {
    setIds(prev => new Set(prev).add(id))
  }, [])
  const remove = useCallback((id: number) => {
    setIds(prev => {
      const next = new Set(prev)
      next.delete(id)
      return next
    })
  }, [])
  return [ids, add, remove] as const
}
