// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route, useNavigate } from 'react-router'
import KioskPage from './KioskPage'
import { renderKiosk, flushMicrotasks } from '../test/kiosk'
import enKiosk from '../../public/locales/en/kiosk.json'
import nbKiosk from '../../public/locales/nb/kiosk.json'
import thKiosk from '../../public/locales/th/kiosk.json'

// KioskPage resolves its status-screen copy through react-i18next; the real
// runtime loads locale JSON over HTTP, so resolve keys against the shipped
// en/kiosk.json instead. The inline defaults the page passes are ignored, so a
// renamed or dropped key fails these tests rather than falling back silently.
vi.mock('react-i18next', async () => {
  const { kioskT } = await import('../test/kioskI18n')
  return {
    useTranslation: () => ({ t: kioskT, i18n: { language: 'en' } }),
  }
})

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

describe('KioskPage – fetch state screens', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout', 'Date'] })
    vi.setSystemTime(new Date('2026-05-27T12:00:00Z'))
    try { localStorage.removeItem('hytte_kiosk_token') } catch { /* ignore */ }
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

  it('shows the loading screen while the first fetch is still in flight', async () => {
    const fetchMock = vi.fn(() => new Promise<Response>(() => {}))
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })

    expect(screen.getByTestId('kiosk-loading')).toBeInTheDocument()
    // None of the normal kiosk panels (and therefore no mock data) are rendered.
    expect(screen.queryByTestId('mock-buses')).not.toBeInTheDocument()
    expect(screen.queryByTestId('mock-weather')).not.toBeInTheDocument()
    expect(screen.queryByTestId('mock-sunrise')).not.toBeInTheDocument()
  })

  it.each([401, 403])('shows the token-rejected panel after a %i response', async (status) => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: false, status, json: () => Promise.resolve({}) } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })

    const panel = screen.getByTestId('kiosk-token-rejected')
    expect(panel).toBeInTheDocument()
    expect(panel.textContent).toMatch(/rejected/i)
    expect(panel.textContent).toMatch(/QR code/i)
    expect(screen.queryByTestId('mock-buses')).not.toBeInTheDocument()
  })

  it('shows the unreachable panel after a 500 response, with copy distinct from the rejected panel', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { unmount } = renderKiosk('/kiosk?token=test-token')
    await act(async () => { await flushMicrotasks() })

    const unreachableText = screen.getByTestId('kiosk-unreachable').textContent
    expect(unreachableText).toMatch(/reach the server/i)
    expect(screen.queryByTestId('kiosk-token-rejected')).not.toBeInTheDocument()
    unmount()

    // Same page, rejected token: the copy must be visibly different.
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: false, status: 401, json: () => Promise.resolve({}) } as Response),
    ))
    renderKiosk('/kiosk?token=test-token')
    await act(async () => { await flushMicrotasks() })

    expect(screen.getByTestId('kiosk-token-rejected').textContent).not.toEqual(unreachableText)
  })

  it('shows the unreachable panel after a network failure', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new Error('network down')))
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })

    expect(screen.getByTestId('kiosk-unreachable')).toBeInTheDocument()
    expect(screen.queryByTestId('mock-buses')).not.toBeInTheDocument()
  })

  it('keeps polling behind the error panel and replaces it once a fetch succeeds', async () => {
    let succeed = false
    const fetchMock = vi.fn(() => {
      if (succeed) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response)
      }
      return Promise.reject(new Error('network down'))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })
    expect(screen.getByTestId('kiosk-unreachable')).toBeInTheDocument()

    // The backoff timer is still running while the panel is displayed.
    succeed = true
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
      await flushMicrotasks()
    })

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(screen.queryByTestId('kiosk-unreachable')).not.toBeInTheDocument()
    expect(screen.getByTestId('mock-buses')).toBeInTheDocument()
  })

  it('keeps last-known data and the stale badge after a failure that follows a success', async () => {
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
    expect(screen.getByTestId('mock-buses')).toBeInTheDocument()

    succeed = false
    await act(async () => {
      vi.advanceTimersByTime(90_000)
      await flushMicrotasks()
    })

    expect(screen.queryByTestId('kiosk-unreachable')).not.toBeInTheDocument()
    expect(screen.getByTestId('mock-buses')).toBeInTheDocument()
    expect(screen.getByTestId('kiosk-stale-badge')).toBeInTheDocument()
  })

  it('renders the demo layout immediately when no token is present', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new Error('should not be called')))
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk')

    await act(async () => { await flushMicrotasks() })

    expect(screen.getByTestId('mock-buses')).toBeInTheDocument()
    expect(screen.queryByTestId('kiosk-loading')).not.toBeInTheDocument()
    expect(screen.queryByTestId('kiosk-unreachable')).not.toBeInTheDocument()
    expect(screen.queryByTestId('kiosk-token-rejected')).not.toBeInTheDocument()
  })
})

