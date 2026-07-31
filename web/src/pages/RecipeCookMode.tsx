import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Check, ChevronLeft, ChevronRight, Pause, Play, RotateCcw, X } from 'lucide-react'
import { useRecipe } from '../hooks/useRecipes'
import { useWakeLock } from '../hooks/useWakeLock'
import { ingredientLine, type Recipe, type RecipeStep } from '../types/recipes'

/**
 * Full-screen cook mode: one step at a time, with a per-step countdown.
 *
 * The recipe is read through the same `useRecipe` hook the detail page uses, so
 * cook mode never holds its own copy of the data. The portion count comes from
 * whatever the detail page was showing — passed as router state by its "start
 * cooking" link, or as a `?portions=` query so a bookmarked cook-mode URL keeps
 * the scaling — falling back to the recipe's own yield. Only the ingredient
 * list is scaled; step text is shown exactly as written.
 *
 * Each step's timer lives in {@link StepTimer}, keyed by step ID. Keying it
 * means moving between steps unmounts the timer rather than reconfiguring it,
 * so the interval is always cleared and the next step starts fresh and paused.
 */

/**
 * How often the countdown recomputes. The remaining time is derived from a
 * deadline rather than decremented per tick, so the tick rate only controls how
 * quickly the display catches up (after a resume, say) and never accumulates
 * drift. A quarter second keeps the seconds digit honest without busy-looping.
 */
const TICK_MS = 250

/** `m:ss`, counting minutes past an hour rather than showing an hours field. */
function formatRemaining(totalSeconds: number): string {
  const safe = Math.max(0, Math.floor(totalSeconds))
  const minutes = Math.floor(safe / 60)
  const seconds = safe % 60
  return `${minutes}:${String(seconds).padStart(2, '0')}`
}

/** Reads a portion count from router state or a query param; null when it is not a usable one. */
function positivePortions(value: unknown): number | null {
  const parsed = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : NaN
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null
}

interface Countdown {
  /** Whole seconds left. */
  remaining: number
  running: boolean
  /** True once a timed step has run out — the caller shows "time is up". */
  done: boolean
  start: () => void
  pause: () => void
  reset: () => void
}

/**
 * A pausable countdown over `durationSeconds`.
 *
 * The interval only exists while the timer runs, and its effect cleanup clears
 * it — on pause, on completion and on unmount — so no timer outlives the step
 * that started it. `remaining` is computed from a deadline on every tick, which
 * keeps a backgrounded tab (where intervals are throttled) honest.
 */
function useCountdown(durationSeconds: number): Countdown {
  const [remaining, setRemaining] = useState(durationSeconds)
  const [running, setRunning] = useState(false)
  const deadlineRef = useRef(0)
  // Mirrors `remaining` so start/pause can read the current value without
  // depending on it (a state updater must stay free of side effects).
  const remainingRef = useRef(durationSeconds)

  const applyRemaining = useCallback((seconds: number) => {
    remainingRef.current = seconds
    setRemaining(seconds)
  }, [])

  /** Whole seconds between now and the deadline, never below zero. */
  const secondsLeft = useCallback(
    () => Math.max(0, Math.ceil((deadlineRef.current - Date.now()) / 1000)),
    [],
  )

  useEffect(() => {
    if (!running) return

    const tick = () => {
      const left = secondsLeft()
      applyRemaining(left)
      if (left === 0) setRunning(false)
    }

    tick()
    const interval = setInterval(tick, TICK_MS)
    return () => {
      clearInterval(interval)
    }
  }, [running, applyRemaining, secondsLeft])

  const start = useCallback(() => {
    // Starting from zero restarts the whole duration; otherwise it resumes.
    const from = remainingRef.current > 0 ? remainingRef.current : durationSeconds
    if (from <= 0) return
    deadlineRef.current = Date.now() + from * 1000
    applyRemaining(from)
    setRunning(true)
  }, [durationSeconds, applyRemaining])

  const pause = useCallback(() => {
    if (running) applyRemaining(secondsLeft())
    setRunning(false)
  }, [running, applyRemaining, secondsLeft])

  const reset = useCallback(() => {
    setRunning(false)
    applyRemaining(durationSeconds)
  }, [durationSeconds, applyRemaining])

  return {
    remaining,
    running,
    done: durationSeconds > 0 && remaining === 0,
    start,
    pause,
    reset,
  }
}

