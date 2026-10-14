import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import type { MacroPlanView, WeekSummary } from '../types/stride'
import type { MacroAction } from '../components/stride/LongTermPlanCard'

// How many past weeks the long-term plan reads for its target-vs-actual bars.
// 24 is the server's HistoryMaxLimit, so this is one request rather than a
// paginated walk, and it covers most of a 26-week block's elapsed weeks.
const MACRO_ACTUALS_WEEKS = 24

// The server bounds a generation at three Claude attempts of 300s each; the
// page waits a little longer than that before giving up on the stream.
const MACRO_OUTCOME_FALLBACK_MS = 16 * 60 * 1000

export interface UseStrideMacroPlanResult {
  /** The active macro block, or null when the athlete has none. */
  macroPlan: MacroPlanView | null
  /** Past weeks behind the block's target-vs-actual bars. */
  macroHistory: WeekSummary[]
  macroHistoryLoading: boolean
  macroHistoryError: boolean
  /** Which action is waiting for its outcome on the training stream, or null. */
  macroAction: MacroAction | null
  macroActionError: string
  /** Re-read the block. Aborted on unmount. */
  reloadMacroPlan: () => Promise<void>
  /** Re-read the past weeks. Aborted on unmount. */
  reloadMacroHistory: () => Promise<void>
  /** Feed a stride_macro_ready / stride_macro_failed event from the training stream. */
  handleOutcome: (kind: 'ready' | 'failed', data: unknown) => void
  /** Regenerate the block; the outcome arrives through handleOutcome. */
  regenerate: () => void
  /** Extend the block; the outcome arrives through handleOutcome. */
  extend: () => void
}

/**
 * Owns the Stride macro block: the active block, the past weeks behind its
 * target-vs-actual bars, and the Regenerate / Extend actions whose outcome
 * arrives later on the training SSE stream (fed in through handleOutcome).
 */
