// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Infra, { POLL_BASE_MS, POLL_MAX_MS } from './Infra'

vi.mock('react-i18next', () => {
  const t = (k: string) => k
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
    if (failStatus) return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })
    return json(statusPayload)
  }
  if (url.includes('/api/infra/modules')) return json(modulesPayload)
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

function releaseHungPolls() {
  const resolvers = hangResolvers
  hangResolvers = []
  for (const r of resolvers) {
    r({ ok: true, status: 200, json: () => Promise.resolve(statusPayload) })
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
})
