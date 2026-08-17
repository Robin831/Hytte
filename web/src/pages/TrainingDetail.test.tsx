// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router'
import TrainingDetail from './TrainingDetail'
import { stubEventSource, type EventSourceStub } from '../test/stubEventSource'

// ── Translation mock ──────────────────────────────────────────────────────────
// Uses an async factory so enTraining is loaded inside the mock (avoids hoisting issues).

vi.mock('react-i18next', async () => {
  const { default: enTraining } = await import('../../public/locales/en/training.json')

  function resolveKey(obj: Record<string, unknown>, parts: string[]): unknown {
    const [head, ...rest] = parts
    const val = obj[head]
    if (rest.length === 0) return val
    if (val && typeof val === 'object' && !Array.isArray(val)) {
      return resolveKey(val as Record<string, unknown>, rest)
    }
    return undefined
  }

  function makeT(translations: Record<string, unknown>) {
    return function t(key: string, opts?: Record<string, unknown>): string {
      if (opts?.defaultValue && typeof opts.defaultValue === 'string') return opts.defaultValue
      const val = resolveKey(translations, key.split('.'))
      if (typeof val === 'string') {
        if (opts) return val.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
        return val
      }
      return key
    }
  }

  const trainingT = makeT(enTraining as Record<string, unknown>)
  const emptyT = makeT({})

  // useTranslation is called with several different namespace argument shapes
  // throughout TrainingDetail. We must return the SAME `t` reference for each
  // shape — the page has `t` in a useEffect dep array, and a fresh function on
  // every render would cause infinite re-runs.
  const tCache = new Map<string, (key: string, opts?: Record<string, unknown>) => string>()
  function getT(namespaces: string[]) {
    const cacheKey = namespaces.join(',')
    const cached = tCache.get(cacheKey)
    if (cached) return cached
    const fn = (key: string, opts?: Record<string, unknown>): string => {
      let namespace: string | null
      let localKey = key
      const colonIdx = key.indexOf(':')
      if (colonIdx >= 0) {
        namespace = key.slice(0, colonIdx)
        localKey = key.slice(colonIdx + 1)
      } else if (typeof opts?.ns === 'string') {
        namespace = opts.ns
      } else {
        namespace = namespaces[0] ?? null
      }
      if (namespace === 'training') return trainingT(localKey, opts)
      return emptyT(localKey, opts)
    }
    tCache.set(cacheKey, fn)
    return fn
  }

  const i18nObj = { language: 'en' }
  return {
    useTranslation: (ns?: string | string[]) => {
      const namespaces = Array.isArray(ns) ? ns : ns ? [ns] : []
      return { t: getT(namespaces), i18n: i18nObj }
    },
    Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
    initReactI18next: { type: '3rdParty', init: () => {} },
  }
})

// ── Auth mock ─────────────────────────────────────────────────────────────────

interface MockUser {
  id: number
  email: string
  name: string
  is_admin: boolean
  features: Record<string, boolean>
}

const authState: { user: MockUser | null; hasFeature: (key: string) => boolean } = {
  user: { id: 1, email: 'a@b.c', name: 'Tester', is_admin: true, features: {} },
  hasFeature: () => false,
}

vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

// ── Mock heavy / irrelevant sub-components ───────────────────────────────────

interface MockedModalProps {
  isOpen: boolean
  onClose: () => void
}

const modalCallbacks = {
  lastOnClose: null as null | (() => void),
}

vi.mock('../components/training/WorkoutContextModal', () => ({
  default: ({ isOpen, onClose }: MockedModalProps) => {
    modalCallbacks.lastOnClose = onClose
    return isOpen
      ? <div data-testid="context-modal">CONTEXT_MODAL_OPEN</div>
      : null
  },
}))

vi.mock('../components/charts/WorkoutHRChart', () => ({ default: () => null }))
vi.mock('../components/charts/WorkoutPaceChart', () => ({ default: () => null }))
vi.mock('../components/training/HRZoneCard', () => ({ default: () => null }))
vi.mock('../components/training/TrendCard', () => ({ default: () => null }))
vi.mock('../components/training/RacePredictionsCard', () => ({ default: () => null }))
vi.mock('../components/LactateImportDialog', () => ({ default: () => null }))
vi.mock('../components/TagBadge', () => ({
  default: ({ tag }: { tag: string }) => <span>{tag}</span>,
}))

