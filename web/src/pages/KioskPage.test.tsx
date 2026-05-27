// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen } from '@testing-library/react'
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
    // Use the synchronous advanceTimersByTime so all setInterval callbacks fire
    // within the same JS tick — React batches the setNow updates and flushes
    // them when act() exits, ensuring the badge is in the DOM before the
    // synchronous getByTestId call below.
    succeed = false
    await act(async () => {
      vi.advanceTimersByTime(90_000)
      await flushMicrotasks()
    })

    const badge = screen.getByTestId('kiosk-stale-badge')
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
    // Cross the threshold so the badge becomes visible. Use synchronous timer
    // advancement so React can batch and flush the setNow updates before act()
    // exits, making getByTestId reliable without fake-timer polling.
    await act(async () => {
      vi.advanceTimersByTime(65_000)
      await flushMicrotasks()
    })
    const badge = screen.getByTestId('kiosk-stale-badge')
    const firstText = badge.textContent

    // Now advance a further chunk of time — the staleness clock should tick
    // even though no fetch succeeds.
    await act(async () => {
      vi.advanceTimersByTime(120_000)
      await flushMicrotasks()
    })
    const updated = screen.getByTestId('kiosk-stale-badge')
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
      vi.advanceTimersByTime(90_000)
      await flushMicrotasks()
    })
    expect(screen.getByTestId('kiosk-stale-badge')).toBeInTheDocument()

    // Recovery: the next poll succeeds, so the badge should clear.
    succeed = true
    await act(async () => {
      vi.advanceTimersByTime(30_000)
      await flushMicrotasks()
    })

    // After synchronous timer advancement and act() flushing, the badge should
    // be gone — no need for waitFor which uses the now-faked setTimeout.
    expect(screen.queryByTestId('kiosk-stale-badge')).not.toBeInTheDocument()
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

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => state,
  })
  document.dispatchEvent(new Event('visibilitychange'))
}

describe('KioskPage – visibility-aware polling and backoff', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout', 'Date'] })
    vi.setSystemTime(new Date('2026-05-27T12:00:00Z'))
    try { localStorage.removeItem('hytte_kiosk_token') } catch { /* ignore */ }
    // Reset to visible at the start of each test.
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'visible',
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it('pauses fetches while the tab is hidden and resumes immediately on visible', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // Tab hidden: no further fetches even after several poll intervals.
    await act(async () => {
      setVisibility('hidden')
      await flushMicrotasks()
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // Visibility returns: should fire immediately (not after a 30s wait).
    await act(async () => {
      setVisibility('visible')
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)

    // Regular interval resumes from there.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('escalates failure delays through 30 → 60 → 120 → 300 → 300 then resets on success', async () => {
    let succeed = false
    const fetchMock = vi.fn(() => {
      if (succeed) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response)
      }
      return Promise.reject(new Error('network down'))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    // Initial fetch fails. The 1st failure delay is 30s.
    await act(async () => { await flushMicrotasks() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // 29s later: still no new fetch.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(29_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)

    // 2nd failure → 60s delay.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(59_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(3)

    // 3rd failure → 120s delay.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(119_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(3)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(4)

    // 4th failure → 300s delay (cap).
    await act(async () => {
      await vi.advanceTimersByTimeAsync(299_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(4)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(5)

    // 5th failure also caps at 300s.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(6)

    // Success resets to the 30s baseline.
    succeed = true
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(7)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(8)
  })

  it('treats !res.ok responses as failures and backs off', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // 1st failure → 30s, 2nd → 60s.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)

    // 30s after the 2nd failure: no fetch yet (delay is 60s).
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('cancels in-flight fetches and pending timers on unmount', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { unmount } = renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    unmount()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('polls unconditionally and skips visibility listener when Page Visibility API is unavailable', async () => {
    // Simulate browsers (e.g. Android 5) that do not implement the Page
    // Visibility API. The beforeEach resets visibilityState to 'visible', so we
    // override it to undefined here so that typeof document.visibilityState ===
    // 'undefined' — matching the feature-detection branch in KioskPage.
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => undefined,
    })

    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)
    const addSpy = vi.spyOn(document, 'addEventListener')

    renderKiosk('/kiosk?token=test-token')

    // Initial fetch fires immediately.
    await act(async () => { await flushMicrotasks() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // No visibilitychange listener should have been attached.
    const attachedVisibility = addSpy.mock.calls.some(
      (call) => call[0] === 'visibilitychange',
    )
    expect(attachedVisibility).toBe(false)

    // Polling continues unconditionally at the 30s baseline interval.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })
    expect(fetchMock).toHaveBeenCalledTimes(3)

    addSpy.mockRestore()
  })

  it('does not schedule fetches or attach visibility listeners in mock mode', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new Error('should not be called')))
    vi.stubGlobal('fetch', fetchMock)
    const addSpy = vi.spyOn(document, 'addEventListener')

    renderKiosk('/kiosk')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000)
      await flushMicrotasks()
    })

    expect(fetchMock).not.toHaveBeenCalled()
    const attachedVisibility = addSpy.mock.calls.some(
      (call) => call[0] === 'visibilitychange',
    )
    expect(attachedVisibility).toBe(false)

    addSpy.mockRestore()
  })
})
