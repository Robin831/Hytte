// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import Dashboard from './Dashboard'

// ── Translation mock ──────────────────────────────────────────────────────────
// Widget titles resolve to their registry key segment ("weather", "quickLinks",
// …) so assertions read naturally; layout strings keep their interpolation.

const LAYOUT_STRINGS: Record<string, string> = {
  'layout.edit': 'Edit layout',
  'layout.done': 'Done',
  'layout.reset': 'Reset to default',
  'layout.hint': 'Drag to reorder',
  'layout.hiddenTitle': 'Hidden widgets',
  'layout.hiddenEmpty': 'No hidden widgets.',
  'layout.hide': 'Hide {{widget}}',
  'layout.show': 'Show {{widget}}',
  'layout.showShort': 'Show',
  'layout.moveUp': 'Move {{widget}} up',
  'layout.moveDown': 'Move {{widget}} down',
  'layout.saveError': 'Could not save your dashboard layout.',
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: { widget?: string }) => {
      if (key.startsWith('widgets.') && key.endsWith('.title')) {
        return key.slice('widgets.'.length, -'.title'.length)
      }
      const raw = LAYOUT_STRINGS[key] ?? key
      return opts?.widget ? raw.replace('{{widget}}', opts.widget) : raw
    },
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Widget stubs ──────────────────────────────────────────────────────────────
// Each widget renders a marker so the rendered order can be read back from the
// DOM without pulling the real widgets' data fetching into the test.

function stub(id: string) {
  return { default: () => <div data-widget={id}>{id}</div> }
}

vi.mock('../components/widgets/GreetingWidget', () => stub('greeting'))
vi.mock('../components/widgets/WeatherWidget', () => stub('weather'))
vi.mock('../components/widgets/DaylightWidget', () => stub('daylight'))
vi.mock('../components/widgets/CalendarWidget', () => stub('calendar'))
vi.mock('../components/widgets/NetatmoWidget', () => stub('netatmo'))
vi.mock('../components/widgets/FitnessWidget', () => stub('training'))
vi.mock('../components/widgets/LactateSummaryWidget', () => stub('lactate'))
vi.mock('../components/widgets/ActivityFeedWidget', () => stub('activity'))
vi.mock('../components/widgets/InfraStatusWidget', () => stub('infra'))
vi.mock('../components/widgets/GitHubStatusWidget', () => stub('github'))
vi.mock('../components/widgets/NorwegianFunWidget', () => stub('norwegian_word'))
vi.mock('../components/widgets/QuickLinksWidget', () => stub('quick_links'))

// ── Auth mock ─────────────────────────────────────────────────────────────────

const authState = {
  user: { id: 1, name: 'Robin' } as { id: number; name: string } | null,
  features: {} as Record<string, boolean>,
}

vi.mock('../auth', () => ({
  useAuth: () => ({
    user: authState.user,
    hasFeature: (key: string) => authState.features[key] ?? false,
  }),
}))

// ── Helpers ───────────────────────────────────────────────────────────────────

const DEFAULT_ORDER = [
  'greeting',
  'weather',
  'daylight',
  'activity',
  'norwegian_word',
  'quick_links',
]

let putBodies: Array<{ order: string[]; hidden: string[] }>

function mockPreferences(stored?: unknown, putOk = true) {
  putBodies = []
  const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
    if (init?.method === 'PUT') {
      const parsed = JSON.parse(String(init.body))
      putBodies.push(parsed.preferences.dashboard_widgets)
      return Promise.resolve({
        ok: putOk,
        status: putOk ? 200 : 400,
        json: () => Promise.resolve({ preferences: {} }),
      } as Response)
    }
    const preferences: Record<string, string> = {}
    if (stored !== undefined) preferences.dashboard_widgets = JSON.stringify(stored)
    return Promise.resolve({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ preferences }),
    } as Response)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderedOrder(): string[] {
  return Array.from(document.querySelectorAll('[data-widget]')).map(
    (el) => el.getAttribute('data-widget') ?? '',
  )
}

async function renderDashboard() {
  const result = render(<Dashboard />)
  // Wait for the preference read to settle so the resolved layout is on screen.
  await waitFor(() => expect(renderedOrder().length).toBeGreaterThan(0))
  return result
}