// Every icon renders as an inert stub, so a new icon anywhere in the page's
// component graph doesn't break the suite. See src/test/lucideStub.tsx.
vi.mock('lucide-react', async () => (await import('../test/lucideStub')).lucideStub)

vi.mock('../utils/formatDate', () => ({
  formatDate: (date: Date | string) => String(date),
  formatTime: () => '08:00',
  formatNumber: (n: number) => String(n),
}))

// ── Test data ─────────────────────────────────────────────────────────────────

const BASE_WORKOUT = {
  id: 42,
  user_id: 1,
  sport: 'running',
  title: 'Morning Run',
  started_at: '2026-04-09T08:00:00Z',
  duration_seconds: 1800,
  distance_meters: 5000,
  avg_heart_rate: 150,
  max_heart_rate: 170,
  avg_pace_sec_per_km: 360,
  avg_cadence: 0,
  calories: 0,
  ascent_meters: 0,
  descent_meters: 0,
  fit_file_hash: 'abc',
  analysis_status: '',
  title_source: 'auto',
  created_at: '2026-04-09T08:00:00Z',
  tags: [],
  laps: [],
}

const SAVED_CONTEXT = {
  workout_id: 42,
  surface: 'Outside',
  run_type: 'slow',
  hr_source: 'watch',
  feel_notes: 'felt strong',
  speed_plan: [],
}

// ── Fetch mock ────────────────────────────────────────────────────────────────

interface FetchOverrides {
  context?: { ok: boolean; status?: number; data?: unknown }
  /** Read per call, so a test can change what lands between two refetches. */
  evaluations?: () => unknown[]
}

