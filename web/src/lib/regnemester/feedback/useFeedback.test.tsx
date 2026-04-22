// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, fireEvent, act, waitFor } from '@testing-library/react'
import { useFeedback } from './useFeedback'
import { soundEngine } from './sound'

function TestHarness() {
  const fb = useFeedback()
  return (
    <div>
      <span data-testid="muted">{fb.muted ? 'yes' : 'no'}</span>
      <button data-testid="toggle" onClick={fb.toggleMute}>toggle</button>
      <input data-testid="input" />
    </div>
  )
}

function installMatchMedia(reduce: boolean) {
  const mql = {
    matches: reduce,
    media: '(prefers-reduced-motion: reduce)',
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }
  Object.defineProperty(window, 'matchMedia', { value: vi.fn(() => mql), configurable: true })
}

function stubAudio() {
  class AudioStub {
    src = ''
    preload = ''
    currentTime = 0
    load = vi.fn()
    play = vi.fn(() => Promise.resolve())
    canPlayType = () => 'probably' as const
  }
  vi.stubGlobal('Audio', AudioStub)
  const realCreate = document.createElement.bind(document)
  vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
    if (tag === 'audio') {
      const el = realCreate('audio')
      Object.defineProperty(el, 'canPlayType', { value: () => 'probably', configurable: true })
      return el
    }
    return realCreate(tag)
  })
}

describe('useFeedback', () => {
  beforeEach(() => {
    installMatchMedia(false)
    stubAudio()
    window.localStorage.clear()
    // Default fetch: no prior preference stored server-side.
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
      ok: true,
      json: () => Promise.resolve({ preferences: {} }),
    })))
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    soundEngine.reset()
    soundEngine.setMuted(false)
  })

  it('defaults to unmuted when localStorage is empty', () => {
    const { getByTestId } = render(<TestHarness />)
    expect(getByTestId('muted').textContent).toBe('no')
  })

  it('reads initial muted state from localStorage', () => {
    window.localStorage.setItem('regnemester_muted', 'true')
    const { getByTestId } = render(<TestHarness />)
    expect(getByTestId('muted').textContent).toBe('yes')
  })

  it('toggleMute flips the state and persists to localStorage and server', async () => {
    const fetchSpy = vi.fn(() => Promise.resolve({
      ok: true,
      json: () => Promise.resolve({ preferences: {} }),
    }))
    vi.stubGlobal('fetch', fetchSpy)
    const { getByTestId } = render(<TestHarness />)
    await act(async () => {
      fireEvent.click(getByTestId('toggle'))
    })
    expect(getByTestId('muted').textContent).toBe('yes')
    expect(window.localStorage.getItem('regnemester_muted')).toBe('true')
    // Expect a PUT to /api/settings/preferences with the new value.
    const putCall = (fetchSpy.mock.calls as unknown as Array<[string, RequestInit]>).find(([, opts]) => opts?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const body = JSON.parse(putCall![1].body as string)
    expect(body).toEqual({ preferences: { regnemester_muted: 'true' } })
  })

  it('reflects mute state into the shared SoundEngine', async () => {
    const { getByTestId } = render(<TestHarness />)
    expect(soundEngine.isMuted()).toBe(false)
    await act(async () => {
      fireEvent.click(getByTestId('toggle'))
    })
    expect(soundEngine.isMuted()).toBe(true)
  })

  it('M keyboard shortcut toggles mute', () => {
    const { getByTestId } = render(<TestHarness />)
    expect(getByTestId('muted').textContent).toBe('no')
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'm' }))
    })
    expect(getByTestId('muted').textContent).toBe('yes')
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'M' }))
    })
    expect(getByTestId('muted').textContent).toBe('no')
  })

  it('M shortcut is ignored while typing in an input', () => {
    const { getByTestId } = render(<TestHarness />)
    const input = getByTestId('input') as HTMLInputElement
    input.focus()
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'm', bubbles: true }))
    })
    expect(getByTestId('muted').textContent).toBe('no')
  })

  it('M shortcut is ignored when modifier keys are held', () => {
    const { getByTestId } = render(<TestHarness />)
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'm', ctrlKey: true }))
    })
    expect(getByTestId('muted').textContent).toBe('no')
  })

  it('hydrates from server preference when present', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
      ok: true,
      json: () => Promise.resolve({ preferences: { regnemester_muted: 'true' } }),
    })))
    const { getByTestId } = render(<TestHarness />)
    await waitFor(() => {
      expect(getByTestId('muted').textContent).toBe('yes')
    })
    expect(window.localStorage.getItem('regnemester_muted')).toBe('true')
  })
})
