// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import KioskPage from './KioskPage'

vi.mock('../components/kiosk/KioskClock', () => ({
  default: () => <div data-testid="mock-clock" />,
}))
vi.mock('../components/kiosk/KioskBusDepartures', () => ({
  default: () => <div data-testid="mock-buses" />,
}))
vi.mock('../components/kiosk/KioskWeather', () => ({
  default: () => <div data-testid="mock-weather" />,
}))
vi.mock('../components/kiosk/KioskSunrise', () => ({
  default: () => <div data-testid="mock-sunrise" />,
}))

const apiPayload = {
  transit: [],
  outdoor: null,
  indoor: null,
  wind: null,
  forecast: null,
  sun: null,
  fetched_at: '2026-05-27T12:00:00Z',
}

function renderKiosk(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/kiosk" element={<KioskPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

async function flushMicrotasks() {
  for (let i = 0; i < 5; i++) {
    await Promise.resolve()
  }
}

describe('KioskPage – stale data badge', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout', 'Date'] })
    vi.setSystemTime(new Date('2026-05-27T12:00:00Z'))
    try { localStorage.removeItem('hytte_kiosk_token') } catch { /* ignore */ }
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it('does not show the stale badge after a successful fetch', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })

    expect(fetchMock).toHaveBeenCalled()
    expect(screen.queryByTestId('kiosk-stale-badge')).not.toBeInTheDocument()

    // Advance by a single poll cycle — fetches keep succeeding so still fresh.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })
    expect(screen.queryByTestId('kiosk-stale-badge')).not.toBeInTheDocument()
  })

  it('shows the stale badge after repeated failures past the threshold', async () => {
    let succeed = true
    const fetchMock = vi.fn(() => {
      if (succeed) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response)
      }
      return Promise.reject(new Error('network down'))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    // Let the initial successful fetch resolve and record last-success time.
    await act(async () => { await flushMicrotasks() })
    expect(screen.queryByTestId('kiosk-stale-badge')).not.toBeInTheDocument()

    // From here on, fetches fail. Advance well past 2 * POLL_INTERVAL_MS (60s).
    succeed = false
    await act(async () => {
      await vi.advanceTimersByTimeAsync(90_000)
      await flushMicrotasks()
    })

    const badge = await screen.findByTestId('kiosk-stale-badge')
    expect(badge).toBeInTheDocument()
    expect(badge.textContent).toMatch(/Updated .* (sec|min|hr) ago/)
  })

  it('updates the stale badge age via the clock tick without new fetches', async () => {
    let succeed = true
    const fetchMock = vi.fn(() => {
      if (succeed) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response)
      }
      return Promise.reject(new Error('network down'))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })

    succeed = false
    // Cross the threshold so the badge becomes visible.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(65_000)
      await flushMicrotasks()
    })
    const badge = await screen.findByTestId('kiosk-stale-badge')
    const firstText = badge.textContent

    // Now advance a further chunk of time — the staleness clock should tick
    // even though no fetch succeeds.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000)
      await flushMicrotasks()
    })
    const updated = await screen.findByTestId('kiosk-stale-badge')
    expect(updated.textContent).not.toEqual(firstText)
  })

  it('clears the stale badge once a fetch succeeds again', async () => {
    let succeed = true
    const fetchMock = vi.fn(() => {
      if (succeed) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response)
      }
      return Promise.reject(new Error('network down'))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })

    succeed = false
    await act(async () => {
      await vi.advanceTimersByTimeAsync(90_000)
      await flushMicrotasks()
    })
    expect(await screen.findByTestId('kiosk-stale-badge')).toBeInTheDocument()

    // Recovery: the next poll succeeds, so the badge should clear.
    succeed = true
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })

    await waitFor(() => {
      expect(screen.queryByTestId('kiosk-stale-badge')).not.toBeInTheDocument()
    })
  })

  it('does not show the badge in mock mode (no token)', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new Error('should not be called')))
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000)
      await flushMicrotasks()
    })

    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.queryByTestId('kiosk-stale-badge')).not.toBeInTheDocument()
  })
})
