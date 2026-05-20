// voicePlayer is a singleton wrapper around a single HTMLAudioElement so the
// Family Chat view can guarantee that at most one voice-note bubble plays at a
// time. Pressing play on a second bubble pauses the first; navigating away
// from the conversation tears the element down via stopAll.
//
// The module is intentionally framework-agnostic: bubbles subscribe to state
// changes via subscribe() and trigger transitions via play/pause/seek. Tests
// can inject a custom HTMLAudioElement factory through setAudioFactory.

export interface VoicePlayerState {
  // currentId is the message id of the bubble that owns the audio element, or
  // null when nothing is loaded / playing.
  currentId: string | null
  playing: boolean
  positionMs: number
  durationMs: number
}

export type VoicePlayerListener = (state: VoicePlayerState) => void

type AudioFactory = () => HTMLAudioElement

let audio: HTMLAudioElement | null = null
let currentId: string | null = null
let lastSrc: string | null = null
let audioFactory: AudioFactory | null = null
// pendingSeekMs queues a seek target for when loadedmetadata fires. Safari
// silently drops currentTime assignments before duration is known, so callers
// that do play().then(() => seek(x)) may land here before metadata loads.
let pendingSeekMs: number | null = null

const listeners = new Set<VoicePlayerListener>()

function defaultFactory(): HTMLAudioElement {
  return new Audio()
}

function getFactory(): AudioFactory {
  if (audioFactory) return audioFactory
  if (typeof Audio !== 'undefined') return defaultFactory
  // Last-resort fallback for environments without window.Audio: return an
  // object with the methods we touch so subscribers don't crash. Playback
  // won't actually emit sound, but the UI stays interactive.
  return () => ({
    src: '',
    currentTime: 0,
    duration: NaN,
    paused: true,
    play: () => Promise.resolve(),
    pause: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  } as unknown as HTMLAudioElement)
}

function snapshot(): VoicePlayerState {
  if (!audio) {
    return { currentId, playing: false, positionMs: 0, durationMs: 0 }
  }
  const duration = Number.isFinite(audio.duration) ? audio.duration : 0
  return {
    currentId,
    playing: !audio.paused && !audio.ended,
    positionMs: Math.max(0, Math.round(audio.currentTime * 1000)),
    durationMs: Math.max(0, Math.round(duration * 1000)),
  }
}

function notify(): void {
  const state = snapshot()
  for (const listener of listeners) {
    try { listener(state) } catch { /* listener errors must not break siblings */ }
  }
}

function handleLoadedMetadata(): void {
  if (pendingSeekMs !== null && audio) {
    try {
      audio.currentTime = pendingSeekMs / 1000
    } catch {
      // Safari may still reject the assignment; the user will start from 0.
    }
    pendingSeekMs = null
  }
  notify()
}

function teardown(): void {
  pendingSeekMs = null
  if (!audio) return
  try { audio.pause() } catch { /* already paused */ }
  audio.removeEventListener('timeupdate', notify)
  audio.removeEventListener('play', notify)
  audio.removeEventListener('pause', notify)
  audio.removeEventListener('ended', notify)
  audio.removeEventListener('loadedmetadata', handleLoadedMetadata)
  audio.removeEventListener('seeked', notify)
  try { audio.src = '' } catch { /* ignore */ }
  audio = null
  lastSrc = null
}

function ensureAudio(): HTMLAudioElement {
  if (audio) return audio
  audio = getFactory()()
  audio.addEventListener('timeupdate', notify)
  audio.addEventListener('play', notify)
  audio.addEventListener('pause', notify)
  audio.addEventListener('ended', notify)
  audio.addEventListener('loadedmetadata', handleLoadedMetadata)
  audio.addEventListener('seeked', notify)
  return audio
}

export function getState(): VoicePlayerState {
  return snapshot()
}

export function subscribe(listener: VoicePlayerListener): () => void {
  listeners.add(listener)
  // Push the current state immediately so subscribers don't have to call
  // getState separately to bootstrap their render.
  try { listener(snapshot()) } catch { /* ignore */ }
  return () => { listeners.delete(listener) }
}

// play attaches `src` to the singleton element (swapping out any previous
// recording) and begins playback. Autoplay errors are caught and swallowed —
// the function always resolves. Callers that need to seek to an offset after
// play should call seek() afterwards; any seek issued before loadedmetadata
// fires is queued and applied automatically.
export async function play(id: string, src: string): Promise<void> {
  const el = ensureAudio()
  if (currentId !== id || lastSrc !== src) {
    try { el.pause() } catch { /* ignore */ }
    pendingSeekMs = null
    currentId = id
    lastSrc = src
    el.src = src
    el.currentTime = 0
  }
  try {
    const result = el.play()
    notify()
    if (result && typeof (result as Promise<void>).then === 'function') {
      await result
    }
  } catch {
    notify()
  }
}

export function pause(): void {
  if (!audio) return
  try { audio.pause() } catch { /* ignore */ }
  notify()
}

export function seek(positionMs: number): void {
  if (!audio) return
  if (!Number.isFinite(audio.duration)) {
    // Metadata has not loaded yet. Queue the seek so handleLoadedMetadata
    // applies it once the duration is known. This is the common Safari path
    // when seek() is called immediately after play() resolves.
    pendingSeekMs = positionMs
    notify()
    return
  }
  pendingSeekMs = null
  const seconds = Math.max(0, positionMs / 1000)
  try {
    audio.currentTime = seconds
  } catch {
    // Some browsers may still throw after loadedmetadata on first load.
  }
  notify()
}

export function stop(): void {
  if (!audio) {
    if (currentId !== null) {
      currentId = null
      notify()
    }
    return
  }
  try { audio.pause() } catch { /* ignore */ }
  try { audio.currentTime = 0 } catch { /* ignore */ }
  notify()
}

// stopAll tears the singleton element down completely. The ChatView effect
// calls this when the active conversation changes or the component unmounts so
// a half-played voice note doesn't keep playing in a different chat.
export function stopAll(): void {
  teardown()
  if (currentId !== null) {
    currentId = null
  }
  notify()
}

export function getCurrentId(): string | null {
  return currentId
}

// setAudioFactory is exposed for tests so a deterministic mock element can be
// injected ahead of the first play() call. Passing null reverts to the
// default factory.
export function setAudioFactory(factory: AudioFactory | null): void {
  // Tearing down here keeps the next ensureAudio() call honest: it will pick
  // the new factory rather than reuse the previous element.
  teardown()
  audioFactory = factory
  currentId = null
  lastSrc = null
}
