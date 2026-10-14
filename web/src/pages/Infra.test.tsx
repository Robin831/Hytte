// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import Infra, { POLL_BASE_MS, POLL_MAX_MS, STALE_AFTER_FAILURES } from './Infra'

vi.mock('react-i18next', () => {
  const t = (k: string, o?: { time?: string }) => (o?.time ? `${k}:${o.time}` : k)
  const i18n = { language: 'en' }
  const hook = { t, i18n }
  return {
    useTranslation: () => hook,
    Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
    initReactI18next: { type: '3rdParty', init: () => {} },
  }
})
vi.mock('../utils/formatDate', () => ({
  formatDate: () => 'Jun 10', formatTime: () => '10:00',
  formatDateTime: () => 'Jun 10 10:00', formatNumber: (n: number) => String(n),
}))
vi.mock('../auth', () => ({
  useAuth: () => ({ user: { name: 'Test', is_admin: false, features: {} } }),
}))

const modulesPayload = {
  modules: [
    { name: 'health', display_name: 'Health Checks', description: 'HTTP checks', enabled: true },
  ],
}
const statusPayload = {
  overall: 'ok',
  modules: [{ name: 'health', status: 'ok', message: '', checked_at: '2026-06-10T10:00:00Z' }],
}

let statusCalls: Array<{ signal: AbortSignal | null }> = []
let failStatus = false
let failStatusCode = 500
let failModulesCode: number | null = null
let abortStatus = false
let hangStatus = false
type MockResponse = { ok: boolean; status: number; json: () => Promise<unknown> }
let hangResolvers: Array<(value: MockResponse) => void> = []

const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
  const url = String(input)
  const json = (data: unknown) =>
    Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(data) })
  if (url.includes('/api/infra/status')) {
    statusCalls.push({ signal: init?.signal ?? null })
    if (hangStatus) return new Promise<MockResponse>(r => { hangResolvers.push(r) })
    if (abortStatus) return Promise.reject(new DOMException('aborted', 'AbortError'))
    if (failStatus) return Promise.resolve({ ok: false, status: failStatusCode, json: () => Promise.resolve({}) })
    return json(statusPayload)
  }
  if (url.includes('/api/infra/modules')) {
    if (failModulesCode !== null) {
      return Promise.resolve({ ok: false, status: failModulesCode, json: () => Promise.resolve({}) })
    }
    return json(modulesPayload)
  }
  return json({})
})

let visibility: DocumentVisibilityState = 'visible'

type CapturedTimer = { id: number; callback: () => void; delay: number }
let capturedTimers: CapturedTimer[] = []
let nextId = 1_000_000
let origSetTimeout: typeof window.setTimeout
let origClearTimeout: typeof window.clearTimeout

async function flushAsync() {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve()
  })
}

async function renderInfra() {
  const result = render(<MemoryRouter><Infra /></MemoryRouter>)
  await waitFor(() => {
    expect(screen.getByText('Health Checks')).toBeInTheDocument()
  })
  return result
}

async function triggerPoll() {
  const toFire = [...capturedTimers]
  capturedTimers = []
  for (const t of toFire) t.callback()
  await flushAsync()
}

async function setVis(state: DocumentVisibilityState) {
  visibility = state
  document.dispatchEvent(new Event('visibilitychange'))
  await flushAsync()
}

function releaseHungPolls(status = 200) {
  const resolvers = hangResolvers
  hangResolvers = []
  for (const r of resolvers) {
    r({ ok: status < 400, status, json: () => Promise.resolve(status < 400 ? statusPayload : {}) })
  }
}

