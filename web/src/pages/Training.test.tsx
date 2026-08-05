// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterAll } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Training from './Training'
import type { Workout } from '../types/training'
import {
  trainingListCacheKey,
  readTrainingListCache,
  TRAINING_LIST_CACHE_VERSION,
  TRAINING_LIST_CACHE_TTL_MS,
} from '../hooks/useTrainingListCache'

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

vi.mock('../auth', () => {
  // The real AuthContext holds `user` in state, so it is referentially stable
  // across re-renders. Keep the stub stable too — a fresh object per call would
  // retrigger every effect that depends on `user` on each render.
  const user = { id: 1, email: 'a@b.c', name: 'Tester', is_admin: true, features: {} }
  const auth = { user, hasFeature: () => false }
  return { useAuth: () => auth }
})

vi.mock('../components/TagBadge', () => ({
  default: ({ tag }: { tag: string }) => <span data-testid={`tag-${tag}`}>{tag}</span>,
}))

vi.mock('lucide-react', () => {
  const Stub = () => null
  return {
    Dumbbell: Stub, Upload: Stub, TrendingUp: Stub, BarChart3: Stub,
    RefreshCw: Stub, X: Stub, Database: Stub, Search: Stub, Sparkles: Stub,
  }
})

vi.mock('../utils/formatDate', () => ({
  formatDate: () => 'Jan 1, 2026',
  formatTime: () => '08:00',
}))

vi.mock('../utils/training', () => ({
  formatDistance: () => '5.0 km',
  formatDuration: () => '30:00',
  formatPace: () => '6:00',
}))

const makeWorkout = (overrides: Partial<Workout> & { id: number; title: string; sport: string }): Workout => ({
  user_id: 1,
  started_at: '2026-01-01T08:00:00Z',
  duration_seconds: 1800,
  distance_meters: 5000,
  avg_heart_rate: 150,
  max_heart_rate: 170,
  avg_pace_sec_per_km: 360,
  avg_cadence: 0,
  calories: 300,
  ascent_meters: 0,
  descent_meters: 0,
  fit_file_hash: '',
  analysis_status: '',
  title_source: '',
  created_at: '2026-01-01T08:00:00Z',
  tags: [],
  ...overrides,
})

const WORKOUTS: Workout[] = [
  makeWorkout({ id: 1, title: 'Morning Run', sport: 'running', tags: ['easy', 'ai:recovery'] }),
  makeWorkout({ id: 2, title: 'Hill Intervals', sport: 'running', tags: ['hard', 'auto:intervals'] }),
  makeWorkout({ id: 3, title: 'Easy Spin', sport: 'cycling', tags: ['easy'] }),
  makeWorkout({ id: 4, title: 'Pool Laps', sport: 'swimming', tags: [] }),
]

// Tags the "server" reports for the user. Deliberately includes a tag that no
// loaded workout carries, so the chips can be shown to come from the endpoint
// rather than from the loaded pages.
const ALL_TAGS = ['ai:recovery', 'auto:intervals', 'easy', 'hard', 'deep-history']

// Must match SEARCH_DEBOUNCE_MS in Training.tsx.
const SEARCH_DEBOUNCE_MS = 300

// Requests the component issued, in order. Reset per test.
let requests: string[] = []

function jsonResponse(body: unknown) {
  return Promise.resolve({ ok: true, json: () => Promise.resolve(body) })
}

// mockFetch stands in for the backend and applies the same filter semantics the
// server does (sport/tag/text AND-combined, tags requiring every selected tag)
// plus keyset paging over the *matches*, so the tests exercise the real
// request-driven flow rather than any client-side narrowing.
function mockFetch(workouts: Workout[], tags: string[] = ALL_TAGS) {
  return vi.fn().mockImplementation((url: string) => {
    requests.push(url)
    const parsed = new URL(url, 'http://localhost')
    const path = parsed.pathname
    const params = parsed.searchParams

    if (path === '/api/training/workouts/latest') {
      return jsonResponse({
        latest_id: workouts.length > 0 ? Math.max(...workouts.map(w => w.id)) : 0,
      })
    }
    if (path === '/api/training/tags') {
      return jsonResponse({ tags })
    }
    if (path === '/api/training/workouts') {
      const sport = params.get('sport') ?? ''
      const wanted = params.getAll('tag')
      const q = (params.get('q') ?? '').toLowerCase()
      const matches = workouts.filter(
        w =>
          (!sport || w.sport === sport) &&
          wanted.every(tag => (w.tags ?? []).includes(tag)) &&
          (!q || w.title.toLowerCase().includes(q)),
      )

      // Without limit/cursor the backend serves the legacy full history and
      // ignores the filter params (Compare/Trends/dashboard contract).
      const limit = Number(params.get('limit') ?? '0')
      if (!limit) return jsonResponse({ workouts, next_cursor: null })

      const cursor = params.get('cursor')
      const start = cursor ? matches.findIndex(w => String(w.id) === cursor) + 1 : 0
      const page = matches.slice(start, start + limit)
      const next = start + limit < matches.length ? String(page[page.length - 1].id) : null
      return jsonResponse({ workouts: page, next_cursor: next })
    }
    if (path === '/api/training/summary') {
      return jsonResponse({ summaries: [] })
    }
    if (path === '/api/training/events') {
      return Promise.resolve({ ok: true })
    }
    return Promise.resolve({ ok: false, json: () => Promise.resolve({}) })
  })
}

