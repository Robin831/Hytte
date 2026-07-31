// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import UseMyLocationButton from './UseMyLocationButton'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) => {
      if (key === 'geolocation.coordinates')
        return `${opts?.lat}°${opts?.latDir}, ${opts?.lon}°${opts?.lonDir}`
      const leaf = key.split('.').pop()
      return leaf ?? key
    },
    i18n: { language: 'en' },
  }),
}))

type SuccessCallback = (position: { coords: { latitude: number; longitude: number } }) => void
type ErrorCallback = (error: { code: number; PERMISSION_DENIED: number; TIMEOUT: number; POSITION_UNAVAILABLE: number }) => void

const PERMISSION_DENIED = 1
const POSITION_UNAVAILABLE = 2
const TIMEOUT = 3

function positionError(code: number) {
  return { code, PERMISSION_DENIED, POSITION_UNAVAILABLE, TIMEOUT }
}

/** Install a navigator.geolocation stub and return its getCurrentPosition mock. */
function stubGeolocation(
  impl: (success: SuccessCallback, error: ErrorCallback) => void,
) {
  const getCurrentPosition = vi.fn(impl)
  Object.defineProperty(navigator, 'geolocation', {
    value: { getCurrentPosition },
    configurable: true,
    writable: true,
  })
  return getCurrentPosition
}

describe('UseMyLocationButton', () => {
  const originalGeolocation = Object.getOwnPropertyDescriptor(navigator, 'geolocation')

  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    if (originalGeolocation) {
      Object.defineProperty(navigator, 'geolocation', originalGeolocation)
    } else {
      Reflect.deleteProperty(navigator as unknown as Record<string, unknown>, 'geolocation')
    }
  })

  it('selects the reverse-geocoded place name on success', async () => {
    stubGeolocation((success) => success({ coords: { latitude: 60.53, longitude: 8.2 } }))
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ result: { name: 'Geilo', country: 'Norge', lat: '60.53', lon: '8.2' } }),
      }),
    )

    const onSelect = vi.fn()
    render(<UseMyLocationButton onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() => expect(onSelect).toHaveBeenCalledTimes(1))
    expect(onSelect).toHaveBeenCalledWith({
      name: 'Geilo',
      country: 'Norge',
      lat: 60.53,
      lon: 8.2,
    })
    expect(screen.queryByRole('alert')).not.toBeTruthy()
  })

  it('forwards the coordinates to the reverse endpoint', async () => {
    stubGeolocation((success) => success({ coords: { latitude: 59.91, longitude: 10.75 } }))
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ result: { name: 'Oslo', country: 'Norge', lat: '59.91', lon: '10.75' } }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<UseMyLocationButton onSelect={vi.fn()} />)
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(fetchMock.mock.calls[0][0]).toBe('/api/weather/reverse?lat=59.91&lon=10.75')
  })

  it('falls back to formatted coordinates when reverse geocoding fails', async () => {
    stubGeolocation((success) => success({ coords: { latitude: 59.91, longitude: 10.75 } }))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({ error: 'boom' }) }))

    const onSelect = vi.fn()
    render(<UseMyLocationButton onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() => expect(onSelect).toHaveBeenCalledTimes(1))
    expect(onSelect).toHaveBeenCalledWith({
      name: '59.91°north, 10.75°east',
      country: '',
      lat: 59.91,
      lon: 10.75,
    })
  })

  it('formats southern and western coordinates with the right hemispheres', async () => {
    stubGeolocation((success) => success({ coords: { latitude: -33.87, longitude: -70.67 } }))
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')))
    vi.spyOn(console, 'warn').mockImplementation(() => {})

    const onSelect = vi.fn()
    render(<UseMyLocationButton onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() => expect(onSelect).toHaveBeenCalledTimes(1))
    expect(onSelect.mock.calls[0][0].name).toBe('33.87°south, 70.67°west')
  })

  it('shows a message and selects nothing when permission is denied', async () => {
    stubGeolocation((_success, error) => error(positionError(PERMISSION_DENIED)))

    const onSelect = vi.fn()
    render(<UseMyLocationButton onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('denied'))
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('shows a message and selects nothing on timeout', async () => {
    stubGeolocation((_success, error) => error(positionError(TIMEOUT)))

    const onSelect = vi.fn()
    render(<UseMyLocationButton onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('timeout'))
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('shows a message and selects nothing when the position is unavailable', async () => {
    stubGeolocation((_success, error) => error(positionError(POSITION_UNAVAILABLE)))

    const onSelect = vi.fn()
    render(<UseMyLocationButton onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('unavailable'))
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('shows a message when the browser has no geolocation support', async () => {
    // Shadow any prototype-level implementation the test DOM provides.
    Object.defineProperty(navigator, 'geolocation', { value: undefined, configurable: true, writable: true })

    const onSelect = vi.fn()
    render(<UseMyLocationButton onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('unsupported'))
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('shows a loading state while the position is pending', async () => {
    let resolvePosition: SuccessCallback | null = null
    stubGeolocation((success) => {
      resolvePosition = success
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ result: { name: 'Oslo', country: 'Norge', lat: '59.91', lon: '10.75' } }),
      }),
    )

    const onSelect = vi.fn()
    render(<UseMyLocationButton onSelect={onSelect} />)
    const button = screen.getByRole('button')
    fireEvent.click(button)

    expect(button.getAttribute('aria-busy')).toBe('true')
    expect((button as HTMLButtonElement).disabled).toBe(true)
    expect(button.textContent).toContain('locating')

    resolvePosition!({ coords: { latitude: 59.91, longitude: 10.75 } })

    await waitFor(() => expect(onSelect).toHaveBeenCalled())
    await waitFor(() => expect(button.getAttribute('aria-busy')).toBe('false'))
    expect(button.textContent).toContain('button')
  })
})
