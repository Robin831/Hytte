import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Trophy } from 'lucide-react'
import { MathAnswerPad } from '../components/math/MathAnswerPad'
import { appendAnswerDigit } from '../components/math/mathUtils'
import { FinishRank } from '../components/math/FinishRank'

const TOTAL = 200

type Op = '*' | '/'

interface Fact {
  a: number
  b: number
  op: Op
  expected: number
}

interface MarathonBest {
  session_id: number
  duration_ms: number
  total_wrong: number
  total_correct: number
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
}

type Phase = 'idle' | 'starting' | 'playing' | 'finishing' | 'done' | 'error'

function buildAllFacts(): Fact[] {
  const facts: Fact[] = []
  for (let a = 1; a <= 10; a++) {
    for (let b = 1; b <= 10; b++) {
      facts.push({ a, b, op: '*', expected: a * b })
    }
  }
  for (let a = 1; a <= 10; a++) {
    for (let b = 1; b <= 10; b++) {
      const c = a * b
      facts.push({ a: c, b, op: '/', expected: a })
    }
  }
  return facts
}

function shuffle<T>(input: T[]): T[] {
  const arr = input.slice()
  for (let i = arr.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[arr[i], arr[j]] = [arr[j], arr[i]]
  }
  return arr
}

function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, ms) / 1000
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds - minutes * 60
  const mm = String(minutes).padStart(2, '0')
  const ss = seconds.toFixed(1).padStart(4, '0')
  return `${mm}:${ss}`
}

function renderProblem(fact: Fact): string {
  const op = fact.op === '*' ? '×' : '÷'
  return `${fact.a} ${op} ${fact.b} = ?`
}

