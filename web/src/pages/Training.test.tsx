// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterAll } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Training from './Training'
import type { Workout } from '../types/training'

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

      const limit = Number(params.get('limit') ?? '0')
      if (!limit) return jsonResponse({ workouts: matches, next_cursor: null })

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
})
