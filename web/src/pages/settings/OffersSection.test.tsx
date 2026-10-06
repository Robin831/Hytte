// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import OffersSection from './OffersSection'

// i18n: return the key verbatim so the assertions target keys, not copy.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', changeLanguage: () => {} },
  }),
}))

function renderSection(preferences: Record<string, string>) {
  const savePreference = vi.fn(async () => {})
  render(<OffersSection preferences={preferences} saving={false} savePreference={savePreference} />)
  return { savePreference }
}

describe('OffersSection – notify toggle', () => {
  it.each([
    ['absent', {}],
    ["'false'", { offers_notify: 'false' }],
    ['a non-exact truthy value', { offers_notify: '1' }],
  ])('is off when the preference is %s', (_label, preferences) => {
    renderSection(preferences)
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
  })

  it("is on only when the preference is exactly 'true'", () => {
    renderSection({ offers_notify: 'true' })
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true')
  })

  it("saves 'true' when clicked while off", () => {
    const { savePreference } = renderSection({})
    fireEvent.click(screen.getByRole('switch'))
    expect(savePreference).toHaveBeenCalledWith('offers_notify', 'true')
  })

  it("saves 'false' when clicked while on", () => {
    const { savePreference } = renderSection({ offers_notify: 'true' })
    fireEvent.click(screen.getByRole('switch'))
    expect(savePreference).toHaveBeenCalledWith('offers_notify', 'false')
  })

  it('is disabled while saving', () => {
    render(<OffersSection preferences={{}} saving={true} savePreference={vi.fn(async () => {})} />)
    expect(screen.getByRole('switch')).toBeDisabled()
  })
})
