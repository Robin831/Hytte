// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterAll } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
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

vi.mock('../auth', () => ({
  useAuth: () => ({
    user: { id: 1, email: 'a@b.c', name: 'Tester', is_admin: true, features: {} },
    hasFeature: () => false,
  }),
}))

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

function mockFetch(workouts: Workout[]) {
  return vi.fn().mockImplementation((url: string) => {
    if (url.includes('/api/training/workouts/latest')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ latest_id: workouts.length > 0 ? Math.max(...workouts.map(w => w.id)) : 0 }),
      })
    }
    if (url.includes('/api/training/workouts')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ workouts, next_cursor: null }),
      })
    }
    if (url.includes('/api/training/summary')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ summaries: [] }),
      })
    }
    if (url.includes('/api/training/events')) {
      return Promise.resolve({ ok: true })
    }
    return Promise.resolve({ ok: false, json: () => Promise.resolve({}) })
  })
}

function renderTraining() {
  return render(
    <MemoryRouter initialEntries={['/training']}>
      <Training />
    </MemoryRouter>,
  )
}

describe('Training filter bar', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    // Stub EventSource globally
    vi.stubGlobal('EventSource', class {
      onopen: (() => void) | null = null
      addEventListener() {}
      close() {}
    })
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
  })

  it('filters by sport', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    const select = screen.getByLabelText('Filter by sport') as HTMLSelectElement
    fireEvent.change(select, { target: { value: 'cycling' } })

    expect(screen.getByText('Easy Spin')).toBeInTheDocument()
    expect(screen.queryByText('Morning Run')).not.toBeInTheDocument()
    expect(screen.queryByText('Hill Intervals')).not.toBeInTheDocument()
    expect(screen.queryByText('Pool Laps')).not.toBeInTheDocument()
  })

  it('filters by title search query', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    const input = screen.getByPlaceholderText(/search by title/i)
    fireEvent.change(input, { target: { value: 'pool' } })

    expect(screen.getByText('Pool Laps')).toBeInTheDocument()
    expect(screen.queryByText('Morning Run')).not.toBeInTheDocument()
  })

  it('filters by tag (AND across multiple tags)', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    const easyBtn = screen.getByRole('button', { name: /easy/i, pressed: false })
    fireEvent.click(easyBtn)

    expect(screen.getByText('Morning Run')).toBeInTheDocument()
    expect(screen.getByText('Easy Spin')).toBeInTheDocument()
    expect(screen.queryByText('Hill Intervals')).not.toBeInTheDocument()

    const aiRecoveryBtn = screen.getByRole('button', { name: /ai:recovery/i })
    fireEvent.click(aiRecoveryBtn)

    expect(screen.getByText('Morning Run')).toBeInTheDocument()
    expect(screen.queryByText('Easy Spin')).not.toBeInTheDocument()
  })

  it('combines sport + tag + query filters with AND', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    const select = screen.getByLabelText('Filter by sport') as HTMLSelectElement
    fireEvent.change(select, { target: { value: 'running' } })

    const easyBtn = screen.getByRole('button', { name: /easy/i, pressed: false })
    fireEvent.click(easyBtn)

    const input = screen.getByPlaceholderText(/search by title/i)
    fireEvent.change(input, { target: { value: 'morning' } })

    expect(screen.getByText('Morning Run')).toBeInTheDocument()
    expect(screen.queryByText('Hill Intervals')).not.toBeInTheDocument()
    expect(screen.queryByText('Easy Spin')).not.toBeInTheDocument()
  })

  it('shows empty state when no workouts match and offers clear-filters', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    const input = screen.getByPlaceholderText(/search by title/i)
    fireEvent.change(input, { target: { value: 'nonexistent workout title xyz' } })

    expect(screen.getByText('No workouts match your filters')).toBeInTheDocument()

    const clearBtn = screen.getAllByRole('button').find(b => b.textContent?.includes('Clear filters'))
    expect(clearBtn).toBeTruthy()
    fireEvent.click(clearBtn!)

    for (const w of WORKOUTS) {
      expect(screen.getByText(w.title)).toBeInTheDocument()
    }
  })

  it('clears all filters when the clear button is clicked', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    const select = screen.getByLabelText('Filter by sport') as HTMLSelectElement
    fireEvent.change(select, { target: { value: 'swimming' } })

    expect(screen.getByText('Pool Laps')).toBeInTheDocument()
    expect(screen.queryByText('Morning Run')).not.toBeInTheDocument()

    const clearBtn = screen.getAllByRole('button').find(b => b.textContent?.includes('Clear filters'))
    expect(clearBtn).toBeTruthy()
    fireEvent.click(clearBtn!)

    for (const w of WORKOUTS) {
      expect(screen.getByText(w.title)).toBeInTheDocument()
    }
  })

  it('deselects a tag when clicked again', async () => {
    renderTraining()
    await screen.findByText('Morning Run')

    const easyBtn = screen.getByRole('button', { name: /easy/i, pressed: false })
    fireEvent.click(easyBtn)
    expect(screen.queryByText('Hill Intervals')).not.toBeInTheDocument()

    fireEvent.click(easyBtn)
    expect(screen.getByText('Hill Intervals')).toBeInTheDocument()
  })
})
