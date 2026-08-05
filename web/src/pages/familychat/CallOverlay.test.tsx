// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import CallOverlay from './CallOverlay'
import { formatCallDuration } from './utils'
import type { UseVoiceCallApi, VoiceCallState, CallKind } from './voice/useVoiceCall'
import type { FilterKind } from './voice/videoFilters'

// ── Translation mock ──────────────────────────────────────────────────────────
// Same stable-key strategy as ChatView.test.tsx: real strings for the keys the
// overlay renders, the key itself for anything else.

const TRANSLATIONS: Record<string, string> = {
  'chat.memberFallback': 'Member #{{id}}',
  'call.incomingLabel': 'Incoming call',
  'call.incomingVideoLabel': 'Incoming video call',
  'call.ringing': 'Ringing…',
  'call.accept': 'Accept',
  'call.decline': 'Decline',
  'call.hangup': 'Hang up',
  'call.mute': 'Mute',
  'call.unmute': 'Unmute',
  'call.speakerOn': 'Speaker on',
  'call.speakerOff': 'Speaker off',
  'call.cameraOn': 'Turn camera on',
  'call.cameraOff': 'Turn camera off',
  'call.switchCamera': 'Switch camera',
  'call.remoteCameraOff': 'Camera off',
  'call.localPreview': 'Your camera preview',
  'call.remoteVideo': 'Remote video',
  'call.ended': 'Call ended — {{duration}}',
  'call.filters.title': 'Effects',
  'call.filters.button': 'Effects',
  'call.filters.none': 'None',
  'call.filters.blur': 'Blur',
  'call.filters.grading': 'Vintage',
}

function stableT(key: string, opts?: Record<string, string | number>): string {
  const val = TRANSLATIONS[key] ?? key
  if (opts) return val.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? ''))
  return val
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// The effects picker only renders when the browser can run more than the
// passthrough filter; happy-dom has no canvas capture pipeline, so the
// capability probe is stubbed to report a realistic set.
vi.mock('./voice/videoFilters', () => ({
  supportedFilters: () => ['none', 'blur', 'grading'],
  FILTER_KINDS: ['none', 'blur', 'grading', 'face'],
}))

// ── Call API fake ─────────────────────────────────────────────────────────────

// The action half of the hook's API, spied on so the overlay's buttons can be
// checked against the calls they are supposed to make.
type CallActionKey =
  | 'startCall'
  | 'acceptCall'
  | 'rejectCall'
  | 'endCall'
  | 'setMuted'
  | 'setCameraEnabled'
  | 'switchCamera'
  | 'setFilter'
  | 'handleSignalEvent'

type CallSpies = { [K in CallActionKey]: Mock<UseVoiceCallApi[K]> }

function makeSpies(): CallSpies {
  return {
    startCall: vi.fn<UseVoiceCallApi['startCall']>().mockResolvedValue(undefined),
    acceptCall: vi.fn<UseVoiceCallApi['acceptCall']>().mockResolvedValue(undefined),
    rejectCall: vi.fn<UseVoiceCallApi['rejectCall']>().mockResolvedValue(undefined),
    endCall: vi.fn<UseVoiceCallApi['endCall']>().mockResolvedValue(undefined),
    setMuted: vi.fn<UseVoiceCallApi['setMuted']>(),
    setCameraEnabled: vi.fn<UseVoiceCallApi['setCameraEnabled']>().mockResolvedValue(undefined),
    switchCamera: vi.fn<UseVoiceCallApi['switchCamera']>().mockResolvedValue(undefined),
    setFilter: vi.fn<UseVoiceCallApi['setFilter']>().mockResolvedValue(undefined),
    handleSignalEvent: vi.fn<UseVoiceCallApi['handleSignalEvent']>().mockResolvedValue(undefined),
  }
}

interface CallOverrides {
  state?: VoiceCallState
  callId?: string | null
  remoteUserId?: number | null
  error?: string | null
  callKind?: CallKind
  remoteStream?: MediaStream | null
  remoteCameraEnabled?: boolean
  localStream?: MediaStream | null
  muted?: boolean
  cameraEnabled?: boolean
  filter?: FilterKind
}

