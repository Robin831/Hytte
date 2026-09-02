// @vitest-environment happy-dom
import { describe, it, expect, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import KioskClock from './KioskClock'
import KioskBusDepartures from './KioskBusDepartures'
import KioskWeather from './KioskWeather'
import KioskSunrise from './KioskSunrise'

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

// The forecast fixture is rebuilt per test so the entries always land inside
// the component's "from now" window.
function forecastFrom(base: number) {
  return {
    properties: {
      timeseries: [
        {
          time: new Date(base + 30 * 60_000).toISOString(),
          data: {
            instant: { details: { air_temperature: 7.4, wind_speed: 2 } },
            next_1_hours: { summary: { symbol_code: 'clearsky_day' } },
          },
        },
      ],
    },
  }
}

const SUN = {
  kind: 'normal',
  sunrise: new Date(2026, 4, 27, 6, 0).toISOString(),
  sunset: new Date(2026, 4, 27, 20, 0).toISOString(),
}

describe('KioskClock – dimmed palette', () => {
  it('uses full-contrast text by default', () => {
    const { container } = render(<KioskClock />)
    const time = container.querySelector('.text-8xl')!
    expect(time.className).toContain('text-white')
    expect(time.className).not.toContain('text-gray-500')
    expect(container.querySelector('[data-dimmed]')).toHaveAttribute('data-dimmed', 'false')
  })

  it('drops to the reduced-contrast palette when dimmed', () => {
    const { container } = render(<KioskClock dimmed />)
    const time = container.querySelector('.text-8xl')!
    expect(time.className).toContain('text-gray-500')
    expect(time.className).not.toContain('text-white')
    expect(container.querySelector('[data-dimmed]')).toHaveAttribute('data-dimmed', 'true')
  })
})

describe('KioskBusDepartures – dimmed palette', () => {
  it('uses full-contrast rows by default', () => {
    const { container } = render(<KioskBusDepartures stops={stops} />)
    const destination = screen.getByText('Galgeberg')
    expect(destination.className).toContain('text-white')
    expect(screen.getByText(/min$/).className).toContain('text-green-400')
    // The line badge keeps its saturated background at full brightness.
    const badge = screen.getByText('20')
    expect(badge.className).toContain('text-white')
    expect(badge.className).not.toContain('opacity-40')
    expect(container.querySelector('[data-dimmed]')).toHaveAttribute('data-dimmed', 'false')
  })

  it('drops to the reduced-contrast palette when dimmed', () => {
    const { container } = render(<KioskBusDepartures stops={stops} dimmed />)
    const destination = screen.getByText('Galgeberg')
    expect(destination.className).toContain('text-gray-500')
    expect(destination.className).not.toContain('text-white')
    expect(screen.getByText(/min$/).className).toContain('text-green-700')
    expect(screen.getByText('Majorstuen').className).toContain('text-gray-600')
    // The badge is the one element dimmed by opacity rather than a colour
    // swap — its saturated background would otherwise stay at day brightness,
    // so the class has to sit on the same element that carries `bg-*`.
    const badge = screen.getByText('20')
    expect(badge.className).toContain('opacity-40')
    expect(badge.className).not.toContain('text-white')
    expect(badge.className).toMatch(/\bbg-/)
    expect(container.querySelector('[data-dimmed]')).toHaveAttribute('data-dimmed', 'true')
  })

  it('dims the empty state too', () => {
    render(<KioskBusDepartures stops={[]} dimmed />)
    expect(screen.getByText('Ingen avganger').className).toContain('text-gray-600')
  })
})

describe('KioskWeather – dimmed palette', () => {
  const outdoor = { Temperature: 12.3, Humidity: 55 }
  const indoor = { Temperature: 21.5, Humidity: 40, CO2: 600, Noise: 35, Pressure: 1010 }

  it('uses full-contrast readings by default', () => {
    const { container } = render(
      <KioskWeather outdoor={outdoor} indoor={indoor} wind={null} forecast={forecastFrom(Date.now())} />,
    )
    // The outdoor temperature is the single brightest element on the page.
    expect(screen.getByText('12.3°').className).toContain('text-white')
    expect(screen.getByText(/CO₂/).parentElement!.className).toContain('text-green-400')
    expect(container.querySelector('.min-w-\\[64px\\]')!.className).toContain('bg-gray-800')
    expect(screen.getByText('7°').className).toContain('text-white')
    expect(container.querySelector('img')!.parentElement!.className).not.toContain('opacity-40')
    expect(container.firstElementChild).toHaveAttribute('data-dimmed', 'false')
  })

  it('drops to the reduced-contrast palette when dimmed', () => {
    const { container } = render(
      <KioskWeather
        outdoor={outdoor}
        indoor={indoor}
        wind={null}
        forecast={forecastFrom(Date.now())}
        dimmed
      />,
    )
    const temp = screen.getByText('12.3°')
    expect(temp.className).toContain('text-gray-500')
    expect(temp.className).not.toContain('text-white')
    expect(screen.getByText(/CO₂/).parentElement!.className).toContain('text-green-700')
    // The forecast cards are dimmed by background, which no text colour reaches.
    const card = container.querySelector('.min-w-\\[64px\\]')!
    expect(card.className).toContain('bg-gray-900')
    expect(card.className).not.toContain('bg-gray-800')
    expect(screen.getByText('7°').className).not.toContain('text-white')
    // Weather symbols are SVG images, so opacity is the only handle on them.
    expect(container.querySelector('img')!.parentElement!.className).toContain('opacity-40')
    expect(container.firstElementChild).toHaveAttribute('data-dimmed', 'true')
  })

  it('dims the "no weather data" fallback too', () => {
    render(<KioskWeather outdoor={null} indoor={null} wind={null} forecast={null} dimmed />)
    expect(screen.getByText('Ingen værdata').className).toContain('text-gray-600')
  })
})

describe('KioskSunrise – dimmed palette', () => {
  it('uses full-contrast sun icons by default', () => {
    const { container } = render(<KioskSunrise sun={SUN} />)
    expect(container.querySelector('.lucide-sunrise')!.getAttribute('class')).toContain(
      'text-yellow-400',
    )
    expect(container.querySelector('.lucide-sunset')!.getAttribute('class')).toContain(
      'text-orange-400',
    )
    expect(container.firstElementChild).toHaveAttribute('data-dimmed', 'false')
  })

  it('drops to the reduced-contrast palette when dimmed', () => {
    const { container } = render(<KioskSunrise sun={SUN} dimmed />)
    const sunriseIcon = container.querySelector('.lucide-sunrise')!.getAttribute('class')!
    const sunsetIcon = container.querySelector('.lucide-sunset')!.getAttribute('class')!
    expect(sunriseIcon).toContain('text-yellow-700')
    expect(sunriseIcon).not.toContain('text-yellow-400')
    expect(sunsetIcon).toContain('text-orange-800')
    expect(sunsetIcon).not.toContain('text-orange-400')
    expect(container.firstElementChild!.className).toContain('text-gray-600')
    expect(container.firstElementChild).toHaveAttribute('data-dimmed', 'true')
  })

  it('dims the polar-kind banners', () => {
    const { container: day } = render(<KioskSunrise sun={{ kind: 'polarDay' }} dimmed />)
    expect(day.firstElementChild!.className).toContain('text-yellow-700')
    const { container: night } = render(<KioskSunrise sun={{ kind: 'polarNight' }} dimmed />)
    expect(night.firstElementChild!.className).toContain('text-blue-800')
  })
})
