import { describe, it, expect, vi } from 'vitest'
import { formatNok, formatReleaseDate } from './format'

// Mock the i18n instance the formatDate utility reads its locale from so the
// tests don't pull in the HTTP backend.
vi.mock('../../i18n', () => ({
  default: { language: 'en' },
}))

describe('formatNok', () => {
  it('renders an em dash for missing prices', () => {
    expect(formatNok(null)).toBe('—')
    expect(formatNok(undefined)).toBe('—')
  })

  it('treats 0 as unpriced rather than free', () => {
    expect(formatNok(0)).toBe('—')
  })

  it('formats a positive amount as whole-krone currency', () => {
    const expected = (1234).toLocaleString('en', {
      style: 'currency',
      currency: 'NOK',
      minimumFractionDigits: 0,
      maximumFractionDigits: 0,
    })
    expect(formatNok(1234)).toBe(expected)
    // No fractional part regardless of the locale's currency conventions.
    expect(formatNok(1234.6)).toBe(
      (1235).toLocaleString('en', {
        style: 'currency',
        currency: 'NOK',
        minimumFractionDigits: 0,
        maximumFractionDigits: 0,
      }),
    )
  })
})

describe('formatReleaseDate', () => {
  it('localizes the "YYYY/MM/DD" format pokemontcg.io returns', () => {
    const expected = new Date(2023, 2, 31).toLocaleDateString('en', { dateStyle: 'medium' })
    expect(formatReleaseDate('2023/03/31')).toBe(expected)
  })

  it('localizes the dashed variant without UTC drift', () => {
    const expected = new Date(2023, 2, 31).toLocaleDateString('en', { dateStyle: 'medium' })
    expect(formatReleaseDate('2023-03-31')).toBe(expected)
    // Parsing from local components keeps the day number intact.
    expect(formatReleaseDate('2023-03-31')).toContain('31')
  })

  it('falls back to the raw string when the input is unparseable', () => {
    expect(formatReleaseDate('coming soon')).toBe('coming soon')
    expect(formatReleaseDate('')).toBe('')
  })
})
