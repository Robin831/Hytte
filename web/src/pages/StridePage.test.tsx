// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import StridePage from './StridePage'
import enStride from '../../public/locales/en/stride.json'

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

const NOTE = {
  id: 1,
  user_id: 1,
  plan_id: null,
  content: 'Feeling good this week',
  created_at: '2024-01-15T10:00:00Z',
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
