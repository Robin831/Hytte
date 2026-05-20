import { useCallback, useEffect, useId, useMemo, useRef, useState, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Play, Pause } from 'lucide-react'
import * as voicePlayer from './voicePlayer'

interface VoiceBubbleProps {
  messageId: number
  src: string
  bars: number[]
  durationMs: number
  // isOwn shifts the colour scheme to match the surrounding own/peer bubble
  // styling in ChatView. Defaults to the peer palette.
  isOwn?: boolean
}

// SVG geometry. The waveform draws into a fixed coordinate space so the
// foreground clip math is straightforward; the outer <svg> scales to fit the
// parent's flex layout via the responsive width class.
const WAVE_WIDTH = 160
const WAVE_HEIGHT = 28
const BAR_GAP = 1.5
const MIN_BAR_HEIGHT = 2

function formatDuration(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000))
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

export default function VoiceBubble({
  messageId,
  src,
  bars,
  durationMs,
  isOwn = false,
}: VoiceBubbleProps) {
  const { t } = useTranslation('familyChat')
  const idKey = String(messageId)
  const clipId = useId().replace(/:/g, '')
  const svgRef = useRef<SVGSVGElement>(null)

  // Local snapshot of the singleton player so the bubble re-renders on
  // play/pause/seek/ended without forcing the player to be React-aware.
  const [playerState, setPlayerState] = useState(() => voicePlayer.getState())

  useEffect(() => {
    return voicePlayer.subscribe(setPlayerState)
  }, [])

  const isActive = playerState.currentId === idKey
  const isPlaying = isActive && playerState.playing

  // Prefer the live duration once metadata has loaded — it's authoritative.
  // When the bubble is idle (no metadata yet) fall back to the precomputed
  // value from waveform.ts.
  const effectiveDurationMs = isActive && playerState.durationMs > 0
    ? playerState.durationMs
    : durationMs

  const positionMs = isActive ? Math.min(playerState.positionMs, effectiveDurationMs) : 0
  const progressRatio = effectiveDurationMs > 0
    ? Math.max(0, Math.min(1, positionMs / effectiveDurationMs))
    : 0

  const safeBars = useMemo(() => {
    if (!Array.isArray(bars) || bars.length === 0) return new Array(32).fill(0)
    return bars
  }, [bars])
  const barCount = safeBars.length
  const barWidth = (WAVE_WIDTH - BAR_GAP * (barCount - 1)) / barCount

  const togglePlay = useCallback(() => {
    if (isPlaying) {
      voicePlayer.pause()
    } else {
      void voicePlayer.play(idKey, src)
    }
  }, [idKey, isPlaying, src])

  // handleSeek maps a pointer X within the SVG to a duration offset.
  const handleSeek = useCallback((event: PointerEvent<SVGSVGElement>) => {
    if (effectiveDurationMs <= 0) return
    const svg = svgRef.current
    if (!svg) return
    const rect = svg.getBoundingClientRect()
    if (rect.width <= 0) return
    const ratio = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width))
    const targetMs = ratio * effectiveDurationMs
    if (!isActive) {
      // Seeking on an idle bubble: start playback from the picked offset.
      void voicePlayer.play(idKey, src).then(() => voicePlayer.seek(targetMs))
    } else {
      voicePlayer.seek(targetMs)
    }
  }, [effectiveDurationMs, idKey, isActive, src])

  const fillWidth = WAVE_WIDTH * progressRatio
  const baseColor = isOwn ? 'rgba(255,255,255,0.45)' : 'rgba(156,163,175,0.6)'
  const accentColor = isOwn ? '#ffffff' : '#60a5fa'

  const playLabel = isPlaying ? t('voice.bubble.pause') : t('voice.bubble.play')

  return (
    <div
      className="flex items-center gap-2"
      data-testid={`voice-bubble-${messageId}`}
    >
      <button
        type="button"
        onClick={togglePlay}
        aria-label={playLabel}
        aria-pressed={isPlaying}
        title={playLabel}
        className={`shrink-0 flex items-center justify-center w-8 h-8 rounded-full transition-colors cursor-pointer ${
          isOwn
            ? 'bg-blue-500/40 hover:bg-blue-500/60 text-white'
            : 'bg-gray-700/70 hover:bg-gray-700 text-gray-100'
        }`}
        data-testid={`voice-bubble-play-${messageId}`}
      >
        {isPlaying ? (
          <Pause size={16} aria-hidden="true" />
        ) : (
          <Play size={16} aria-hidden="true" />
        )}
      </button>
      <svg
        ref={svgRef}
        viewBox={`0 0 ${WAVE_WIDTH} ${WAVE_HEIGHT}`}
        preserveAspectRatio="none"
        className="h-7 flex-1 min-w-[100px] max-w-[200px] cursor-pointer touch-none"
        onClick={handleSeek}
        role="slider"
        aria-label={t('voice.bubble.seek')}
        aria-valuemin={0}
        aria-valuemax={effectiveDurationMs}
        aria-valuenow={positionMs}
        data-testid={`voice-bubble-wave-${messageId}`}
      >
        <defs>
          <clipPath id={`voice-bubble-clip-${clipId}`}>
            <rect x={0} y={0} width={fillWidth} height={WAVE_HEIGHT} />
          </clipPath>
        </defs>
        <g fill={baseColor} data-testid={`voice-bubble-bars-${messageId}`}>
          {safeBars.map((value, i) => {
            const h = Math.max(MIN_BAR_HEIGHT, value * WAVE_HEIGHT)
            const y = (WAVE_HEIGHT - h) / 2
            const x = i * (barWidth + BAR_GAP)
            return (
              <rect
                key={i}
                x={x}
                y={y}
                width={barWidth}
                height={h}
                rx={barWidth / 2}
                data-testid={`voice-bubble-bar-${messageId}-${i}`}
              />
            )
          })}
        </g>
        <g
          fill={accentColor}
          clipPath={`url(#voice-bubble-clip-${clipId})`}
          data-testid={`voice-bubble-fill-${messageId}`}
        >
          {safeBars.map((value, i) => {
            const h = Math.max(MIN_BAR_HEIGHT, value * WAVE_HEIGHT)
            const y = (WAVE_HEIGHT - h) / 2
            const x = i * (barWidth + BAR_GAP)
            return (
              <rect
                key={i}
                x={x}
                y={y}
                width={barWidth}
                height={h}
                rx={barWidth / 2}
              />
            )
          })}
        </g>
      </svg>
      <span
        className={`shrink-0 text-[11px] font-mono tabular-nums ${
          isOwn ? 'text-blue-50/90' : 'text-gray-400'
        }`}
        data-testid={`voice-bubble-duration-${messageId}`}
      >
        {formatDuration(isActive && isPlaying ? positionMs : effectiveDurationMs)}
      </span>
    </div>
  )
}
