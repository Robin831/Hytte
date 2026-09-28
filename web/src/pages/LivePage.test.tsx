// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import LivePage from './LivePage'

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (key === 'list.startedAt') return `Started ${opts?.time}`
  if (key === 'list.viewers') return `${opts?.count} viewers`
  return key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

function renderPage() {
  return render(
    <MemoryRouter>
      <LivePage />
    </MemoryRouter>,
  )
}

function stubList(payload: object, ok = true) {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok, status: ok ? 200 : 500, json: () => Promise.resolve(payload) })))
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('LivePage', () => {
  it('lists live sessions with owner, on-air state and viewers', async () => {
    stubList({
      configured: true,
      heartbeat_interval: 15,
      sessions: [
        { id: 3, user_id: 1, owner_name: 'Robin', title: 'Tempo run', status: 'live', started_at: '2026-09-28T10:00:00Z', is_owner: false, on_air: true, viewers: 2 },
        { id: 4, user_id: 2, owner_name: 'Me', title: '', status: 'live', started_at: '2026-09-28T10:05:00Z', is_owner: true, on_air: false, viewers: 0 },
      ],
    })
    renderPage()

    await waitFor(() => expect(screen.getByText('Tempo run')).toBeInTheDocument())
    expect(screen.getByText('Tempo run').closest('a')).toHaveAttribute('href', '/live/3')
    expect(screen.getByText('list.onAir')).toBeInTheDocument()
    expect(screen.getByText('list.connecting')).toBeInTheDocument()
    // Untitled session falls back to the generic title; own session says "you".
    expect(screen.getByText('list.untitled')).toBeInTheDocument()
    expect(screen.getByText(/list\.you/)).toBeInTheDocument()
    expect(screen.getByLabelText('2 viewers')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /list\.goLive/ })).toHaveAttribute('href', '/live/broadcast')
  })

  it('shows the empty state', async () => {
    stubList({ configured: true, heartbeat_interval: 15, sessions: [] })
    renderPage()
    await waitFor(() => expect(screen.getByText('list.empty')).toBeInTheDocument())
  })

  it('explains when the media server is not configured and hides Go live', async () => {
    stubList({ configured: false, heartbeat_interval: 15, sessions: [] })
    renderPage()
    await waitFor(() => expect(screen.getByText('list.notConfigured')).toBeInTheDocument())
    expect(screen.queryByRole('link', { name: /list\.goLive/ })).not.toBeInTheDocument()
  })

  it('shows an error when loading fails', async () => {
    stubList({ error: 'boom' }, false)
    renderPage()
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('list.loadError'))
  })
})

describe('LivePage replays', () => {
  it('lists replays with status and storage', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      const body = url.includes('/recordings')
        ? { used_bytes: 600_000_000, free_bytes: 11_000_000_000, recordings: [
            { id: 9, session_id: 3, owner_name: 'Robin', title: 'Long run', started_at: '2026-09-28T08:00:00Z', status: 'ready', duration_seconds: 3725, size_bytes: 600_000_000, is_owner: false, has_track: true },
            { id: 10, session_id: 4, owner_name: 'Me', title: '', started_at: '2026-09-28T09:00:00Z', status: 'processing', duration_seconds: 0, size_bytes: 0, is_owner: true, has_track: false },
          ] }
        : { configured: true, recording_available: true, heartbeat_interval: 15, sessions: [] }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) })
    }))
    renderPage()
    await waitFor(() => expect(screen.getByText('replays.title')).toBeInTheDocument())
    expect(screen.getByText('Long run').closest('a')).toHaveAttribute('href', '/live/replay/9')
    expect(screen.getByText(/1:02:05/)).toBeInTheDocument()
    expect(screen.getByText('replays.status.processing')).toBeInTheDocument()
  })
})
