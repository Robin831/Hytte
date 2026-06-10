// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import EventsPanel from './EventsPanel'
import type { WorkerEvent } from '../LiveActivity'

// ── i18n mock: return the key so we can assert on stable strings ──────────────
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// BeadLifecycleModal does its own data loading; keep it inert for this test.
vi.mock('./BeadLifecycleModal', () => ({ default: () => null }))

// Stub EventSource so the hook's SSE path is a no-op; the tests drive the
// initial fetch / polling fallback path instead.
class FakeEventSource {
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  constructor(_url: string) {}
  close() {}
}

const fetchMock = vi.fn()

function jsonResponse(data: unknown) {
  return Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve(data),
  } as Response)
}

function ev(partial: Partial<WorkerEvent> & { id: number }): WorkerEvent {
  return {
    timestamp: '2026-01-01T12:00:00Z',
    type: 'worker_start',
    message: `event ${partial.id}`,
    ...partial,
  }
}

function urlsCalled(): string[] {
  return fetchMock.mock.calls.map(c => String(c[0]))
}

function renderPanel() {
  return render(
    <MemoryRouter>
      <EventsPanel />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('EventsPanel', () => {
  it('fetches the unfiltered window with no level/group params by default', async () => {
    fetchMock.mockImplementation(() =>
      jsonResponse([ev({ id: 1, message: 'hello world', anvil: 'anvil1' })]),
    )

    renderPanel()

    await screen.findByText('hello world')

    const urls = urlsCalled()
    expect(urls.some(u => u.includes('/api/forge/events?') && u.includes('limit=50'))).toBe(true)
    expect(urls.every(u => !u.includes('level=') && !u.includes('group='))).toBe(true)
  })

  it('refetches with level=error when the Errors filter is selected', async () => {
    fetchMock.mockImplementation(() =>
      jsonResponse([ev({ id: 2, type: 'worker_fail', message: 'it broke', anvil: 'anvil1' })]),
    )

    renderPanel()
    await screen.findByText('it broke')

    const select = screen.getByLabelText('mezzanine.events.filterLabel')
    fireEvent.change(select, { target: { value: 'errors' } })

    await waitFor(() => {
      expect(urlsCalled().some(u => u.includes('level=error'))).toBe(true)
    })
  })

  it('refetches with group=prs when the PRs filter is selected', async () => {
    fetchMock.mockImplementation(() =>
      jsonResponse([ev({ id: 3, type: 'pr_merged', message: 'merged it', anvil: 'anvil1' })]),
    )

    renderPanel()
    await screen.findByText('merged it')

    const select = screen.getByLabelText('mezzanine.events.filterLabel')
    fireEvent.change(select, { target: { value: 'prs' } })

    await waitFor(() => {
      expect(urlsCalled().some(u => u.includes('group=prs'))).toBe(true)
    })
  })

  it('refetches with the anvil param when an anvil filter is selected', async () => {
    fetchMock.mockImplementation(() =>
      jsonResponse([ev({ id: 4, message: 'on anvil1', anvil: 'anvil1' })]),
    )

    renderPanel()
    // The initial unfiltered load seeds the anvil dropdown option.
    await screen.findByText('on anvil1')

    const select = screen.getByLabelText('mezzanine.events.filterLabel')
    fireEvent.change(select, { target: { value: 'anvil:anvil1' } })

    await waitFor(() => {
      expect(urlsCalled().some(u => u.includes('anvil=anvil1'))).toBe(true)
    })
  })
})
