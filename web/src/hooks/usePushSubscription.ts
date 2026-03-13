import { useState, useEffect, useCallback } from 'react'

type PushState = 'loading' | 'unsupported' | 'denied' | 'subscribed' | 'unsubscribed'

function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(base64)
  const arr = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) {
    arr[i] = raw.charCodeAt(i)
  }
  return arr
}

export function usePushSubscription() {
  const [state, setState] = useState<PushState>('loading')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function checkState() {
      if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
        setState('unsupported')
        return
      }

      const permission = Notification.permission
      if (permission === 'denied') {
        setState('denied')
        return
      }

      try {
        const registration = await navigator.serviceWorker.ready
        const subscription = await registration.pushManager.getSubscription()
        if (!cancelled) {
          setState(subscription ? 'subscribed' : 'unsubscribed')
        }
      } catch {
        if (!cancelled) setState('unsubscribed')
      }
    }

    checkState()
    return () => { cancelled = true }
  }, [])

  const subscribe = useCallback(async () => {
    setError(null)
    try {
      // Fetch the VAPID public key from the server
      const keyRes = await fetch('/api/push/vapid-key', { credentials: 'include' })
      if (!keyRes.ok) {
        throw new Error('Failed to fetch VAPID key')
      }
      const { publicKey } = await keyRes.json()

      const registration = await navigator.serviceWorker.ready
      const subscription = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(publicKey),
      })

      // Send subscription to the server
      const res = await fetch('/api/push/subscribe', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(subscription.toJSON()),
      })

      if (!res.ok) {
        // Unsubscribe locally if the server rejected it
        await subscription.unsubscribe()
        throw new Error('Server rejected push subscription')
      }

      setState('subscribed')
    } catch (err) {
      if (Notification.permission === 'denied') {
        setState('denied')
      }
      setError(err instanceof Error ? err.message : 'Failed to enable notifications')
    }
  }, [])

  const unsubscribe = useCallback(async () => {
    setError(null)
    try {
      const registration = await navigator.serviceWorker.ready
      const subscription = await registration.pushManager.getSubscription()
      if (subscription) {
        await subscription.unsubscribe()
        // Notify the server
        await fetch('/api/push/subscribe', {
          method: 'DELETE',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
          body: JSON.stringify({ endpoint: subscription.endpoint }),
        })
      }
      setState('unsubscribed')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to disable notifications')
    }
  }, [])

  return { state, error, subscribe, unsubscribe }
}
