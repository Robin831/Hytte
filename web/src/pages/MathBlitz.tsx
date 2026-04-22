import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Trophy } from 'lucide-react'
import { MathAnswerPad } from '../components/math/MathAnswerPad'
import { appendAnswerDigit } from '../components/math/mathUtils'
import { FinishRank } from '../components/math/FinishRank'
import { UnlockedAchievementsBanner, type UnlockedAchievement } from '../components/math/UnlockedAchievements'
import { MuteToggle } from '../components/math/MuteToggle'
import { SpeedCallout, FAST_THRESHOLD_MS } from '../components/regnemester/SpeedCallout'
import { useFeedback } from '../lib/regnemester/feedback'
import { burst, blitzIntensityForScore } from '../lib/regnemester/confetti'

const DURATION_MS = 60_000
const STREAK_MILESTONE_STEP = 5
const STREAK_POP_CLASS = 'regnemester-streak-pop'
const STREAK_POP_MS = 450

type Op = '*' | '/'

interface Fact {
  a: number
  b: number
  op: Op
  expected: number
}

interface BlitzBest {
  session_id: number
  score_num: number
  best_streak: number
  total_correct: number
  total_wrong: number
  ended_at: string
}

interface FinishSummary {
  session_id: number
  mode: string
  started_at: string
  ended_at: string
  duration_ms: number
  total_correct: number
  total_wrong: number
  score_num: number
  best_streak: number
}

interface FinishResponse {
  summary: FinishSummary
  unlocked_achievements?: UnlockedAchievement[]
  leaderboard_rank?: number | null
}

type Phase = 'idle' | 'starting' | 'playing' | 'finishing' | 'done' | 'error'

// Mirrors ComputeBlitzPoints in internal/math/session.go so the live score
// displayed during play matches the server-computed score stored in Finish.
function computeBlitzPoints(responseMs: number, streakBefore: number): number {
  const safeStreak = Math.max(0, streakBefore)
  let speedBonus: number
  if (responseMs < 1000) speedBonus = 1.5
  else if (responseMs < 2000) speedBonus = 1.2
  else speedBonus = 1.0
  const streakMult = Math.min(3.0, 1.0 + safeStreak / 10)
  return Math.round(speedBonus * streakMult)
}

function renderProblem(fact: Fact): string {
  const op = fact.op === '*' ? '×' : '÷'
  return `${fact.a} ${op} ${fact.b} = ?`
}

function formatSeconds(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000))
  return `${s}s`
}

