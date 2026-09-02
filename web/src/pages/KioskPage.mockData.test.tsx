// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router'
import KioskPage from './KioskPage'
import mockData from '../mocks/kioskData.json'

// Unlike KioskPage.test.tsx this suite renders the real kiosk panels, so it can
// assert on the actual values from mocks/kioskData.json reaching (or not
// reaching) the DOM.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, defaultValue?: string) => defaultValue ?? key,
    i18n: { language: 'en' },
  }),
}))

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

// Values that only exist in the demo fixture — none of them may appear on a
// tokened kiosk that has not fetched successfully yet. Departure destinations
// are deliberately not asserted on: every fixture departure_time predates
// `fetched_at`, so relativizing shifts them into the past and the departure
// rows are filtered out. Only the stop headers survive on the demo path.
const MOCK_STOP_NAMES = mockData.transit.map((stop) => stop.stop_name)
const MOCK_OUTDOOR_TEMP = `${mockData.outdoor.Temperature.toFixed(1)}°`
const MOCK_SUNRISE = (() => {
  const d = new Date(mockData.sun.sunrise)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
})()

describe('KioskPage – mock data is limited to the demo path', () => {
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

  it('renders relativized mock data when no token is present', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new Error('should not be called')))
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk')

    await act(async () => { await flushMicrotasks() })

    const text = document.body.textContent ?? ''
    for (const stopName of MOCK_STOP_NAMES) {
      expect(text).toContain(stopName)
    }
    expect(text).toContain(MOCK_OUTDOOR_TEMP)
    expect(text).toContain(MOCK_SUNRISE)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('shows no mock values while a tokened kiosk waits for its first fetch', async () => {
    const fetchMock = vi.fn(() => new Promise<Response>(() => {}))
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })

    expect(screen.getByTestId('kiosk-loading')).toBeInTheDocument()
    const text = document.body.textContent ?? ''
    for (const stopName of MOCK_STOP_NAMES) {
      expect(text).not.toContain(stopName)
    }
    expect(text).not.toContain(MOCK_OUTDOOR_TEMP)
    expect(text).not.toContain(MOCK_SUNRISE)
  })

  it('shows no mock values once a tokened kiosk fetch has failed', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: false, status: 401, json: () => Promise.resolve({}) } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)

    renderKiosk('/kiosk?token=test-token')

    await act(async () => { await flushMicrotasks() })

    expect(screen.getByTestId('kiosk-token-rejected')).toBeInTheDocument()
    const text = document.body.textContent ?? ''
    for (const stopName of MOCK_STOP_NAMES) {
      expect(text).not.toContain(stopName)
    }
    expect(text).not.toContain(MOCK_OUTDOOR_TEMP)
    expect(text).not.toContain(MOCK_SUNRISE)
  })
})
