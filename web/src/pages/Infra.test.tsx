// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Infra, { POLL_BASE_MS, POLL_MAX_MS } from './Infra'

// ── i18n mock ─────────────────────────────────────────────────────────────────
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en' },
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: () => 'Jun 10',
  formatTime: () => '10:00',
  formatDateTime: () => 'Jun 10 10:00',
  formatNumber: (n: number) => String(n),
}))

// ── Auth mock (non-admin keeps ToolVersionsPanel admin fetches out) ──────────
vi.mock('../auth', () => ({
  useAuth: () => ({ user: { name: 'Test', is_admin: false, features: {} } }),
}))

// ── Fetch mock ────────────────────────────────────────────────────────────────

const modulesPayload = {
  modules: [
    { name: 'health', display_name: 'Health Checks', description: 'HTTP checks', enabled: true },
  ],
}

const statusPayload = {
  overall: 'ok',
  modules: [{ name: 'health', status: 'ok', message: '', checked_at: '2026-06-10T10:00:00Z' }],
}

// Each /api/infra/status request is recorded here so tests can count polls
// and inspect the AbortSignal each one was given.
let statusCalls: Array<{ signal: AbortSignal | null }> = []
let failStatus = false
let hangStatus = false

type MockResponse = { ok: boolean; status: number; json: () => Promise<unknown> }

// Resolvers for hung /api/infra/status requests, so tests can complete an
// in-flight poll at a chosen moment instead of leaving it pending forever.
let hangResolvers: Array<(value: MockResponse) => void> = []

const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
  const url = String(input)
  const json = (data: unknown) =>
    Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(data) })
  if (url.includes('/api/infra/status')) {
    statusCalls.push({ signal: init?.signal ?? null })
    if (hangStatus) {
      return new Promise<MockResponse>((resolve) => {
        hangResolvers.push(resolve)
      })
    }
    if (failStatus) {
      return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })
    }
    return json(statusPayload)
  }
  if (url.includes('/api/infra/modules')) {
    return json(modulesPayload)
  }
  // ToolVersionsPanel and friends — empty but valid responses.
  return json({})
})

// ── Visibility stub ───────────────────────────────────────────────────────────

let visibility: DocumentVisibilityState = 'visible'

async function setVisibility(state: DocumentVisibilityState) {
  visibility = state
  await act(async () => {
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0)
  })
}

// ── Helpers ───────────────────────────────────────────────────────────────────

async function renderInfra() {
  const result = render(
    <MemoryRouter>
      <Infra />
    </MemoryRouter>
  )
  // Flush the initial (foreground) load.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
  return result
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

// Completes every hung /api/infra/status request with a successful response.
async function releaseHungPolls() {
  const resolvers = hangResolvers
  hangResolvers = []
  await act(async () => {
    for (const resolve of resolvers) {
      resolve({ ok: true, status: 200, json: () => Promise.resolve(statusPayload) })
    }
    await vi.advanceTimersByTimeAsync(0)
  })
}

beforeEach(() => {
  vi.useFakeTimers()
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
  fetchMock.mockClear()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('Infra background polling', () => {
  it('polls roughly every 60s while the tab is visible', async () => {
    await renderInfra()
    expect(statusCalls.length).toBe(1) // initial load only

    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(2)

    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(3)
  })

  it('does not poll while hidden and refreshes immediately on return', async () => {
    await renderInfra()
    expect(statusCalls.length).toBe(1)

    await setVisibility('hidden')
    await advance(POLL_BASE_MS * 5)
    expect(statusCalls.length).toBe(1) // nothing fired while hidden

    await setVisibility('visible')
    expect(statusCalls.length).toBe(2) // immediate refresh on return

    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(3) // interval resumed
  })

  it('backs off exponentially on failures, caps, and resets on success', async () => {
    await renderInfra()
    failStatus = true

    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(2) // first failed poll → next in 120s

    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(2) // only 60s of the 120s elapsed

    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(3) // second failure → next in 240s

    await advance(POLL_BASE_MS * 2)
    expect(statusCalls.length).toBe(3) // only 120s of the 240s elapsed

    await advance(POLL_BASE_MS * 2)
    expect(statusCalls.length).toBe(4) // third failure → next capped at 480s

    failStatus = false
    await advance(POLL_MAX_MS)
    expect(statusCalls.length).toBe(5) // capped-interval poll succeeds

    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(6) // delay reset to the base interval
  })

  it('keeps previously rendered data on screen when a background poll fails', async () => {
    await renderInfra()
    expect(screen.getByText('Health Checks')).toBeInTheDocument()

    failStatus = true
    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(2)
    expect(screen.getByText('Health Checks')).toBeInTheDocument()
  })

  it('aborts an in-flight background poll on unmount', async () => {
    const { unmount } = await renderInfra()

    hangStatus = true
    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(2)
    const pollSignal = statusCalls[1].signal
    expect(pollSignal?.aborted).toBe(false)

    unmount()
    expect(pollSignal?.aborted).toBe(true)
  })

  it('skips a poll tick while a manual refresh is in flight', async () => {
    await renderInfra()

    hangStatus = true
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /actions\.refresh/ }))
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(statusCalls.length).toBe(2) // the manual refresh itself
    const manualSignal = statusCalls[1].signal

    await advance(POLL_BASE_MS * 2)
    expect(statusCalls.length).toBe(2) // poll ticks skipped, not aborted
    expect(manualSignal?.aborted).toBe(false)
  })

  it('keeps a single timer chain when visibility flips during an in-flight poll', async () => {
    await renderInfra()

    // A background poll fires and hangs, so it is still in flight when the
    // tab is hidden and shown again.
    hangStatus = true
    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(2)

    await setVisibility('hidden')
    await setVisibility('visible')

    // Let the in-flight poll complete. Both the resumed poll and the
    // visibility handler have now scheduled — only one chain may survive.
    hangStatus = false
    await releaseHungPolls()

    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(3) // one poll per interval, not two

    await advance(POLL_BASE_MS)
    expect(statusCalls.length).toBe(4)
  })

  it('stops polling after unmount', async () => {
    const { unmount } = await renderInfra()
    expect(statusCalls.length).toBe(1)

    unmount()
    await advance(POLL_BASE_MS * 3)
    expect(statusCalls.length).toBe(1)
  })
})
