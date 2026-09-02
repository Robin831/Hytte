// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route, useNavigate } from 'react-router'
import KioskPage from './KioskPage'
import { isNightMode, pixelShift, PIXEL_SHIFT_MAX_PX } from '../components/kiosk/nightMode'
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

// All four panel mocks echo the `dimmed` prop back into the DOM so the
// night-mode tests below can assert that KioskPage threads it down to every
// panel, without having to reason about the real components' Tailwind classes.
// (The real components' palettes are pinned by KioskDimmedPalette.test.tsx.)
// The clock mock also counts its renders, so a test can assert that a kiosk
// which has never fetched successfully produces no clock ticks at all.
const renderCounts = vi.hoisted(() => ({ clock: 0 }))

vi.mock('../components/kiosk/KioskClock', () => ({
  default: ({ dimmed }: { dimmed?: boolean }) => {
    renderCounts.clock++
    return <div data-testid="mock-clock" data-dimmed={dimmed ? 'true' : 'false'} />
  },
}))
vi.mock('../components/kiosk/KioskBusDepartures', () => ({
  default: ({ dimmed }: { dimmed?: boolean }) => (
    <div data-testid="mock-buses" data-dimmed={dimmed ? 'true' : 'false'} />
  ),
}))
vi.mock('../components/kiosk/KioskWeather', () => ({
  default: ({ dimmed }: { dimmed?: boolean }) => (
    <div data-testid="mock-weather" data-dimmed={dimmed ? 'true' : 'false'} />
  ),
}))
vi.mock('../components/kiosk/KioskSunrise', () => ({
  default: ({ dimmed }: { dimmed?: boolean }) => (
    <div data-testid="mock-sunrise" data-dimmed={dimmed ? 'true' : 'false'} />
  ),
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

    // The badge is position:fixed, so it must stay outside the pixel-shift
    // wrapper: a transformed ancestor would become its containing block and
    // the badge would ride (and get clipped with) the shifted layout instead
    // of staying pinned to the viewport.
    expect(screen.getByTestId('kiosk-shift')).not.toContainElement(badge)
    expect(screen.getByTestId('kiosk-root')).toContainElement(badge)
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


// ── Night mode & burn-in pixel shift ─────────────────────────────────────────
// Every fixture below is built from local-time constructors rather than UTC
// strings, because both features read local hours/minutes — hard-coding "Z"
// timestamps would make the suite pass or fail depending on the machine's TZ.

const DAY = { year: 2026, month: 4, day: 27 } // 27 May 2026, local

function localTime(hours: number, minutes = 0): Date {
  return new Date(DAY.year, DAY.month, DAY.day, hours, minutes, 0, 0)
}

// RFC3339 timestamp for a local wall-clock time, matching what the backend
// emits for sunrise/sunset.
function localStamp(hours: number, minutes = 0): string {
  return localTime(hours, minutes).toISOString()
}

const NORMAL_SUN = {
  kind: 'normal',
  sunrise: localStamp(6, 0),
  sunset: localStamp(20, 0),
}

describe('isNightMode', () => {
  it('is sun-driven when no override is configured', () => {
    expect(isNightMode(localTime(12, 0), NORMAL_SUN, null)).toBe(false)
    expect(isNightMode(localTime(5, 59), NORMAL_SUN, null)).toBe(true)
    expect(isNightMode(localTime(6, 0), NORMAL_SUN, null)).toBe(false)
    expect(isNightMode(localTime(19, 59), NORMAL_SUN, null)).toBe(false)
    expect(isNightMode(localTime(20, 0), NORMAL_SUN, null)).toBe(true)
    expect(isNightMode(localTime(23, 30), NORMAL_SUN, null)).toBe(true)
    expect(isNightMode(localTime(2, 0), NORMAL_SUN, null)).toBe(true)
  })

  it('never dims when dim.enabled is false, even deep in the sun-driven night', () => {
    expect(isNightMode(localTime(2, 0), NORMAL_SUN, { enabled: false })).toBe(false)
    expect(isNightMode(localTime(23, 0), NORMAL_SUN, { enabled: false })).toBe(false)
    // enabled:false wins over an explicit window and over polar night too.
    expect(
      isNightMode(localTime(23, 0), NORMAL_SUN, { enabled: false, start: '22:00', end: '06:00' }),
    ).toBe(false)
    expect(isNightMode(localTime(12, 0), { kind: 'polarNight' }, { enabled: false })).toBe(false)
  })

  it('keeps the sun-driven default when dim.enabled is true but no window is set', () => {
    expect(isNightMode(localTime(12, 0), NORMAL_SUN, { enabled: true })).toBe(false)
    expect(isNightMode(localTime(23, 0), NORMAL_SUN, { enabled: true })).toBe(true)
  })

  it('lets an explicit window override the sun window', () => {
    const dim = { start: '23:00', end: '05:00' }
    // 21:00 is after sunset but before the configured window starts.
    expect(isNightMode(localTime(21, 0), NORMAL_SUN, dim)).toBe(false)
    expect(isNightMode(localTime(23, 0), NORMAL_SUN, dim)).toBe(true)
    // 05:30 is before sunrise but after the configured window ends.
    expect(isNightMode(localTime(5, 30), NORMAL_SUN, dim)).toBe(false)
  })

  it('handles a window that crosses midnight', () => {
    const dim = { start: '22:00', end: '06:00' }
    expect(isNightMode(localTime(23, 30), undefined, dim)).toBe(true)
    expect(isNightMode(localTime(2, 0), undefined, dim)).toBe(true)
    expect(isNightMode(localTime(5, 59), undefined, dim)).toBe(true)
    expect(isNightMode(localTime(6, 0), undefined, dim)).toBe(false)
    expect(isNightMode(localTime(7, 0), undefined, dim)).toBe(false)
    expect(isNightMode(localTime(21, 59), undefined, dim)).toBe(false)
  })

  it('handles a same-day window', () => {
    const dim = { start: '01:00', end: '04:00' }
    expect(isNightMode(localTime(0, 30), undefined, dim)).toBe(false)
    expect(isNightMode(localTime(1, 0), undefined, dim)).toBe(true)
    expect(isNightMode(localTime(3, 59), undefined, dim)).toBe(true)
    expect(isNightMode(localTime(4, 0), undefined, dim)).toBe(false)
  })

  it('treats polarDay as always day and polarNight as always dimmed', () => {
    for (let hour = 0; hour < 24; hour++) {
      expect(isNightMode(localTime(hour, 0), { kind: 'polarDay' }, null)).toBe(false)
      expect(isNightMode(localTime(hour, 0), { kind: 'polarNight' }, null)).toBe(true)
    }
    // An explicit window still outranks the polar kinds.
    expect(
      isNightMode(localTime(12, 0), { kind: 'polarNight' }, { start: '22:00', end: '06:00' }),
    ).toBe(false)
    expect(
      isNightMode(localTime(23, 0), { kind: 'polarDay' }, { start: '22:00', end: '06:00' }),
    ).toBe(true)
  })

  it('never dims a token with no location and no window', () => {
    for (let hour = 0; hour < 24; hour++) {
      expect(isNightMode(localTime(hour, 0), null, null)).toBe(false)
      expect(isNightMode(localTime(hour, 0), undefined, undefined)).toBe(false)
      // dim.enabled=true alone cannot dim a kiosk that has no sun times.
      expect(isNightMode(localTime(hour, 0), null, { enabled: true })).toBe(false)
    }
  })

  it('accepts a single-digit hour in a window edge', () => {
    // A hand-written "7:30" must behave exactly like "07:30" rather than being
    // discarded as unparsable and silently falling back to the sun window.
    expect(isNightMode(localTime(12, 0), NORMAL_SUN, { start: '7:30', end: '22:00' })).toBe(true)
    expect(isNightMode(localTime(7, 0), NORMAL_SUN, { start: '7:30', end: '22:00' })).toBe(false)
    expect(isNightMode(localTime(23, 0), NORMAL_SUN, { start: '7:30', end: '22:00' })).toBe(false)
  })

  it('warns when a configured window is unusable instead of failing silently', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})

    // Only one edge set — the fallback is deliberate, but it must be visible.
    expect(isNightMode(localTime(23, 0), NORMAL_SUN, { start: '04:15' })).toBe(true)
    expect(warn).toHaveBeenCalledTimes(1)
    expect(String(warn.mock.calls[0][0])).toContain('04:15')

    // Deduped per distinct value: isNightMode runs on every clock tick.
    isNightMode(localTime(23, 30), NORMAL_SUN, { start: '04:15' })
    expect(warn).toHaveBeenCalledTimes(1)

    // A different unusable value warns again.
    expect(isNightMode(localTime(23, 0), NORMAL_SUN, { start: 'zzz', end: 'qqq' })).toBe(true)
    expect(warn).toHaveBeenCalledTimes(2)

    // A usable window is silent.
    expect(isNightMode(localTime(23, 0), NORMAL_SUN, { start: '22:00', end: '06:00' })).toBe(true)
    expect(warn).toHaveBeenCalledTimes(2)

    warn.mockRestore()
  })

  it('ignores a half-configured or unparsable window instead of wedging', () => {
    // Only one edge set: fall back to the sun window.
    expect(isNightMode(localTime(23, 0), NORMAL_SUN, { start: '01:00' })).toBe(true)
    expect(isNightMode(localTime(12, 0), NORMAL_SUN, { end: '01:00' })).toBe(false)
    // Garbage values: fall back to the sun window rather than dimming forever.
    expect(isNightMode(localTime(12, 0), NORMAL_SUN, { start: 'nope', end: '06:00' })).toBe(false)
    expect(isNightMode(localTime(23, 0), NORMAL_SUN, { start: 'nope', end: 'nope' })).toBe(true)
    // A zero-length window matches nothing.
    expect(isNightMode(localTime(3, 0), NORMAL_SUN, { start: '03:00', end: '03:00' })).toBe(false)
    // Broken sun timestamps degrade to "day" rather than throwing.
    expect(isNightMode(localTime(3, 0), { kind: 'normal', sunrise: 'x', sunset: 'y' }, null)).toBe(
      false,
    )
  })
})