export default function MathMarathon() {
  const { t } = useTranslation('regnemester')

  const [phase, setPhase] = useState<Phase>('idle')
  const [error, setError] = useState('')
  const [sessionId, setSessionId] = useState<number | null>(null)
  const [facts, setFacts] = useState<Fact[]>(() => shuffle(buildAllFacts()))
  const [index, setIndex] = useState(0)
  const [wrongCount, setWrongCount] = useState(0)
  const [input, setInput] = useState('')
  const [summary, setSummary] = useState<FinishSummary | null>(null)
  const [priorBest, setPriorBest] = useState<MarathonBest | null>(null)
  const [elapsed, setElapsed] = useState(0)
  const [submitting, setSubmitting] = useState(false)

  // Refs to keep wall-clock bookkeeping out of React state churn.
  const startedAtRef = useRef<number>(0)
  const questionShownAtRef = useRef<number>(0)

  const currentFact = facts[index]

  // Fetch the user's prior PB once on mount so the result screen can decide
  // whether to award a "New PB!" badge.
  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/math/marathon/best', { credentials: 'include', signal: controller.signal })
      .then(res => (res.ok ? res.json() : { best: null }))
      .then((data: { best: MarathonBest | null }) => {
        setPriorBest(data.best ?? null)
      })
      .catch(() => { /* PB lookup is non-critical; treat as no prior */ })
    return () => { controller.abort() }
  }, [])

  // Live-elapsed timer: tick from the wall-clock start, not by accumulating
  // intervals — so a backgrounded tab still reports the correct duration.
  // Keep ticking through 'finishing' so the display doesn't freeze early.
  useEffect(() => {
    if (phase !== 'playing' && phase !== 'finishing') return
    const tick = () => setElapsed(performance.now() - startedAtRef.current)
    const id = window.setInterval(tick, 100)
    return () => window.clearInterval(id)
  }, [phase])

  const startGame = useCallback(async () => {
    setError('')
    setPhase('starting')
    // Start wall clock before the server request so the displayed elapsed
    // time accounts for start-request latency and matches duration_ms.
    startedAtRef.current = performance.now()
    try {
      const res = await fetch('/api/math/sessions', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: 'marathon' }),
      })
      if (!res.ok) throw new Error(t('errors.failedToStart'))
      const data = await res.json()
      setSessionId(data.session_id)
      setFacts(shuffle(buildAllFacts()))
      questionShownAtRef.current = performance.now()
      setIndex(0)
      setWrongCount(0)
      setInput('')
      setElapsed(0)
      setPhase('playing')
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToStart')
      setError(message)
      setPhase('error')
    }
  }, [t, setFacts])

  const finishGame = useCallback(async (id: number) => {
    setPhase('finishing')
    try {
      const res = await fetch(`/api/math/sessions/${id}/finish`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(t('errors.failedToFinish'))
      const data = await res.json()
      // Trust the server's duration_ms and total_wrong: those are what get
      // stored and what future PB lookups compare against. Sync elapsed so
      // there's no visible jump between the running timer and the result.
      const s = data.summary as FinishSummary
      setSummary(s)
      setElapsed(s.duration_ms)
      setPhase('done')
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToFinish')
      setError(message)
      setPhase('error')
    }
  }, [t])

  const submitAnswer = useCallback(async () => {
    if (phase !== 'playing' || submitting) return
    if (sessionId == null) return
    if (input.length === 0) return
    const fact = facts[index]
    const userAnswer = parseInt(input, 10)
    if (Number.isNaN(userAnswer)) return
    const responseMs = Math.max(0, Math.round(performance.now() - questionShownAtRef.current))
    const isCorrect = userAnswer === fact.expected
    const nextWrong = isCorrect ? wrongCount : wrongCount + 1

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
        // Treat a transient attempt failure as fatal to the run rather
        // than letting the score silently desync from the server.
        throw new Error(t('errors.failedToRecord'))
      }
      // Don't wait for the response body to advance — it only contains
      // the next random question, which marathon ignores.
      void res.json().catch(() => null)

      if (!isCorrect) setWrongCount(nextWrong)
      const nextIndex = index + 1
      setInput('')
      if (nextIndex >= facts.length) {
        await finishGame(sessionId)
      } else {
        setIndex(nextIndex)
        questionShownAtRef.current = performance.now()
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToRecord')
      setError(message)
      setPhase('error')
    } finally {
      setSubmitting(false)
    }
  }, [phase, submitting, sessionId, input, index, facts, wrongCount, t, finishGame])

  const appendDigit = useCallback((digit: string) => {
    if (phase !== 'playing' || submitting) return
    setInput(prev => appendAnswerDigit(prev, digit))
  }, [phase, submitting])

  const backspace = useCallback(() => {
    if (phase !== 'playing' || submitting) return
    setInput(prev => prev.slice(0, -1))
  }, [phase, submitting])

  const handleSubmit = useCallback(() => { void submitAnswer() }, [submitAnswer])

  // Warn before navigating away from a run in progress so a stray back-
  // tap or refresh doesn't silently lose 5 minutes of grinding.
  useEffect(() => {
    if (phase !== 'playing') return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [phase])

  const isNewPB = useMemo(() => {
    if (!summary) return false
    if (!priorBest) return true
    if (summary.duration_ms < priorBest.duration_ms) return true
    if (summary.duration_ms === priorBest.duration_ms && summary.total_wrong < priorBest.total_wrong) return true
    return false
  }, [summary, priorBest])

  if (phase === 'idle' || phase === 'starting' || phase === 'error') {
    return (
      <div className="max-w-2xl mx-auto p-4 sm:p-6">
        <Link to="/math" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4">
          <ArrowLeft size={16} />
          {t('back')}
        </Link>
        <h1 className="text-2xl sm:text-3xl font-bold text-white mb-2">{t('marathon.title')}</h1>
        <p className="text-gray-400 mb-6">{t('marathon.intro', { count: TOTAL })}</p>
        {priorBest && (
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4 mb-6 flex items-center gap-3">
            <Trophy size={20} className="text-yellow-400 shrink-0" />
            <div>
              <div className="text-sm text-gray-400">{t('marathon.priorBestLabel')}</div>
              <div className="text-lg font-semibold text-white tabular-nums">
                {formatDuration(priorBest.duration_ms)}
                <span className="text-sm font-normal text-gray-400 ml-2">
                  {t('marathon.wrongCount', { count: priorBest.total_wrong })}
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
          {phase === 'starting' ? t('marathon.starting') : t('marathon.start')}
        </button>
      </div>
    )
  }

  if (phase === 'done' && summary) {
    return (
      <div className="max-w-2xl mx-auto p-4 sm:p-6">
        <h1 className="text-2xl sm:text-3xl font-bold text-white mb-2">{t('marathon.resultTitle')}</h1>
        <p className="text-gray-400 mb-6">{t('marathon.resultSubtitle')}</p>

        {isNewPB && (
          <div className="mb-6 rounded-lg border border-yellow-400/40 bg-yellow-400/10 px-4 py-3 flex items-center gap-3">
            <Trophy size={24} className="text-yellow-400 shrink-0" />
            <div className="font-semibold text-yellow-300">{t('marathon.newPB')}</div>
          </div>
        )}

        <div className="grid grid-cols-2 gap-3 sm:gap-4 mb-6">
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
            <div className="text-xs uppercase tracking-wide text-gray-400 mb-1">{t('marathon.timeLabel')}</div>
            <div className="text-3xl sm:text-4xl font-bold text-white tabular-nums">
              {formatDuration(summary.duration_ms)}
            </div>
          </div>
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
            <div className="text-xs uppercase tracking-wide text-gray-400 mb-1">{t('marathon.wrongLabel')}</div>
            <div className="text-3xl sm:text-4xl font-bold text-white tabular-nums">{summary.total_wrong}</div>
            <div className="text-xs text-gray-500 mt-1">
              {t('marathon.outOf', { total: TOTAL })}
            </div>
          </div>
        </div>

        {priorBest && !isNewPB && (
          <div className="mb-6 text-sm text-gray-400">
            {t('marathon.priorBestRecap', {
              time: formatDuration(priorBest.duration_ms),
              wrong: priorBest.total_wrong,
            })}
          </div>
        )}

        <div className="mb-6">
          <FinishRank mode="marathon" sessionId={summary.session_id} />
        </div>

        <div className="flex flex-col sm:flex-row gap-3">
          <button
            type="button"
            onClick={() => {
              setSummary(null)
              setSessionId(null)
              setPhase('idle')
              setIndex(0)
              setWrongCount(0)
              setInput('')
              setElapsed(0)
              // Refresh prior best so a back-to-back run compares against
              // the run we just stored.
              setPriorBest(prev => {
                if (!summary) return prev
                if (!prev) {
                  return {
                    session_id: summary.session_id,
                    duration_ms: summary.duration_ms,
                    total_wrong: summary.total_wrong,
                    total_correct: summary.total_correct,
                    ended_at: summary.ended_at,
                  }
                }
                if (
                  summary.duration_ms < prev.duration_ms ||
                  (summary.duration_ms === prev.duration_ms && summary.total_wrong < prev.total_wrong)
                ) {
                  return {
                    session_id: summary.session_id,
                    duration_ms: summary.duration_ms,
                    total_wrong: summary.total_wrong,
                    total_correct: summary.total_correct,
                    ended_at: summary.ended_at,
                  }
                }
                return prev
              })
            }}
            className="px-5 py-3 rounded-lg bg-blue-600 hover:bg-blue-500 active:bg-blue-700 text-white font-semibold"
          >
            {t('marathon.playAgain')}
          </button>
          <Link
            to="/math"
            className="px-5 py-3 rounded-lg border border-gray-700 hover:border-gray-500 text-gray-300 hover:text-white font-medium text-center"
          >
            {t('marathon.backToModes')}
          </Link>
        </div>
      </div>
    )
  }

  // playing or finishing — render the play surface.
  const progressLabel = `${index + 1} / ${TOTAL}`
  const isFinishing = phase === 'finishing'

  return (
    <div className="min-h-[calc(100vh-3.5rem)] md:min-h-screen flex flex-col max-w-3xl mx-auto p-3 sm:p-6">
      <div className="flex items-center justify-between mb-4 sm:mb-6">
        <div className="text-sm sm:text-base text-gray-400 tabular-nums">
          <span className="text-white font-semibold">{progressLabel}</span>
        </div>
        <div className="text-sm sm:text-base text-gray-400 tabular-nums">
          <span className="text-white font-semibold">{formatDuration(elapsed)}</span>
        </div>
        <div className="text-sm sm:text-base text-gray-400 tabular-nums">
          {t('marathon.wrongShort')} <span className="text-white font-semibold">{wrongCount}</span>
        </div>
      </div>

      <div className="flex-1 flex flex-col items-center justify-center mb-6">
        <div className="text-4xl sm:text-6xl md:text-7xl font-bold text-white text-center tabular-nums">
          {currentFact ? renderProblem(currentFact) : ''}
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