export function useStrideMacroPlan(): UseStrideMacroPlanResult {
  const { t } = useTranslation('stride')
  // The active macro block, or null when the athlete has none (404) — the
  // timeline then falls back to the A-race heuristic and the long-term plan
  // section stays hidden.
  const [macroPlan, setMacroPlan] = useState<MacroPlanView | null>(null)
  // Past weeks behind the block's target-vs-actual bars. Loaded separately from
  // the Plan History section, which paginates its own copy.
  const [macroHistory, setMacroHistory] = useState<WeekSummary[]>([])
  const [macroHistoryLoading, setMacroHistoryLoading] = useState(true)
  const [macroHistoryError, setMacroHistoryError] = useState(false)
  // Which macro action is waiting for its outcome, or null. The POST answers
  // 202 as soon as the generation is under way — a 26-week block takes minutes,
  // longer than a silent request survives the Cloudflare edge — and the result
  // arrives on the training SSE stream as stride_macro_ready / _failed. Both
  // endpoints take the athlete's per-user lock server-side, so only one can
  // run at a time.
  const [macroAction, setMacroAction] = useState<MacroAction | null>(null)
  const [macroActionError, setMacroActionError] = useState('')
  const macroFallbackTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Lives for as long as the hook is mounted; reloads triggered outside the
  // mount effect use it so a response that lands after unmount is dropped.
  const lifetimeRef = useRef<AbortController | null>(null)

  // The macro block behind the weekly plans. A 404 means the athlete has no
  // block yet, which is a normal state rather than an error, so it clears the
  // plan instead of surfacing anything.
  const loadMacroPlan = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/stride/macro/current', { credentials: 'include', signal })
      if (!res.ok) {
        if (!signal?.aborted) setMacroPlan(null)
        return
      }
      const data = (await res.json()) as MacroPlanView | null
      if (!signal?.aborted) {
        setMacroPlan(data?.plan ? data : null)
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load macro plan', error)
      if (!signal?.aborted) setMacroPlan(null)
    }
  }, [])

  // Completed weeks for the block's target-vs-actual bars. A failure here only
  // costs the bars, so it surfaces inside the week list rather than as a page
  // error, with a Retry that re-runs this.
  const loadMacroHistory = useCallback(async (signal?: AbortSignal) => {
    setMacroHistoryError(false)
    setMacroHistoryLoading(true)
    try {
      const res = await fetch(`/api/stride/history?limit=${MACRO_ACTUALS_WEEKS}&offset=0`, {
        credentials: 'include',
        signal,
      })
      if (!res.ok) throw new Error(`Failed to load history: ${res.status}`)
      const data = await res.json()
      if (!signal?.aborted) setMacroHistory((data.weeks ?? []) as WeekSummary[])
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load plan history for the macro block', error)
      if (!signal?.aborted) setMacroHistoryError(true)
    } finally {
      if (!signal?.aborted) setMacroHistoryLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    lifetimeRef.current = controller
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    void loadMacroPlan(controller.signal)
    void loadMacroHistory(controller.signal)
    return () => {
      controller.abort()
      if (lifetimeRef.current === controller) lifetimeRef.current = null
      if (macroFallbackTimerRef.current) clearTimeout(macroFallbackTimerRef.current)
    }
  }, [loadMacroPlan, loadMacroHistory])

  const reloadMacroPlan = useCallback(
    () => loadMacroPlan(lifetimeRef.current?.signal),
    [loadMacroPlan],
  )
  const reloadMacroHistory = useCallback(
    () => loadMacroHistory(lifetimeRef.current?.signal),
    [loadMacroHistory],
  )

  // The macro block outcome rides the training stream. Ready re-reads the block
  // whether or not this tab asked for it — another tab, or a reload while the
  // generation ran, still gets the new block without a refresh. Failed shows
  // the server's reason, which names what the athlete can act on (Stride
  // switched off, a race that is not theirs), falling back to the generic
  // message for the action the event names.
  const handleOutcome = useCallback((kind: 'ready' | 'failed', data: unknown) => {
    if (macroFallbackTimerRef.current) {
      clearTimeout(macroFallbackTimerRef.current)
      macroFallbackTimerRef.current = null
    }
    setMacroAction(null)
    if (kind === 'ready') {
      setMacroActionError('')
      void reloadMacroPlan()
      void reloadMacroHistory()
      return
    }
    const payload = (data && typeof data === 'object' ? data : {}) as { action?: string; error?: string }
    const action: MacroAction = payload.action === 'extend' ? 'extend' : 'regenerate'
    setMacroActionError(payload.error || t(`longTermPlan.actions.${action}Error`))
  }, [reloadMacroPlan, reloadMacroHistory, t])

  // Regenerate / Extend the macro block. Both are minutes long — the server
  // budgets a full Claude call — so the button stays busy until the outcome
  // arrives and a failure leaves the block on screen untouched.
  const runMacroAction = useCallback(async (action: MacroAction, path: string) => {
    setMacroActionError('')
    setMacroAction(action)
    try {
      const res = await fetch(path, { method: 'POST', credentials: 'include' })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setMacroActionError(data.error ?? t(`longTermPlan.actions.${action}Error`))
        setMacroAction(null)
        return
      }
      // 202: the block is being generated in the background. The button stays
      // busy until stride_macro_ready / stride_macro_failed arrives on the
      // training stream (handleOutcome). Should the stream miss the event — a
      // reconnect at the wrong moment — the fallback re-reads the block and
      // releases the button instead of spinning forever.
      if (macroFallbackTimerRef.current) clearTimeout(macroFallbackTimerRef.current)
      macroFallbackTimerRef.current = setTimeout(() => {
        macroFallbackTimerRef.current = null
        setMacroAction(null)
        void reloadMacroPlan()
        void reloadMacroHistory()
      }, MACRO_OUTCOME_FALLBACK_MS)
    } catch {
      setMacroActionError(t(`longTermPlan.actions.${action}Error`))
      setMacroAction(null)
    }
  }, [reloadMacroPlan, reloadMacroHistory, t])

  const regenerate = useCallback(() => {
    void runMacroAction('regenerate', '/api/stride/macro/generate')
  }, [runMacroAction])

  const extend = useCallback(() => {
    void runMacroAction('extend', '/api/stride/macro/extend')
  }, [runMacroAction])

  return {
    macroPlan,
    macroHistory,
    macroHistoryLoading,
    macroHistoryError,
    macroAction,
    macroActionError,
    reloadMacroPlan,
    reloadMacroHistory,
    handleOutcome,
    regenerate,
    extend,
  }
}
