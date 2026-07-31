// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import RecipeCookMode from './RecipeCookMode'
import type { Recipe } from '../types/recipes'

// ── Translation mock ──────────────────────────────────────────────────────────
// `mockT` must be a stable reference: the recipe hook has `t` in its effect
// dependencies, so a fresh function per render would re-fetch forever.

const TRANSLATIONS: Record<string, string> = {
  'cook.title': 'Cook mode',
  'cook.step': 'Step {{current}} of {{total}}',
  'cook.next': 'Next step',
  'cook.prev': 'Previous step',
  'cook.finish': 'Done cooking',
  'cook.exit': 'Exit cook mode',
  'cook.timeRemaining': 'Time remaining',
  'cook.timerStart': 'Start timer',
  'cook.timerPause': 'Pause timer',
  'cook.timerResume': 'Resume timer',
  'cook.timerReset': 'Reset timer',
  'cook.timerDone': 'Time is up',
  'cook.noSteps': 'This recipe has no steps to cook through',
  'detail.ingredients': 'Ingredients',
  'detail.scaledFrom': 'Scaled from {{count}} servings',
  'detail.loading': 'Loading recipe…',
  'errors.notFound': 'That recipe does not exist',
  'errors.failedToLoadRecipe': 'Could not load this recipe',
  'errors.retry': 'Try again',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  const raw = TRANSLATIONS[key]
  if (raw === undefined) return key
  return raw.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(opts?.[name] ?? ''))
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

/**
 * Three steps: an untimed one, then two timed ones so a timer can be started on
 * one step and asserted to be gone from the next. Four servings, so a portion
 * override lands on whole numbers.
 */
const RECIPE: Recipe = {
  id: 1,
  title: 'Fish gratin',
  notes: '',
  servings: 4,
  rating: null,
  rated_at: null,
  last_cooked_at: null,
  created_at: '2026-01-01T12:00:00Z',
  updated_at: '2026-01-01T12:00:00Z',
  ingredients: [
    { id: 11, position: 1, text: '400 g cod, cubed', quantity: 400, unit: 'g', name: 'cod' },
    { id: 12, position: 2, text: '2 dl cream', quantity: 2, unit: 'dl', name: 'cream' },
  ],
  steps: [
    { id: 21, position: 1, text: 'Cube the cod.', duration_seconds: 0 },
    { id: 22, position: 2, text: 'Bake until golden.', duration_seconds: 1800 },
    { id: 23, position: 3, text: 'Rest before serving.', duration_seconds: 300 },
  ],
  tags: [],
}

// ── Fetch mock ────────────────────────────────────────────────────────────────

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: () => Promise.resolve(body) }
}

function mockFetch(recipe: Recipe = RECIPE) {
  const fetchMock = vi.fn(() => Promise.resolve(jsonResponse({ recipe })))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

// ── Helpers ───────────────────────────────────────────────────────────────────

interface RenderOptions {
  recipe?: Recipe
  /** Appended to the cook-mode URL, e.g. `?portions=8`. */
  search?: string
  /** Router state, the way the detail page's "start cooking" link passes it. */
  state?: unknown
}

async function renderCookMode(options: RenderOptions = {}) {
  const { recipe = RECIPE, search = '', state } = options
  mockFetch(recipe)
  const view = render(
    <MemoryRouter initialEntries={[{ pathname: '/recipes/1/cook', search, state }]}>
      <Routes>
        <Route path="/recipes/:id/cook" element={<RecipeCookMode />} />
      </Routes>
    </MemoryRouter>,
  )
  await screen.findByRole('heading', { name: 'Fish gratin' })
  return view
}

function prevButton() {
  return screen.getByRole('button', { name: 'Previous step' })
}

function nextButton() {
  return screen.getByRole('button', { name: 'Next step' })
}

/** The countdown display, or null when the current step is untimed. */
function timerDisplay() {
  return screen.queryByRole('timer', { name: 'Time remaining' })
}

/**
 * Installs a fake `navigator.wakeLock` and returns a teardown that removes it
 * again. happy-dom has no Wake Lock API of its own, so without this the hook
 * takes its feature-detect branch and never calls anything.
 */
function stubWakeLock(request: () => Promise<unknown>) {
  Object.defineProperty(navigator, 'wakeLock', {
    value: { request },
    configurable: true,
    writable: true,
  })
  return () => {
    Reflect.deleteProperty(navigator as unknown as object, 'wakeLock')
  }
}

/** Runs the fake clock forward inside `act` so React flushes the tick. */
function advance(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms)
  })
}

