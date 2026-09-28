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
