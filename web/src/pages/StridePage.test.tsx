// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import StridePage from './StridePage'
import enStride from '../../public/locales/en/stride.json'
import type { DayPlan } from '../types/stride'

// ── StrideChatDrawer mock ─────────────────────────────────────────────────────

const chatDrawerCallbacks = vi.hoisted(() => ({
  onPlanUpdated: null as ((plan: DayPlan[]) => void) | null,
}))

vi.mock('../components/stride/StrideChatDrawer', () => ({
  default: ({ onPlanUpdated }: { planId: number; onPlanUpdated: (plan: DayPlan[]) => void }) => {
    chatDrawerCallbacks.onPlanUpdated = onPlanUpdated
    return null
  },
}))

// ── Translation helpers ───────────────────────────────────────────────────────

type JsonValue = string | number | boolean | null | JsonObject | JsonValue[]
interface JsonObject { [key: string]: JsonValue }

function resolveKey(obj: JsonObject, parts: string[]): JsonValue | undefined {
  const [head, ...rest] = parts
  const val = obj[head]
  if (rest.length === 0) return val
  if (val && typeof val === 'object' && !Array.isArray(val)) {
    return resolveKey(val as JsonObject, rest)
  }
  return undefined
}

function makeT(translations: JsonObject) {
  return function t(key: string, opts?: Record<string, unknown>): string {
    if (opts?.defaultValue && typeof opts.defaultValue === 'string') return opts.defaultValue

    if (opts?.count !== undefined) {
      const suffix = Number(opts.count) === 1 ? '_one' : '_other'
      const pluralVal = resolveKey(translations, (key + suffix).split('.'))
      if (typeof pluralVal === 'string') {
        return pluralVal.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
      }
    }

    const val = resolveKey(translations, key.split('.'))
    if (typeof val === 'string') {
      if (opts) {
        return val.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
      }
      return val
    }
    return key
  }
}

vi.mock('react-i18next', () => {
  const cache = new Map<string, ReturnType<typeof makeT>>()
  function getT(ns: string, translations: JsonObject) {
    if (!cache.has(ns)) cache.set(ns, makeT(translations))
    return cache.get(ns)!
  }
  return {
    useTranslation: (ns?: string) => ({
      t: ns === 'stride'
        ? getT('stride', enStride as unknown as JsonObject)
        : getT('__empty__', {}),
      i18n: { language: 'en' },
    }),
    Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
    initReactI18next: { type: '3rdParty', init: () => {} },
  }
})

vi.mock('react-markdown', () => ({
  default: ({ children }: { children: string }) => children,
}))
vi.mock('remark-gfm', () => ({ default: () => {} }))
vi.mock('react-syntax-highlighter', () => ({
  Prism: ({ children }: { children: string }) => children,
}))
vi.mock('react-syntax-highlighter/dist/esm/styles/prism', () => ({ vscDarkPlus: {} }))

vi.mock('lucide-react', () => ({
  Trash2: () => null,
  Plus: () => null,
  Trophy: () => null,
  Zap: () => null,
  ChevronDown: () => null,
  ChevronUp: () => null,
  RefreshCw: () => null,
  CheckCircle2: () => null,
  Circle: () => null,
  AlertTriangle: () => null,
  XCircle: () => null,
  History: () => null,
  Flag: () => null,
  MessageCircle: () => null,
  Send: () => null,
  Loader2: () => null,
  Bot: () => null,
  User: () => null,
  X: () => null,
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: (date: Date | string, options?: Intl.DateTimeFormatOptions) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return d.toLocaleDateString('en', options)
  },
  formatDateTime: (date: Date | string) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return d.toLocaleString('en')
  },
}))

// ── Test data ─────────────────────────────────────────────────────────────────

const RACE = {
  id: 1,
  user_id: 1,
  name: 'Bergen City Marathon',
  date: '2099-06-15',
  distance_m: 42195,
  target_time: 10800,
  priority: 'A',
  notes: '',
  result_time: null,
  created_at: '2024-01-01T00:00:00Z',
}

const PAST_RACE = {
  id: 2,
  user_id: 1,
  name: 'Old Race',
  date: '2020-01-01',
  distance_m: 10000,
  target_time: null,
  priority: 'C',
  notes: '',
  result_time: 3600,
  created_at: '2019-12-01T00:00:00Z',
}

