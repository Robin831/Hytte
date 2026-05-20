// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import VoiceBubble from '../VoiceBubble'
import * as voicePlayer from '../voicePlayer'

const TRANSLATIONS: Record<string, string> = {
  'voice.bubble.play': 'Play voice note',
  'voice.bubble.pause': 'Pause voice note',
  'voice.bubble.seek': 'Voice note position',
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => TRANSLATIONS[key] ?? key,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// Mock the voicePlayer singleton so we can assert on play/pause/seek calls
// without spinning up an actual HTMLAudioElement.
vi.mock('../voicePlayer', () => {
  let listeners = new Set<(state: ReturnType<typeof getState>) => void>()
  let state = { currentId: null as string | null, playing: false, positionMs: 0, durationMs: 0 }
  function getState() { return state }
  function setState(next: typeof state) {
    state = next
    for (const l of listeners) l(state)
  }
  return {
    getState,
    subscribe: vi.fn((listener: (s: typeof state) => void) => {
      listeners.add(listener)
      listener(state)
      return () => { listeners.delete(listener) }
    }),
    play: vi.fn().mockResolvedValue(undefined),
    pause: vi.fn(),
    seek: vi.fn(),
    stop: vi.fn(),
    stopAll: vi.fn(),
    getCurrentId: vi.fn(() => state.currentId),
    setAudioFactory: vi.fn(),
    __setState: setState,
    __reset: () => {
      listeners = new Set()
      state = { currentId: null, playing: false, positionMs: 0, durationMs: 0 }
    },
  }
})

const mockedPlayer = voicePlayer as unknown as typeof voicePlayer & {
  __setState: (next: { currentId: string | null; playing: boolean; positionMs: number; durationMs: number }) => void
  __reset: () => void
}

function makeBars(): number[] {
  return Array.from({ length: 32 }, (_, i) => (i + 1) / 32)
}

beforeEach(() => {
  mockedPlayer.__reset()
  vi.clearAllMocks()
})

afterEach(() => {
  mockedPlayer.__reset()
})

describe('VoiceBubble – rendering', () => {
  it('renders 32 bars + duration label given bars+durationMs props', () => {
    render(
      <VoiceBubble
        messageId={42}
        src="/api/familychat/conversations/1/attachments/42"
        bars={makeBars()}
        durationMs={5000}
      />,
    )

    // Bars: there are two groups (base + foreground fill), each with 32 rects.
    // The base layer carries data-testid suffixed by index so we can assert count.
    const baseBars = screen.getAllByTestId(/voice-bubble-bar-42-/)
    expect(baseBars).toHaveLength(32)

    expect(screen.getByTestId('voice-bubble-duration-42')).toHaveTextContent('0:05')
  })

  it('falls back to 32 zero bars when bars prop is empty', () => {
    render(
      <VoiceBubble
        messageId={7}
        src="/api/familychat/conversations/1/attachments/7"
        bars={[]}
        durationMs={0}
      />,
    )
    const baseBars = screen.getAllByTestId(/voice-bubble-bar-7-/)
    expect(baseBars).toHaveLength(32)
    // Duration label still renders 0:00 so the row layout stays stable.
    expect(screen.getByTestId('voice-bubble-duration-7')).toHaveTextContent('0:00')
  })
})

describe('VoiceBubble – play / pause', () => {
  it('clicking play calls voicePlayer.play with the message src', () => {
    render(
      <VoiceBubble
        messageId={42}
        src="/audio/42.webm"
        bars={makeBars()}
        durationMs={5000}
      />,
    )

    const button = screen.getByTestId('voice-bubble-play-42')
    expect(button).toHaveAttribute('aria-label', 'Play voice note')
    fireEvent.click(button)

    expect(voicePlayer.play).toHaveBeenCalledWith('42', '/audio/42.webm')
  })

  it('toggles to pause icon when the singleton reports this id is playing', () => {
    render(
      <VoiceBubble
        messageId={42}
        src="/audio/42.webm"
        bars={makeBars()}
        durationMs={5000}
      />,
    )

    expect(screen.getByTestId('voice-bubble-play-42')).toHaveAttribute('aria-label', 'Play voice note')

    act(() => {
      mockedPlayer.__setState({ currentId: '42', playing: true, positionMs: 0, durationMs: 5000 })
    })

    const button = screen.getByTestId('voice-bubble-play-42')
    expect(button).toHaveAttribute('aria-label', 'Pause voice note')
    expect(button).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(button)
    expect(voicePlayer.pause).toHaveBeenCalled()
  })

  it('does not toggle to pause when another bubble is the active player', () => {
    render(
      <VoiceBubble
        messageId={42}
        src="/audio/42.webm"
        bars={makeBars()}
        durationMs={5000}
      />,
    )

    act(() => {
      mockedPlayer.__setState({ currentId: '99', playing: true, positionMs: 1000, durationMs: 5000 })
    })

    expect(screen.getByTestId('voice-bubble-play-42')).toHaveAttribute('aria-label', 'Play voice note')
  })
})

describe('VoiceBubble – seek', () => {
  it('clicking the waveform invokes voicePlayer.seek with a ratio-derived ms', () => {
    render(
      <VoiceBubble
        messageId={42}
        src="/audio/42.webm"
        bars={makeBars()}
        durationMs={10000}
      />,
    )

    // Make the bubble the active player first so seek targets it.
    act(() => {
      mockedPlayer.__setState({ currentId: '42', playing: true, positionMs: 0, durationMs: 10000 })
    })

    const svg = screen.getByTestId('voice-bubble-wave-42')
    // happy-dom returns a zero-width DOMRect by default — stub getBoundingClientRect
    // so the seek math has a concrete coordinate space.
    Object.defineProperty(svg, 'getBoundingClientRect', {
      value: () => ({ left: 0, top: 0, right: 200, bottom: 28, width: 200, height: 28, x: 0, y: 0, toJSON() { return {} } }),
      configurable: true,
    })

    fireEvent.click(svg, { clientX: 100, clientY: 14 })

    expect(voicePlayer.seek).toHaveBeenCalled()
    const target = vi.mocked(voicePlayer.seek).mock.calls[0][0]
    expect(target).toBeGreaterThan(4000)
    expect(target).toBeLessThan(6000)
  })

  it('the seek slider is focusable and exposes a slider role with value bounds', () => {
    render(
      <VoiceBubble
        messageId={42}
        src="/audio/42.webm"
        bars={makeBars()}
        durationMs={10000}
      />,
    )
    const svg = screen.getByTestId('voice-bubble-wave-42')
    expect(svg).toHaveAttribute('role', 'slider')
    expect(svg).toHaveAttribute('tabindex', '0')
    expect(svg).toHaveAttribute('aria-valuemin', '0')
    expect(svg).toHaveAttribute('aria-valuemax', '10000')
    expect(svg).toHaveAttribute('aria-valuenow', '0')
    expect(svg).toHaveAttribute('aria-orientation', 'horizontal')
  })

  it('arrow keys step the position by 5s on the active bubble', () => {
    render(
      <VoiceBubble
        messageId={42}
        src="/audio/42.webm"
        bars={makeBars()}
        durationMs={30000}
      />,
    )
    act(() => {
      mockedPlayer.__setState({ currentId: '42', playing: true, positionMs: 10000, durationMs: 30000 })
    })

    const svg = screen.getByTestId('voice-bubble-wave-42')
    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    expect(voicePlayer.seek).toHaveBeenLastCalledWith(15000)

    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    // The slider drives off the singleton-reported position, so each press
    // bases its delta on the same 10000ms snapshot in this test.
    expect(voicePlayer.seek).toHaveBeenLastCalledWith(5000)

    fireEvent.keyDown(svg, { key: 'Home' })
    expect(voicePlayer.seek).toHaveBeenLastCalledWith(0)

    fireEvent.keyDown(svg, { key: 'End' })
    expect(voicePlayer.seek).toHaveBeenLastCalledWith(30000)
  })

  it('keyboard seek clamps to the duration bounds', () => {
    render(
      <VoiceBubble
        messageId={42}
        src="/audio/42.webm"
        bars={makeBars()}
        durationMs={4000}
      />,
    )
    act(() => {
      mockedPlayer.__setState({ currentId: '42', playing: true, positionMs: 1000, durationMs: 4000 })
    })

    const svg = screen.getByTestId('voice-bubble-wave-42')
    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    expect(voicePlayer.seek).toHaveBeenLastCalledWith(0)

    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    expect(voicePlayer.seek).toHaveBeenLastCalledWith(4000)
  })

  it('clicking the waveform on an idle bubble starts playback at the picked offset', async () => {
    render(
      <VoiceBubble
        messageId={42}
        src="/audio/42.webm"
        bars={makeBars()}
        durationMs={10000}
      />,
    )

    const svg = screen.getByTestId('voice-bubble-wave-42')
    Object.defineProperty(svg, 'getBoundingClientRect', {
      value: () => ({ left: 0, top: 0, right: 200, bottom: 28, width: 200, height: 28, x: 0, y: 0, toJSON() { return {} } }),
      configurable: true,
    })

    fireEvent.click(svg, { clientX: 150, clientY: 14 })

    // play is called immediately; seek follows once the play promise resolves.
    expect(voicePlayer.play).toHaveBeenCalledWith('42', '/audio/42.webm')
    await Promise.resolve()
    await Promise.resolve()
    expect(voicePlayer.seek).toHaveBeenCalled()
  })
})