// listRequests returns just the workout-list requests, which are the ones the
// filters drive.
function listRequests() {
  return requests.filter(u => new URL(u, 'http://localhost').pathname === '/api/training/workouts')
}

function lastListRequest() {
  const all = listRequests()
  return new URL(all[all.length - 1], 'http://localhost')
}

function renderTraining() {
  return render(
    <MemoryRouter initialEntries={['/training']}>
      <Training />
    </MemoryRouter>,
  )
}

function stubEventSource() {
  vi.stubGlobal('EventSource', class {
    onopen: (() => void) | null = null
    addEventListener() {}
    close() {}
  })
}

describe('Training filter bar', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    // The page now caches its loaded pages per tab — leaking a snapshot between
    // tests would let one test hydrate from another's list.
    window.sessionStorage.clear()
    requests = []
    stubEventSource()
    vi.stubGlobal('fetch', mockFetch(WORKOUTS))
  })

  afterAll(() => {
    vi.unstubAllGlobals()
  })

  it('shows all workouts when no filter is active', async () => {
    renderTraining()
    for (const w of WORKOUTS) {
      expect(await screen.findByText(w.title)).toBeInTheDocument()
    }
    const first = lastListRequest()
    expect(first.searchParams.get('limit')).toBe('25')
    expect(first.searchParams.get('sport')).toBeNull()
    expect(first.searchParams.get('q')).toBeNull()
    expect(first.searchParams.getAll('tag')).toEqual([])
  })

  it('issues exactly one list request on mount', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    // Let the search debounce window elapse: an untouched search box must not
    // commit an (identical) empty query and refetch page 1.
    await new Promise(resolve => setTimeout(resolve, SEARCH_DEBOUNCE_MS + 100))
    expect(listRequests().length).toBe(1)
  })

  it('lists every tag the user has, not only tags on loaded workouts', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    // "deep-history" is on no loaded workout — it can only come from the
    // /api/training/tags endpoint.
    expect(await screen.findByRole('button', { name: 'deep-history' })).toBeInTheDocument()
    expect(requests.some(u => u.startsWith('/api/training/tags'))).toBe(true)
  })

  it('requests the backend with sport= and resets to page 1 when a sport is picked', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    const select = screen.getByLabelText('Filter by sport') as HTMLSelectElement
    fireEvent.change(select, { target: { value: 'cycling' } })

    await waitFor(() => {
      expect(lastListRequest().searchParams.get('sport')).toBe('cycling')
    })
    const req = lastListRequest()
    expect(req.searchParams.get('cursor')).toBeNull()

    expect(await screen.findByText('Easy Spin')).toBeInTheDocument()
    expect(screen.queryByText('Morning Run')).not.toBeInTheDocument()
    expect(screen.queryByText('Hill Intervals')).not.toBeInTheDocument()
    expect(screen.queryByText('Pool Laps')).not.toBeInTheDocument()
  })

  it('sends one tag param per selected tag and combines them with AND', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    fireEvent.click(screen.getByRole('button', { name: 'easy' }))
    await waitFor(() => {
      expect(lastListRequest().searchParams.getAll('tag')).toEqual(['easy'])
    })
    expect(await screen.findByText('Easy Spin')).toBeInTheDocument()
    expect(screen.queryByText('Hill Intervals')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'ai:recovery' }))
    await waitFor(() => {
      expect(lastListRequest().searchParams.getAll('tag')).toEqual(['easy', 'ai:recovery'])
    })
    expect(await screen.findByText('Morning Run')).toBeInTheDocument()
    expect(screen.queryByText('Easy Spin')).not.toBeInTheDocument()
  })

  it('debounces typing into a single q= request', async () => {
    renderTraining()
    await screen.findByText('Morning Run')
    const before = listRequests().length

    const input = screen.getByPlaceholderText(/search by title/i)
    fireEvent.change(input, { target: { value: 'p' } })
    fireEvent.change(input, { target: { value: 'po' } })
    fireEvent.change(input, { target: { value: 'poo' } })
    fireEvent.change(input, { target: { value: 'pool' } })

    await waitFor(() => {
      expect(lastListRequest().searchParams.get('q')).toBe('pool')
    })
    // Four keystrokes, one request.
    expect(listRequests().length).toBe(before + 1)

    expect(await screen.findByText('Pool Laps')).toBeInTheDocument()
    expect(screen.queryByText('Morning Run')).not.toBeInTheDocument()
  })

  it('combines sport + tag + query filters with AND in one request', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    fireEvent.change(screen.getByLabelText('Filter by sport'), { target: { value: 'running' } })
    fireEvent.click(screen.getByRole('button', { name: 'easy' }))
    fireEvent.change(screen.getByPlaceholderText(/search by title/i), { target: { value: 'morning' } })

    await waitFor(() => {
      const req = lastListRequest()
      expect(req.searchParams.get('sport')).toBe('running')
      expect(req.searchParams.getAll('tag')).toEqual(['easy'])
      expect(req.searchParams.get('q')).toBe('morning')
    })

    expect(await screen.findByText('Morning Run')).toBeInTheDocument()
    expect(screen.queryByText('Hill Intervals')).not.toBeInTheDocument()
    expect(screen.queryByText('Easy Spin')).not.toBeInTheDocument()
  })

  it('shows the no-matches state when the backend returns nothing, and clears back to page 1', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    fireEvent.change(screen.getByPlaceholderText(/search by title/i), {
      target: { value: 'nonexistent workout title xyz' },
    })

    expect(await screen.findByText('No workouts match your filters')).toBeInTheDocument()

    const clearBtn = screen.getAllByRole('button').find(b => b.textContent?.includes('Clear filters'))
    expect(clearBtn).toBeTruthy()
    fireEvent.click(clearBtn!)

    await waitFor(() => {
      const req = lastListRequest()
      expect(req.searchParams.get('q')).toBeNull()
      expect(req.searchParams.get('sport')).toBeNull()
      expect(req.searchParams.getAll('tag')).toEqual([])
      expect(req.searchParams.get('cursor')).toBeNull()
    })
    for (const w of WORKOUTS) {
      expect(await screen.findByText(w.title)).toBeInTheDocument()
    }
  })

  it('clears all filters when the clear button is clicked', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    fireEvent.change(screen.getByLabelText('Filter by sport'), { target: { value: 'swimming' } })

    expect(await screen.findByText('Pool Laps')).toBeInTheDocument()
    expect(screen.queryByText('Morning Run')).not.toBeInTheDocument()

    const clearBtn = screen.getAllByRole('button').find(b => b.textContent?.includes('Clear filters'))
    expect(clearBtn).toBeTruthy()
    fireEvent.click(clearBtn!)

    for (const w of WORKOUTS) {
      expect(await screen.findByText(w.title)).toBeInTheDocument()
    }
  })

  it('deselects a tag when clicked again', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    fireEvent.click(screen.getByRole('button', { name: 'easy' }))
    await waitFor(() => {
      expect(screen.queryByText('Hill Intervals')).not.toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: 'easy' }))
    expect(await screen.findByText('Hill Intervals')).toBeInTheDocument()
  })
})

