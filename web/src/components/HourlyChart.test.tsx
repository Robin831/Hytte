// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import HourlyChart from './HourlyChart'
import type { TimeseriesEntry } from '../lib/weatherForecast'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) => {
      if (key === 'chart.ariaChart')
        return `Temperature and precipitation for the next ${opts?.hours} hours, from ${opts?.min}° to ${opts?.max}°`
      if (key === 'chart.table.precipValue') return `${opts?.mm} mm`
      const leaf = key.split('.').pop()
      return leaf ?? key
    },
    i18n: { language: 'en' },
  }),
}))

vi.mock('../weatherUtils', () => ({
  getWeatherIcon: (_code: string, _size: number, alt?: string) => (
    <span data-testid="weather-icon" data-alt={alt ?? ''} />
  ),
  getWeatherDescription: (code: string) => code.replace(/_/g, ' '),
}))

vi.mock('../utils/formatDate', () => ({
  formatTime: (date: string | Date) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return String(d.getUTCHours()).padStart(2, '0')
  },
}))

function makeEntry(hour: number, temp: number, precip = 0, symbol = 'clearsky_day'): TimeseriesEntry {
  return {
    time: `2026-06-29T${String(hour).padStart(2, '0')}:00:00Z`,
    data: {
      instant: {
        details: {
          air_temperature: temp,
          wind_speed: 3,
          relative_humidity: 60,
        },
      },
      next_1_hours: {
        summary: { symbol_code: symbol },
        details: { precipitation_amount: precip },
      },
    },
  }
}

describe('HourlyChart', () => {
  it('renders nothing for empty timeseries', () => {
    const { container } = render(<HourlyChart timeseries={[]} />)
    expect(container.innerHTML).toBe('')
  })

  it('renders SVG chart with aria label', () => {
    const entries = [makeEntry(10, 15), makeEntry(11, 18), makeEntry(12, 20)]
    render(<HourlyChart timeseries={entries} />)

    const svg = document.querySelector('svg[role="img"]')
    expect(svg).toBeTruthy()
    expect(svg?.getAttribute('aria-label')).toContain('3 hours')
    expect(svg?.getAttribute('aria-label')).toContain('15°')
    expect(svg?.getAttribute('aria-label')).toContain('20°')
  })

  it('shows expand/collapse button with correct aria-expanded', () => {
    const entries = [makeEntry(10, 15), makeEntry(11, 18)]
    render(<HourlyChart timeseries={entries} />)

    const button = screen.getByRole('button')
    expect(button).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(button)
    expect(button).toHaveAttribute('aria-expanded', 'true')

    fireEvent.click(button)
    expect(button).toHaveAttribute('aria-expanded', 'false')
  })

  it('renders expanded table with correct data', () => {
    const entries = [
      makeEntry(10, 15, 0, 'clearsky_day'),
      makeEntry(11, 18, 2.5, 'rain'),
    ]
    render(<HourlyChart timeseries={entries} />)

    fireEvent.click(screen.getByRole('button'))

    const table = screen.getByRole('table')
    expect(table).toBeTruthy()

    const rows = screen.getAllByRole('row')
    expect(rows).toHaveLength(3) // header + 2 data rows

    expect(screen.getAllByText('15°').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('18°').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('2.5 mm')).toBeTruthy()
  })

  it('renders weather icon with alt text for accessibility', () => {
    const entries = [makeEntry(10, 15, 0, 'clearsky_day')]
    render(<HourlyChart timeseries={entries} />)

    fireEvent.click(screen.getByRole('button'))

    const icons = document.querySelectorAll('[data-testid="weather-icon"]')
    const tableIcon = Array.from(icons).find(
      (el) => el.getAttribute('data-alt') !== '',
    )
    expect(tableIcon).toBeTruthy()
    expect(tableIcon?.getAttribute('data-alt')).toBe('clearsky day')
  })

  it('handles single data point gracefully', () => {
    const entries = [makeEntry(10, 15)]
    const { container } = render(<HourlyChart timeseries={entries} />)
    expect(container.querySelector('svg')).toBeTruthy()
  })
})
