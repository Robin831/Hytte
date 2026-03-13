import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { usePushSubscription } from './usePushSubscription'

function mockPushManager(subscription: unknown = null) {
  return {
    getSubscription: vi.fn().mockResolvedValue(subscription),
    subscribe: vi.fn(),
  }
}

function mockServiceWorker(pushManager: ReturnType<typeof mockPushManager>) {
  const registration = { pushManager }
  Object.defineProperty(navigator, 'serviceWorker', {
    value: {
      ready: Promise.resolve(registration),
      controller: {},
      register: vi.fn().mockResolvedValue(registration),
    },
    writable: true,
    configurable: true,
  })
  return registration
}

function mockNotification(permission: NotificationPermission = 'default') {
  Object.defineProperty(window, 'Notification', {
    value: { permission },
    writable: true,
    configurable: true,
  })
}

function mockPushManagerOnWindow() {
  Object.defineProperty(window, 'PushManager', {
    value: {},
    writable: true,
    configurable: true,
  })
}

function clearMocks() {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (navigator as any).serviceWorker
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).PushManager
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).Notification
}

describe('usePushSubscription', () => {
  beforeEach(() => {
    clearMocks()
    vi.restoreAllMocks()
  })

  afterEach(() => {
    clearMocks()
  })

  it('returns unsupported when serviceWorker is not available', async () => {
    mockNotification()
    const { result } = renderHook(() => usePushSubscription(true))
    await waitFor(() => expect(result.current.state).toBe('unsupported'))
  })

  it('returns unsupported when PushManager is not available', async () => {
    Object.defineProperty(navigator, 'serviceWorker', {
      value: { ready: Promise.resolve({}), controller: {} },
      writable: true,
      configurable: true,
    })
    mockNotification()
    const { result } = renderHook(() => usePushSubscription(true))
    await waitFor(() => expect(result.current.state).toBe('unsupported'))
  })

  it('returns unsubscribed when user is not authenticated', async () => {
    const pm = mockPushManager()
    mockServiceWorker(pm)
    mockPushManagerOnWindow()
    mockNotification()
    const { result } = renderHook(() => usePushSubscription(false))
    await waitFor(() => expect(result.current.state).toBe('unsubscribed'))
    // Should NOT register the service worker when unauthenticated
    expect(pm.getSubscription).not.toHaveBeenCalled()
  })

  it('returns denied when notification permission is denied', async () => {
    mockServiceWorker(mockPushManager())
    mockPushManagerOnWindow()
    mockNotification('denied')
    const { result } = renderHook(() => usePushSubscription(true))
    await waitFor(() => expect(result.current.state).toBe('denied'))
  })

  it('returns subscribed when existing subscription found', async () => {
    const existingSub = { endpoint: 'https://push.example.com/sub1' }
    mockServiceWorker(mockPushManager(existingSub))
    mockPushManagerOnWindow()
    mockNotification('granted')
    const { result } = renderHook(() => usePushSubscription(true))
    await waitFor(() => expect(result.current.state).toBe('subscribed'))
  })

  it('returns unsubscribed when no existing subscription', async () => {
    mockServiceWorker(mockPushManager(null))
    mockPushManagerOnWindow()
    mockNotification('granted')
    const { result } = renderHook(() => usePushSubscription(true))
    await waitFor(() => expect(result.current.state).toBe('unsubscribed'))
  })

  it('subscribe fetches VAPID key and creates subscription', async () => {
    const mockSub = {
      endpoint: 'https://push.example.com/sub1',
      toJSON: () => ({ endpoint: 'https://push.example.com/sub1' }),
      unsubscribe: vi.fn(),
    }
    const pm = mockPushManager(null)
    pm.subscribe.mockResolvedValue(mockSub)
    mockServiceWorker(pm)
    mockPushManagerOnWindow()
    mockNotification('default')

    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ publicKey: 'dGVzdA' }) })
      .mockResolvedValueOnce({ ok: true }))

    const { result } = renderHook(() => usePushSubscription(true))
    await waitFor(() => expect(result.current.state).toBe('unsubscribed'))

    await act(async () => {
      await result.current.subscribe()
    })

    expect(result.current.state).toBe('subscribed')
    expect(fetch).toHaveBeenCalledWith('/api/push/vapid-key', { credentials: 'include' })
    expect(pm.subscribe).toHaveBeenCalled()
  })

  it('subscribe rolls back on server rejection', async () => {
    const mockSub = {
      endpoint: 'https://push.example.com/sub1',
      toJSON: () => ({ endpoint: 'https://push.example.com/sub1' }),
      unsubscribe: vi.fn().mockResolvedValue(true),
    }
    const pm = mockPushManager(null)
    pm.subscribe.mockResolvedValue(mockSub)
    mockServiceWorker(pm)
    mockPushManagerOnWindow()
    mockNotification('default')

    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ publicKey: 'dGVzdA' }) })
      .mockResolvedValueOnce({ ok: false, status: 500 }))

    const { result } = renderHook(() => usePushSubscription(true))
    await waitFor(() => expect(result.current.state).toBe('unsubscribed'))

    await act(async () => {
      await result.current.subscribe()
    })

    expect(mockSub.unsubscribe).toHaveBeenCalled()
    expect(result.current.error).toBe('Server rejected push subscription')
  })

  it('unsubscribe removes subscription and notifies server', async () => {
    const mockSub = {
      endpoint: 'https://push.example.com/sub1',
      unsubscribe: vi.fn().mockResolvedValue(true),
    }
    const pm = mockPushManager(mockSub)
    mockServiceWorker(pm)
    mockPushManagerOnWindow()
    mockNotification('granted')

    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))

    const { result } = renderHook(() => usePushSubscription(true))
    await waitFor(() => expect(result.current.state).toBe('subscribed'))

    await act(async () => {
      await result.current.unsubscribe()
    })

    expect(result.current.state).toBe('unsubscribed')
    expect(mockSub.unsubscribe).toHaveBeenCalled()
    expect(fetch).toHaveBeenCalledWith('/api/push/subscribe', expect.objectContaining({ method: 'DELETE' }))
  })
})