describe('Training filtered pagination', () => {
  // 30 runs and 30 rides, interleaved: a filtered page can only be assembled by
  // the backend, and paging must walk matches rather than raw history.
  const MANY: Workout[] = []
  for (let i = 0; i < 30; i++) {
    MANY.push(makeWorkout({ id: i * 2 + 1, title: `Run ${i}`, sport: 'running', tags: ['easy'] }))
    MANY.push(makeWorkout({ id: i * 2 + 2, title: `Ride ${i}`, sport: 'cycling', tags: [] }))
  }

  beforeEach(() => {
    vi.restoreAllMocks()
    // The page now caches its loaded pages per tab — leaking a snapshot between
    // tests would let one test hydrate from another's list.
    window.sessionStorage.clear()
    requests = []
    stubEventSource()
    vi.stubGlobal('fetch', mockFetch(MANY, ['easy']))
  })

  afterAll(() => {
    vi.unstubAllGlobals()
  })

  it('sends the cursor together with the active filters when loading more', async () => {
    renderTraining()
    await screen.findByText('Run 0')

    fireEvent.change(screen.getByLabelText('Filter by sport'), { target: { value: 'running' } })
    await waitFor(() => {
      expect(lastListRequest().searchParams.get('sport')).toBe('running')
    })
    // 30 matching runs, page size 25 — a second page of matches remains.
    const loadMore = await screen.findByRole('button', { name: /load more/i })

    fireEvent.click(loadMore)
    await waitFor(() => {
      const req = lastListRequest()
      expect(req.searchParams.get('cursor')).toBeTruthy()
      expect(req.searchParams.get('sport')).toBe('running')
    })

    // The tail of the matching runs is appended, and the control disappears
    // once next_cursor is null for the filtered set.
    expect(await screen.findByText('Run 29')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: /load more/i })).not.toBeInTheDocument()
    })
    expect(screen.queryByText('Ride 0')).not.toBeInTheDocument()
  })

  it('drops an in-flight "load more" when the filters change underneath it', async () => {
    const base = mockFetch(MANY, ['easy'])
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    let cursorFetches = 0
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      const parsed = new URL(url, 'http://localhost')
      if (parsed.pathname === '/api/training/workouts' && parsed.searchParams.get('cursor')) {
        // Hold the append open so a filter change can land while it is still
        // in flight, with its cursor pointing into the old result set.
        cursorFetches++
        return gate.then(() => base(url))
      }
      return base(url)
    }))

    renderTraining()
    await screen.findByText('Run 0')

    fireEvent.change(screen.getByLabelText('Filter by sport'), { target: { value: 'running' } })
    const loadMore = await screen.findByRole('button', { name: /load more/i })
    fireEvent.click(loadMore)
    await waitFor(() => { expect(cursorFetches).toBe(1) })

    fireEvent.change(screen.getByLabelText('Filter by sport'), { target: { value: 'cycling' } })
    expect(await screen.findByText('Ride 0')).toBeInTheDocument()

    await act(async () => {
      release()
      await new Promise(resolve => setTimeout(resolve, 20))
    })

    // The superseded page of runs must not be appended on top of the rides.
    expect(screen.queryByText('Run 29')).not.toBeInTheDocument()
    expect(screen.queryByText('Run 0')).not.toBeInTheDocument()
    expect(screen.getByText('Ride 0')).toBeInTheDocument()
  })
})