describe('pixelShift', () => {
  it('stays within a few pixels on both axes all day', () => {
    for (let minute = 0; minute < 24 * 60; minute++) {
      const { x, y } = pixelShift(localTime(0, minute))
      expect(x).toBeGreaterThanOrEqual(0)
      expect(x).toBeLessThanOrEqual(PIXEL_SHIFT_MAX_PX)
      expect(y).toBeGreaterThanOrEqual(0)
      expect(y).toBeLessThanOrEqual(PIXEL_SHIFT_MAX_PX)
    }
    expect(PIXEL_SHIFT_MAX_PX).toBeLessThanOrEqual(4)
  })

  it('holds each offset for minutes and eventually visits every offset', () => {
    const first = pixelShift(localTime(0, 0))
    expect(pixelShift(localTime(0, 1))).toEqual(first)
    // The offset must actually move over a minutes-scale period.
    const seen = new Set<string>()
    for (let minute = 0; minute < 24 * 60; minute++) {
      const { x, y } = pixelShift(localTime(0, minute))
      seen.add(`${x},${y}`)
    }
    expect(seen.size).toBe((PIXEL_SHIFT_MAX_PX + 1) ** 2)
  })
})

const TRANSFORM_RE = /^translate\(\s*(\d+)px,\s*(\d+)px\s*\)$/