// Build target dates relative to "today" so tests stay deterministic regardless
// of when they run — Coach Notes now splits on a rolling 7-day window.
function isoDate(daysFromToday: number): string {
  const d = new Date()
  d.setDate(d.getDate() + daysFromToday)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const NOTE = {
  id: 1,
  user_id: 1,
  plan_id: null,
  content: 'Feeling good this week',
  target_date: isoDate(0),
  consumed_at: null,
  consumed_by: null,
  scope: 'any',
  created_at: '2024-01-15T10:00:00Z',
}

const OLDER_NOTE = {
  id: 2,
  user_id: 1,
  plan_id: null,
  content: 'Old reflection from months ago',
  target_date: isoDate(-60),
  consumed_at: null,
  consumed_by: null,
  scope: 'any',
  created_at: '2023-11-15T10:00:00Z',
}

const OLDER_CONSUMED_NOTE = {
  id: 3,
  user_id: 1,
  plan_id: null,
  content: 'Long-since-consumed weekly note',
  target_date: isoDate(-30),
  consumed_at: '2023-12-15T10:00:00Z',
  consumed_by: 'weekly',
  scope: 'weekly',
  created_at: '2023-12-01T10:00:00Z',
}

// ── Fetch mock ────────────────────────────────────────────────────────────────

type FetchOverrides = {
  races?: unknown
  notes?: unknown
}

function makeFetchMock(overrides: FetchOverrides = {}) {
  return vi.fn((url: string) => {
    const makeResponse = (data: unknown, ok = true) =>
      Promise.resolve({ ok, json: () => Promise.resolve(data) } as Response)

    if (url.includes('/api/stride/races')) {
      return makeResponse(overrides.races ?? { races: [RACE] })
    }
    if (url.includes('/api/stride/notes')) {
      return makeResponse(overrides.notes ?? { notes: [NOTE] })
    }
    return makeResponse({})
  })
}

function renderPage() {
  return render(
    <MemoryRouter>
      <StridePage />
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('StridePage – loading and empty states', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('shows loading state while data is fetching', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    renderPage()
    expect(screen.getAllByText('Loading...').length).toBeGreaterThan(0)
  })

  it('shows empty race state when there are no races', async () => {
    vi.stubGlobal('fetch', makeFetchMock({ races: { races: [] }, notes: { notes: [] } }))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('No upcoming races. Add a race to get started.')).toBeInTheDocument()
    })
  })

  it('shows empty notes state when there are no notes', async () => {
    vi.stubGlobal('fetch', makeFetchMock({ races: { races: [] }, notes: { notes: [] } }))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('No notes yet. Add a note for your AI coach.')).toBeInTheDocument()
    })
  })
})

describe('StridePage – rendering data', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', makeFetchMock())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('renders the page title', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Stride')).toBeInTheDocument()
    })
  })

  it('renders race name', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getAllByText('Bergen City Marathon').length).toBeGreaterThan(0)
    })
  })

  it('renders note content', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Feeling good this week')).toBeInTheDocument()
    })
  })

  it('renders section headings', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Race Calendar')).toBeInTheDocument()
      expect(screen.getByText('Coach Notes')).toBeInTheDocument()
    })
  })
})

describe('StridePage – past races', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('shows past races in collapsible section', async () => {
    vi.stubGlobal('fetch', makeFetchMock({ races: { races: [PAST_RACE] }, notes: { notes: [] } }))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('1 past race')).toBeInTheDocument()
    })
  })
})

