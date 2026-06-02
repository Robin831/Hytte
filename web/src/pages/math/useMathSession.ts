import { useCallback, useEffect, useRef, useState } from 'react'
import type { Dispatch, MutableRefObject, SetStateAction } from 'react'
import { useTranslation } from 'react-i18next'
import { emitAchievementUnlock } from '../../lib/regnemester/feedback'
import type {
  FinishResponse,
  FinishSummary,
  MathMode,
  Phase,
  SessionStartResponse,
  UnlockedAchievement,
} from './types'

// Context handed to a mode's celebration callback once the finish flow has
// committed the shared state (summary, unlocked, phase='done'). Mode-specific
// score/timer syncing and confetti live here, off the shared plumbing.
export interface FinishContext {
  summary: FinishSummary
  response: FinishResponse
  unlocked: UnlockedAchievement[]
}

export interface StartHandlers {
  // Runs synchronously before the start request fires. Marathon uses this to
  // anchor its count-up clock to the moment the player tapped Start, so the
  // displayed elapsed time includes start-request latency.
  onBeforeRequest?: () => void
  // Runs after the session is created, with the server's start response. Each
  // mode seeds its own play state (facts/score/timer anchors) here.
  onStarted: (data: SessionStartResponse) => void
}

export interface UseMathSessionOptions {
  mode: MathMode
  // Endpoint for the prior personal best, e.g. '/api/math/marathon/best'.
  bestPath: string
}

export interface UseMathSession<TBest> {
  phase: Phase
  phaseRef: MutableRefObject<Phase>
  setPhase: (next: Phase) => void
  error: string
  setError: Dispatch<SetStateAction<string>>
  sessionId: number | null
  setSessionId: Dispatch<SetStateAction<number | null>>
  priorBest: TBest | null
  setPriorBest: Dispatch<SetStateAction<TBest | null>>
  summary: FinishSummary | null
  setSummary: Dispatch<SetStateAction<FinishSummary | null>>
  unlocked: UnlockedAchievement[]
  setUnlocked: Dispatch<SetStateAction<UnlockedAchievement[]>>
  // Wall-clock anchors kept in refs so timer ticks don't thrash React state.
  // startedAtRef drives Marathon's count-up; endAtRef drives Blitz's countdown.
  startedAtRef: MutableRefObject<number>
  endAtRef: MutableRefObject<number>
  questionShownAtRef: MutableRefObject<number>
  startSession: (handlers: StartHandlers) => Promise<void>
  finishSession: (id: number, celebrate: (ctx: FinishContext) => void) => Promise<void>
  // Surface an attempt error only if the run still owns the UI. After Finish
  // has run (phase moved to finishing/done/error), a late attempt POST that
  // fails must not clobber the result screen with an error.
  failActiveSession: (message: string) => void
  // Reset the shared state for a play-again. Mode-specific state and the prior
  // best are reset by the page.
  resetSession: () => void
}

// useMathSession owns the session plumbing shared by every math mode: the
// phase state machine (mirrored into phaseRef so async callers can tell whether
// the run still owns the UI), the wall-clock timing refs, the prior-PB fetch
// with AbortController cleanup, and the finish flow that surfaces
// unlocked_achievements + leaderboard_rank. Each mode page layers its own
// scoring and UI on top.
export function useMathSession<TBest>({ mode, bestPath }: UseMathSessionOptions): UseMathSession<TBest> {
  const { t } = useTranslation('regnemester')

  const [phase, setPhaseState] = useState<Phase>('idle')
  const [error, setError] = useState('')
  const [sessionId, setSessionId] = useState<number | null>(null)
  const [priorBest, setPriorBest] = useState<TBest | null>(null)
  const [summary, setSummary] = useState<FinishSummary | null>(null)
  const [unlocked, setUnlocked] = useState<UnlockedAchievement[]>([])

  // phaseRef mirrors phase so async callers (the attempt POST) can tell whether
  // the run still owns the UI by the time their request lands. The setter
  // updates the ref synchronously alongside state so the guard never lags a
  // render behind the real phase.
  const phaseRef = useRef<Phase>('idle')
  const setPhase = useCallback((next: Phase) => {
    phaseRef.current = next
    setPhaseState(next)
  }, [])

  const startedAtRef = useRef<number>(0)
  const endAtRef = useRef<number>(0)
  const questionShownAtRef = useRef<number>(0)

  // Fetch the user's prior PB once on mount so the result screen can decide
  // whether to award a "New PB!" badge. Non-critical — a failed lookup is
  // treated as no prior best.
  useEffect(() => {
    const controller = new AbortController()
    fetch(bestPath, { credentials: 'include', signal: controller.signal })
      .then(res => (res.ok ? res.json() : { best: null }))
      .then((data: { best: TBest | null }) => {
        setPriorBest(data.best ?? null)
      })
      .catch(() => { /* PB lookup is non-critical; treat as no prior */ })
    return () => { controller.abort() }
  }, [bestPath])

  const startSession = useCallback(async (handlers: StartHandlers) => {
    setError('')
    setPhase('starting')
    try {
      handlers.onBeforeRequest?.()
      const res = await fetch('/api/math/sessions', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode }),
      })
      if (!res.ok) throw new Error(t('errors.failedToStart'))
      const data = await res.json() as SessionStartResponse
      setSessionId(data.session_id)
      handlers.onStarted(data)
      setPhase('playing')
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToStart')
      setError(message)
      setPhase('error')
    }
  }, [mode, t, setPhase])

  const finishSession = useCallback(async (id: number, celebrate: (ctx: FinishContext) => void) => {
    setPhase('finishing')
    try {
      const res = await fetch(`/api/math/sessions/${id}/finish`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(t('errors.failedToFinish'))
      const data = await res.json() as FinishResponse
      // Trust the server's summary: those values are what get stored and what
      // future PB lookups compare against.
      const s = data.summary
      setSummary(s)
      const unlockedItems = data.unlocked_achievements ?? []
      setUnlocked(unlockedItems)
      setPhase('done')
      // Broadcast to the global AchievementUnlockOverlay so each unlock gets
      // its own celebration on top of the result screen. The in-page banner
      // still renders as the persistent record.
      emitAchievementUnlock(unlockedItems)
      // Mode-specific score/timer syncing and confetti. Wrapped so a
      // celebration bug doesn't flip an otherwise successful finish to 'error'.
      try {
        celebrate({ summary: s, response: data, unlocked: unlockedItems })
      } catch { /* celebrate is best-effort */ }
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToFinish')
      setError(message)
      setPhase('error')
    }
  }, [t, setPhase])

  const failActiveSession = useCallback((message: string) => {
    // Don't override 'finishing'/'done' with an error from a late attempt POST
    // — the run already finished cleanly.
    if (phaseRef.current === 'playing') {
      setError(message)
      setPhase('error')
    }
  }, [setPhase])

  const resetSession = useCallback(() => {
    setSummary(null)
    setSessionId(null)
    setUnlocked([])
    setError('')
    setPhase('idle')
  }, [setPhase])

  return {
    phase,
    phaseRef,
    setPhase,
    error,
    setError,
    sessionId,
    setSessionId,
    priorBest,
    setPriorBest,
    summary,
    setSummary,
    unlocked,
    setUnlocked,
    startedAtRef,
    endAtRef,
    questionShownAtRef,
    startSession,
    finishSession,
    failActiveSession,
    resetSession,
  }
}