function transformOf(el: HTMLElement): string {
  return el.style.transform
}

describe('KioskPage – night mode on the clock tick', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout', 'Date'] })
    vi.setSystemTime(localTime(19, 58))
    try { localStorage.removeItem('hytte_kiosk_token') } catch { /* ignore */ }
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  async function mountWith(payload: Record<string, unknown>) {
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ ...apiPayload, ...payload }),
      } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)
    renderKiosk('/kiosk?token=test-token')
    await act(async () => { await flushMicrotasks() })
    return fetchMock
  }

  function root(): HTMLElement {
    return screen.getByTestId('kiosk-root')
  }

  it('switches from day to night on a tick, without remounting or refetching', async () => {
    const fetchMock = await mountWith({ sun: NORMAL_SUN })

    expect(root()).toHaveAttribute('data-dimmed', 'false')
    const clockBefore = screen.getByTestId('mock-clock')
    const fetchesBefore = fetchMock.mock.calls.length

    // Cross sunset (20:00) purely by letting the existing tick fire.
    await act(async () => {
      vi.advanceTimersByTime(3 * 60_000)
      await flushMicrotasks()
    })

    expect(root()).toHaveAttribute('data-dimmed', 'true')
    // Same DOM node -> the page re-rendered rather than remounting/reloading.
    expect(screen.getByTestId('mock-clock')).toBe(clockBefore)
    // Polling continued at its normal cadence; night mode added no fetches.
    expect(fetchMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
  })

  it('switches back from night to day on a tick', async () => {
    vi.setSystemTime(localTime(5, 58))
    await mountWith({ sun: NORMAL_SUN })

    expect(root()).toHaveAttribute('data-dimmed', 'true')

    await act(async () => {
      vi.advanceTimersByTime(3 * 60_000)
      await flushMicrotasks()
    })

    expect(root()).toHaveAttribute('data-dimmed', 'false')
  })

  it('stays bright through the night when dim.enabled is false', async () => {
    vi.setSystemTime(localTime(23, 0))
    await mountWith({ sun: NORMAL_SUN, dim: { enabled: false } })

    expect(root()).toHaveAttribute('data-dimmed', 'false')
    expect(screen.getByTestId('mock-clock')).toHaveAttribute('data-dimmed', 'false')
  })

  it('prefers an explicit window over the sun window', async () => {
    vi.setSystemTime(localTime(21, 0))
    await mountWith({ sun: NORMAL_SUN, dim: { start: '23:00', end: '05:00' } })

    // 21:00 is past sunset but outside the configured window.
    expect(root()).toHaveAttribute('data-dimmed', 'false')

    await act(async () => {
      vi.advanceTimersByTime(2 * 60 * 60_000 + 60_000)
      await flushMicrotasks()
    })
    expect(root()).toHaveAttribute('data-dimmed', 'true')
  })

  it('dims under polarNight even at midday', async () => {
    vi.setSystemTime(localTime(12, 0))
    await mountWith({ sun: { kind: 'polarNight' } })
    expect(root()).toHaveAttribute('data-dimmed', 'true')
    expect(screen.getByTestId('mock-buses')).toHaveAttribute('data-dimmed', 'true')
  })

  it('stays bright under polarDay even at 02:00', async () => {
    vi.setSystemTime(localTime(2, 0))
    await mountWith({ sun: { kind: 'polarDay' } })
    expect(root()).toHaveAttribute('data-dimmed', 'false')

    await act(async () => {
      vi.advanceTimersByTime(60 * 60_000)
      await flushMicrotasks()
    })
    expect(root()).toHaveAttribute('data-dimmed', 'false')
  })

  it('never dims a token with no location and no window', async () => {
    vi.setSystemTime(localTime(2, 0))
    await mountWith({ sun: null })
    expect(root()).toHaveAttribute('data-dimmed', 'false')

    await act(async () => {
      vi.advanceTimersByTime(60 * 60_000)
      await flushMicrotasks()
    })
    expect(root()).toHaveAttribute('data-dimmed', 'false')
  })

  it('passes dimmed down to every panel, not just the clock and departures', async () => {
    vi.setSystemTime(localTime(19, 58))
    await mountWith({ sun: NORMAL_SUN })

    const panels = ['mock-clock', 'mock-buses', 'mock-weather', 'mock-sunrise']
    for (const panel of panels) {
      expect(screen.getByTestId(panel)).toHaveAttribute('data-dimmed', 'false')
    }

    await act(async () => {
      vi.advanceTimersByTime(3 * 60_000)
      await flushMicrotasks()
    })

    // Every panel dims together: leaving the weather strip's large white
    // temperature or the yellow sun icons at day brightness would defeat the
    // point of night mode on a wall-mounted screen.
    for (const panel of panels) {
      expect(screen.getByTestId(panel)).toHaveAttribute('data-dimmed', 'true')
    }
  })

  it('produces no clock ticks on the token-less demo path', async () => {
    // The tick is gated on a token and a first successful fetch, so the demo
    // preview (and any kiosk that has never fetched) renders once and then
    // stays put — a low-power wall panel should not repaint for nothing.
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new Error('should not be called'))))
    renderKiosk('/kiosk')
    await act(async () => { await flushMicrotasks() })

    const rendersAfterMount = renderCounts.clock
    expect(rendersAfterMount).toBeGreaterThan(0)

    await act(async () => {
      vi.advanceTimersByTime(10 * 60_000)
      await flushMicrotasks()
    })

    expect(renderCounts.clock).toBe(rendersAfterMount)
  })

  it('starts no clock tick until a tokened kiosk has fetched successfully', async () => {
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new Error('network down'))))

    renderKiosk('/kiosk?token=test-token')
    await act(async () => { await flushMicrotasks() })
    await act(async () => {
      vi.advanceTimersByTime(2 * 60_000)
      await flushMicrotasks()
    })

    // Polling keeps running behind the status panel, but the 5s tick that
    // drives the stale badge and night mode never starts: with no data there
    // is neither a badge nor a dimmable panel on screen.
    const tickIntervals = setIntervalSpy.mock.calls.filter(([, delay]) => delay === 5_000)
    expect(tickIntervals).toHaveLength(0)
    setIntervalSpy.mockRestore()
  })

  it('shifts the layout by a few pixels as the clock advances', async () => {
    await mountWith({ sun: NORMAL_SUN })

    const shifted = screen.getByTestId('kiosk-shift')
    const first = transformOf(shifted)
    expect(first).toMatch(TRANSFORM_RE)

    const offsets = new Set<string>([first])
    // Sample across a couple of hours of ticks; every sample must stay within
    // the documented bound and the offset must actually move.
    for (let i = 0; i < 12; i++) {
      await act(async () => {
        vi.advanceTimersByTime(10 * 60_000)
        await flushMicrotasks()
      })
      const value = transformOf(screen.getByTestId('kiosk-shift'))
      const match = TRANSFORM_RE.exec(value)
      expect(match).not.toBeNull()
      expect(Number(match![1])).toBeLessThanOrEqual(PIXEL_SHIFT_MAX_PX)
      expect(Number(match![2])).toBeLessThanOrEqual(PIXEL_SHIFT_MAX_PX)
      offsets.add(value)
    }
    expect(offsets.size).toBeGreaterThan(1)

    // The outer frame clips the overhang, so the shift can never add a
    // scrollbar at kiosk viewport sizes.
    expect(screen.getByTestId('kiosk-root').className).toContain('overflow-hidden')
  })
})
