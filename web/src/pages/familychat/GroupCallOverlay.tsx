import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Mic, MicOff, Video, VideoOff, PhoneOff, Users } from 'lucide-react'
import type { CallKind } from './voice/useVoiceCall'
import type { GroupParticipant, UseGroupCallApi } from './voice/useGroupCall'

// GroupCallOverlay renders the in-call UI for a group (3+ participant) mesh
// call: a responsive grid of remote participant tiles plus a local self-view,
// with mute / camera / leave controls. It is intentionally presentational —
// all state and media handles come from the useGroupCall hook.

interface MemberInfo {
  label: string
  emoji: string
}

interface GroupCallOverlayProps {
  call: UseGroupCallApi
  memberLabel: (id: number) => string
  memberInfo: (id: number) => MemberInfo
  selfEmoji: string
}

// RemoteTile binds one participant's MediaStream into a <video> (video calls)
// or a hidden <audio> (voice calls) and overlays the member's name. When the
// peer's camera is off (or no video has arrived yet) it shows their avatar.
function RemoteTile({ participant, kind, label, emoji }: {
  participant: GroupParticipant
  kind: CallKind
  label: string
  emoji: string
}) {
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const audioRef = useRef<HTMLAudioElement | null>(null)

  useEffect(() => {
    const el = videoRef.current
    if (!el) return
    el.srcObject = participant.stream ?? null
    if (participant.stream) void el.play().catch(() => {})
  }, [participant.stream])

  useEffect(() => {
    const el = audioRef.current
    if (!el) return
    el.srcObject = participant.stream ?? null
    if (participant.stream) void el.play().catch(() => {})
  }, [participant.stream])

  const showVideo = kind === 'video' && participant.cameraEnabled && participant.stream

  return (
    <div className="relative bg-gray-800 rounded-xl overflow-hidden flex items-center justify-center aspect-video min-h-[120px]">
      {kind === 'video' ? (
        <video
          ref={videoRef}
          autoPlay
          playsInline
          className={`w-full h-full object-cover ${showVideo ? '' : 'hidden'}`}
          aria-label={label}
        />
      ) : (
        <audio ref={audioRef} autoPlay className="hidden" aria-label={label} />
      )}
      {!showVideo && (
        <div className="flex flex-col items-center gap-2 text-gray-300">
          <span className="text-4xl" aria-hidden="true">{emoji}</span>
        </div>
      )}
      <span className="absolute bottom-1.5 left-1.5 px-2 py-0.5 rounded-md bg-black/50 text-xs text-white truncate max-w-[90%]">
        {label}
      </span>
    </div>
  )
}

export default function GroupCallOverlay({ call, memberLabel, memberInfo, selfEmoji }: GroupCallOverlayProps) {
  const { t } = useTranslation('familyChat')
  const localVideoRef = useRef<HTMLVideoElement | null>(null)

  useEffect(() => {
    const el = localVideoRef.current
    if (!el) return
    el.srcObject = call.localStream ?? null
    if (call.localStream) void el.play().catch(() => {})
  }, [call.localStream])

  if (call.state !== 'active') return null

  const isVideo = call.callKind === 'video'
  const showLocalVideo = isVideo && call.cameraEnabled && call.localStream

  return (
    <div
      className="fixed inset-0 z-50 bg-gray-950/95 flex flex-col"
      role="dialog"
      aria-modal="true"
      aria-label={t('call.group.title')}
      data-testid="family-chat-group-call"
    >
      <div className="flex items-center gap-2 px-4 py-3 text-white shrink-0">
        <Users size={18} aria-hidden="true" className="text-blue-300" />
        <span className="font-medium">{t('call.group.title')}</span>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto px-3 sm:px-4 pb-3">
        {call.participants.length === 0 && (
          <p className="text-center text-gray-400 text-sm py-8">{t('call.group.waiting')}</p>
        )}
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-2 sm:gap-3">
          {call.participants.map(p => {
            const info = memberInfo(p.userId)
            return (
              <RemoteTile
                key={p.userId}
                participant={p}
                kind={call.callKind}
                label={memberLabel(p.userId)}
                emoji={info.emoji}
              />
            )
          })}

          {/* Local self-view tile. */}
          <div className="relative bg-gray-800 rounded-xl overflow-hidden flex items-center justify-center aspect-video min-h-[120px]">
            {isVideo ? (
              <video
                ref={localVideoRef}
                autoPlay
                playsInline
                muted
                className={`w-full h-full object-cover ${showLocalVideo ? '' : 'hidden'}`}
                aria-label={t('call.localPreview')}
              />
            ) : null}
            {!showLocalVideo && (
              <div className="flex flex-col items-center gap-2 text-gray-300">
                <span className="text-4xl" aria-hidden="true">{selfEmoji}</span>
              </div>
            )}
            <span className="absolute bottom-1.5 left-1.5 px-2 py-0.5 rounded-md bg-black/50 text-xs text-white">
              {t('chat.you')}
            </span>
            {call.muted && (
              <span className="absolute top-1.5 right-1.5 p-1 rounded-full bg-black/50 text-red-300">
                <MicOff size={14} aria-hidden="true" />
              </span>
            )}
          </div>
        </div>
      </div>

      <div className="flex items-center justify-center gap-3 px-4 py-4 shrink-0">
        <button
          type="button"
          onClick={() => call.setMuted(!call.muted)}
          aria-label={call.muted ? t('call.unmute') : t('call.mute')}
          aria-pressed={call.muted}
          className={`flex flex-col items-center gap-1 p-3 rounded-full cursor-pointer ${
            call.muted ? 'bg-amber-500/20 text-amber-300' : 'bg-gray-700 text-gray-200 hover:bg-gray-600'
          }`}
        >
          {call.muted ? <MicOff size={22} aria-hidden="true" /> : <Mic size={22} aria-hidden="true" />}
        </button>

        {isVideo && (
          <button
            type="button"
            onClick={() => { void call.setCameraEnabled(!call.cameraEnabled) }}
            aria-label={call.cameraEnabled ? t('call.cameraOff') : t('call.cameraOn')}
            aria-pressed={!call.cameraEnabled}
            className={`flex flex-col items-center gap-1 p-3 rounded-full cursor-pointer ${
              call.cameraEnabled ? 'bg-gray-700 text-gray-200 hover:bg-gray-600' : 'bg-amber-500/20 text-amber-300'
            }`}
          >
            {call.cameraEnabled ? <Video size={22} aria-hidden="true" /> : <VideoOff size={22} aria-hidden="true" />}
          </button>
        )}

        <button
          type="button"
          onClick={() => { void call.leaveCall() }}
          aria-label={t('call.group.leave')}
          className="flex flex-col items-center gap-1 p-3 rounded-full bg-red-600 text-white hover:bg-red-500 cursor-pointer"
          data-testid="family-chat-group-leave"
        >
          <PhoneOff size={22} aria-hidden="true" />
        </button>
      </div>
    </div>
  )
}