export default function MathBlitz() {
  const { t } = useTranslation('regnemester')
  const feedback = useFeedback()
  const problemRef = useRef<HTMLDivElement | null>(null)
  const streakBadgeRef = useRef<HTMLSpanElement | null>(null)
  const streakPopTimerRef = useRef<number | null>(null)

  const [phase, setPhase] = useState<Phase>('idle')
  const [error, setError] = useState('')
  const [sessionId, setSessionId] = useState<number | null>(null)
  const [currentFact, setCurrentFact] = useState<Fact | null>(null)
  const [score, setScore] = useState(0)
  const [streak, setStreak] = useState(0)
  const [speedMs, setSpeedMs] = useState<number | null>(null)
  const [speedKey, setSpeedKey] = useState(0)
  const [input, setInput] = useState('')
  const [summary, setSummary] = useState<FinishSummary | null>(null)
  const [priorBest, setPriorBest] = useState<BlitzBest | null>(null)
  const [timeLeftMs, setTimeLeftMs] = useState(DURATION_MS)
  const [submitting, setSubmitting] = useState(false)
  const [unlocked, setUnlocked] = useState<UnlockedAchievement[]>([])

  // Wall-clock anchors; kept in refs so timer updates don't thrash React state.
  const endAtRef = useRef<number>(0)
  const questionShownAtRef = useRef<number>(0)
  // phaseRef mirrors phase so async callers (the attempt POST) can tell
  // whether the run still owns the UI by the time their request lands —
  // if Finish ran while we were in flight, we must not clobber 'done'
  // with an 'error' from a late network failure.
  const phaseRef = useRef<Phase>('idle')
  useEffect(() => { phaseRef.current = phase }, [phase])

  // Fetch the user's prior PB once on mount so the result screen can show
  // a New PB badge when beaten.
  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/math/blitz/best', { credentials: 'include', signal: controller.signal })
      .then(res => (res.ok ? res.json() : { best: null }))
      .then((data: { best: BlitzBest | null }) => {
        setPriorBest(data.best ?? null)
      })
      .catch(() => { /* PB lookup is non-critical */ })
    return () => { controller.abort() }
  }, [])

  const finishGame = useCallback(async (id: number) => {
    setPhase('finishing')
    try {
      const res = await fetch(`/api/math/sessions/${id}/finish`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(t('errors.failedToFinish'))
      const data = await res.json() as FinishResponse
      const s = data.summary
      setSummary(s)
      // Trust the server's score for the result banner — the client tally
      // should already match, but the stored value is what future PB lookups
      // compare against.
      setScore(s.score_num)
      setTimeLeftMs(0)
      setUnlocked(data.unlocked_achievements ?? [])
      setPhase('done')
      const beatPB = priorBest ? s.score_num > priorBest.score_num : s.score_num > 0
      feedback.play(beatPB ? 'milestone' : 'fanfare')
      // Confetti intensity scales with score; #1 all-time leaderboard rank
      // earns the full-screen burst regardless of absolute score.
      if (s.score_num > 0) {
        const isTopRank = data.leaderboard_rank === 1
        if (isTopRank) {
          burst('full', 'rainbow')
        } else {
          burst(blitzIntensityForScore(s.score_num), beatPB ? 'golden' : 'default')
        }
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToFinish')
      setError(message)
      setPhase('error')
    }
  }, [t, feedback, priorBest])

  // Countdown timer: ticks off the wall-clock deadline so a backgrounded tab
  // still finishes at the right moment when it returns to the foreground.
  useEffect(() => {
    if (phase !== 'playing') return
    let id: number | null = null
    const tick = () => {
      const remaining = endAtRef.current - performance.now()
      if (remaining <= 0) {
        setTimeLeftMs(0)
        if (id !== null) window.clearInterval(id)
        if (sessionId != null) void finishGame(sessionId)
        return
      }
      setTimeLeftMs(remaining)
    }
    id = window.setInterval(tick, 100)
    tick()
    return () => {
      if (id !== null) window.clearInterval(id)
    }
  }, [phase, sessionId, finishGame])

  const startGame = useCallback(async () => {
    setError('')
    setPhase('starting')
    try {
      const res = await fetch('/api/math/sessions', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: 'blitz' }),
      })
      if (!res.ok) throw new Error(t('errors.failedToStart'))
      const data = await res.json()
      setSessionId(data.session_id)
      setCurrentFact(data.first_question as Fact)
      setScore(0)
      setStreak(0)
      setSpeedMs(null)
      setSpeedKey(0)
      setInput('')
      setSummary(null)
      // Anchor the countdown to "now" rather than the server-request start —
      // the player shouldn't lose seconds to network latency.
      const now = performance.now()
      endAtRef.current = now + DURATION_MS
      questionShownAtRef.current = now
      setTimeLeftMs(DURATION_MS)
      setPhase('playing')
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToStart')
      setError(message)
      setPhase('error')
    }
  }, [t])

  const submitAnswer = useCallback(async () => {
    if (phase !== 'playing' || submitting) return
    if (sessionId == null || currentFact == null) return
    if (input.length === 0) return
    const userAnswer = parseInt(input, 10)
    if (Number.isNaN(userAnswer)) return
    const now = performance.now()
    const responseMs = Math.max(0, Math.round(now - questionShownAtRef.current))
    const fact = currentFact

    setSubmitting(true)
    try {
      const res = await fetch(`/api/math/sessions/${sessionId}/attempts`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          a: fact.a,
          b: fact.b,
          op: fact.op,
          user_answer: userAnswer,
          response_ms: responseMs,
        }),
      })
      if (!res.ok) {
        if (res.status === 409) {
          // The session expired server-side while this request was in flight.
          // Immediately sync the UI to the server-authoritative finished state.
          setInput('')
          setCurrentFact(null)
          endAtRef.current = performance.now()
          if (phaseRef.current === 'playing') {
            void finishGame(sessionId)
          }
          return
        }
        throw new Error(t('errors.failedToRecord'))
      }
      const data = await res.json() as { is_correct: boolean; next_question: Fact | null }

      if (data.is_correct) {
        const pointsEarned = computeBlitzPoints(responseMs, streak)
        const nextStreak = streak + 1
        setScore(prev => prev + pointsEarned)
        setStreak(nextStreak)
        feedback.play('correct')
        feedback.vibrateCorrect()
        feedback.flashCorrect(problemRef.current)
        if (responseMs < FAST_THRESHOLD_MS) {
          setSpeedMs(responseMs)
          setSpeedKey(prev => prev + 1)
        }
        // Streak milestone: every 5 correct in a row gets a small confetti
        // burst and a scale-pop on the streak badge.
        if (nextStreak > 0 && nextStreak % STREAK_MILESTONE_STEP === 0) {
          burst('small')
          const badge = streakBadgeRef.current
          if (badge) {
            if (streakPopTimerRef.current !== null) {
              window.clearTimeout(streakPopTimerRef.current)
              badge.classList.remove(STREAK_POP_CLASS)
              // Force reflow so the animation restarts cleanly on back-to-back milestones.
              void badge.offsetWidth
            }
            badge.classList.add(STREAK_POP_CLASS)
            streakPopTimerRef.current = window.setTimeout(() => {
              badge.classList.remove(STREAK_POP_CLASS)
              streakPopTimerRef.current = null
            }, STREAK_POP_MS)
          }
        }
      } else {
        setStreak(0)
        feedback.play('wrong')
        feedback.vibrateWrong()
        feedback.flashWrong(problemRef.current)
      }
      setInput('')
      // Use the server-supplied next question so the draw is authoritative.
      if (data.next_question) {
        setCurrentFact(data.next_question)
      }
      questionShownAtRef.current = performance.now()
    } catch (err) {
      // Don't override 'finishing' or 'done' with an error from a late
      // attempt POST — the timer already finished the run cleanly.
      if (phaseRef.current === 'playing') {
        const message = err instanceof Error ? err.message : t('errors.failedToRecord')
        setError(message)
        setPhase('error')
      }
    } finally {
      setSubmitting(false)
    }
  }, [phase, submitting, sessionId, currentFact, input, streak, t, finishGame, feedback])

  const appendDigit = useCallback((digit: string) => {
    if (phase !== 'playing' || submitting) return
    setInput(prev => appendAnswerDigit(prev, digit))
  }, [phase, submitting])

  const backspace = useCallback(() => {
    if (phase !== 'playing' || submitting) return
    setInput(prev => prev.slice(0, -1))
  }, [phase, submitting])

  const handleSubmit = useCallback(() => { void submitAnswer() }, [submitAnswer])

  // Warn before navigating away from a Blitz in progress.
  useEffect(() => {
    if (phase !== 'playing') return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [phase])

  // Cancel any lingering streak-pop timer when the component unmounts so we
  // don't touch a detached element after it's gone.
  useEffect(() => {
    return () => {
      if (streakPopTimerRef.current !== null) {
        window.clearTimeout(streakPopTimerRef.current)
        streakPopTimerRef.current = null
      }
    }
  }, [])

  const isNewPB = useMemo(() => {
    if (!summary) return false
    if (summary.score_num === 0) return false
    if (!priorBest) return true
    return summary.score_num > priorBest.score_num
  }, [summary, priorBest])

  if (phase === 'idle' || phase === 'starting' || phase === 'error') {
    return (
      <div className="max-w-2xl mx-auto p-4 sm:p-6">
        <Link to="/math" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4">
          <ArrowLeft size={16} />
          {t('back')}
        </Link>
        <h1 className="text-2xl sm:text-3xl font-bold text-white mb-2">{t('blitz.title')}</h1>
        <p className="text-gray-400 mb-6">{t('blitz.intro')}</p>
        {priorBest && (
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4 mb-6 flex items-center gap-3">
            <Trophy size={20} className="text-yellow-400 shrink-0" />
            <div>
              <div className="text-sm text-gray-400">{t('blitz.priorBestLabel')}</div>
              <div className="text-lg font-semibold text-white tabular-nums">
                {priorBest.score_num}
                <span className="text-sm font-normal text-gray-400 ml-2">
                  {t('blitz.priorBestRecap', { streak: priorBest.best_streak })}
                </span>
              </div>
            </div>
          </div>
        )}
        {error && (
          <div className="mb-4 rounded border border-red-500/50 bg-red-500/10 px-3 py-2 text-sm text-red-300">
            {error}
          </div>
        )}
        <button
          type="button"
          onClick={() => { void startGame() }}
          disabled={phase === 'starting'}
          className="w-full sm:w-auto px-6 py-3 rounded-lg bg-blue-600 hover:bg-blue-500 active:bg-blue-700 text-white font-semibold disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {phase === 'starting' ? t('blitz.starting') : t('blitz.start')}
        </button>
      </div>
    )
  }

  if (phase === 'done' && summary) {
    return (
      <div className="max-w-2xl mx-auto p-4 sm:p-6">
        <h1 className="text-2xl sm:text-3xl font-bold text-white mb-2">{t('blitz.resultTitle')}</h1>
        <p className="text-gray-400 mb-6">{t('blitz.resultSubtitle')}</p>

        {isNewPB && (
          <div className="mb-6 rounded-lg border border-yellow-400/40 bg-yellow-400/10 px-4 py-3 flex items-center gap-3">
            <Trophy size={24} className="text-yellow-400 shrink-0" />
            <div className="font-semibold text-yellow-300">{t('blitz.newPB')}</div>
          </div>
        )}

        <UnlockedAchievementsBanner items={unlocked} />

        <div className="grid grid-cols-2 gap-3 sm:gap-4 mb-6">
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
            <div className="text-xs uppercase tracking-wide text-gray-400 mb-1">{t('blitz.scoreLabel')}</div>
            <div className="text-3xl sm:text-4xl font-bold text-white tabular-nums">{summary.score_num}</div>
          </div>
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
            <div className="text-xs uppercase tracking-wide text-gray-400 mb-1">{t('blitz.bestStreakLabel')}</div>
            <div className="text-3xl sm:text-4xl font-bold text-white tabular-nums">{summary.best_streak}</div>
          </div>
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
            <div className="text-xs uppercase tracking-wide text-gray-400 mb-1">{t('blitz.correctLabel')}</div>
            <div className="text-3xl sm:text-4xl font-bold text-green-400 tabular-nums">{summary.total_correct}</div>
          </div>
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
            <div className="text-xs uppercase tracking-wide text-gray-400 mb-1">{t('blitz.wrongLabel')}</div>
            <div className="text-3xl sm:text-4xl font-bold text-red-400 tabular-nums">{summary.total_wrong}</div>
          </div>
        </div>

        {priorBest && !isNewPB && (
          <div className="mb-6 text-sm text-gray-400">
            {t('blitz.priorBestDetailedRecap', {
              score: priorBest.score_num,
              streak: priorBest.best_streak,
            })}
          </div>
        )}

        <div className="mb-6">
          <FinishRank mode="blitz" sessionId={summary.session_id} />
        </div>

        <div className="flex flex-col sm:flex-row gap-3">
          <button
            type="button"
            onClick={() => {
              const latest = summary
              setSummary(null)
              setSessionId(null)
              setCurrentFact(null)
              setScore(0)
              setStreak(0)
              setSpeedMs(null)
              setSpeedKey(0)
              setInput('')
              setTimeLeftMs(DURATION_MS)
              setUnlocked([])
              setPhase('idle')
              // Refresh prior best so a back-to-back run compares against
              // the run we just stored.
              setPriorBest(prev => {
                if (!latest) return prev
                const candidate: BlitzBest = {
                  session_id: latest.session_id,
                  score_num: latest.score_num,
                  best_streak: latest.best_streak,
                  total_correct: latest.total_correct,
                  total_wrong: latest.total_wrong,
                  ended_at: latest.ended_at,
                }
                if (!prev) return candidate
                if (latest.score_num > prev.score_num) return candidate
                return prev
              })
            }}
            className="px-5 py-3 rounded-lg bg-blue-600 hover:bg-blue-500 active:bg-blue-700 text-white font-semibold"
          >
            {t('blitz.playAgain')}
          </button>
          <Link
            to="/math"
            className="px-5 py-3 rounded-lg border border-gray-700 hover:border-gray-500 text-gray-300 hover:text-white font-medium text-center"
          >
            {t('blitz.backToModes')}
          </Link>
        </div>
      </div>
    )
  }

  // playing or finishing — render the play surface.
  const isFinishing = phase === 'finishing'
  const lowTime = timeLeftMs <= 10_000

  return (
    <div className="min-h-[calc(100vh-3.5rem)] md:min-h-screen flex flex-col max-w-3xl mx-auto p-3 sm:p-6">
      <div className="flex items-center justify-between mb-4 sm:mb-6 gap-2 flex-wrap">
        <div className="text-sm sm:text-base text-gray-400 tabular-nums">
          {t('blitz.scoreShort')} <span className="text-white font-semibold">{score}</span>
        </div>
        <div
          className={`text-2xl sm:text-3xl font-bold tabular-nums ${lowTime ? 'text-red-400' : 'text-white'}`}
          aria-live="polite"
          aria-label={t('blitz.timeLeftAria')}
        >
          {formatSeconds(timeLeftMs)}
        </div>
        <div className="flex items-center gap-3">
          <div className="text-sm sm:text-base text-gray-400 tabular-nums">
            {t('blitz.streakShort')}{' '}
            <span ref={streakBadgeRef} className="text-white font-semibold">{streak}</span>
          </div>
          <MuteToggle muted={feedback.muted} onToggle={feedback.toggleMute} />
        </div>
      </div>

      <div className="flex-1 flex flex-col items-center justify-center mb-6">
        {/* Reserved vertical space above the problem so the SpeedCallout has room
            to animate upward without clipping the header, and so the problem
            sits at the same y-position as before the callout was introduced. */}
        <div className="h-8" aria-hidden="true" />
        <div className="relative">
          <SpeedCallout key={speedKey} responseMs={speedKey > 0 ? speedMs : null} />
          <div
            ref={problemRef}
            className="text-4xl sm:text-6xl md:text-7xl font-bold text-white text-center tabular-nums rounded-lg px-4 py-2"
          >
            {currentFact ? renderProblem(currentFact) : ''}
          </div>
        </div>
      </div>

      <MathAnswerPad
        input={input}
        onDigit={appendDigit}
        onBackspace={backspace}
        onSubmit={handleSubmit}
        disabled={isFinishing}
        busy={isFinishing}
      />
    </div>
  )
}
