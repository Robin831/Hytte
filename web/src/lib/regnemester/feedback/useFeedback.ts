import { useCallback, useEffect, useRef, useState } from 'react'
import { soundEngine, type SoundName } from './sound'
import { flashCorrect as flashCorrectEl, flashWrong as flashWrongEl } from './flash'
import { vibrate, vibrateCorrect, vibrateWrong } from './haptics'

const STORAGE_KEY = 'regnemester_muted'
const PREF_KEY = 'regnemester_muted'

function readLocalMuted(): boolean {
  if (typeof window === 'undefined') return false
  try {
    return window.localStorage.getItem(STORAGE_KEY) === 'true'
  } catch {
    return false
  }
}

function writeLocalMuted(muted: boolean): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(STORAGE_KEY, muted ? 'true' : 'false')
  } catch {
    // Storage quota or privacy-mode Safari — ignore.
  }
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  if (target.isContentEditable) return true
  return false
}

export interface UseFeedbackResult {
  play: (name: SoundName) => void
  flashCorrect: (el: HTMLElement | null) => void
  flashWrong: (el: HTMLElement | null) => void
  vibrate: (pattern: number | number[]) => void
  vibrateCorrect: () => void
  vibrateWrong: () => void
  muted: boolean
  toggleMute: () => void
  setMuted: (muted: boolean) => void
}

export function useFeedback(): UseFeedbackResult {
  const [muted, setMutedState] = useState<boolean>(() => readLocalMuted())
  // Track the latest muted value inside the keydown listener without having
  // to re-register the handler on every toggle.
  const mutedRef = useRef(muted)

  // Preload sound buffers once on mount so the first play() is snappy.
  useEffect(() => {
    soundEngine.preload()
  }, [])

  // Hydrate mute preference from the server so it persists across devices.
  useEffect(() => {
    let cancelled = false
    fetch('/api/settings/preferences', { credentials: 'include' })
      .then(res => (res.ok ? res.json() : { preferences: {} }))
      .then((data: { preferences?: Record<string, string> }) => {
        if (cancelled) return
        const raw = data.preferences?.[PREF_KEY]
        if (raw === 'true' || raw === 'false') {
          const serverMuted = raw === 'true'
          setMutedState(serverMuted)
          writeLocalMuted(serverMuted)
        }
      })
      .catch(() => { /* non-critical — fall back to local storage value */ })
    return () => { cancelled = true }
  }, [])

  // Mirror mute state into the sound engine and local storage as it changes.
  useEffect(() => {
    mutedRef.current = muted
    soundEngine.setMuted(muted)
    writeLocalMuted(muted)
  }, [muted])

  const persistMuted = useCallback((next: boolean) => {
    fetch('/api/settings/preferences', {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ preferences: { [PREF_KEY]: next ? 'true' : 'false' } }),
    }).catch(() => { /* local storage already holds the value */ })
  }, [])

  const setMuted = useCallback((next: boolean) => {
    setMutedState(prev => {
      if (prev === next) return prev
      persistMuted(next)
      return next
    })
  }, [persistMuted])

  const toggleMute = useCallback(() => {
    setMutedState(prev => {
      const next = !prev
      persistMuted(next)
      return next
    })
  }, [persistMuted])

  // Global 'M' keyboard shortcut — registered once per consumer mount.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'm' && e.key !== 'M') return
      if (e.ctrlKey || e.metaKey || e.altKey) return
      if (isEditableTarget(e.target)) return
      e.preventDefault()
      setMutedState(prev => {
        const next = !prev
        persistMuted(next)
        return next
      })
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [persistMuted])

  const play = useCallback((name: SoundName) => { soundEngine.play(name) }, [])
  const flashCorrect = useCallback((el: HTMLElement | null) => { flashCorrectEl(el) }, [])
  const flashWrong = useCallback((el: HTMLElement | null) => { flashWrongEl(el) }, [])
  const vibrateFn = useCallback((pattern: number | number[]) => { vibrate(pattern) }, [])
  const vibrateCorrectFn = useCallback(() => { vibrateCorrect() }, [])
  const vibrateWrongFn = useCallback(() => { vibrateWrong() }, [])

  return {
    play,
    flashCorrect,
    flashWrong,
    vibrate: vibrateFn,
    vibrateCorrect: vibrateCorrectFn,
    vibrateWrong: vibrateWrongFn,
    muted,
    toggleMute,
    setMuted,
  }
}
