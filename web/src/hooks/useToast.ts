import { useState, useCallback, useRef, useEffect } from 'react'

export interface ToastAction {
  label: string
  onClick: () => void
}

export interface Toast {
  id: number
  message: string
  type: 'success' | 'error' | 'warning'
  action?: ToastAction
}

export interface ShowToastOptions {
  /** Optional button rendered inside the toast (e.g. "Undo"). */
  action?: ToastAction
  /** Overrides the default auto-dismiss delay. */
  durationMs?: number
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

  const dismissToast = useCallback((id: number) => {
    const timeoutId = timeoutsRef.current.get(id)
    if (timeoutId !== undefined) {
      clearTimeout(timeoutId)
      timeoutsRef.current.delete(id)
    }
    setToasts(prev => prev.filter(t => t.id !== id))
  }, [])

  const showToast = useCallback((
    message: string,
    type: 'success' | 'error' | 'warning',
    options?: ShowToastOptions,
  ) => {
    const id = ++counterRef.current
    setToasts(prev => [...prev, { id, message, type, action: options?.action }])
    const timeoutId = setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id))
      timeoutsRef.current.delete(id)
    }, options?.durationMs ?? TOAST_DURATION_MS)
    timeoutsRef.current.set(id, timeoutId)
    return id
  }, [])

  return { toasts, showToast, dismissToast }
}