function makeFetchMock(overrides: FetchOverrides = {}) {
  return vi.fn((url: string) => {
    const ok = (data: unknown) =>
      Promise.resolve(new Response(JSON.stringify(data), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }))
    const notFound = () =>
      Promise.resolve(new Response(JSON.stringify({}), {
        status: 404,
        headers: { 'Content-Type': 'application/json' },
      }))

    if (url.endsWith(`/api/training/workouts/${BASE_WORKOUT.id}`)) {
      return ok({ workout: BASE_WORKOUT })
    }
    if (url.endsWith('/zones')) return ok({ zones: [] })
    if (url.endsWith('/similar')) return ok({ similar: [] })
    if (url.includes('/api/stride/evaluations')) {
      return ok({ evaluations: overrides.evaluations?.() ?? [] })
    }
    if (url.endsWith('/context')) {
      const ctx = overrides.context
      if (!ctx) return notFound()
      if (!ctx.ok) {
        return Promise.resolve(new Response(JSON.stringify({}), {
          status: ctx.status ?? 500,
          headers: { 'Content-Type': 'application/json' },
        }))
      }
      return ok(ctx.data ?? { context: null })
    }
    if (url.endsWith('/analysis')) return ok({ analysis: null })
    if (url.endsWith('/insights')) return ok({ insights: null })
    if (url.includes('/api/training/predictions')) return ok({ predictions: [] })
    return ok({})
  })
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={[`/training/${BASE_WORKOUT.id}`]}>
      <Routes>
        <Route path="/training/:id" element={<TrainingDetail />} />
      </Routes>
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('TrainingDetail – workout context flow', () => {
  beforeEach(() => {
    modalCallbacks.lastOnClose = null
    authState.user = { id: 1, email: 'a@b.c', name: 'Tester', is_admin: true, features: {} }
    // The page subscribes to the training SSE hub for stride_eval_started/ready;
    // without a stubbed EventSource every render throws on mount.
    stubEventSource()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('auto-opens the context modal once when the workout has no context', async () => {
    vi.stubGlobal('fetch', makeFetchMock({ context: { ok: false, status: 404 } }))
    renderPage()

    await waitFor(() => {
      expect(screen.getByTestId('context-modal')).toBeInTheDocument()
    })
  })

  it('does not auto-open the modal when the workout already has context', async () => {
    vi.stubGlobal('fetch', makeFetchMock({
      context: { ok: true, data: { context: SAVED_CONTEXT } },
    }))
    renderPage()

    // Wait for the page to finish loading by asserting on the title.
    await waitFor(() => {
      expect(screen.getByText('Morning Run')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('context-modal')).toBeNull()
    // Empty-state pending block must also be absent.
    expect(
      screen.queryByText('AI summary pending — provide workout context'),
    ).toBeNull()
  })

  it('shows the empty state with a CTA after dismissing the modal without saving', async () => {
    vi.stubGlobal('fetch', makeFetchMock({ context: { ok: false, status: 404 } }))
    renderPage()

    await waitFor(() => expect(screen.getByTestId('context-modal')).toBeInTheDocument())

    // Dismiss the modal without providing context (refetch still returns 404).
    expect(modalCallbacks.lastOnClose).not.toBeNull()
    modalCallbacks.lastOnClose!()

    await waitFor(() => {
      expect(
        screen.getByText('AI summary pending — provide workout context'),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('Provide context')).toBeInTheDocument()
    // Modal stays closed; auto-open must not re-fire.
    expect(screen.queryByTestId('context-modal')).toBeNull()
  })

  it('reopens the modal when the empty-state CTA is clicked', async () => {
    vi.stubGlobal('fetch', makeFetchMock({ context: { ok: false, status: 404 } }))
    renderPage()

    await waitFor(() => expect(screen.getByTestId('context-modal')).toBeInTheDocument())
    modalCallbacks.lastOnClose!()
    await waitFor(() => expect(screen.getByText('Provide context')).toBeInTheDocument())

    fireEvent.click(screen.getByText('Provide context'))

    await waitFor(() => {
      expect(screen.getByTestId('context-modal')).toBeInTheDocument()
    })
  })

  it('does not auto-open the modal or show the empty state when the context fetch returns a 5xx error', async () => {
    vi.stubGlobal('fetch', makeFetchMock({ context: { ok: false, status: 500 } }))
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Morning Run')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('context-modal')).toBeNull()
    expect(
      screen.queryByText('AI summary pending — provide workout context'),
    ).toBeNull()
  })

  it('does not auto-open the modal for non-admin users when context is missing', async () => {
    authState.user = { id: 1, email: 'a@b.c', name: 'Tester', is_admin: false, features: {} }
    vi.stubGlobal('fetch', makeFetchMock({ context: { ok: false, status: 404 } }))
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Morning Run')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('context-modal')).toBeNull()
  })

  it('flips the empty state away when the refetch returns a saved context', async () => {
    // First /context call returns 404 (initial load), second returns the saved context.
    let contextCall = 0
    const fetchMock = vi.fn((url: string) => {
      const ok = (data: unknown) =>
        Promise.resolve(new Response(JSON.stringify(data), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }))
      const notFound = () =>
        Promise.resolve(new Response(JSON.stringify({}), {
          status: 404,
          headers: { 'Content-Type': 'application/json' },
        }))

      if (url.endsWith(`/api/training/workouts/${BASE_WORKOUT.id}`)) {
        return ok({ workout: BASE_WORKOUT })
      }
      if (url.endsWith('/zones')) return ok({ zones: [] })
      if (url.endsWith('/similar')) return ok({ similar: [] })
      if (url.includes('/api/stride/evaluations')) return ok({ evaluations: [] })
      if (url.endsWith('/context')) {
        contextCall += 1
        if (contextCall === 1) return notFound()
        return ok({ context: SAVED_CONTEXT })
      }
      if (url.endsWith('/analysis')) return ok({ analysis: null })
      if (url.endsWith('/insights')) return ok({ insights: null })
      if (url.includes('/api/training/predictions')) return ok({ predictions: [] })
      return ok({})
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByTestId('context-modal')).toBeInTheDocument())
    modalCallbacks.lastOnClose!()

    await waitFor(() => {
      expect(
        screen.queryByText('AI summary pending — provide workout context'),
      ).toBeNull()
    })
  })
})

// The coach evaluation lands from a background Claude call with no status column
// to poll, so the SSE hub is the only thing that puts it on screen. These tests
// drive the stream directly: the stub keeps its listeners, so the pending strip,
// the event matching (id or date) and the refetch are exercised rather than
// merely stubbed away.
describe('TrainingDetail – stride evaluation over SSE', () => {
  const PENDING_STRIP = 'The coach is evaluating this workout…'

  // The evaluation the coach stores for workout 42; absent until a test says so.
  const READY_EVAL = {
    id: 7,
    user_id: 1,
    plan_id: 3,
    workout_id: BASE_WORKOUT.id,
    eval: {
      planned_type: 'easy',
      actual_type: 'easy',
      compliance: 'compliant',
      notes: 'Held the HR cap the whole way',
      flags: [],
      adjustments: '',
    },
    created_at: '2026-04-09T20:00:00Z',
  }

  let sse: EventSourceStub
  let stored: unknown[]

  beforeEach(async () => {
    authState.user = { id: 1, email: 'a@b.c', name: 'Tester', is_admin: true, features: {} }
    stored = []
    sse = stubEventSource()
    vi.stubGlobal('fetch', makeFetchMock({
      context: { ok: true, data: { context: SAVED_CONTEXT } },
      evaluations: () => stored,
    }))
    renderPage()
    // The date-matching branch needs the loaded workout's started_at, so wait
    // for the page body before dispatching anything.
    await waitFor(() => expect(screen.getByText('Morning Run')).toBeInTheDocument())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('subscribes to the training stream and shows the pending strip on a matching workout id', async () => {
    expect(sse.urls()).toContain('/api/training/events')
    expect(screen.queryByText(PENDING_STRIP)).toBeNull()

    act(() => sse.dispatch('stride_eval_started', { workout_id: BASE_WORKOUT.id }))

    expect(await screen.findByText(PENDING_STRIP)).toBeInTheDocument()
  })

  it('matches on the evaluated date when the event carries no workout id', async () => {
    // A stride evaluation covers a date, so an event for 2026-04-09 belongs to
    // this workout even with workout_id unset.
    act(() => sse.dispatch('stride_eval_started', { workout_id: 0, date: '2026-04-09' }))

    expect(await screen.findByText(PENDING_STRIP)).toBeInTheDocument()
  })

  it('ignores events for another workout, another date, or a malformed payload', async () => {
    act(() => {
      sse.dispatch('stride_eval_started', { workout_id: 999 })
      sse.dispatch('stride_eval_started', { date: '2026-04-10' })
      sse.dispatch('stride_eval_started', { date: '' })
      sse.dispatch('stride_eval_started', 'not json{')
    })

    await waitFor(() => expect(screen.getByText('Morning Run')).toBeInTheDocument())
    expect(screen.queryByText(PENDING_STRIP)).toBeNull()
  })

  it('clears the pending strip and loads the stored evaluation on stride_eval_ready', async () => {
    act(() => sse.dispatch('stride_eval_started', { workout_id: BASE_WORKOUT.id }))
    expect(await screen.findByText(PENDING_STRIP)).toBeInTheDocument()

    // The coach's result is now stored, and the ready event triggers the refetch.
    stored = [READY_EVAL]
    act(() => sse.dispatch('stride_eval_ready', { workout_id: BASE_WORKOUT.id }))

    expect(await screen.findByText('Held the HR cap the whole way')).toBeInTheDocument()
    expect(screen.getByText('Stride says')).toBeInTheDocument()
    expect(screen.queryByText(PENDING_STRIP)).toBeNull()
  })

  it('does not refetch for a stride_eval_ready belonging to another workout', async () => {
    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    const evalCalls = () =>
      fetchMock.mock.calls.filter(([url]) => String(url).includes('/api/stride/evaluations')).length
    const before = evalCalls()

    stored = [READY_EVAL]
    act(() => sse.dispatch('stride_eval_ready', { workout_id: 999 }))

    await waitFor(() => expect(screen.getByText('Morning Run')).toBeInTheDocument())
    expect(evalCalls()).toBe(before)
    expect(screen.queryByText('Stride says')).toBeNull()
  })
})