afterEach(() => {
  // Restore spies before the clock: `clearInterval` is spied on *after* the
  // fake timers are installed, so restoring it later would put the fake
  // implementation back over the real one.
  vi.restoreAllMocks()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('RecipeCookMode', () => {
  it('shows one step at a time with its position', async () => {
    await renderCookMode()

    expect(screen.getByText('Cube the cod.')).toBeInTheDocument()
    expect(screen.queryByText('Bake until golden.')).toBeNull()
    expect(screen.getByText('Step 1 of 3')).toBeInTheDocument()

    fireEvent.click(nextButton())
    expect(screen.getByText('Bake until golden.')).toBeInTheDocument()
    expect(screen.getByText('Step 2 of 3')).toBeInTheDocument()
    expect(screen.queryByText('Cube the cod.')).toBeNull()

    fireEvent.click(prevButton())
    expect(screen.getByText('Step 1 of 3')).toBeInTheDocument()
  })

  it('bounds navigation at the first and last step', async () => {
    await renderCookMode()

    expect(prevButton()).toBeDisabled()
    expect(nextButton()).toBeEnabled()

    fireEvent.click(nextButton())
    fireEvent.click(nextButton())
    expect(screen.getByText('Step 3 of 3')).toBeInTheDocument()
    expect(nextButton()).toBeDisabled()
    expect(prevButton()).toBeEnabled()

    // Clicking through a disabled button changes nothing.
    fireEvent.click(nextButton())
    expect(screen.getByText('Step 3 of 3')).toBeInTheDocument()

    fireEvent.click(prevButton())
    fireEvent.click(prevButton())
    expect(screen.getByText('Step 1 of 3')).toBeInTheDocument()
    fireEvent.click(prevButton())
    expect(screen.getByText('Step 1 of 3')).toBeInTheDocument()
  })

  it('only offers a timer on steps that carry a duration', async () => {
    await renderCookMode()

    expect(timerDisplay()).toBeNull()
    expect(screen.queryByRole('button', { name: 'Start timer' })).toBeNull()

    fireEvent.click(nextButton())
    expect(timerDisplay()).toHaveTextContent('30:00')
    expect(screen.getByRole('button', { name: 'Start timer' })).toBeInTheDocument()
  })

  it('counts down, pauses and resets the step timer', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await renderCookMode()

    fireEvent.click(nextButton())
    expect(timerDisplay()).toHaveTextContent('30:00')

    fireEvent.click(screen.getByRole('button', { name: 'Start timer' }))
    advance(3000)
    expect(timerDisplay()).toHaveTextContent('29:57')

    fireEvent.click(screen.getByRole('button', { name: 'Pause timer' }))
    const paused = timerDisplay()?.textContent
    advance(5000)
    expect(timerDisplay()).toHaveTextContent(String(paused))
    // Paused mid-count, the primary action resumes rather than restarts.
    expect(screen.getByRole('button', { name: 'Resume timer' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Resume timer' }))
    advance(2000)
    expect(timerDisplay()).toHaveTextContent('29:55')

    fireEvent.click(screen.getByRole('button', { name: 'Reset timer' }))
    expect(timerDisplay()).toHaveTextContent('30:00')
    advance(4000)
    expect(timerDisplay()).toHaveTextContent('30:00')
  })

  it('keeps the part-second already spent across a pause', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await renderCookMode()

    fireEvent.click(nextButton())
    fireEvent.click(screen.getByRole('button', { name: 'Start timer' }))

    // Two pauses landing mid-second. Rounding the remainder up at each pause
    // would hand a whole second back to the cook, so 30 seconds of running
    // would show as 29:31 instead of 29:30.
    advance(500)
    fireEvent.click(screen.getByRole('button', { name: 'Pause timer' }))
    fireEvent.click(screen.getByRole('button', { name: 'Resume timer' }))
    advance(500)
    fireEvent.click(screen.getByRole('button', { name: 'Pause timer' }))
    fireEvent.click(screen.getByRole('button', { name: 'Resume timer' }))
    advance(29_000)

    expect(timerDisplay()).toHaveTextContent('29:30')
  })

  it('stops at zero and announces that the time is up', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await renderCookMode()

    // Step 3 is the short one — five minutes.
    fireEvent.click(nextButton())
    fireEvent.click(nextButton())
    expect(timerDisplay()).toHaveTextContent('5:00')

    fireEvent.click(screen.getByRole('button', { name: 'Start timer' }))
    advance(300_000)
    expect(timerDisplay()).toHaveTextContent('0:00')
    expect(screen.getByRole('status')).toHaveTextContent('Time is up')

    // The interval is cleared once it hits zero; the clock cannot go negative.
    advance(10_000)
    expect(timerDisplay()).toHaveTextContent('0:00')
  })

  it('clears the interval on unmount and starts each step’s timer fresh', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval')
    const { unmount } = await renderCookMode()

    fireEvent.click(nextButton())
    fireEvent.click(screen.getByRole('button', { name: 'Start timer' }))
    advance(3000)
    expect(timerDisplay()).toHaveTextContent('29:57')

    // Moving on while the timer runs tears it down and mounts the next step's
    // timer paused at its own full duration.
    clearIntervalSpy.mockClear()
    fireEvent.click(nextButton())
    expect(clearIntervalSpy).toHaveBeenCalled()
    expect(timerDisplay()).toHaveTextContent('5:00')
    expect(screen.getByRole('button', { name: 'Start timer' })).toBeInTheDocument()

    // Coming back finds the earlier step's timer reset too — nothing kept running.
    fireEvent.click(prevButton())
    expect(timerDisplay()).toHaveTextContent('30:00')

    fireEvent.click(screen.getByRole('button', { name: 'Start timer' }))
    clearIntervalSpy.mockClear()
    unmount()
    expect(clearIntervalSpy).toHaveBeenCalled()
  })

  it('scales the ingredient list to portions passed as router state', async () => {
    await renderCookMode({ state: { portions: 8 } })

    expect(screen.getByText('800 g cod, cubed')).toBeInTheDocument()
    expect(screen.getByText('4 dl cream')).toBeInTheDocument()
    expect(screen.getByText('Scaled from 4 servings')).toBeInTheDocument()
  })

  it('falls back to the portions query param, then to the recipe’s own yield', async () => {
    const { unmount } = await renderCookMode({ search: '?portions=2' })
    expect(screen.getByText('200 g cod, cubed')).toBeInTheDocument()
    unmount()

    await renderCookMode({ search: '?portions=nonsense' })
    expect(screen.getByText('400 g cod, cubed')).toBeInTheDocument()
    expect(screen.queryByText('Scaled from 4 servings')).toBeNull()
  })

  it('holds a screen wake lock while cooking and releases it on exit', async () => {
    const release = vi.fn(() => Promise.resolve())
    const request = vi.fn(() => Promise.resolve({ released: false, release }))
    const restoreWakeLock = stubWakeLock(request)

    try {
      const { unmount } = await renderCookMode()
      // Let the request promise settle so the sentinel is stored before unmount.
      await act(async () => {})
      expect(request).toHaveBeenCalledWith('screen')

      unmount()
      expect(release).toHaveBeenCalled()
    } finally {
      restoreWakeLock()
    }
  })

  it('cooks on when the browser refuses the wake lock', async () => {
    const request = vi.fn(() => Promise.reject(new Error('denied')))
    const restoreWakeLock = stubWakeLock(request)

    try {
      await renderCookMode()
      await act(async () => {})

      expect(request).toHaveBeenCalled()
      expect(screen.getByText('Cube the cod.')).toBeInTheDocument()
      expect(nextButton()).toBeEnabled()
    } finally {
      restoreWakeLock()
    }
  })

  it('says so when the recipe has no steps to cook through', async () => {
    await renderCookMode({ recipe: { ...RECIPE, steps: [] } })

    expect(screen.getByText('This recipe has no steps to cook through')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Next step' })).toBeNull()
  })
})