describe('Training zero-workout user', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    // The page now caches its loaded pages per tab — leaking a snapshot between
    // tests would let one test hydrate from another's list.
    window.sessionStorage.clear()
    requests = []
    stubEventSource()
    vi.stubGlobal('fetch', mockFetch([], []))
  })

  afterAll(() => {
    vi.unstubAllGlobals()
  })

  it('shows the empty state when the user has no workouts', async () => {
    renderTraining()
    expect(await screen.findByText('No workouts yet')).toBeInTheDocument()
    expect(screen.queryByText('Workouts')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /trends/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /compare/i })).not.toBeInTheDocument()
  })
})

describe('Training error handling', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    // The page now caches its loaded pages per tab — leaking a snapshot between
    // tests would let one test hydrate from another's list.
    window.sessionStorage.clear()
    requests = []
    stubEventSource()
  })

  afterAll(() => {
    vi.unstubAllGlobals()
  })

  it('clears stale workouts when a filtered request fails', async () => {
    const base = mockFetch(WORKOUTS)
    vi.stubGlobal('fetch', base)

    renderTraining()
    await screen.findByText('Morning Run')

    // Make the next workout-list request fail.
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      const parsed = new URL(url, 'http://localhost')
      if (parsed.pathname === '/api/training/workouts') {
        return Promise.resolve({ ok: false, json: () => Promise.resolve({ error: 'boom' }) })
      }
      return base(url)
    }))

    fireEvent.change(screen.getByLabelText('Filter by sport'), { target: { value: 'cycling' } })

    await waitFor(() => {
      expect(screen.getByText('Failed to load workouts')).toBeInTheDocument()
    })
    // Stale workouts from the previous filter must not remain visible.
    expect(screen.queryByText('Morning Run')).not.toBeInTheDocument()
    expect(screen.queryByText('Hill Intervals')).not.toBeInTheDocument()
  })
})

