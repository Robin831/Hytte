import { useState, useCallback, useRef, useEffect } from 'react'

export interface Toast {
  id: number
  message: string
  type: 'success' | 'error' | 'warning'
}

const TOAST_DURATION_MS = 3500

export function useToast() {
  const [toasts, setToasts] = useState<Toast[]>([])
  const counterRef = useRef(0)
  const timeoutsRef = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map())

  useEffect(() => {
    return () => {
      timeoutsRef.current.forEach(id => clearTimeout(id))
    }
  }, [])

  const showToast = useCallback((message: string, type: 'success' | 'error' | 'warning') => {
    const id = ++counterRef.current
    setToasts(prev => [...prev, { id, message, type }])
    const timeoutId = setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id))
      timeoutsRef.current.delete(id)
    }, TOAST_DURATION_MS)
    timeoutsRef.current.set(id, timeoutId)
  }, [])

  return { toasts, showToast }
}
