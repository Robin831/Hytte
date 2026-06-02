import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Target } from 'lucide-react'
import { MathAnswerPad } from '../components/math/MathAnswerPad'
import { appendAnswerDigit } from '../components/math/mathUtils'
import { MuteToggle } from '../components/math/MuteToggle'
import { useFeedback } from '../lib/regnemester/feedback'

// Operation the user wants to drill. Maps directly to a backend session mode
// (mult / div / mixed) — all valid modes the engine already accepts.
type Operation = 'mult' | 'div' | 'mixed'
// Which set of facts to practise: the heatmap's weakest cells, or a single
// fact the user dials in by hand.
type Selection = 'weakest' | 'specific'
type Op = '*' | '/'

interface Fact {
  a: number
  b: number
  op: Op
  expected: number
}

type Level = 'unseen' | 'red' | 'yellow' | 'green'

interface StatsCell {
  a: number
  b: number
  op: Op
  level: Level
}

interface StatsResponse {
  multiplication: StatsCell[][]
  division: StatsCell[][]
}

type Phase = 'setup' | 'starting' | 'playing' | 'finishing' | 'summary' | 'error'

function shuffle<T>(input: T[]): T[] {
  const arr = input.slice()
  for (let i = arr.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[arr[i], arr[j]] = [arr[j], arr[i]]
  }
  return arr
}

// Convert a heatmap cell into a drillable fact. Multiplication cells store the
// factors directly; division cells store the quotient (a) and divisor (b), so
// the rendered problem is (a*b) ÷ b = a.
function cellToFact(cell: StatsCell): Fact {
  if (cell.op === '*') return { a: cell.a, b: cell.b, op: '*', expected: cell.a * cell.b }
  return { a: cell.a * cell.b, b: cell.b, op: '/', expected: cell.a }
}

function buildAllFacts(operation: Operation): Fact[] {
  const facts: Fact[] = []
  if (operation === 'mult' || operation === 'mixed') {
    for (let a = 1; a <= 10; a++) {
      for (let b = 1; b <= 10; b++) {
        facts.push({ a, b, op: '*', expected: a * b })
      }
    }
  }
  if (operation === 'div' || operation === 'mixed') {
    for (let a = 1; a <= 10; a++) {
      for (let b = 1; b <= 10; b++) {
        const c = a * b
        facts.push({ a: c, b, op: '/', expected: a })
      }
    }
  }
  return facts
}

// Pull the weakest facts for an operation out of the stats grids: red cells
// (needs work) first, then yellow (getting there). Green and unseen cells are
// left out so practice targets the facts the heatmap is worried about.
function weakestFacts(operation: Operation, stats: StatsResponse): Fact[] {
  const grids: StatsCell[][][] = []
  if (operation === 'mult' || operation === 'mixed') grids.push(stats.multiplication)
  if (operation === 'div' || operation === 'mixed') grids.push(stats.division)
  const red: Fact[] = []
  const yellow: Fact[] = []
  for (const grid of grids) {
    for (const row of grid) {
      for (const cell of row) {
        if (cell.level === 'red') red.push(cellToFact(cell))
        else if (cell.level === 'yellow') yellow.push(cellToFact(cell))
      }
    }
  }
  return [...red, ...yellow]
}

function renderProblem(fact: Fact): string {
  const op = fact.op === '*' ? '×' : '÷'
  return `${fact.a} ${op} ${fact.b} = ?`
}

const OPERATIONS: Operation[] = ['mult', 'div', 'mixed']