describe('Training list cache', () => {
  // A history deep enough to need paging, ordered the way the list endpoint
  // orders it: started_at DESC (id DESC as the tiebreak). Index 0 is newest.
  const HISTORY: Workout[] = []
  for (let i = 0; i < 60; i++) {
    HISTORY.push(makeWorkout({
      id: 1000 - i,
      title: `History ${i}`,
      sport: 'running',
      started_at: new Date(Date.UTC(2026, 0, 1) - i * 86_400_000).toISOString(),
    }))
  }

  // What the user had loaded when they clicked into a workout: two pages of
  // history, a cursor pointing past them, and a scroll offset.
  const CACHED = HISTORY.slice(0, 50)
  const CACHED_CURSOR = String(CACHED[CACHED.length - 1].id)

  function primeCache(overrides: Record<string, unknown> = {}, userId = '1') {
    window.sessionStorage.setItem(trainingListCacheKey(userId), JSON.stringify({
      version: TRAINING_LIST_CACHE_VERSION,
      userId,
      savedAt: Date.now(),
      workouts: CACHED,
      nextCursor: CACHED_CURSOR,
      latestWorkoutId: HISTORY[0].id,
      scrollY: 420,
      ...overrides,
    }))
  }

  // Patched onto window itself (not just globalThis) because that is what the
  // page calls, and happy-dom keeps the two property slots separate.
  let scrollToSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.restoreAllMocks()
    window.sessionStorage.clear()
    requests = []
    stubEventSource()
    scrollToSpy = vi.fn()
    Object.defineProperty(window, 'scrollTo', {
      value: scrollToSpy,
      configurable: true,
      writable: true,
    })
    vi.stubGlobal('fetch', mockFetch(HISTORY, []))
  })

  afterAll(() => {
    vi.unstubAllGlobals()
  })

  it('renders the cached pages on the first paint, without a loading skeleton', () => {
    primeCache()
    const { container } = renderTraining()

    // No await: the restored rows must be there before any request resolves.
    expect(screen.getByText('History 0')).toBeInTheDocument()
    expect(screen.getByText('History 49')).toBeInTheDocument()
    expect(container.querySelector('.animate-pulse')).toBeNull()
  })

  it('keeps the cached cursor so "Load more" continues where the user left off', async () => {
    // The refreshed page 1 carries an edited title, which is how the test knows
    // the background refresh has landed.
    const edited = HISTORY.map(w => w.id === HISTORY[0].id ? { ...w, title: 'History 0 renamed' } : w)
    vi.stubGlobal('fetch', mockFetch(edited, []))

    primeCache()
    renderTraining()

    // The edit reconciles in place — no second row for the same workout.
    expect(await screen.findByText('History 0 renamed')).toBeInTheDocument()
    expect(screen.queryByText('History 0')).not.toBeInTheDocument()
    expect(screen.getByText('History 49')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /load more/i }))

    await waitFor(() => {
      expect(lastListRequest().searchParams.get('cursor')).toBe(CACHED_CURSOR)
    })
    expect(await screen.findByText('History 59')).toBeInTheDocument()
    expect(screen.getByText('History 49')).toBeInTheDocument()
  })

  it('merges a workout uploaded while away without duplicating restored rows', async () => {
    const uploaded = makeWorkout({
      id: 2000,
      title: 'New Upload',
      sport: 'running',
      started_at: '2026-06-01T08:00:00Z',
    })
    vi.stubGlobal('fetch', mockFetch([uploaded, ...HISTORY], []))

    primeCache()
    renderTraining()

    expect(await screen.findByText('New Upload')).toBeInTheDocument()
    // Every restored row survives, exactly once each.
    expect(screen.getAllByText('History 0')).toHaveLength(1)
    expect(screen.getAllByText('History 24')).toHaveLength(1)
    expect(screen.getByText('History 49')).toBeInTheDocument()
  })

  it('drops a workout deleted while away', async () => {
    vi.stubGlobal('fetch', mockFetch(HISTORY.filter(w => w.title !== 'History 5'), []))

    primeCache()
    renderTraining()

    await waitFor(() => {
      expect(screen.queryByText('History 5')).not.toBeInTheDocument()
    })
    // Only the deleted workout goes: its neighbours and the deeper pages stay.
    expect(screen.getByText('History 4')).toBeInTheDocument()
    expect(screen.getByText('History 6')).toBeInTheDocument()
    expect(screen.getByText('History 49')).toBeInTheDocument()
  })

  it('replaces the restored list when the refreshed page 1 is detached from it', async () => {
    // More workouts imported while away than a page holds: page 1 of the refresh
    // reaches back to none of the restored rows, so the rows in between were
    // never fetched. Folding would hide them behind the restored cursor, which
    // points past the restored pages — the page wins and brings its own cursor.
    const imported: Workout[] = []
    for (let i = 0; i < 30; i++) {
      imported.push(makeWorkout({
        id: 2000 - i,
        title: `Import ${i}`,
        sport: 'running',
        started_at: new Date(Date.UTC(2026, 5, 1) - i * 86_400_000).toISOString(),
      }))
    }
    vi.stubGlobal('fetch', mockFetch([...imported, ...HISTORY], []))

    primeCache()
    renderTraining()

    expect(await screen.findByText('Import 0')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByText('History 0')).not.toBeInTheDocument()
    })
    // Nothing is stranded: the restored rows are reachable again by paging on.
    fireEvent.click(screen.getByRole('button', { name: /load more/i }))
    expect(await screen.findByText('Import 29')).toBeInTheDocument()
    expect(screen.getByText('History 0')).toBeInTheDocument()
  })

  it('does a normal fresh load when the snapshot is older than the TTL', async () => {
    primeCache({ savedAt: Date.now() - TRAINING_LIST_CACHE_TTL_MS - 1000 })
    const { container } = renderTraining()

    expect(container.querySelector('.animate-pulse')).not.toBeNull()
    expect(await screen.findByText('History 0')).toBeInTheDocument()
    // Only page 1 — the stale deeper pages are not restored.
    expect(screen.queryByText('History 49')).not.toBeInTheDocument()
    expect(screen.queryByText('History 30')).not.toBeInTheDocument()
  })

  it('does a normal fresh load after a reload navigation', async () => {
    vi.spyOn(performance, 'getEntriesByType').mockReturnValue([
      { type: 'reload' } as unknown as PerformanceEntry,
    ])
    primeCache()
    renderTraining()

    expect(await screen.findByText('History 0')).toBeInTheDocument()
    expect(screen.queryByText('History 49')).not.toBeInTheDocument()
  })

  it('never restores another user\'s snapshot', async () => {
    primeCache({}, '2')
    renderTraining()

    expect(await screen.findByText('History 0')).toBeInTheDocument()
    expect(screen.queryByText('History 49')).not.toBeInTheDocument()
  })

  it('restores the cached scroll offset once the rows are rendered', () => {
    primeCache({ scrollY: 640 })
    renderTraining()

    expect(scrollToSpy).toHaveBeenCalledWith({ top: 640, behavior: 'auto' })
  })

  it('does not scroll on a fresh load', async () => {
    renderTraining()
    expect(await screen.findByText('History 0')).toBeInTheDocument()
    expect(scrollToSpy).not.toHaveBeenCalled()
  })

  it('persists the loaded pages and the scroll offset for the next mount', async () => {
    const { unmount } = renderTraining()
    await screen.findByText('History 0')

    fireEvent.click(await screen.findByRole('button', { name: /load more/i }))
    expect(await screen.findByText('History 30')).toBeInTheDocument()

    // Leaving for a workout detail flushes the offset even mid-debounce.
    Object.defineProperty(window, 'scrollY', { value: 512, configurable: true })
    unmount()

    const snapshot = readTrainingListCache('1')
    expect(snapshot).not.toBeNull()
    expect(snapshot!.workouts.length).toBe(50)
    expect(snapshot!.nextCursor).not.toBeNull()
    expect(snapshot!.scrollY).toBe(512)
  })

  it('drops the snapshot while a filter is active, since filters are not restored', async () => {
    primeCache()
    renderTraining()

    expect(await screen.findByText('History 0')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Filter by sport'), { target: { value: 'cycling' } })

    await waitFor(() => {
      expect(readTrainingListCache('1')).toBeNull()
    })
    // A filtered load replaces the restored list rather than merging into it.
    expect(screen.queryByText('History 49')).not.toBeInTheDocument()
  })

  it('falls back to a fresh load when sessionStorage throws', async () => {
    primeCache()
    vi.spyOn(window.sessionStorage, 'getItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError')
    })
    vi.spyOn(window.sessionStorage, 'setItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError')
    })

    const { unmount } = renderTraining()
    expect(await screen.findByText('History 0')).toBeInTheDocument()
    expect(screen.queryByText('History 49')).not.toBeInTheDocument()
    expect(() => unmount()).not.toThrow()
  })
})

