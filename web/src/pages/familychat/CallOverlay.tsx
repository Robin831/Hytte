import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Phone,
  PhoneOff,
  PhoneIncoming,
  Mic,
  MicOff,
  Volume2,
  VolumeX,
  Video,
  VideoOff,
  SwitchCamera,
  Sparkles,
} from 'lucide-react'
import type { UseVoiceCallApi } from './voice/useVoiceCall'
import { supportedFilters, type FilterKind } from './voice/videoFilters'
import { formatCallDuration } from './utils'

// CallOverlay renders every full-screen surface of a 1:1 voice/video call:
// the incoming-call prompt, the voice-only in-call screen, the video screen
// with its draggable local preview and effects picker, the shared control bar,
// and the "call ended — m:ss" banner that follows a hang-up. The hidden audio
// sink for the peer's stream lives here too.
//
// All call state and media handles come from the useVoiceCall hook, passed in
// as `call`. What stays local is presentation only: the speaker toggle, the
// elapsed-time counter, the PiP drag position and whether the effects popover
// is open. Group (3+ member) calls are a separate mesh — see GroupCallOverlay.

interface CallOverlayProps {
  call: UseVoiceCallApi
  // conversationId scopes the "call ended" banner: switching chats drops a
  // summary left over from the conversation the user just navigated away from.
  conversationId: number | null
  // peerLabel resolves a member id to the friendly name shown on the incoming
  // and in-call surfaces, falling back to "Member #id" for unknown ids.
  peerLabel: (id: number | null) => string
  // fallbackPeerId is the other member of the 1:1 conversation. Used while an
  // outgoing call is still ringing and the hook has not yet learned the remote
  // user id from the answer.
  fallbackPeerId: number | null
}

// CALL_ENDED_BANNER_MS is how long the post-call summary stays on screen. Long
// enough to read the duration, short enough not to feel sticky.
const CALL_ENDED_BANNER_MS = 5000

// PIP_MARGIN_PX keeps the dragged local preview from being pushed flush against
// (or off) the edge of the viewport.
const PIP_MARGIN_PX = 8

// Local overlay state is tagged with the call (or conversation) it belongs to
// rather than being cleared by an effect, so a new call always starts from the
// default PiP corner with the effects popover closed, and a post-call banner
// never follows the user into the next chat.
interface PipDrag {
  callId: string | null
  x: number
  y: number
}

interface FilterPicker {
  callId: string | null
  open: boolean
}

interface EndedCallSummary {
  conversationId: number | null
  durationSec: number
}