describe('Infra background polling', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => visibility,
    })
    visibility = 'visible'
    statusCalls = []
    failStatus = false
    failStatusCode = 500
    failModulesCode = null
    abortStatus = false
    hangStatus = false
    hangResolvers = []
    capturedTimers = []
    nextId = 1_000_000
    fetchMock.mockClear()

    origSetTimeout = window.setTimeout.bind(window)
    origClearTimeout = window.clearTimeout.bind(window)

    vi.spyOn(window, 'setTimeout').mockImplementation(((cb: TimerHandler, delay?: number, ...args: unknown[]) => {
      if (typeof cb === 'function' && (delay ?? 0) >= 1000) {
        const id = nextId++
        capturedTimers.push({ id, callback: cb as () => void, delay: delay ?? 0 })
        return id
      }
      return origSetTimeout(cb, delay, ...args)
    }) as typeof window.setTimeout)

    vi.spyOn(window, 'clearTimeout').mockImplementation(((id?: number) => {
      const idx = capturedTimers.findIndex(t => t.id === id)
      if (idx >= 0) capturedTimers.splice(idx, 1)
      else origClearTimeout(id)
    }) as typeof window.clearTimeout)
  })

  afterEach(() => {
    delete (document as unknown as Record<string, unknown>).visibilityState
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('schedules a poll at the base interval after mount', async () => {
    await renderInfra()
    expect(statusCalls.length).toBe(1)
    expect(capturedTimers.length).toBe(1)
    expect(capturedTimers[0].delay).toBe(POLL_BASE_MS)
  })

  it('fires poll and reschedules at base interval', async () => {
    await renderInfra()
    expect(statusCalls.length).toBe(1)

    await triggerPoll()
    expect(statusCalls.length).toBe(2)
    expect(capturedTimers.length).toBe(1)
    expect(capturedTimers[0].delay).toBe(POLL_BASE_MS)

    await triggerPoll()
    expect(statusCalls.length).toBe(3)
  })

  it('backs off exponentially on failures and resets on success', async () => {
    await renderInfra()
    failStatus = true

    await triggerPoll()
    expect(statusCalls.length).toBe(2)
    expect(capturedTimers[0].delay).toBe(POLL_BASE_MS * 2)

    await triggerPoll()
    expect(statusCalls.length).toBe(3)
    expect(capturedTimers[0].delay).toBe(POLL_BASE_MS * 4)

    await triggerPoll()
    expect(statusCalls.length).toBe(4)
    expect(capturedTimers[0].delay).toBe(POLL_MAX_MS)

    failStatus = false
    await triggerPoll()
    expect(statusCalls.length).toBe(5)
    expect(capturedTimers[0].delay).toBe(POLL_BASE_MS)
  })

  it('keeps previously rendered data when a background poll fails and does not show error banner', async () => {
    await renderInfra()
    expect(screen.getByText('Health Checks')).toBeInTheDocument()

    failStatus = true
    await triggerPoll()
    expect(statusCalls.length).toBe(2)
    expect(screen.getByText('Health Checks')).toBeInTheDocument()
    expect(screen.queryByText('Failed to load status (500)')).not.toBeInTheDocument()
  })

  it('does not poll while hidden and refreshes on return', async () => {
    await renderInfra()
    expect(statusCalls.length).toBe(1)

    await setVis('hidden')
    const countAfterHide = statusCalls.length
    if (capturedTimers.length > 0) await triggerPoll()
    expect(statusCalls.length).toBeLessThanOrEqual(countAfterHide + 1)

    await setVis('visible')
    await flushAsync()
    expect(statusCalls.length).toBeGreaterThan(1)
  })

  it('aborts an in-flight background poll on unmount', async () => {
    const { unmount } = await renderInfra()
    hangStatus = true

    await triggerPoll()
    expect(statusCalls.length).toBe(2)
    const pollSignal = statusCalls[1].signal
    expect(pollSignal?.aborted).toBe(false)

    unmount()
    expect(pollSignal?.aborted).toBe(true)
  })

  it('skips a poll tick while a manual refresh is in flight', async () => {
    await renderInfra()

    hangStatus = true
    fireEvent.click(screen.getByText('actions.refresh'))
    await flushAsync()
    expect(statusCalls.length).toBe(2)
    const manualSignal = statusCalls[1].signal

    await triggerPoll()
    expect(statusCalls.length).toBe(2)
    expect(manualSignal?.aborted).toBe(false)
  })

  it('keeps a single timer chain when visibility flips during an in-flight poll', async () => {
    await renderInfra()

    hangStatus = true
    await triggerPoll()
    expect(statusCalls.length).toBe(2)

    await setVis('hidden')
    await setVis('visible')

    hangStatus = false
    releaseHungPolls()
    await flushAsync()

    const countAfter = statusCalls.length
    await triggerPoll()
    expect(statusCalls.length).toBe(countAfter + 1)

    await triggerPoll()
    expect(statusCalls.length).toBe(countAfter + 2)
  })

  it('stops polling after unmount', async () => {
    const { unmount } = await renderInfra()
    expect(statusCalls.length).toBe(1)

    unmount()
    capturedTimers = []
    await flushAsync()
    expect(statusCalls.length).toBe(1)
  })

  describe('staleness', () => {
    const statusContent = () => screen.getByTestId('infra-status-content')

    it('shows a freshness line after the first load and keeps it on a failed poll', async () => {
      await renderInfra()
      expect(screen.getByTestId('infra-freshness')).toHaveTextContent('updatedAgo:justNow')

      failStatus = true
      await triggerPoll()
      expect(screen.getByTestId('infra-freshness')).toHaveTextContent('updatedAgo:justNow')
      expect(screen.queryByText('neverUpdated')).not.toBeInTheDocument()
    })

    it('ages the freshness line without a reload', async () => {
      const intervals: Array<() => void> = []
      vi.spyOn(window, 'setInterval').mockImplementation(((cb: TimerHandler) => {
        if (typeof cb === 'function') intervals.push(cb as () => void)
        return 1
      }) as typeof window.setInterval)
      vi.spyOn(window, 'clearInterval').mockImplementation(() => {})

      await renderInfra()
      expect(screen.getByTestId('infra-freshness')).toHaveTextContent('updatedAgo:justNow')

      // Mock the clock only after the load so testing-library's own timing
      // is unaffected; the last success happened just before this point.
      vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 3 * 60_000)
      act(() => { for (const cb of intervals) cb() })
      expect(screen.getByTestId('infra-freshness')).toHaveTextContent('updatedAgo:3 minutes ago')
    })

    it('does not show the stale banner after a single failed poll', async () => {
      await renderInfra()
      failStatus = true
      await triggerPoll()
      expect(screen.queryByText('staleBanner')).not.toBeInTheDocument()
      expect(statusContent()).toHaveAttribute('data-stale', 'false')
    })

    it('shows the stale banner and dims the cards after consecutive failed polls', async () => {
      await renderInfra()
      failStatus = true
      for (let i = 0; i < STALE_AFTER_FAILURES; i++) await triggerPoll()
      expect(screen.getByText('staleBanner')).toBeInTheDocument()
      expect(statusContent()).toHaveAttribute('data-stale', 'true')
      expect(statusContent().className).toContain('opacity-50')
    })

    it('retry reloads, clears the banner and resets the failure counter', async () => {
      await renderInfra()
      failStatus = true
      await triggerPoll()
      await triggerPoll()
      expect(screen.getByText('staleBanner')).toBeInTheDocument()

      failStatus = false
      const before = statusCalls.length
      fireEvent.click(screen.getByText('retry'))
      await flushAsync()
      expect(statusCalls.length).toBe(before + 1)
      expect(screen.queryByText('staleBanner')).not.toBeInTheDocument()
      expect(statusContent()).toHaveAttribute('data-stale', 'false')

      // Counter was reset: one further failure must not bring the banner back.
      failStatus = true
      await triggerPoll()
      expect(screen.queryByText('staleBanner')).not.toBeInTheDocument()
    })

    it('a successful poll clears the banner and resets the poll delay', async () => {
      await renderInfra()
      failStatus = true
      await triggerPoll()
      await triggerPoll()
      expect(screen.getByText('staleBanner')).toBeInTheDocument()

      failStatus = false
      await triggerPoll()
      expect(screen.queryByText('staleBanner')).not.toBeInTheDocument()
      expect(capturedTimers[0].delay).toBe(POLL_BASE_MS)
    })

    it('a successful manual refresh resets the backoff to the base delay', async () => {
      await renderInfra()
      failStatus = true
      await triggerPoll()
      await triggerPoll()
      expect(capturedTimers[0].delay).toBe(POLL_BASE_MS * 4)

      failStatus = false
      fireEvent.click(screen.getByText('actions.refresh'))
      await flushAsync()
      expect(screen.queryByText('staleBanner')).not.toBeInTheDocument()

      // The pending timer still carries the old delay; the next failure must
      // double from the base delay rather than from the backed-off one.
      failStatus = true
      await triggerPoll()
      expect(capturedTimers[0].delay).toBe(POLL_BASE_MS * 2)
    })

    it('shows the session-expired prompt after one 401 from status', async () => {
      await renderInfra()
      failStatus = true
      failStatusCode = 401
      await triggerPoll()
      expect(screen.getByText('sessionExpired')).toBeInTheDocument()
      expect(screen.getByRole('link', { name: 'signIn' })).toHaveAttribute('href', '/api/auth/google/login')
      expect(screen.queryByText('staleBanner')).not.toBeInTheDocument()
      expect(statusContent()).toHaveAttribute('data-stale', 'true')
      // Auth failures do not double the backoff.
      expect(capturedTimers[0].delay).toBe(POLL_BASE_MS)
    })

    it('treats a 403 (feature not enabled) as a normal failure, not an expired session', async () => {
      await renderInfra()
      failStatus = true
      failStatusCode = 403
      await triggerPoll()
      expect(screen.queryByText('sessionExpired')).not.toBeInTheDocument()
      // It backs off like any other failure instead of polling at the base rate.
      expect(capturedTimers[0].delay).toBe(POLL_BASE_MS * 2)

      await triggerPoll()
      expect(screen.queryByText('sessionExpired')).not.toBeInTheDocument()
      expect(screen.getByText('staleBanner')).toBeInTheDocument()
    })

    it('does not show the red error banner for a 401 on manual refresh', async () => {
      await renderInfra()
      failStatus = true
      failStatusCode = 401
      fireEvent.click(screen.getByText('actions.refresh'))
      await flushAsync()
      expect(screen.getByText('sessionExpired')).toBeInTheDocument()
      expect(screen.queryByText('Failed to load status (401)')).not.toBeInTheDocument()
    })

    it('shows the session-expired prompt when the modules endpoint returns 401', async () => {
      await renderInfra()
      failModulesCode = 401
      await triggerPoll()
      expect(screen.getByText('sessionExpired')).toBeInTheDocument()
    })

    it('prefers the session-expired prompt over a concurrent server error', async () => {
      await renderInfra()
      failModulesCode = 500
      failStatus = true
      failStatusCode = 401
      await triggerPoll()
      expect(screen.getByText('sessionExpired')).toBeInTheDocument()
    })

    it('clears the session-expired prompt after a successful load', async () => {
      await renderInfra()
      failStatus = true
      failStatusCode = 401
      await triggerPoll()
      expect(screen.getByText('sessionExpired')).toBeInTheDocument()

      failStatus = false
      await triggerPoll()
      expect(screen.queryByText('sessionExpired')).not.toBeInTheDocument()
      expect(statusContent()).toHaveAttribute('data-stale', 'false')
    })

    it('does not count aborted polls as failures', async () => {
      await renderInfra()
      abortStatus = true
      await triggerPoll()
      await triggerPoll()
      await triggerPoll()
      expect(screen.queryByText('staleBanner')).not.toBeInTheDocument()
      expect(statusContent()).toHaveAttribute('data-stale', 'false')
    })

    it('does not count a poll superseded by a module toggle as a failure', async () => {
      await renderInfra()
      failStatus = true
      await triggerPoll()

      // Next poll hangs and is then aborted by a module toggle whose reload
      // fails. (The refresh button is disabled while the poll is in flight.)
      hangStatus = true
      await triggerPoll()
      const pollSignal = statusCalls[statusCalls.length - 1].signal
      hangStatus = false
      fireEvent.click(screen.getByRole('button', { name: 'toggle.disableLabel' }))
      await flushAsync()
      expect(pollSignal?.aborted).toBe(true)
      // The superseded poll then fails too; it must not count as a failure.
      releaseHungPolls(500)
      await flushAsync()

      // Only one background failure was recorded, so no stale banner.
      expect(screen.queryByText('staleBanner')).not.toBeInTheDocument()
      expect(statusContent()).toHaveAttribute('data-stale', 'false')
    })

    describe('module detail view', () => {
      const detailContent = () => screen.getByTestId('infra-detail-content')

      async function openDetail() {
        await renderInfra()
        fireEvent.click(screen.getByText('Health Checks'))
        await waitFor(() => expect(screen.getByText('backToOverview')).toBeInTheDocument())
        expect(detailContent()).toHaveAttribute('data-stale', 'false')
      }

      it('shows the stale banner and dims the detail after consecutive failed polls', async () => {
        await openDetail()
        failStatus = true
        for (let i = 0; i < STALE_AFTER_FAILURES; i++) await triggerPoll()
        expect(screen.getByText('staleBanner')).toBeInTheDocument()
        expect(detailContent()).toHaveAttribute('data-stale', 'true')
        expect(detailContent().className).toContain('opacity-50')
      })

      it('shows the session-expired prompt and dims the detail after a 401', async () => {
        await openDetail()
        failStatus = true
        failStatusCode = 401
        await triggerPoll()
        expect(screen.getByText('sessionExpired')).toBeInTheDocument()
        expect(detailContent()).toHaveAttribute('data-stale', 'true')
      })
    })
  })
})