/** The countdown for one timed step. Mount it keyed by step so it resets on navigation. */
function StepTimer({ durationSeconds }: { durationSeconds: number }) {
  const { t } = useTranslation('recipes')
  const { remaining, running, done, start, pause, reset } = useCountdown(durationSeconds)

  // Started once and now paused mid-way — the primary action reads "resume".
  const resumable = !running && remaining > 0 && remaining < durationSeconds

  return (
    <div className="mt-6 rounded-2xl bg-gray-800/60 border border-gray-700 p-4">
      <p
        role="timer"
        aria-label={t('cook.timeRemaining')}
        className={`text-center tabular-nums text-5xl sm:text-6xl font-semibold ${
          done ? 'text-amber-300' : 'text-white'
        }`}
      >
        {formatRemaining(remaining)}
      </p>

      {done && (
        <p role="status" className="mt-2 text-center text-amber-300 font-medium">
          {t('cook.timerDone')}
        </p>
      )}

      <div className="mt-4 flex items-center justify-center gap-3">
        <button
          type="button"
          onClick={running ? pause : start}
          className="flex items-center justify-center gap-2 min-h-12 min-w-[7.5rem] px-5 py-3 rounded-xl bg-blue-600 hover:bg-blue-500 text-white transition-colors cursor-pointer"
        >
          {running ? (
            <Pause size={20} aria-hidden="true" />
          ) : (
            <Play size={20} aria-hidden="true" />
          )}
          {running
            ? t('cook.timerPause')
            : resumable
              ? t('cook.timerResume')
              : t('cook.timerStart')}
        </button>
        <button
          type="button"
          onClick={reset}
          disabled={!running && remaining === durationSeconds}
          className="flex items-center justify-center gap-2 min-h-12 px-5 py-3 rounded-xl bg-gray-700 hover:bg-gray-600 text-gray-100 transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <RotateCcw size={20} aria-hidden="true" />
          {t('cook.timerReset')}
        </button>
      </div>
    </div>
  )
}

/** The scaled ingredient list, kept on screen so a step never sends the cook back. */
function CookIngredients({ recipe, portions }: { recipe: Recipe; portions: number }) {
  const { t } = useTranslation('recipes')
  if (recipe.ingredients.length === 0) return null

  return (
    <section aria-labelledby="cook-ingredients-heading" className="mt-6">
      <h2 id="cook-ingredients-heading" className="text-base font-semibold text-gray-300">
        {t('detail.ingredients')}
      </h2>
      {portions !== recipe.servings && recipe.servings > 0 && (
        <p className="text-sm text-gray-500">{t('detail.scaledFrom', { count: recipe.servings })}</p>
      )}
      <ul className="mt-2 space-y-1 text-gray-300">
        {recipe.ingredients.map(ing => (
          <li key={ing.id} className="break-words">
            {ingredientLine(ing, recipe.servings, portions)}
          </li>
        ))}
      </ul>
    </section>
  )
}