// The rescan test needs to change the ?token= query parameter on a *mounted*
// page (remounting would reset component state and hide the bug), so it builds
// its own router with a navigating harness instead of using renderKiosk.
function RescanHarness() {
  const navigate = useNavigate()
  return (
    <button data-testid="rescan" onClick={() => navigate('/kiosk?token=new-token')}>
      rescan
    </button>
  )
}

describe('KioskPage – token revocation and rescan', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout', 'Date'] })
    vi.setSystemTime(new Date('2026-05-27T12:00:00Z'))
    try { localStorage.removeItem('hytte_kiosk_token') } catch { /* ignore */ }
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

  it('replaces live data with the rejected panel when the token is revoked after a success', async () => {
    let rejected = false
    const fetchMock = vi.fn(() => {
      if (rejected) {
        return Promise.resolve({ ok: false, status: 401, json: () => Promise.resolve({}) } as Response)
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response)
    })
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })
    expect(screen.getByTestId('mock-buses')).toBeInTheDocument()

    // The owner revokes the token from Settings; every subsequent poll 401s.
    rejected = true
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600_000)
      await flushMicrotasks()
    })

    expect(screen.getByTestId('kiosk-token-rejected')).toBeInTheDocument()
    expect(screen.queryByTestId('mock-buses')).not.toBeInTheDocument()
    expect(screen.queryByTestId('mock-weather')).not.toBeInTheDocument()
    expect(screen.queryByTestId('kiosk-stale-badge')).not.toBeInTheDocument()
  })

  it('clears the rejected panel again once a later poll succeeds', async () => {
    // The rejected panel outranks existing data, so a single transient 401
    // blanks a working kiosk. Recovery through the poll that keeps running
    // behind the panel is therefore load-bearing: without it a kiosk would be
    // bricked until someone physically rescans the QR code.
    let rejected = false
    const fetchMock = vi.fn(() => {
      if (rejected) {
        return Promise.resolve({ ok: false, status: 401, json: () => Promise.resolve({}) } as Response)
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response)
    })
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })
    expect(screen.getByTestId('mock-buses')).toBeInTheDocument()

    rejected = true
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600_000)
      await flushMicrotasks()
    })
    expect(screen.getByTestId('kiosk-token-rejected')).toBeInTheDocument()

    // The server recovers (e.g. the 401 came from a restart mid-request).
    rejected = false
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600_000)
      await flushMicrotasks()
    })

    expect(screen.queryByTestId('kiosk-token-rejected')).not.toBeInTheDocument()
    expect(screen.getByTestId('mock-buses')).toBeInTheDocument()
  })

  it('sends the kiosk token as a Bearer header rather than a query parameter', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=secret-token')

    await act(async () => { await flushMicrotasks() })

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).not.toContain('secret-token')
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer secret-token')
  })

  it('drops the rejected panel while the rescanned token is still being fetched', async () => {
    const fetchMock = vi.fn((_url: string, init: RequestInit) => {
      const auth = (init.headers as Record<string, string>).Authorization
      if (auth === 'Bearer new-token') {
        // Slow network (captive portal): the rescanned token never resolves.
        return new Promise<Response>(() => {})
      }
      return Promise.resolve({ ok: false, status: 401, json: () => Promise.resolve({}) } as Response)
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/kiosk?token=old-token']}>
        <Routes>
          <Route
            path="/kiosk"
            element={
              <>
                <KioskPage />
                <RescanHarness />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    )

    await act(async () => { await flushMicrotasks() })
    expect(screen.getByTestId('kiosk-token-rejected')).toBeInTheDocument()

    // The user follows the panel's instruction and rescans the QR code.
    await act(async () => {
      screen.getByTestId('rescan').click()
      await flushMicrotasks()
    })

    expect(screen.queryByTestId('kiosk-token-rejected')).not.toBeInTheDocument()
    expect(screen.getByTestId('kiosk-loading')).toBeInTheDocument()
  })

  it('does not re-show the revoked token\'s payload after a rescan', async () => {
    // Kiosk tokens are individually scoped (stop IDs, location, Netatmo user),
    // so the old token's departures/weather/indoor readings must not reappear
    // under the new token while its first request is still in flight.
    let oldTokenRevoked = false
    const fetchMock = vi.fn((_url: string, init: RequestInit) => {
      const auth = (init.headers as Record<string, string>).Authorization
      if (auth === 'Bearer new-token') {
        // Slow network (captive portal): the rescanned token never resolves.
        return new Promise<Response>(() => {})
      }
      if (oldTokenRevoked) {
        return Promise.resolve({ ok: false, status: 401, json: () => Promise.resolve({}) } as Response)
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve(apiPayload) } as Response)
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/kiosk?token=old-token']}>
        <Routes>
          <Route
            path="/kiosk"
            element={
              <>
                <KioskPage />
                <RescanHarness />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    )

    // old-token succeeds first, so a payload is on screen before revocation.
    await act(async () => { await flushMicrotasks() })
    expect(screen.getByTestId('mock-buses')).toBeInTheDocument()

    oldTokenRevoked = true
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600_000)
      await flushMicrotasks()
    })
    expect(screen.getByTestId('kiosk-token-rejected')).toBeInTheDocument()

    await act(async () => {
      screen.getByTestId('rescan').click()
      await flushMicrotasks()
    })

    expect(screen.queryByTestId('kiosk-token-rejected')).not.toBeInTheDocument()
    expect(screen.getByTestId('kiosk-loading')).toBeInTheDocument()
    expect(screen.queryByTestId('mock-buses')).not.toBeInTheDocument()
    expect(screen.queryByTestId('mock-weather')).not.toBeInTheDocument()

    // ...and it stays blank rather than falling back to the old payload while
    // the new token's fetch remains outstanding.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600_000)
      await flushMicrotasks()
    })
    expect(screen.getByTestId('kiosk-loading')).toBeInTheDocument()
    expect(screen.queryByTestId('mock-buses')).not.toBeInTheDocument()
    expect(screen.queryByTestId('kiosk-stale-badge')).not.toBeInTheDocument()
  })
})

// Key paths of every leaf string in a locale file, e.g. "state.rejected.title".
function leafKeys(obj: Record<string, unknown>, prefix = ''): string[] {
  return Object.entries(obj).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return value && typeof value === 'object' && !Array.isArray(value)
      ? leafKeys(value as Record<string, unknown>, path)
      : [path]
  })
}

describe('kiosk locale files', () => {
  it('defines every key the page looks up', () => {
    const keys = leafKeys(enKiosk as unknown as Record<string, unknown>)
    for (const key of [
      'state.loading.title',
      'state.loading.body',
      'state.rejected.title',
      'state.rejected.body',
      'state.unreachable.title',
      'state.unreachable.body',
    ]) {
      expect(keys).toContain(key)
    }
  })

  it.each([
    ['nb', nbKiosk],
    ['th', thKiosk],
  ])('keeps %s in sync with en', (_lang, locale) => {
    expect(leafKeys(locale as unknown as Record<string, unknown>).sort()).toEqual(
      leafKeys(enKiosk as unknown as Record<string, unknown>).sort(),
    )
  })
})