describe('StridePage – race form', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', makeFetchMock())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('shows the add race form when button is clicked', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Add Race')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Add Race'))

    expect(screen.getByLabelText('Race name')).toBeInTheDocument()
    expect(screen.getByLabelText('Date')).toBeInTheDocument()
  })

  it('hides form when cancel is clicked', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Add Race')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Add Race'))
    expect(screen.getByLabelText('Race name')).toBeInTheDocument()

    fireEvent.click(screen.getByText('Cancel'))
    expect(screen.queryByLabelText('Race name')).not.toBeInTheDocument()
  })

  it('shows create error when race creation fails', async () => {
    const failFetch = vi.fn((url: string, init?: RequestInit) => {
      if (url.includes('/api/stride/races') && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          json: () => Promise.resolve({ error: 'Failed to create race' }),
        } as Response)
      }
      if (url.includes('/api/stride/races')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ races: [] }) } as Response)
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ notes: [] }) } as Response)
    })
    vi.stubGlobal('fetch', failFetch)
    renderPage()

    // Wait for initial load to complete
    await waitFor(() => {
      expect(screen.getByText('No upcoming races. Add a race to get started.')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Add Race'))

    await waitFor(() => {
      expect(screen.getByLabelText('Race name')).toBeInTheDocument()
    })

    fireEvent.change(screen.getByLabelText('Race name'), { target: { value: 'Test Race' } })
    fireEvent.change(screen.getByLabelText('Date'), { target: { value: '2099-01-01' } })
    fireEvent.change(screen.getByLabelText('Distance (km)'), { target: { value: '10' } })

    // Submit using the form element directly
    const form = screen.getByLabelText('Race name').closest('form')!
    await act(async () => {
      fireEvent.submit(form)
    })

    await waitFor(() => {
      expect(screen.getByText('Failed to create race')).toBeInTheDocument()
    }, { timeout: 3000 })
  })
})

describe('StridePage – delete race', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('removes race from list on successful delete', async () => {
    const successFetch = vi.fn((url: string, init?: RequestInit) => {
      if (url.includes('/api/stride/races/') && init?.method === 'DELETE') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ status: 'ok' }),
        } as Response)
      }
      if (url.includes('/api/stride/races')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ races: [RACE] }) } as Response)
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ notes: [] }) } as Response)
    })
    vi.stubGlobal('fetch', successFetch)
    renderPage()

    await waitFor(() => {
      expect(screen.getAllByText('Bergen City Marathon').length).toBeGreaterThan(0)
    })

    const deleteButtons = screen.getAllByLabelText('Delete race')
    await act(async () => {
      fireEvent.click(deleteButtons[0])
    })

    await waitFor(() => {
      expect(screen.queryAllByText('Bergen City Marathon')).toHaveLength(0)
    })
  })
})

describe('StridePage – workout context panel on evaluation', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  function makePlanFetchMock(evaluation: Record<string, unknown>) {
    const planDay: DayPlan = {
      date: '2099-01-13',
      rest_day: false,
      session: {
        description: 'Easy run',
        warmup: '',
        main_set: '30 min easy',
        cooldown: '',
        strides: '',
        target_hr_cap: 150,
      },
    }
    const plan = {
      id: 1,
      user_id: 1,
      week_start: '2099-01-13',
      week_end: '2099-01-19',
      phase: 'Base',
      model: 'test',
      created_at: '2099-01-13T00:00:00Z',
      plan: [planDay],
    }
    return vi.fn((url: string) => {
      const make = (data: unknown) =>
        Promise.resolve({ ok: true, json: () => Promise.resolve(data) } as Response)
      if (url.includes('/api/stride/plans/current')) return make({ plan })
      if (url.includes('/api/stride/plans?limit=2')) return make({ plans: [plan] })
      if (url.includes('/api/stride/plans?limit=1')) return make({ total: 1 })
      if (url.includes('/api/stride/evaluations')) return make({ evaluations: [evaluation] })
      if (url.includes('/api/training/workouts')) {
        return make({ workouts: [{ id: 42, started_at: '2099-01-13T08:00:00Z' }] })
      }
      if (url.includes('/api/stride/races')) return make({ races: [] })
      if (url.includes('/api/stride/notes')) return make({ notes: [] })
      return make({})
    })
  }

  it('renders the panel title when workout_context_summary is non-empty', async () => {
    vi.stubGlobal(
      'fetch',
      makePlanFetchMock({
        id: 1,
        user_id: 1,
        plan_id: 1,
        workout_id: 42,
        eval: {
          planned_type: 'easy',
          actual_type: 'easy',
          compliance: 'compliant',
          notes: 'Solid easy run',
          flags: [],
          adjustments: '',
        },
        created_at: '2099-01-13T20:00:00Z',
        workout_context_summary: 'Feel notes: legs felt fresh | Context: surface=road',
      }),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('What coach saw for this day')).toBeInTheDocument()
    })
    expect(
      screen.getByText('Feel notes: legs felt fresh | Context: surface=road'),
    ).toBeInTheDocument()
  })

  it('omits the panel when workout_context_summary is null/empty', async () => {
    vi.stubGlobal(
      'fetch',
      makePlanFetchMock({
        id: 1,
        user_id: 1,
        plan_id: 1,
        workout_id: 42,
        eval: {
          planned_type: 'easy',
          actual_type: 'easy',
          compliance: 'compliant',
          notes: 'Solid easy run',
          flags: [],
          adjustments: '',
        },
        created_at: '2099-01-13T20:00:00Z',
      }),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Solid easy run')).toBeInTheDocument()
    })
    expect(screen.queryByText('What coach saw for this day')).not.toBeInTheDocument()
  })
})

