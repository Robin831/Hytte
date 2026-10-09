// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
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

  it('turns a kind off with an explicit "false" and back on with "true"', () => {
    const { savePreference } = renderSection({ races_notify_changes: 'false' })
    expect(screen.getByRole('switch', { name: 'races.changesTitle' })).toHaveAttribute('aria-checked', 'false')
    fireEvent.click(screen.getByRole('switch', { name: 'races.deadlinesTitle' }))
    expect(savePreference).toHaveBeenCalledWith('races_notify_deadlines', 'false')
    fireEvent.click(screen.getByRole('switch', { name: 'races.changesTitle' }))
    expect(savePreference).toHaveBeenCalledWith('races_notify_changes', 'true')
  })
})
