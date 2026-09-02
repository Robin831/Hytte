// @vitest-environment happy-dom
import { describe, it, expect, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import KioskClock from './KioskClock'
import KioskBusDepartures from './KioskBusDepartures'

// The kiosk strips take a `dimmed` flag from KioskPage and swap to a
// reduced-contrast palette for night mode. These tests pin the two invariants
// that matter: the dimmed palette is actually applied, and the default
// (daytime) rendering is untouched.

const stops = [
  {
    stop_id: 'NSR:StopPlace:1',
    stop_name: 'Majorstuen',
    departures: [
      {
        line: '20',
        destination: 'Galgeberg',
        // Far enough out that the countdown lands in the "green" band.
        departure_time: new Date(Date.now() + 12 * 60_000).toISOString(),
        is_realtime: true,
        delay_minutes: 0,
      },
    ],
  },
]

afterEach(() => cleanup())

describe('KioskClock – dimmed palette', () => {
  it('uses full-contrast text by default', () => {
    const { container } = render(<KioskClock />)
    const time = container.querySelector('.text-8xl')!
    expect(time.className).toContain('text-white')
    expect(time.className).not.toContain('text-gray-500')
  })

  it('drops to the reduced-contrast palette when dimmed', () => {
    const { container } = render(<KioskClock dimmed />)
    const time = container.querySelector('.text-8xl')!
    expect(time.className).toContain('text-gray-500')
    expect(time.className).not.toContain('text-white')
  })
})

describe('KioskBusDepartures – dimmed palette', () => {
  it('uses full-contrast rows by default', () => {
    render(<KioskBusDepartures stops={stops} />)
    const destination = screen.getByText('Galgeberg')
    expect(destination.className).toContain('text-white')
    expect(screen.getByText(/min$/).className).toContain('text-green-400')
  })

  it('drops to the reduced-contrast palette when dimmed', () => {
    render(<KioskBusDepartures stops={stops} dimmed />)
    const destination = screen.getByText('Galgeberg')
    expect(destination.className).toContain('text-gray-500')
    expect(destination.className).not.toContain('text-white')
    expect(screen.getByText(/min$/).className).toContain('text-green-800')
    expect(screen.getByText('Majorstuen').className).toContain('text-gray-600')
  })

  it('dims the empty state too', () => {
    render(<KioskBusDepartures stops={[]} dimmed />)
    expect(screen.getByText('Ingen avganger').className).toContain('text-gray-600')
  })
})