describe('StridePage – plan highlight on update', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    chatDrawerCallbacks.onPlanUpdated = null
  })

  it('highlights changed day cards and removes highlight after 3s', async () => {
    const planDay: DayPlan = { date: '2099-01-13', rest_day: true }
    const plan = {
      id: 1,
      user_id: 1,
      week_start: '2099-01-13',
      week_end: '2099-01-19',
      phase: 'Base',
      model: 'test',
      created_at: '2099-01-13T00:00:00Z',
      plan: [planDay],
    }

    const fetchMock = vi.fn((url: string) => {
      const make = (data: unknown) =>
        Promise.resolve({ ok: true, json: () => Promise.resolve(data) } as Response)
      if (url.includes('/api/stride/plans/current')) return make({ plan })
      if (url.includes('/api/stride/plans?limit=2')) return make({ plans: [plan] })
      if (url.includes('/api/stride/plans?limit=1')) return make({ total: 1 })
      if (url.includes('/api/stride/evaluations')) return make({ evaluations: [] })
      if (url.includes('/api/training/workouts')) return make({ workouts: [] })
      if (url.includes('/api/stride/races')) return make({ races: [] })
      if (url.includes('/api/stride/notes')) return make({ notes: [] })
      return make({})
    })
    vi.stubGlobal('fetch', fetchMock)

    const { container } = renderPage()

    // Wait for the rest-day card to render (real timers for initial load)
    await waitFor(() => {
      expect(screen.getByText('Rest')).toBeInTheDocument()
    })

    // No highlight ring yet
    expect(container.querySelector('.ring-2')).toBeNull()

    // Switch to fake timers now that initial load is complete
    vi.useFakeTimers()

    // Trigger plan update with a changed day
    const changedDay: DayPlan = {
      date: '2099-01-13',
      rest_day: false,
      session: {
        description: 'Easy run',
        warmup: '',
        main_set: '30 min easy',
        cooldown: '',
        strides: '',
        target_hr_cap: 150,
      },
    }

    await act(async () => {
      chatDrawerCallbacks.onPlanUpdated!([changedDay])
    })

    // Ring should now be present on the changed card
    expect(container.querySelector('.ring-2')).not.toBeNull()

    // Advance time past the 3-second timeout
    await act(async () => {
      vi.advanceTimersByTime(3001)
    })

    // Ring should be cleared
    expect(container.querySelector('.ring-2')).toBeNull()
  })
})