beforeEach(() => {
  authState.user = { id: 1, name: 'Robin' }
  authState.features = {}
  mockPreferences()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('Dashboard – layout resolution', () => {
  it('renders the default order when no preference is stored', async () => {
    await renderDashboard()
    await waitFor(() => expect(renderedOrder()).toEqual(DEFAULT_ORDER))
  })

  it('applies a stored order', async () => {
    mockPreferences({ order: ['quick_links', 'weather', 'greeting'], hidden: [] })
    await renderDashboard()
    await waitFor(() =>
      expect(renderedOrder()).toEqual([
        'quick_links',
        'weather',
        'greeting',
        // Registry widgets missing from the stored order keep their default order.
        'daylight',
        'activity',
        'norwegian_word',
      ]),
    )
  })

  it('ignores stored ids that are not in the registry', async () => {
    mockPreferences({ order: ['ghost_widget', 'weather'], hidden: ['another_ghost'] })
    await renderDashboard()
    await waitFor(() =>
      expect(renderedOrder()).toEqual([
        'weather',
        'greeting',
        'daylight',
        'activity',
        'norwegian_word',
        'quick_links',
      ]),
    )
  })

  it('does not render a hidden widget but lists it in edit mode', async () => {
    mockPreferences({ order: [], hidden: ['weather'] })
    await renderDashboard()
    await waitFor(() => expect(renderedOrder()).not.toContain('weather'))

    fireEvent.click(screen.getByRole('button', { name: 'Edit layout' }))
    expect(screen.getByRole('button', { name: 'Show weather' })).toBeInTheDocument()
  })

  it('never renders a widget whose feature is disabled, even when stored and unhidden', async () => {
    mockPreferences({ order: ['lactate', 'quick_links', 'calendar', 'greeting'], hidden: [] })
    await renderDashboard()
    await waitFor(() =>
      expect(renderedOrder()).toEqual([
        'quick_links',
        'greeting',
        'weather',
        'daylight',
        'activity',
        'norwegian_word',
      ]),
    )
  })

  it('renders feature-gated widgets once their feature is enabled', async () => {
    authState.features = { calendar: true, infra: true }
    await renderDashboard()
    await waitFor(() =>
      expect(renderedOrder()).toEqual([
        'greeting',
        'weather',
        'daylight',
        'calendar',
        'activity',
        'infra',
        'github',
        'norwegian_word',
        'quick_links',
      ]),
    )
  })
})

describe('Dashboard – edit mode', () => {
  it('exposes translated keyboard move controls and reorders without a mouse', async () => {
    await renderDashboard()
    fireEvent.click(screen.getByRole('button', { name: 'Edit layout' }))

    const moveDown = screen.getByRole('button', { name: 'Move weather down' })
    expect(moveDown).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Move weather up' })).toBeInTheDocument()

    fireEvent.click(moveDown)

    await waitFor(() =>
      expect(renderedOrder()).toEqual([
        'greeting',
        'daylight',
        'weather',
        'activity',
        'norwegian_word',
        'quick_links',
      ]),
    )
    // The persisted order covers every registry widget, not just the visible ones.
    expect(putBodies).toHaveLength(1)
    expect(putBodies[0].order.slice(0, 3)).toEqual(['greeting', 'daylight', 'weather'])
    expect(putBodies[0].order).toContain('lactate')
    expect(putBodies[0].hidden).toEqual([])
  })

  it('disables move-up on the first widget and move-down on the last', async () => {
    await renderDashboard()
    fireEvent.click(screen.getByRole('button', { name: 'Edit layout' }))

    expect(screen.getByRole('button', { name: 'Move greeting up' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move quickLinks down' })).toBeDisabled()
  })

  it('hides a widget and restores it from the hidden list', async () => {
    await renderDashboard()
    fireEvent.click(screen.getByRole('button', { name: 'Edit layout' }))

    fireEvent.click(screen.getByRole('button', { name: 'Hide daylight' }))
    await waitFor(() => expect(renderedOrder()).not.toContain('daylight'))
    expect(putBodies[0].hidden).toEqual(['daylight'])

    fireEvent.click(screen.getByRole('button', { name: 'Show daylight' }))
    await waitFor(() => expect(renderedOrder()).toContain('daylight'))
    expect(putBodies[1].hidden).toEqual([])
  })

  it('offers no hide control for the always-visible greeting widget', async () => {
    await renderDashboard()
    fireEvent.click(screen.getByRole('button', { name: 'Edit layout' }))

    expect(screen.queryByRole('button', { name: 'Hide greeting' })).not.toBeInTheDocument()
  })

  it('resets to the default layout', async () => {
    mockPreferences({ order: ['quick_links', 'greeting'], hidden: ['weather'] })
    await renderDashboard()
    await waitFor(() => expect(renderedOrder()[0]).toBe('quick_links'))

    fireEvent.click(screen.getByRole('button', { name: 'Edit layout' }))
    fireEvent.click(screen.getByRole('button', { name: 'Reset to default' }))

    await waitFor(() => expect(renderedOrder()).toEqual(DEFAULT_ORDER))
    expect(putBodies[0]).toEqual({ order: [], hidden: [] })
  })

  it('rolls back and warns when the layout cannot be saved', async () => {
    mockPreferences(undefined, false)
    await renderDashboard()
    fireEvent.click(screen.getByRole('button', { name: 'Edit layout' }))
    fireEvent.click(screen.getByRole('button', { name: 'Hide daylight' }))

    await waitFor(() =>
      expect(screen.getByText('Could not save your dashboard layout.')).toBeInTheDocument(),
    )
    expect(renderedOrder()).toEqual(DEFAULT_ORDER)
  })

  it('leaves edit mode when Done is pressed', async () => {
    await renderDashboard()
    fireEvent.click(screen.getByRole('button', { name: 'Edit layout' }))
    expect(screen.getByText('Hidden widgets')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(screen.queryByText('Hidden widgets')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit layout' })).toBeInTheDocument()
  })
})