export default function CallOverlay({
  call,
  conversationId,
  peerLabel,
  fallbackPeerId,
}: CallOverlayProps) {
  const { t } = useTranslation('familyChat')

  // speakerOn controls the remote audio volume: true = full volume (1.0),
  // false = muted (0) so "Speaker off" means no audio is heard.
  const [speakerOn, setSpeakerOn] = useState(true)
  const [filterPicker, setFilterPicker] = useState<FilterPicker | null>(null)
  // endedCallSummary is shown briefly after a call wraps up. Tracks the final
  // duration in seconds so the banner can render "Call ended — m:ss".
  const [endedCallSummary, setEndedCallSummary] = useState<EndedCallSummary | null>(null)
  // callElapsedSec is the running second-counter shown while a call is active.
  // It rounds to whole seconds so the UI updates once per tick.
  const [callElapsedSec, setCallElapsedSec] = useState(0)
  const callStartedAtRef = useRef<number | null>(null)
  const remoteAudioRef = useRef<HTMLAudioElement | null>(null)
  // Video sinks for video calls: remote is the big pane, local is the PiP on
  // mobile and a separate side-by-side pane on desktop. Both local elements
  // bind to the same MediaStream so the layout can switch via CSS without
  // re-acquiring the camera.
  const remoteVideoRef = useRef<HTMLVideoElement | null>(null)
  const localVideoRef = useRef<HTMLVideoElement | null>(null)
  const localVideoDesktopRef = useRef<HTMLVideoElement | null>(null)
  // pipDrag holds the {x, y} offset for the draggable local-preview window,
  // tagged with the call it was dragged during. While it is null — and for any
  // call other than the one it belongs to — the PiP renders in its default
  // top-right corner via Tailwind classes rather than inline styles.
  const [pipDrag, setPipDrag] = useState<PipDrag | null>(null)
  const pipDragRef = useRef<{ offsetX: number; offsetY: number; pointerId: number } | null>(null)

  // The dragged offset only applies to the live call it was made during, so a
  // stale position can never leak onto the next call's preview.
  const activePipPosition =
    (call.state === 'active' || call.state === 'outgoing-ringing')
      && pipDrag !== null
      && pipDrag.callId === call.callId
      ? { x: pipDrag.x, y: pipDrag.y }
      : null

  // conversationIdRef lets the call-state effect below stamp the conversation
  // onto a call summary without re-running whenever the user switches chats.
  const conversationIdRef = useRef(conversationId)
  useEffect(() => {
    conversationIdRef.current = conversationId
  })

  // Wire the remote audio stream into the hidden <audio> element so the
  // browser actually plays the peer's voice. The peer connection only opens
  // the data path; the page is responsible for piping it into an audio sink.
  // For video calls the same MediaStream is also bound to the remote <video>
  // element below; the <audio> tag is still required for voice calls (no
  // <video> mounts) and as a redundant audio sink while the video pane loads.
  useEffect(() => {
    const el = remoteAudioRef.current
    if (!el) return
    if (call.remoteStream) {
      el.srcObject = call.remoteStream
      el.volume = speakerOn ? 1 : 0
      void el.play().catch(() => {
        // Autoplay rejection — the accept-button click already counts as a
        // user gesture in every supported browser, but a stricter policy
        // would surface here. Nothing actionable from this layer.
      })
    } else {
      el.srcObject = null
    }
  }, [call.remoteStream, speakerOn])

  // Wire the remote video stream into the big remote <video> pane when the
  // pane is mounted (i.e. during an active video call). We also push the
  // stream into the local PiP <video> from call.localStream. Both are
  // muted=true at the element level — the audio sink above plays the sound.
  useEffect(() => {
    const el = remoteVideoRef.current
    if (!el) return
    el.srcObject = call.remoteStream ?? null
    if (call.remoteStream) {
      void el.play().catch(() => { /* autoplay policy — acceptable to ignore */ })
    }
  }, [call.remoteStream, call.state])

  useEffect(() => {
    const el = localVideoRef.current
    if (!el) return
    el.srcObject = call.localStream ?? null
    if (call.localStream) {
      void el.play().catch(() => { /* same as above */ })
    }
  }, [call.localStream, call.state])

  useEffect(() => {
    const el = localVideoDesktopRef.current
    if (!el) return
    el.srcObject = call.localStream ?? null
    if (call.localStream) {
      void el.play().catch(() => { /* autoplay policy — acceptable to ignore */ })
    }
  }, [call.localStream, call.state])

  // Drive the elapsed-time counter while a call is active. Reset on every
  // state transition so the timer always reads from the moment we entered
  // 'active', not from the moment startCall was first invoked.
  useEffect(() => {
    if (call.state !== 'active') {
      // Capture the final elapsed seconds when the call leaves the active
      // state so the "Call ended — m:ss" banner can show the right total.
      if (callStartedAtRef.current !== null) {
        const total = Math.floor((Date.now() - callStartedAtRef.current) / 1000)
        callStartedAtRef.current = null
        if (call.state === 'ended') {
          setEndedCallSummary({ conversationId: conversationIdRef.current, durationSec: total })
        }
      }
      setCallElapsedSec(0)
      return
    }
    callStartedAtRef.current = Date.now()
    setCallElapsedSec(0)
    const interval = setInterval(() => {
      if (callStartedAtRef.current === null) return
      setCallElapsedSec(Math.floor((Date.now() - callStartedAtRef.current) / 1000))
    }, 1000)
    return () => clearInterval(interval)
  }, [call.state])

  // Auto-dismiss the "Call ended" banner after a short hold so it doesn't sit
  // on screen indefinitely.
  useEffect(() => {
    if (!endedCallSummary) return
    const timer = setTimeout(() => setEndedCallSummary(null), CALL_ENDED_BANNER_MS)
    return () => clearTimeout(timer)
  }, [endedCallSummary])

  // The popover is scoped to the call it was opened during, so it can never
  // still be open when the next call's control bar appears.
  const effectiveFilterPickerOpen =
    filterPicker !== null
    && filterPicker.open
    && filterPicker.callId === call.callId
    && call.state === 'active'

  // Video effects the current browser can actually run, in display order. When
  // the device can't run any effect (no canvas pipeline) this is just ['none']
  // and the effects button stays hidden. Computed once — capability is static.
  const availableVideoFilters = useMemo<FilterKind[]>(() => supportedFilters(), [])
  const canFilterVideo = availableVideoFilters.length > 1

  // selectVideoFilter applies a chosen effect and closes the popover. setFilter
  // itself is a no-op for unsupported effects, but we only render supported ones.
  const selectVideoFilter = useCallback((kind: FilterKind) => {
    setFilterPicker({ callId: call.callId, open: false })
    void call.setFilter(kind)
  }, [call])

  // PiP drag handlers. Use Pointer Events so touch + mouse share one code
  // path; setPointerCapture keeps the move/up events targeted even if the
  // pointer briefly leaves the small PiP element while dragging.
  const handlePipPointerDown = useCallback((e: PointerEvent<HTMLDivElement>) => {
    // Only initiate drag for primary button / single touch.
    if (e.button !== 0 && e.pointerType === 'mouse') return
    const target = e.currentTarget
    const rect = target.getBoundingClientRect()
    pipDragRef.current = {
      offsetX: e.clientX - rect.left,
      offsetY: e.clientY - rect.top,
      pointerId: e.pointerId,
    }
    try { target.setPointerCapture(e.pointerId) } catch { /* fine */ }
    e.preventDefault()
  }, [])

  const handlePipPointerMove = useCallback((e: PointerEvent<HTMLDivElement>) => {
    const drag = pipDragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    const target = e.currentTarget
    const rect = target.getBoundingClientRect()
    // Clamp to viewport so the PiP can't be dragged off-screen.
    const viewW = typeof window !== 'undefined' ? window.innerWidth : rect.width
    const viewH = typeof window !== 'undefined' ? window.innerHeight : rect.height
    const rawX = e.clientX - drag.offsetX
    const rawY = e.clientY - drag.offsetY
    const clampedX = Math.max(PIP_MARGIN_PX, Math.min(viewW - rect.width - PIP_MARGIN_PX, rawX))
    const clampedY = Math.max(PIP_MARGIN_PX, Math.min(viewH - rect.height - PIP_MARGIN_PX, rawY))
    setPipDrag({ callId: call.callId, x: clampedX, y: clampedY })
  }, [call.callId])

  const handlePipPointerUp = useCallback((e: PointerEvent<HTMLDivElement>) => {
    const drag = pipDragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    pipDragRef.current = null
    try { e.currentTarget.releasePointerCapture(e.pointerId) } catch { /* fine */ }
  }, [])

  const incomingCallerLabel = peerLabel(call.remoteUserId)
  const activeCallPeerLabel = peerLabel(call.remoteUserId ?? fallbackPeerId)
  const inCall = call.state === 'outgoing-ringing' || call.state === 'active'

  return (
    <>
      {/* Hidden audio sink for the remote peer's stream. Kept outside the
          conditional overlays so the element survives state transitions and
          srcObject assignment isn't fighting React re-renders. */}
      <audio ref={remoteAudioRef} autoPlay playsInline className="hidden" />

      {call.state === 'incoming-ringing' && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="family-chat-incoming-call-title"
          className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-black/85 text-white p-6"
          data-testid="family-chat-incoming-overlay"
        >
          <div className="flex flex-col items-center text-center max-w-sm">
            <div className="mb-6 p-5 rounded-full bg-green-500/20 animate-pulse">
              {call.callKind === 'video'
                ? <Video size={48} aria-hidden="true" className="text-blue-300" />
                : <PhoneIncoming size={48} aria-hidden="true" className="text-green-300" />}
            </div>
            <p
              className="text-sm uppercase tracking-wide text-gray-400"
              data-testid="family-chat-incoming-kind-label"
            >
              {call.callKind === 'video'
                ? t('call.incomingVideoLabel')
                : t('call.incomingLabel')}
            </p>
            <h2
              id="family-chat-incoming-call-title"
              className="mt-2 text-2xl font-semibold"
            >
              {incomingCallerLabel}
            </h2>
            <div className="mt-8 flex items-center justify-center gap-6">
              <button
                type="button"
                onClick={() => { void call.rejectCall() }}
                aria-label={t('call.decline')}
                className="flex flex-col items-center gap-1 text-red-300 hover:text-red-200 cursor-pointer"
                data-testid="family-chat-call-decline"
              >
                <span className="p-4 rounded-full bg-red-600 text-white hover:bg-red-500">
                  <PhoneOff size={28} aria-hidden="true" />
                </span>
                <span className="text-xs">{t('call.decline')}</span>
              </button>
              <button
                type="button"
                onClick={() => { void call.acceptCall() }}
                aria-label={t('call.accept')}
                className="flex flex-col items-center gap-1 text-green-300 hover:text-green-200 cursor-pointer"
                data-testid="family-chat-call-accept"
              >
                <span className="p-4 rounded-full bg-green-600 text-white hover:bg-green-500">
                  <Phone size={28} aria-hidden="true" />
                </span>
                <span className="text-xs">{t('call.accept')}</span>
              </button>
            </div>
          </div>
        </div>
      )}

      {inCall && call.callKind === 'voice' && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="family-chat-active-call-title"
          className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-black/85 text-white p-6"
          data-testid="family-chat-active-overlay"
        >
          <div className="flex flex-col items-center text-center max-w-sm w-full">
            <div className="mb-6 p-5 rounded-full bg-green-500/20">
              <Phone size={48} aria-hidden="true" className="text-green-300" />
            </div>
            <h2
              id="family-chat-active-call-title"
              className="text-2xl font-semibold"
            >
              {activeCallPeerLabel}
            </h2>
            <p
              className="mt-2 text-sm text-gray-300"
              data-testid="family-chat-call-status"
            >
              {call.state === 'outgoing-ringing'
                ? t('call.ringing')
                : formatCallDuration(callElapsedSec)}
            </p>
            {call.error && (
              <p className="mt-2 text-xs text-red-400">{call.error}</p>
            )}
            <div className="mt-8 flex items-center justify-center gap-4">
              <button
                type="button"
                onClick={() => call.setMuted(!call.muted)}
                aria-label={call.muted ? t('call.unmute') : t('call.mute')}
                aria-pressed={call.muted}
                disabled={call.state !== 'active'}
                className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                  call.muted ? 'text-amber-300' : 'text-gray-200'
                }`}
                data-testid="family-chat-call-mute"
              >
                <span className={`p-3 rounded-full ${
                  call.muted ? 'bg-amber-500/20' : 'bg-gray-700'
                }`}>
                  {call.muted
                    ? <MicOff size={24} aria-hidden="true" />
                    : <Mic size={24} aria-hidden="true" />}
                </span>
                <span className="text-xs">
                  {call.muted ? t('call.unmute') : t('call.mute')}
                </span>
              </button>
              <button
                type="button"
                onClick={() => { void call.endCall() }}
                aria-label={t('call.hangup')}
                className="flex flex-col items-center gap-1 text-white cursor-pointer"
                data-testid="family-chat-call-hangup"
              >
                <span className="p-4 rounded-full bg-red-600 hover:bg-red-500">
                  <PhoneOff size={28} aria-hidden="true" />
                </span>
                <span className="text-xs">{t('call.hangup')}</span>
              </button>
              <button
                type="button"
                onClick={() => setSpeakerOn(prev => !prev)}
                aria-label={speakerOn ? t('call.speakerOff') : t('call.speakerOn')}
                aria-pressed={speakerOn}
                disabled={call.state !== 'active'}
                className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                  speakerOn ? 'text-blue-300' : 'text-gray-200'
                }`}
                data-testid="family-chat-call-speaker"
              >
                <span className={`p-3 rounded-full ${
                  speakerOn ? 'bg-blue-500/20' : 'bg-gray-700'
                }`}>
                  {speakerOn
                    ? <Volume2 size={24} aria-hidden="true" />
                    : <VolumeX size={24} aria-hidden="true" />}
                </span>
                <span className="text-xs">
                  {speakerOn ? t('call.speakerOff') : t('call.speakerOn')}
                </span>
              </button>
            </div>
          </div>
        </div>
      )}

      {inCall && call.callKind === 'video' && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="family-chat-active-video-call-title"
          className="fixed inset-0 z-50 flex flex-col bg-black text-white"
          data-testid="family-chat-active-video-overlay"
        >
          {/* Video panes: full-screen remote with draggable PiP on mobile;
              side-by-side remote + local panes on desktop (md:flex-row). */}
          <div className="relative flex-1 min-h-0 flex flex-col md:flex-row">
            <div className="relative flex-1 min-h-0 bg-gray-950">
              <video
                ref={remoteVideoRef}
                autoPlay
                playsInline
                muted
                aria-label={t('call.remoteVideo')}
                className="absolute inset-0 w-full h-full object-cover"
                data-testid="family-chat-call-remote-video"
              />
              {/* Shown when the remote peer disables their camera. Sits above
                  the frozen video frame so the viewer has a clear indicator. */}
              {!call.remoteCameraEnabled && (
                <div
                  className="absolute inset-0 flex flex-col items-center justify-center bg-gray-950/80 text-gray-300"
                  data-testid="family-chat-call-remote-camera-off"
                >
                  <VideoOff size={32} aria-hidden="true" />
                  <span className="mt-2 text-sm">{t('call.remoteCameraOff')}</span>
                </div>
              )}
              {/* Translucent header with peer label + status. */}
              <div className="absolute top-0 inset-x-0 p-3 sm:p-4 flex items-start gap-3 bg-gradient-to-b from-black/60 to-transparent">
                <div className="flex-1 min-w-0">
                  <h2
                    id="family-chat-active-video-call-title"
                    className="text-base sm:text-lg font-semibold truncate"
                  >
                    {activeCallPeerLabel}
                  </h2>
                  <p
                    className="text-xs text-gray-300"
                    data-testid="family-chat-call-status"
                  >
                    {call.state === 'outgoing-ringing'
                      ? t('call.ringing')
                      : formatCallDuration(callElapsedSec)}
                  </p>
                </div>
              </div>

              {/* Mobile PiP local preview. Hidden on md+ where the local pane
                  below takes over. Defaults to top-right; switches to inline
                  style once dragged. */}
              {call.localStream && (
                <div
                  data-testid="family-chat-call-local-pip"
                  onPointerDown={handlePipPointerDown}
                  onPointerMove={handlePipPointerMove}
                  onPointerUp={handlePipPointerUp}
                  onPointerCancel={handlePipPointerUp}
                  className={`md:hidden absolute touch-none cursor-move w-28 h-40 sm:w-36 sm:h-48 rounded-lg overflow-hidden border border-gray-700 bg-gray-900 shadow-lg ${
                    activePipPosition === null ? 'top-4 right-4' : ''
                  }`}
                  style={activePipPosition === null ? undefined : { top: activePipPosition.y, left: activePipPosition.x }}
                >
                  <video
                    ref={localVideoRef}
                    autoPlay
                    playsInline
                    muted
                    aria-label={t('call.localPreview')}
                    className="w-full h-full object-cover scale-x-[-1]"
                    data-testid="family-chat-call-local-video"
                  />
                  {!call.cameraEnabled && (
                    <div
                      className="absolute inset-0 flex items-center justify-center bg-gray-900/80 text-xs text-gray-300"
                      data-testid="family-chat-call-local-camera-off"
                    >
                      <VideoOff size={18} aria-hidden="true" />
                    </div>
                  )}
                </div>
              )}

              {call.error && (
                <div className="absolute bottom-24 left-4 right-4 sm:left-auto sm:right-4 sm:max-w-sm">
                  <p className="text-xs text-red-300 bg-red-900/40 border border-red-700 rounded-md px-2 py-1">
                    {call.error}
                  </p>
                </div>
              )}
            </div>

            {/* Desktop-only local pane — sits side-by-side with the remote on
                md+ screens, where the mobile PiP is hidden. */}
            {call.localStream && (
              <div
                data-testid="family-chat-call-local-pane"
                className="hidden md:block relative flex-1 min-h-0 bg-gray-900 border-l border-gray-800"
              >
                <video
                  ref={localVideoDesktopRef}
                  autoPlay
                  playsInline
                  muted
                  aria-label={t('call.localPreview')}
                  className="absolute inset-0 w-full h-full object-cover scale-x-[-1]"
                  data-testid="family-chat-call-local-video-desktop"
                />
                {!call.cameraEnabled && (
                  <div
                    className="absolute inset-0 flex items-center justify-center bg-gray-900/80 text-sm text-gray-300"
                    data-testid="family-chat-call-local-camera-off-desktop"
                  >
                    <VideoOff size={24} aria-hidden="true" />
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Bottom control bar — full width on mobile, sits across the bottom
              on desktop too (matches the spec). */}
          <div className="shrink-0 bg-gray-950 border-t border-gray-800 px-4 py-3 flex items-center justify-center gap-3 sm:gap-5">
            <button
              type="button"
              onClick={() => call.setMuted(!call.muted)}
              aria-label={call.muted ? t('call.unmute') : t('call.mute')}
              aria-pressed={call.muted}
              disabled={call.state !== 'active'}
              className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                call.muted ? 'text-amber-300' : 'text-gray-200'
              }`}
              data-testid="family-chat-call-mute"
            >
              <span className={`p-3 rounded-full ${
                call.muted ? 'bg-amber-500/20' : 'bg-gray-700'
              }`}>
                {call.muted
                  ? <MicOff size={24} aria-hidden="true" />
                  : <Mic size={24} aria-hidden="true" />}
              </span>
              <span className="text-xs hidden sm:inline">
                {call.muted ? t('call.unmute') : t('call.mute')}
              </span>
            </button>
            <button
              type="button"
              onClick={() => { void call.setCameraEnabled(!call.cameraEnabled) }}
              aria-label={call.cameraEnabled ? t('call.cameraOff') : t('call.cameraOn')}
              aria-pressed={!call.cameraEnabled}
              disabled={call.state !== 'active'}
              className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                call.cameraEnabled ? 'text-gray-200' : 'text-amber-300'
              }`}
              data-testid="family-chat-call-camera"
            >
              <span className={`p-3 rounded-full ${
                call.cameraEnabled ? 'bg-gray-700' : 'bg-amber-500/20'
              }`}>
                {call.cameraEnabled
                  ? <Video size={24} aria-hidden="true" />
                  : <VideoOff size={24} aria-hidden="true" />}
              </span>
              <span className="text-xs hidden sm:inline">
                {call.cameraEnabled ? t('call.cameraOff') : t('call.cameraOn')}
              </span>
            </button>
            <button
              type="button"
              onClick={() => { void call.switchCamera() }}
              aria-label={t('call.switchCamera')}
              disabled={call.state !== 'active' || !call.cameraEnabled}
              className="flex flex-col items-center gap-1 cursor-pointer text-gray-200 disabled:opacity-40 disabled:cursor-not-allowed"
              data-testid="family-chat-call-switch-camera"
            >
              <span className="p-3 rounded-full bg-gray-700">
                <SwitchCamera size={24} aria-hidden="true" />
              </span>
              <span className="text-xs hidden sm:inline">
                {t('call.switchCamera')}
              </span>
            </button>
            {canFilterVideo && (
              <div className="relative flex flex-col items-center">
                {/* Effects popover — opens above the control bar. */}
                {effectiveFilterPickerOpen && (
                  <div
                    role="menu"
                    aria-label={t('call.filters.title')}
                    className="absolute bottom-full mb-3 left-1/2 -translate-x-1/2 w-44 rounded-xl bg-gray-800 border border-gray-700 shadow-xl p-1.5 flex flex-col gap-0.5"
                    data-testid="family-chat-call-filter-menu"
                  >
                    <p role="presentation" className="px-2 py-1 text-[11px] uppercase tracking-wide text-gray-400">
                      {t('call.filters.title')}
                    </p>
                    {availableVideoFilters.map(kind => {
                      const active = call.filter === kind
                      return (
                        <button
                          key={kind}
                          type="button"
                          role="menuitemradio"
                          aria-checked={active}
                          onClick={() => selectVideoFilter(kind)}
                          className={`flex items-center justify-between gap-2 rounded-lg px-3 py-2 text-sm cursor-pointer text-left ${
                            active
                              ? 'bg-blue-500/20 text-blue-200'
                              : 'text-gray-200 hover:bg-gray-700'
                          }`}
                          data-testid={`family-chat-call-filter-${kind}`}
                        >
                          <span>{t(`call.filters.${kind}`)}</span>
                          {active && <span aria-hidden="true">✓</span>}
                        </button>
                      )
                    })}
                  </div>
                )}
                <button
                  type="button"
                  onClick={() => setFilterPicker({ callId: call.callId, open: !effectiveFilterPickerOpen })}
                  aria-label={t('call.filters.button')}
                  aria-haspopup="menu"
                  aria-expanded={effectiveFilterPickerOpen}
                  disabled={call.state !== 'active'}
                  className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                    call.filter !== 'none' ? 'text-blue-300' : 'text-gray-200'
                  }`}
                  data-testid="family-chat-call-filter"
                >
                  <span className={`p-3 rounded-full ${
                    call.filter !== 'none' ? 'bg-blue-500/20' : 'bg-gray-700'
                  }`}>
                    <Sparkles size={24} aria-hidden="true" />
                  </span>
                  <span className="text-xs hidden sm:inline">
                    {t('call.filters.button')}
                  </span>
                </button>
              </div>
            )}
            <button
              type="button"
              onClick={() => { void call.endCall() }}
              aria-label={t('call.hangup')}
              className="flex flex-col items-center gap-1 text-white cursor-pointer"
              data-testid="family-chat-call-hangup"
            >
              <span className="p-4 rounded-full bg-red-600 hover:bg-red-500">
                <PhoneOff size={28} aria-hidden="true" />
              </span>
              <span className="text-xs hidden sm:inline">{t('call.hangup')}</span>
            </button>
            <button
              type="button"
              onClick={() => setSpeakerOn(prev => !prev)}
              aria-label={speakerOn ? t('call.speakerOff') : t('call.speakerOn')}
              aria-pressed={speakerOn}
              disabled={call.state !== 'active'}
              className={`flex flex-col items-center gap-1 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                speakerOn ? 'text-blue-300' : 'text-gray-200'
              }`}
              data-testid="family-chat-call-speaker"
            >
              <span className={`p-3 rounded-full ${
                speakerOn ? 'bg-blue-500/20' : 'bg-gray-700'
              }`}>
                {speakerOn
                  ? <Volume2 size={24} aria-hidden="true" />
                  : <VolumeX size={24} aria-hidden="true" />}
              </span>
              <span className="text-xs hidden sm:inline">
                {speakerOn ? t('call.speakerOff') : t('call.speakerOn')}
              </span>
            </button>
          </div>
        </div>
      )}

      {endedCallSummary
        && endedCallSummary.conversationId === conversationId
        && !inCall
        && call.state !== 'incoming-ringing' && (
        <div
          role="status"
          aria-live="polite"
          className="fixed top-4 left-1/2 -translate-x-1/2 z-50 px-3 py-1.5 rounded-full bg-gray-800/95 border border-gray-700 text-gray-100 text-xs shadow-lg"
          data-testid="family-chat-call-ended"
        >
          {t('call.ended', { duration: formatCallDuration(endedCallSummary.durationSec) })}
        </div>
      )}
    </>
  )
}
