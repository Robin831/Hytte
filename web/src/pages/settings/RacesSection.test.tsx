// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, act, waitFor } from '@testing-library/react'
import RacesSection from './RacesSection'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

function renderSection(preferences: Record<string, string>) {
  const savePreference = vi.fn().mockResolvedValue(undefined)
  render(<RacesSection preferences={preferences} saving={false} savePreference={savePreference} />)
  return { savePreference }
}

describe('RacesSection', () => {
  afterEach(cleanup)

  it('treats missing preferences as on (tracking a race is the opt-in)', () => {
    renderSection({})
    expect(screen.getByRole('switch', { name: 'races.deadlinesTitle' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('switch', { name: 'races.changesTitle' })).toHaveAttribute('aria-checked', 'true')
  })

  it('turns calendar sync on, syncs right away and reports the result', async () => {
    const calls: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      calls.push(url)
      if (url === '/api/calendar/calendars') {
        return { ok: true, json: async () => ({ connected: true, calendars: [{ id: 'primary', summary: 'Me', primary: true }] }) } as Response
      }
      return { ok: true, json: async () => ({ result: { created: 3, updated: 0, deleted: 0 } }) } as Response
    }))
    const savePreference = vi.fn().mockResolvedValue(undefined)
    render(<RacesSection preferences={{}} saving={false} savePreference={savePreference} calendarEnabled />)
    const toggle = await screen.findByRole('switch', { name: 'races.calendarTitle' })
    await waitFor(() => expect(toggle).not.toBeDisabled())
    await act(async () => { fireEvent.click(toggle) })
    expect(savePreference).toHaveBeenCalledWith('races_calendar_sync', 'true')
    expect(calls).toContain('/api/races/calendar/sync')
    expect(screen.getByRole('status')).toHaveTextContent('races.calendarResult')
    vi.unstubAllGlobals()
  })

  it('turns a kind off with an explicit "false" and back on with "true"', () => {
    const { savePreference } = renderSection({ races_notify_changes: 'false' })
    expect(screen.getByRole('switch', { name: 'races.changesTitle' })).toHaveAttribute('aria-checked', 'false')
    fireEvent.click(screen.getByRole('switch', { name: 'races.deadlinesTitle' }))
    expect(savePreference).toHaveBeenCalledWith('races_notify_deadlines', 'false')
    fireEvent.click(screen.getByRole('switch', { name: 'races.changesTitle' }))
    expect(savePreference).toHaveBeenCalledWith('races_notify_changes', 'true')
  })
})