export default function MathPractice() {
  const { t } = useTranslation('regnemester')
  const feedback = useFeedback()
  const problemRef = useRef<HTMLDivElement | null>(null)
  const questionShownAtRef = useRef<number>(0)

  // Setup choices.
  const [operation, setOperation] = useState<Operation>('mult')
  const [selection, setSelection] = useState<Selection>('weakest')
  const [factorA, setFactorA] = useState(1)
  const [factorB, setFactorB] = useState(1)

  // Loaded heatmap stats, used to derive the weakest-facts set.
  const [stats, setStats] = useState<StatsResponse | null>(null)
  const [statsLoading, setStatsLoading] = useState(true)

  // Play state.
  const [phase, setPhase] = useState<Phase>('setup')
  const [error, setError] = useState('')
  const [sessionId, setSessionId] = useState<number | null>(null)
  const [facts, setFacts] = useState<Fact[]>([])
  const [index, setIndex] = useState(0)
  const [input, setInput] = useState('')
  const [answered, setAnswered] = useState(0)
  const [correct, setCorrect] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  // Set when a weakest-facts run found nothing to target and fell back to the
  // full fact set, so the play surface can show a friendly note.
  const [usedFallback, setUsedFallback] = useState(false)
  // The most recent answer's outcome, shown inline as gentle feedback. Cleared
  // when the next question is shown.
  const [lastResult, setLastResult] = useState<{ correct: boolean; expected: number } | null>(null)

  const currentFact = facts[index]

  // Fetch the user's mastery grid once so we can size and build the weakest
  // set. Non-critical: if it fails, weakest practice just falls back to all
  // facts at start time.
  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/math/stats', { credentials: 'include', signal: controller.signal })
      .then(res => (res.ok ? res.json() : Promise.reject(new Error('stats fetch failed'))))
      .then((data: StatsResponse) => {
        if (controller.signal.aborted) return
        setStats(data)
        setStatsLoading(false)
      })
      .catch(err => {
        if (controller.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
        setStatsLoading(false)
      })
    return () => { controller.abort() }
  }, [])

  // How many weak facts the current operation has — drives the setup hint and
  // the empty-state fallback message.
  const weakCount = useMemo(() => {
    if (!stats) return 0
    return weakestFacts(operation, stats).length
  }, [stats, operation])

  const startPractice = useCallback(async () => {
    setError('')
    setPhase('starting')

    let pool: Fact[]
    let fallback = false
    if (selection === 'specific') {
      // A single fact needs a concrete operation; "mixed" picks multiplication.
      const op: Op = operation === 'div' ? '/' : '*'
      const fact: Fact = op === '*'
        ? { a: factorA, b: factorB, op: '*', expected: factorA * factorB }
        : { a: factorA * factorB, b: factorB, op: '/', expected: factorA }
      pool = [fact]
    } else {
      const weak = stats ? weakestFacts(operation, stats) : []
      if (weak.length > 0) {
        pool = weak
      } else {
        pool = buildAllFacts(operation)
        fallback = true
      }
    }

    try {
      const res = await fetch('/api/math/sessions', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: operation }),
      })
      if (!res.ok) throw new Error(t('errors.failedToStart'))
      const data = await res.json()
      setSessionId(data.session_id)
      setFacts(shuffle(pool))
      setIndex(0)
      setInput('')
      setAnswered(0)
      setCorrect(0)
      setLastResult(null)
      setUsedFallback(fallback)
      questionShownAtRef.current = performance.now()
      setPhase('playing')
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToStart')
      setError(message)
      setPhase('error')
    }
  }, [selection, operation, factorA, factorB, stats, t])

  const finishPractice = useCallback(async () => {
    if (sessionId == null) {
      setPhase('summary')
      return
    }
    setPhase('finishing')
    try {
      const res = await fetch(`/api/math/sessions/${sessionId}/finish`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(t('errors.failedToFinish'))
      // The summary is computed client-side from the running counters, so we
      // don't need anything from the response body — just drain it.
      void res.json().catch(() => null)
      setPhase('summary')
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToFinish')
      setError(message)
      setPhase('error')
    }
  }, [sessionId, t])

  const submitAnswer = useCallback(async () => {
    if (phase !== 'playing' || submitting) return
    if (sessionId == null || !currentFact) return
    if (input.length === 0) return
    const userAnswer = parseInt(input, 10)
    if (Number.isNaN(userAnswer)) return
    const responseMs = Math.max(0, Math.round(performance.now() - questionShownAtRef.current))
    const isCorrect = userAnswer === currentFact.expected

    setSubmitting(true)
    try {
      const res = await fetch(`/api/math/sessions/${sessionId}/attempts`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          a: currentFact.a,
          b: currentFact.b,
          op: currentFact.op,
          user_answer: userAnswer,
          response_ms: responseMs,
        }),
      })
      if (!res.ok) throw new Error(t('errors.failedToRecord'))
      void res.json().catch(() => null)

      setAnswered(prev => prev + 1)
      if (isCorrect) {
        setCorrect(prev => prev + 1)
        feedback.play('correct')
        feedback.vibrateCorrect()
        feedback.flashCorrect(problemRef.current)
      } else {
        feedback.play('wrong')
        feedback.vibrateWrong()
        feedback.flashWrong(problemRef.current)
      }
      setLastResult({ correct: isCorrect, expected: currentFact.expected })

      // Loop through the (shuffled) facts indefinitely — practice has no fixed
      // length and ends only when the user chooses to. Reshuffle on wrap so a
      // long session doesn't repeat the exact same order.
      const nextIndex = index + 1
      setInput('')
      if (nextIndex >= facts.length) {
        setFacts(prev => shuffle(prev))
        setIndex(0)
      } else {
        setIndex(nextIndex)
      }
      questionShownAtRef.current = performance.now()
    } catch (err) {
      const message = err instanceof Error ? err.message : t('errors.failedToRecord')
      setError(message)
      setPhase('error')
    } finally {
      setSubmitting(false)
    }
  }, [phase, submitting, sessionId, currentFact, input, index, facts, feedback, t])

  const appendDigit = useCallback((digit: string) => {
    if (phase !== 'playing' || submitting) return
    setInput(prev => appendAnswerDigit(prev, digit))
  }, [phase, submitting])

  const backspace = useCallback(() => {
    if (phase !== 'playing' || submitting) return
    setInput(prev => prev.slice(0, -1))
  }, [phase, submitting])

  const handleSubmit = useCallback(() => { void submitAnswer() }, [submitAnswer])

  const resetToSetup = useCallback(() => {
    setPhase('setup')
    setSessionId(null)
    setFacts([])
    setIndex(0)
    setInput('')
    setAnswered(0)
    setCorrect(0)
    setLastResult(null)
    setUsedFallback(false)
    setError('')
  }, [])

  // Setup screen.
  if (phase === 'setup' || phase === 'starting' || phase === 'error') {
    const opLabel = (op: Operation): string =>
      op === 'mult' ? t('practice.opMultiplication') : op === 'div' ? t('practice.opDivision') : t('practice.opMixed')

    return (
      <div className="max-w-2xl mx-auto p-4 sm:p-6">
        <Link to="/math" className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4">
          <ArrowLeft size={16} />
          {t('back')}
        </Link>
        <div className="flex items-center gap-3 mb-2">
          <Target size={26} className="text-blue-400 shrink-0" />
          <h1 className="text-2xl sm:text-3xl font-bold text-white">{t('practice.title')}</h1>
        </div>
        <p className="text-gray-400 mb-6">{t('practice.intro')}</p>

        {error && (
          <div className="mb-4 rounded border border-red-500/50 bg-red-500/10 px-3 py-2 text-sm text-red-300">
            {error}
          </div>
        )}

        <fieldset className="mb-6">
          <legend className="text-sm font-medium text-gray-300 mb-2">{t('practice.operationLabel')}</legend>
          <div className="flex flex-wrap gap-2">
            {OPERATIONS.map(op => {
              // A specific fact must drill one operation, so "mixed" is only
              // available for the weakest-facts set.
              const disabled = selection === 'specific' && op === 'mixed'
              const active = operation === op
              return (
                <button
                  key={op}
                  type="button"
                  disabled={disabled}
                  aria-pressed={active}
                  onClick={() => setOperation(op)}
                  className={`px-4 py-2 rounded-lg border text-sm font-medium transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                    active
                      ? 'border-blue-500 bg-blue-500/15 text-blue-200'
                      : 'border-gray-700 bg-gray-800 text-gray-300 hover:border-gray-500'
                  }`}
                >
                  {opLabel(op)}
                </button>
              )
            })}
          </div>
        </fieldset>

        <fieldset className="mb-6">
          <legend className="text-sm font-medium text-gray-300 mb-2">{t('practice.selectionLabel')}</legend>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <button
              type="button"
              aria-pressed={selection === 'weakest'}
              onClick={() => setSelection('weakest')}
              className={`text-left rounded-lg border p-3 transition-colors ${
                selection === 'weakest'
                  ? 'border-blue-500 bg-blue-500/10'
                  : 'border-gray-700 bg-gray-800 hover:border-gray-500'
              }`}
            >
              <div className="font-semibold text-white">{t('practice.weakest')}</div>
              <div className="text-sm text-gray-400">{t('practice.weakestHint')}</div>
            </button>
            <button
              type="button"
              aria-pressed={selection === 'specific'}
              onClick={() => {
                setSelection('specific')
                // A single fact needs a concrete operation; drop "mixed".
                if (operation === 'mixed') setOperation('mult')
              }}
              className={`text-left rounded-lg border p-3 transition-colors ${
                selection === 'specific'
                  ? 'border-blue-500 bg-blue-500/10'
                  : 'border-gray-700 bg-gray-800 hover:border-gray-500'
              }`}
            >
              <div className="font-semibold text-white">{t('practice.specific')}</div>
              <div className="text-sm text-gray-400">{t('practice.specificHint')}</div>
            </button>
          </div>
        </fieldset>

        {selection === 'weakest' && (
          <p className="mb-6 text-sm text-gray-400" aria-live="polite">
            {statsLoading
              ? t('practice.loadingStats')
              : weakCount > 0
                ? t('practice.weakestCount', { count: weakCount })
                : t('practice.weakestEmpty')}
          </p>
        )}

        {selection === 'specific' && (
          <div className="mb-6 flex flex-wrap items-end gap-3">
            <label className="flex flex-col gap-1 text-sm text-gray-300">
              {t('practice.factorALabel')}
              <select
                value={factorA}
                onChange={e => setFactorA(parseInt(e.target.value, 10))}
                className="rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-white"
              >
                {Array.from({ length: 10 }, (_, i) => i + 1).map(n => (
                  <option key={n} value={n}>{n}</option>
                ))}
              </select>
            </label>
            <span className="pb-2 text-2xl font-bold text-gray-400">
              {operation === 'div' ? '÷' : '×'}
            </span>
            <label className="flex flex-col gap-1 text-sm text-gray-300">
              {t('practice.factorBLabel')}
              <select
                value={factorB}
                onChange={e => setFactorB(parseInt(e.target.value, 10))}
                className="rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-white"
              >
                {Array.from({ length: 10 }, (_, i) => i + 1).map(n => (
                  <option key={n} value={n}>{n}</option>
                ))}
              </select>
            </label>
            <span className="pb-2 text-sm text-gray-400 tabular-nums">
              {operation === 'div'
                ? `= ${factorA * factorB} ÷ ${factorB}`
                : `= ${factorA} × ${factorB}`}
            </span>
          </div>
        )}

        <button
          type="button"
          onClick={() => { void startPractice() }}
          disabled={phase === 'starting'}
          className="w-full sm:w-auto px-6 py-3 rounded-lg bg-blue-600 hover:bg-blue-500 active:bg-blue-700 text-white font-semibold disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {phase === 'starting' ? t('practice.starting') : t('practice.start')}
        </button>
      </div>
    )
  }

  if (phase === 'summary') {
    return (
      <div className="max-w-2xl mx-auto p-4 sm:p-6">
        <h1 className="text-2xl sm:text-3xl font-bold text-white mb-2">{t('practice.resultTitle')}</h1>
        <p className="text-gray-400 mb-6">{t('practice.resultSubtitle')}</p>

        <div className="grid grid-cols-2 gap-3 sm:gap-4 mb-6">
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
            <div className="text-xs uppercase tracking-wide text-gray-400 mb-1">{t('practice.answeredLabel')}</div>
            <div className="text-3xl sm:text-4xl font-bold text-white tabular-nums">{answered}</div>
          </div>
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
            <div className="text-xs uppercase tracking-wide text-gray-400 mb-1">{t('practice.correctLabel')}</div>
            <div className="text-3xl sm:text-4xl font-bold text-white tabular-nums">{correct}</div>
          </div>
        </div>

        <div className="flex flex-col sm:flex-row gap-3">
          <button
            type="button"
            onClick={resetToSetup}
            className="px-5 py-3 rounded-lg bg-blue-600 hover:bg-blue-500 active:bg-blue-700 text-white font-semibold"
          >
            {t('practice.playAgain')}
          </button>
          <Link
            to="/math"
            className="px-5 py-3 rounded-lg border border-gray-700 hover:border-gray-500 text-gray-300 hover:text-white font-medium text-center"
          >
            {t('practice.backToModes')}
          </Link>
        </div>
      </div>
    )
  }

  // playing or finishing — render the play surface.
  const isFinishing = phase === 'finishing'

  return (
    <div className="min-h-[calc(100vh-3.5rem)] md:min-h-screen flex flex-col max-w-3xl mx-auto p-3 sm:p-6">
      <div className="flex items-center justify-between mb-4 sm:mb-6 gap-2">
        <div className="flex items-center gap-3 text-sm sm:text-base text-gray-400 tabular-nums">
          <span>
            {t('practice.answeredLabel')} <span className="text-white font-semibold">{answered}</span>
          </span>
          <span>
            {t('practice.correctLabel')} <span className="text-white font-semibold">{correct}</span>
          </span>
        </div>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => { void finishPractice() }}
            disabled={isFinishing}
            className="px-3 py-1.5 rounded-lg border border-gray-700 hover:border-gray-500 text-sm text-gray-300 hover:text-white font-medium disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {isFinishing ? t('practice.ending') : t('practice.end')}
          </button>
          <MuteToggle muted={feedback.muted} onToggle={feedback.toggleMute} />
        </div>
      </div>

      {usedFallback && (
        <div className="mb-4 rounded border border-gray-700 bg-gray-800/60 px-3 py-2 text-sm text-gray-300 text-center">
          {t('practice.fallbackBanner')}
        </div>
      )}

      <div className="flex-1 flex flex-col items-center justify-center mb-6">
        <div
          ref={problemRef}
          className="text-4xl sm:text-6xl md:text-7xl font-bold text-white text-center tabular-nums rounded-lg px-4 py-2"
        >
          {currentFact ? renderProblem(currentFact) : ''}
        </div>
        <div className="h-6 mt-3 text-sm font-medium" aria-live="polite">
          {lastResult && (
            lastResult.correct
              ? <span className="text-emerald-400">{t('practice.feedbackCorrect')}</span>
              : <span className="text-red-400">{t('practice.feedbackWrong', { answer: lastResult.expected })}</span>
          )}
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