function makeCall(spies: CallSpies, overrides: CallOverrides = {}): UseVoiceCallApi {
  return {
    state: 'idle',
    callId: null,
    remoteUserId: null,
    error: null,
    callKind: 'voice',
    remoteStream: null,
    remoteCameraEnabled: true,
    localStream: null,
    muted: false,
    cameraEnabled: true,
    facingMode: 'user',
    filter: 'none',
    ...spies,
    ...overrides,
  }
}

// A MediaStream stand-in. happy-dom has no WebRTC; the overlay only ever hands
// the value to <video>.srcObject, whose setter is stubbed below.
function fakeStream(): MediaStream {
  return { id: 'fake-stream' } as unknown as MediaStream
}

function peerLabel(id: number | null): string {
  if (id === null) return 'Member #0'
  return id === 2 ? 'Bob' : `Member #${id}`
}

function renderOverlay(call: UseVoiceCallApi, fallbackPeerId: number | null = 2) {
  return render(
    <CallOverlay
      call={call}
      conversationId={1}
      peerLabel={peerLabel}
      fallbackPeerId={fallbackPeerId}
    />,
  )
}

beforeEach(() => {
  // happy-dom's HTMLMediaElement has no real playback, and its srcObject setter
  // rejects anything that isn't a genuine MediaStream. Stub both so the
  // stream-wiring effects run without throwing.
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(() => Promise.resolve())
  vi.spyOn(HTMLMediaElement.prototype, 'srcObject', 'set').mockImplementation(() => {})
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('CallOverlay – idle', () => {
  it('renders no call surface while the call machine is idle', () => {
    renderOverlay(makeCall(makeSpies()))

    expect(screen.queryByTestId('family-chat-incoming-overlay')).not.toBeInTheDocument()
    expect(screen.queryByTestId('family-chat-active-overlay')).not.toBeInTheDocument()
    expect(screen.queryByTestId('family-chat-active-video-overlay')).not.toBeInTheDocument()
    expect(screen.queryByTestId('family-chat-call-ended')).not.toBeInTheDocument()
  })

  it('keeps the hidden remote audio sink mounted so it survives state changes', () => {
    const { container } = renderOverlay(makeCall(makeSpies()))
    expect(container.querySelector('audio')).not.toBeNull()
  })
})

describe('CallOverlay – incoming call', () => {
  it('names the caller and labels a voice offer', () => {
    renderOverlay(makeCall(makeSpies(), { state: 'incoming-ringing', remoteUserId: 2 }))

    expect(screen.getByTestId('family-chat-incoming-overlay')).toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
    expect(screen.getByTestId('family-chat-incoming-kind-label').textContent).toBe('Incoming call')
  })

  it('labels a video offer differently', () => {
    renderOverlay(makeCall(makeSpies(), {
      state: 'incoming-ringing',
      remoteUserId: 2,
      callKind: 'video',
    }))

    expect(screen.getByTestId('family-chat-incoming-kind-label').textContent)
      .toBe('Incoming video call')
  })

  it('accepting and declining call through to the hook', () => {
    const spies = makeSpies()
    renderOverlay(makeCall(spies, { state: 'incoming-ringing', remoteUserId: 2 }))

    fireEvent.click(screen.getByTestId('family-chat-call-accept'))
    expect(spies.acceptCall).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByTestId('family-chat-call-decline'))
    expect(spies.rejectCall).toHaveBeenCalledTimes(1)
  })
})

describe('CallOverlay – active voice call', () => {
  it('shows "Ringing…" while the outgoing call has not connected', () => {
    renderOverlay(makeCall(makeSpies(), { state: 'outgoing-ringing' }))

    expect(screen.getByTestId('family-chat-active-overlay')).toBeInTheDocument()
    expect(screen.getByTestId('family-chat-call-status').textContent).toBe('Ringing…')
    // The remote id is unknown until the answer arrives, so the peer falls back
    // to the other member of the conversation.
    expect(screen.getByText('Bob')).toBeInTheDocument()
  })

  it('shows a running duration once the call is active', () => {
    vi.useFakeTimers()
    renderOverlay(makeCall(makeSpies(), { state: 'active', remoteUserId: 2 }))

    expect(screen.getByTestId('family-chat-call-status').textContent).toBe('0:00')
    act(() => { vi.advanceTimersByTime(65_000) })
    expect(screen.getByTestId('family-chat-call-status').textContent).toBe('1:05')
  })

  it('mute and hang-up invoke the hook, and the speaker toggle flips locally', () => {
    const spies = makeSpies()
    renderOverlay(makeCall(spies, { state: 'active', remoteUserId: 2 }))

    fireEvent.click(screen.getByTestId('family-chat-call-mute'))
    expect(spies.setMuted).toHaveBeenCalledWith(true)

    fireEvent.click(screen.getByTestId('family-chat-call-hangup'))
    expect(spies.endCall).toHaveBeenCalledTimes(1)

    const speaker = screen.getByTestId('family-chat-call-speaker')
    expect(speaker.getAttribute('aria-label')).toBe('Speaker off')
    fireEvent.click(speaker)
    expect(screen.getByTestId('family-chat-call-speaker').getAttribute('aria-label'))
      .toBe('Speaker on')
  })

  it('offers to unmute once muted', () => {
    const spies = makeSpies()
    renderOverlay(makeCall(spies, { state: 'active', remoteUserId: 2, muted: true }))

    const mute = screen.getByTestId('family-chat-call-mute')
    expect(mute.getAttribute('aria-label')).toBe('Unmute')
    fireEvent.click(mute)
    expect(spies.setMuted).toHaveBeenCalledWith(false)
  })
})

describe('CallOverlay – active video call', () => {
  function videoCall(spies: CallSpies, overrides: CallOverrides = {}) {
    return makeCall(spies, {
      state: 'active',
      callKind: 'video',
      remoteUserId: 2,
      localStream: fakeStream(),
      remoteStream: fakeStream(),
      ...overrides,
    })
  }

  it('renders the remote pane, the local PiP and the desktop local pane', () => {
    renderOverlay(videoCall(makeSpies()))

    expect(screen.getByTestId('family-chat-active-video-overlay')).toBeInTheDocument()
    expect(screen.getByTestId('family-chat-call-remote-video')).toBeInTheDocument()
    expect(screen.getByTestId('family-chat-call-local-pip')).toBeInTheDocument()
    expect(screen.getByTestId('family-chat-call-local-pane')).toBeInTheDocument()
  })

  it('shows the camera-off placeholders for both ends', () => {
    renderOverlay(videoCall(makeSpies(), { remoteCameraEnabled: false, cameraEnabled: false }))

    expect(screen.getByTestId('family-chat-call-remote-camera-off')).toBeInTheDocument()
    expect(screen.getByTestId('family-chat-call-local-camera-off')).toBeInTheDocument()
  })

  it('camera toggle and switch-camera invoke the hook', () => {
    const spies = makeSpies()
    renderOverlay(videoCall(spies))

    fireEvent.click(screen.getByTestId('family-chat-call-camera'))
    expect(spies.setCameraEnabled).toHaveBeenCalledWith(false)

    fireEvent.click(screen.getByTestId('family-chat-call-switch-camera'))
    expect(spies.switchCamera).toHaveBeenCalledTimes(1)
  })

  it('opens the effects picker and applies the chosen filter', () => {
    const spies = makeSpies()
    renderOverlay(videoCall(spies))

    expect(screen.queryByTestId('family-chat-call-filter-menu')).not.toBeInTheDocument()
    fireEvent.click(screen.getByTestId('family-chat-call-filter'))
    expect(screen.getByTestId('family-chat-call-filter-menu')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('family-chat-call-filter-blur'))
    expect(spies.setFilter).toHaveBeenCalledWith('blur')
    // Choosing an effect closes the popover.
    expect(screen.queryByTestId('family-chat-call-filter-menu')).not.toBeInTheDocument()
  })

  it('marks the active filter as checked', () => {
    renderOverlay(videoCall(makeSpies(), { filter: 'grading' }))

    fireEvent.click(screen.getByTestId('family-chat-call-filter'))
    expect(screen.getByTestId('family-chat-call-filter-grading').getAttribute('aria-checked'))
      .toBe('true')
    expect(screen.getByTestId('family-chat-call-filter-none').getAttribute('aria-checked'))
      .toBe('false')
  })

  it('drags the local PiP and clamps it inside the viewport', () => {
    renderOverlay(videoCall(makeSpies()))
    const pip = screen.getByTestId('family-chat-call-local-pip')

    // Undragged, the PiP sits in its default corner via Tailwind classes.
    expect(pip.style.left).toBe('')
    expect(pip.className).toContain('top-4 right-4')

    // happy-dom reports a zero-sized rect for unlaid-out elements, so the PiP's
    // origin is (0,0) and the grab offset is the pointer-down position: the
    // element ends up at the pointer delta, here (+40, +80).
    fireEvent.pointerDown(pip, { pointerId: 1, clientX: 100, clientY: 100, button: 0 })
    fireEvent.pointerMove(pip, { pointerId: 1, clientX: 140, clientY: 180 })

    const dragged = screen.getByTestId('family-chat-call-local-pip')
    expect(dragged.style.left).toBe('40px')
    expect(dragged.style.top).toBe('80px')
    expect(dragged.className).not.toContain('top-4 right-4')

    // Dragging past the top-left corner clamps to the 8px margin.
    fireEvent.pointerMove(pip, { pointerId: 1, clientX: -500, clientY: -500 })
    expect(screen.getByTestId('family-chat-call-local-pip').style.left).toBe('8px')
    expect(screen.getByTestId('family-chat-call-local-pip').style.top).toBe('8px')

    fireEvent.pointerUp(pip, { pointerId: 1 })
    // After the pointer is released further movement is ignored.
    fireEvent.pointerMove(pip, { pointerId: 1, clientX: 300, clientY: 300 })
    expect(screen.getByTestId('family-chat-call-local-pip').style.left).toBe('8px')
  })

  it('resets the PiP to its default corner when a new call starts', () => {
    const spies = makeSpies()
    const { rerender } = renderOverlay(videoCall(spies, { callId: 'call-1' }))
    const pip = screen.getByTestId('family-chat-call-local-pip')

    fireEvent.pointerDown(pip, { pointerId: 1, clientX: 100, clientY: 100, button: 0 })
    fireEvent.pointerMove(pip, { pointerId: 1, clientX: 140, clientY: 180 })
    expect(screen.getByTestId('family-chat-call-local-pip').style.left).toBe('40px')

    rerender(
      <CallOverlay
        call={videoCall(spies, { callId: 'call-2' })}
        conversationId={1}
        peerLabel={peerLabel}
        fallbackPeerId={2}
      />,
    )

    const fresh = screen.getByTestId('family-chat-call-local-pip')
    expect(fresh.style.left).toBe('')
    expect(fresh.className).toContain('top-4 right-4')
  })
})

describe('CallOverlay – ended banner', () => {
  it('shows the call duration after the call ends and auto-dismisses it', () => {
    vi.useFakeTimers()
    const spies = makeSpies()
    const { rerender } = renderOverlay(makeCall(spies, { state: 'active', remoteUserId: 2 }))

    act(() => { vi.advanceTimersByTime(12_000) })

    rerender(
      <CallOverlay
        call={makeCall(spies, { state: 'ended', remoteUserId: 2 })}
        conversationId={1}
        peerLabel={peerLabel}
        fallbackPeerId={2}
      />,
    )

    expect(screen.getByTestId('family-chat-call-ended').textContent)
      .toBe('Call ended — 0:12')

    act(() => { vi.advanceTimersByTime(5000) })
    expect(screen.queryByTestId('family-chat-call-ended')).not.toBeInTheDocument()
  })
})

describe('formatCallDuration – the helper behind the in-call timer', () => {
  it('renders whole seconds as m:ss and floors negatives to zero', () => {
    expect(formatCallDuration(0)).toBe('0:00')
    expect(formatCallDuration(9)).toBe('0:09')
    expect(formatCallDuration(61)).toBe('1:01')
    expect(formatCallDuration(3599)).toBe('59:59')
    expect(formatCallDuration(-5)).toBe('0:00')
  })
})