describe('Training new-workouts banner', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    // The page now caches its loaded pages per tab — leaking a snapshot between
    // tests would let one test hydrate from another's list.
    window.sessionStorage.clear()
    requests = []
  })

  afterAll(() => {
    vi.unstubAllGlobals()
  })

  // A short history in the order the list endpoint serves it: started_at DESC.
  const BANNER_WORKOUTS: Workout[] = [
    makeWorkout({ id: 10, title: 'Recent Run', sport: 'running', started_at: '2026-03-03T08:00:00Z' }),
    makeWorkout({ id: 9, title: 'Older Ride', sport: 'cycling', started_at: '2026-03-02T08:00:00Z' }),
    makeWorkout({ id: 8, title: 'Oldest Swim', sport: 'swimming', started_at: '2026-03-01T08:00:00Z' }),
  ]

  // A history deep enough that page 1 (25) does not cover it, newest first.
  const BANNER_HISTORY: Workout[] = []
  for (let i = 0; i < 30; i++) {
    BANNER_HISTORY.push(makeWorkout({
      id: 500 - i,
      title: `Deep ${i}`,
      sport: 'running',
      started_at: new Date(Date.UTC(2026, 1, 1) - i * 86_400_000).toISOString(),
    }))
  }

  // The ids of the rendered workout rows, top to bottom. Each row is a link to
  // /training/<id>, so the hrefs are the rendered order.
  function renderedWorkoutIds(): number[] {
    return screen.getAllByRole('link')
      .map(a => a.getAttribute('href') ?? '')
      .filter(href => /^\/training\/\d+$/.test(href))
      .map(href => Number(href.slice('/training/'.length)))
  }

  // Renders with `initial` loaded, swaps the backend over to `updated`, fires the
  // SSE event that raises the banner and returns the banner button.
  async function raiseBanner(initial: Workout[], updated: Workout[], latestId: number) {
    let onWorkoutNew: ((e: MessageEvent) => void) | null = null
    // No auto-open: the filter-independent effect sets the baseline
    // latestWorkoutIdRef first, so the event trips the banner via a genuine
    // id > seen comparison.
    vi.stubGlobal('EventSource', class {
      onopen: (() => void) | null = null
      addEventListener(event: string, handler: (e: MessageEvent) => void) {
        if (event === 'workout_new') onWorkoutNew = handler
      }
      close() {}
    })
    vi.stubGlobal('fetch', mockFetch(initial))
    renderTraining()
    // Wait for the mount load to land before swapping the backend — otherwise a
    // late response could overwrite what the banner click loads.
    if (initial.length > 0) await screen.findByText(initial[0].title)
    else await screen.findByText('No workouts yet')

    vi.stubGlobal('fetch', mockFetch(updated))
    await act(async () => {
      onWorkoutNew?.(new MessageEvent('workout_new', {
        data: JSON.stringify({ latest_id: latestId }),
      }))
    })
    const banner = await screen.findByText(/New workouts available/)
    const fire = (id: number) => act(async () => {
      onWorkoutNew?.(new MessageEvent('workout_new', {
        data: JSON.stringify({ latest_id: id }),
      }))
    })
    return { banner, fire }
  }

  it('shows banner when SSE fires and loads new workouts on click', async () => {
    const newWorkout = makeWorkout({
      id: 99,
      title: 'New Upload',
      sport: 'running',
      started_at: '2026-12-01T08:00:00Z',
    })
    // Baseline latest id is 4, so 99 trips the banner.
    const { banner } = await raiseBanner(WORKOUTS, [newWorkout, ...WORKOUTS], 99)
    expect(banner).toBeInTheDocument()

    fireEvent.click(banner)

    expect(await screen.findByText('New Upload')).toBeInTheDocument()
    // Banner should be dismissed.
    await waitFor(() => {
      expect(screen.queryByText(/New workouts available/)).not.toBeInTheDocument()
    })
  })

  it('inserts a backdated import in its chronological slot, not at the top', async () => {
    const backdated = makeWorkout({
      id: 99,
      title: 'Backdated Import',
      sport: 'running',
      // Between "Older Ride" and "Oldest Swim" — a .fit imported long after the
      // activity happened.
      started_at: '2026-03-01T20:00:00Z',
    })
    const updated = [BANNER_WORKOUTS[0], BANNER_WORKOUTS[1], backdated, BANNER_WORKOUTS[2]]

    const { banner } = await raiseBanner(BANNER_WORKOUTS, updated, 99)
    fireEvent.click(banner)

    expect(await screen.findByText('Backdated Import')).toBeInTheDocument()
    await waitFor(() => {
      expect(renderedWorkoutIds()).toEqual([10, 9, 99, 8])
    })
  })

  it('breaks started_at ties by id DESC', async () => {
    const sameDay = makeWorkout({
      id: 99,
      title: 'Same Second',
      sport: 'running',
      started_at: BANNER_WORKOUTS[1].started_at,
    })
    const updated = [BANNER_WORKOUTS[0], sameDay, BANNER_WORKOUTS[1], BANNER_WORKOUTS[2]]

    const { banner } = await raiseBanner(BANNER_WORKOUTS, updated, 99)
    fireEvent.click(banner)

    expect(await screen.findByText('Same Second')).toBeInTheDocument()
    await waitFor(() => {
      expect(renderedWorkoutIds()).toEqual([10, 99, 9, 8])
    })
  })

  it('replaces an edited workout in place instead of duplicating it', async () => {
    const upload = makeWorkout({
      id: 99,
      title: 'New Upload',
      sport: 'running',
      started_at: '2026-03-04T08:00:00Z',
    })
    const updated = [
      upload,
      BANNER_WORKOUTS[0],
      { ...BANNER_WORKOUTS[1], title: 'Older Ride renamed' },
      BANNER_WORKOUTS[2],
    ]

    const { banner } = await raiseBanner(BANNER_WORKOUTS, updated, 99)
    fireEvent.click(banner)

    expect(await screen.findByText('Older Ride renamed')).toBeInTheDocument()
    expect(screen.queryByText('Older Ride')).not.toBeInTheDocument()
    expect(renderedWorkoutIds().filter(id => id === 9)).toHaveLength(1)
    expect(renderedWorkoutIds()).toEqual([99, 10, 9, 8])
  })

  it('drops a workout deleted elsewhere when page 1 is the whole result set', async () => {
    const upload = makeWorkout({
      id: 99,
      title: 'New Upload',
      sport: 'running',
      started_at: '2026-03-04T08:00:00Z',
    })
    // "Older Ride" was deleted while the page was open, and page 1 covers
    // everything (next_cursor null), so its absence is authoritative.
    const updated = [upload, BANNER_WORKOUTS[0], BANNER_WORKOUTS[2]]

    const { banner } = await raiseBanner(BANNER_WORKOUTS, updated, 99)
    fireEvent.click(banner)

    expect(await screen.findByText('New Upload')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByText('Older Ride')).not.toBeInTheDocument()
    })
    expect(screen.getByText('Oldest Swim')).toBeInTheDocument()
  })

  it('leaves workouts older than the page-1 window alone and keeps the cursor', async () => {
    const upload = makeWorkout({
      id: 900,
      title: 'New Upload',
      sport: 'running',
      started_at: '2026-03-01T08:00:00Z',
    })
    const { banner, fire } = await raiseBanner(BANNER_HISTORY, [upload, ...BANNER_HISTORY], 900)

    // Page in the rest of the history first, so rows below the page-1 window are
    // on screen when the banner is clicked. That also exhausts the cursor, which
    // is what makes an unwanted cursor refresh visible: page 1 comes back with a
    // non-null next_cursor.
    fireEvent.click(screen.getByRole('button', { name: /load more/i }))
    expect(await screen.findByText('Deep 29')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /load more/i })).not.toBeInTheDocument()

    fireEvent.click(banner)

    expect(await screen.findByText('New Upload')).toBeInTheDocument()
    // Deep 24..29 fall outside the refreshed page-1 window — page 1 says nothing
    // about them, so they stay.
    expect(screen.getByText('Deep 24')).toBeInTheDocument()
    expect(screen.getByText('Deep 29')).toBeInTheDocument()
    expect(renderedWorkoutIds()).toHaveLength(BANNER_HISTORY.length + 1)
    // The cursor is untouched: "Load more" must not come back for pages that are
    // already on screen.
    expect(screen.queryByRole('button', { name: /load more/i })).not.toBeInTheDocument()

    // The baseline advanced, so the same id does not re-raise the banner.
    await fire(900)
    expect(screen.queryByText(/New workouts available/)).not.toBeInTheDocument()
  })

  it('replaces the list when a bulk import leaves page 1 detached from it', async () => {
    // A bulk import bigger than a page: the refreshed page 1 shares nothing with
    // the rows on screen, so the workouts between them were never fetched. The
    // kept cursor points past the loaded pages, so folding would strand the
    // in-between rows — page 1 replaces the list and brings its own cursor.
    const imported: Workout[] = []
    for (let i = 0; i < 30; i++) {
      imported.push(makeWorkout({
        id: 900 - i,
        title: `Imported ${i}`,
        sport: 'running',
        started_at: new Date(Date.UTC(2026, 5, 1) - i * 86_400_000).toISOString(),
      }))
    }
    const { banner } = await raiseBanner(BANNER_HISTORY, [...imported, ...BANNER_HISTORY], 900)

    fireEvent.click(banner)

    expect(await screen.findByText('Imported 0')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByText('Deep 0')).not.toBeInTheDocument()
    })
    expect(renderedWorkoutIds()).toHaveLength(25)
    // Nothing is stranded: the rest of the import and the old rows are reachable
    // by paging on from the adopted cursor.
    fireEvent.click(screen.getByRole('button', { name: /load more/i }))
    expect(await screen.findByText('Imported 29')).toBeInTheDocument()
    expect(screen.getByText('Deep 0')).toBeInTheDocument()
  })

  it('takes the cursor from the response when the list was empty', async () => {
    const { banner } = await raiseBanner([], BANNER_HISTORY, 500)
    const before = requests.length
    fireEvent.click(banner)

    expect(await screen.findByText('Deep 0')).toBeInTheDocument()
    expect(screen.getByText('Deep 24')).toBeInTheDocument()
    // 30 workouts, 25 per page: the response carries a cursor, so paging on is
    // reachable without a full reload.
    const loadMore = await screen.findByRole('button', { name: /load more/i })
    fireEvent.click(loadMore)
    expect(await screen.findByText('Deep 29')).toBeInTheDocument()

    // Summaries and tags refresh alongside the list.
    const after = requests.slice(before)
    expect(after.some(u => u.startsWith('/api/training/summary'))).toBe(true)
    expect(after.some(u => u.startsWith('/api/training/tags'))).toBe(true)
  })

  it('discards the fetched list when a filter change supersedes the fetch', async () => {
    const upload = makeWorkout({
      id: 99,
      title: 'New Upload',
      sport: 'running',
      started_at: '2026-03-04T08:00:00Z',
    })
    const updated = [upload, ...BANNER_WORKOUTS]
    const { banner } = await raiseBanner(BANNER_WORKOUTS, updated, 99)

    // Hold the banner's page-1 response open so a filter change can land first.
    const base = mockFetch(updated)
    let release: (() => void) | null = null
    let gateArmed = true
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      const path = new URL(url, 'http://localhost').pathname
      if (path === '/api/training/workouts' && gateArmed) {
        gateArmed = false
        return new Promise(resolve => { release = () => resolve(base(url)) })
      }
      return base(url)
    }))

    fireEvent.click(banner)
    fireEvent.change(screen.getByLabelText('Filter by sport'), { target: { value: 'cycling' } })
    // The running workouts disappearing is how the test knows the filtered load
    // landed while the banner's page 1 was still in flight.
    await waitFor(() => {
      expect(screen.queryByText('Recent Run')).not.toBeInTheDocument()
    })
    expect(screen.getByText('Older Ride')).toBeInTheDocument()

    await act(async () => { release?.() })

    // The superseded response must not push running workouts into the cycling
    // result set.
    expect(screen.queryByText('New Upload')).not.toBeInTheDocument()
    expect(screen.queryByText('Recent Run')).not.toBeInTheDocument()
    expect(screen.getByText('Older Ride')).toBeInTheDocument()
  })
})