describe('StridePage – Coach Notes older-bucket collapse', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  function notesFetch(notes: unknown[]) {
    return vi.fn((url: string, init?: RequestInit) => {
      const make = (data: unknown, ok = true) =>
        Promise.resolve({ ok, json: () => Promise.resolve(data) } as Response)
      if (url.includes('/api/stride/notes') && (init?.method === 'DELETE' || init?.method === 'PATCH')) {
        return make({ status: 'ok' })
      }
      if (url.includes('/api/stride/notes')) return make({ notes })
      if (url.includes('/api/stride/races')) return make({ races: [] })
      return make({})
    })
  }

  it('renders active notes inline (visible without expanding)', async () => {
    vi.stubGlobal('fetch', notesFetch([NOTE, OLDER_NOTE]))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Feeling good this week')).toBeInTheDocument()
    })
    // Active note is not inside the <details> wrapper.
    const activeNote = screen.getByText('Feeling good this week')
    expect(activeNote.closest('details')).toBeNull()
  })

  it('hides older notes inside a collapsed <details> by default', async () => {
    vi.stubGlobal('fetch', notesFetch([NOTE, OLDER_NOTE, OLDER_CONSUMED_NOTE]))
    const { container } = renderPage()
    await waitFor(() => {
      expect(screen.getByText('Feeling good this week')).toBeInTheDocument()
    })

    // Summary visible with count.
    expect(screen.getByText('Older notes (2)')).toBeInTheDocument()

    // The older notes are wrapped in a <details> that is closed by default.
    const olderNoteEl = screen.getByText('Old reflection from months ago')
    const detailsEl = olderNoteEl.closest('details') as HTMLDetailsElement | null
    expect(detailsEl).not.toBeNull()
    expect(detailsEl!.open).toBe(false)

    // Both older entries (active + consumed) live inside the same <details>.
    expect(container.querySelectorAll('details').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('Long-since-consumed weekly note').closest('details')).toBe(detailsEl)
  })

  it('expands older notes when the summary is clicked', async () => {
    vi.stubGlobal('fetch', notesFetch([NOTE, OLDER_NOTE]))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Older notes (1)')).toBeInTheDocument()
    })

    const summary = screen.getByText('Older notes (1)')
    const detailsEl = summary.closest('details') as HTMLDetailsElement
    expect(detailsEl.open).toBe(false)

    // <summary> click toggles its parent <details>; happy-dom honours this.
    await act(async () => {
      fireEvent.click(summary)
    })

    expect(detailsEl.open).toBe(true)
  })

  it('counts only older notes in the summary label', async () => {
    vi.stubGlobal(
      'fetch',
      notesFetch([
        { ...NOTE, id: 10, target_date: isoDate(-1), content: 'within-week note A' },
        { ...NOTE, id: 11, target_date: isoDate(0), content: 'within-week note B' },
        { ...OLDER_NOTE, id: 12, content: 'older A' },
        { ...OLDER_NOTE, id: 13, content: 'older B' },
        { ...OLDER_CONSUMED_NOTE, id: 14, content: 'older consumed' },
      ]),
    )
    renderPage()

    await waitFor(() => {
      // 2 older active + 1 older consumed = 3 older
      expect(screen.getByText('Older notes (3)')).toBeInTheDocument()
    })

    // Active notes remain outside the collapsible.
    expect(screen.getByText('within-week note A').closest('details')).toBeNull()
    expect(screen.getByText('within-week note B').closest('details')).toBeNull()
  })

  it('deletes an active note from the inline list', async () => {
    vi.stubGlobal('fetch', notesFetch([NOTE, OLDER_NOTE]))
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Feeling good this week')).toBeInTheDocument()
    })

    // The active note has both edit + delete; older active note (inside details)
    // also has the same controls. Target the active one by traversing from the
    // note text.
    const activeNoteCard = screen.getByText('Feeling good this week').closest('div.group')!
    const deleteBtn = activeNoteCard.querySelectorAll('button[aria-label="Delete note"]')[0] as HTMLButtonElement
    expect(deleteBtn).toBeDefined()

    await act(async () => {
      fireEvent.click(deleteBtn)
    })

    await waitFor(() => {
      expect(screen.queryByText('Feeling good this week')).not.toBeInTheDocument()
    })
    // Older note still present.
    expect(screen.getByText('Old reflection from months ago')).toBeInTheDocument()
  })

  it('deletes an older note from inside the collapsed group', async () => {
    vi.stubGlobal('fetch', notesFetch([NOTE, OLDER_NOTE]))
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Old reflection from months ago')).toBeInTheDocument()
    })

    const olderCard = screen.getByText('Old reflection from months ago').closest('div.group')!
    const deleteBtn = olderCard.querySelectorAll('button[aria-label="Delete note"]')[0] as HTMLButtonElement
    await act(async () => {
      fireEvent.click(deleteBtn)
    })

    await waitFor(() => {
      expect(screen.queryByText('Old reflection from months ago')).not.toBeInTheDocument()
    })
    // Older-notes summary disappears once the collapsed bucket is empty.
    expect(screen.queryByText(/Older notes/)).not.toBeInTheDocument()
    // Active note still rendered.
    expect(screen.getByText('Feeling good this week')).toBeInTheDocument()
  })
})