export default function RecipeCookMode() {
  const { t } = useTranslation('recipes')
  const { id } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const { recipe, loading, error, notFound, refresh } = useRecipe(id)
  const [stepIndex, setStepIndex] = useState(0)

  // Cooking means wet hands and long pauses; the screen must not sleep.
  useWakeLock()

  const steps: RecipeStep[] = recipe?.steps ?? []
  const lastIndex = steps.length - 1
  // Clamped rather than trusted: a refresh that returns a shorter method must
  // not leave the view pointing past the end of the list.
  const safeIndex = Math.min(Math.max(stepIndex, 0), Math.max(lastIndex, 0))
  const step = steps[safeIndex]
  const isFirst = safeIndex === 0
  const isLast = safeIndex === lastIndex

  const goPrev = useCallback(() => {
    setStepIndex(index => Math.max(0, index - 1))
  }, [])

  const goNext = useCallback(() => {
    setStepIndex(index => Math.min(steps.length - 1, index + 1))
  }, [steps.length])

  const backTo = id ? `/recipes/${id}` : '/recipes'

  const exitLink = (
    <Link
      to={backTo}
      className="inline-flex items-center gap-2 min-h-11 px-3 py-2 -ml-3 text-sm text-gray-400 hover:text-gray-200 transition-colors"
    >
      <X size={16} aria-hidden="true" />
      {t('cook.exit')}
    </Link>
  )

  if (loading) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-6">
        {exitLink}
        <p role="status" aria-busy="true" className="mt-4 text-gray-400">
          {t('detail.loading')}
        </p>
      </div>
    )
  }

  if (notFound) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-6">
        {exitLink}
        <p className="mt-4 text-gray-400">{t('errors.notFound')}</p>
      </div>
    )
  }

  if (!recipe) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-6">
        {exitLink}
        <div className="mt-4 px-4 py-2 bg-red-900/50 border border-red-800 rounded-lg text-red-300 text-sm">
          {error || t('errors.failedToLoadRecipe')}
        </div>
        <button
          type="button"
          onClick={refresh}
          className="mt-3 min-h-11 px-4 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm"
        >
          {t('errors.retry')}
        </button>
      </div>
    )
  }

  const routePortions = positivePortions((location.state as { portions?: unknown } | null)?.portions)
  const portions = routePortions ?? positivePortions(searchParams.get('portions')) ?? recipe.servings

  if (steps.length === 0) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-6">
        {exitLink}
        <h1 className="mt-3 text-xl font-semibold break-words">{recipe.title}</h1>
        <p className="mt-4 text-gray-400">{t('cook.noSteps')}</p>
        <CookIngredients recipe={recipe} portions={portions} />
      </div>
    )
  }

  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      {exitLink}

      <h1 className="mt-3 text-xl font-semibold break-words">{recipe.title}</h1>
      <p className="mt-1 text-sm text-gray-400">
        {t('cook.step', { current: safeIndex + 1, total: steps.length })}
      </p>

      {/* Progress across the method, so the position is readable at a glance. */}
      <ol aria-hidden="true" className="mt-2 flex gap-1">
        {steps.map((s, index) => (
          <li
            key={s.id}
            className={`h-1.5 flex-1 rounded-full ${
              index <= safeIndex ? 'bg-blue-500' : 'bg-gray-700'
            }`}
          />
        ))}
      </ol>

      <p className="mt-6 text-2xl sm:text-3xl leading-relaxed break-words whitespace-pre-wrap">
        {step.text}
      </p>

      {step.duration_seconds > 0 && (
        <StepTimer key={step.id} durationSeconds={step.duration_seconds} />
      )}

      <div className="mt-6 flex items-center gap-3">
        <button
          type="button"
          onClick={goPrev}
          disabled={isFirst}
          aria-disabled={isFirst}
          className="flex flex-1 items-center justify-center gap-2 min-h-14 px-4 py-3 rounded-xl bg-gray-800 hover:bg-gray-700 text-gray-100 transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
        >
          <ChevronLeft size={20} aria-hidden="true" />
          {t('cook.prev')}
        </button>
        <button
          type="button"
          onClick={goNext}
          disabled={isLast}
          aria-disabled={isLast}
          className="flex flex-1 items-center justify-center gap-2 min-h-14 px-4 py-3 rounded-xl bg-blue-600 hover:bg-blue-500 text-white transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {t('cook.next')}
          <ChevronRight size={20} aria-hidden="true" />
        </button>
      </div>

      {isLast && (
        <button
          type="button"
          onClick={() => navigate(backTo)}
          className="mt-3 flex w-full items-center justify-center gap-2 min-h-14 px-4 py-3 rounded-xl bg-green-700 hover:bg-green-600 text-white transition-colors cursor-pointer"
        >
          <Check size={20} aria-hidden="true" />
          {t('cook.finish')}
        </button>
      )}

      <CookIngredients recipe={recipe} portions={portions} />
    </div>
  )
}
