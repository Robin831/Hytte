// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import Dashboard from './Dashboard'
import { WIDGET_REGISTRY } from '../components/widgets/registry'
import enDashboard from '../../public/locales/en/dashboard.json'
import nbDashboard from '../../public/locales/nb/dashboard.json'
import thDashboard from '../../public/locales/th/dashboard.json'

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
vi.mock('../components/widgets/NextUpWidget', () => stub('next_up'))
vi.mock('../components/widgets/WorkHoursWidget', () => stub('work_hours'))
vi.mock('../components/widgets/TripsWidget', () => stub('trips'))
vi.mock('../components/widgets/RacesWidget', () => stub('races'))
vi.mock('../components/widgets/StrideWidget', () => stub('stride'))
vi.mock('../components/widgets/FamilyChatWidget', () => stub('family_chat'))
vi.mock('../components/widgets/StarsWidget', () => stub('stars'))
vi.mock('../components/widgets/ChoresWidget', () => stub('chores'))
vi.mock('../components/widgets/HomeworkWidget', () => stub('homework'))
vi.mock('../components/widgets/WordfeudWidget', () => stub('wordfeud'))

// ── Auth mock ─────────────────────────────────────────────────────────────────

const authState = {
  user: { id: 1, name: 'Robin' } as { id: number; name: string } | null,
  features: {} as Record<string, boolean>,
  familyStatus: null as { is_parent: boolean; is_child: boolean } | null,
}

vi.mock('../auth', () => ({
  useAuth: () => ({
    user: authState.user,
    hasFeature: (key: string) => authState.features[key] ?? false,
    familyStatus: authState.familyStatus,
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

/** Every rendered widget, in DOM order. */
function everyWidget(): string[] {
  return Array.from(document.querySelectorAll('[data-widget]')).map(
    (el) => el.getAttribute('data-widget') ?? '',
  )
}

/**
 * The rendered order without the "next up" list, which every user has and
 * whose own position is covered by a test of its own.
 */
function renderedOrder(): string[] {
  return everyWidget().filter((id) => id !== 'next_up')
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
  authState.familyStatus = null
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

  it('puts the "next up" list right under the greeting', async () => {
    await renderDashboard()
    await waitFor(() => expect(everyWidget().slice(0, 3)).toEqual(['greeting', 'next_up', 'weather']))
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
        'calendar',
        'weather',
        'daylight',
        'activity',
        'infra',
        'github',
        'norwegian_word',
        'quick_links',
      ]),
    )
  })

  it('puts what needs doing today ahead of the ambient widgets', async () => {
    authState.features = { work_hours: true, trips: true, races: true, stride: true, wordfeud: true, family_chat: true }
    await renderDashboard()
    await waitFor(() => expect(renderedOrder()).toContain('wordfeud'))
    expect(renderedOrder().slice(0, 7)).toEqual(['greeting', 'work_hours', 'trips', 'races', 'stride', 'weather', 'family_chat'])
  })

  it("gives a child their own things first and keeps the kids' widgets from adults", async () => {
    authState.features = { kids_stars: true, kids_allowance: true, homework: true, races: true, family_chat: true, training: true }
    authState.familyStatus = { is_parent: false, is_child: true }
    const view = await renderDashboard()
    await waitFor(() => expect(renderedOrder()).toContain('races'))
    expect(renderedOrder().slice(0, 7)).toEqual(['greeting', 'stars', 'chores', 'homework', 'races', 'family_chat', 'weather'])
    view.unmount()

    // A parent with the same features sees approvals, but not stars or homework.
    authState.familyStatus = { is_parent: true, is_child: false }
    await renderDashboard()
    await waitFor(() => expect(renderedOrder()).toContain('races'))
    expect(renderedOrder()).toContain('chores')
    expect(renderedOrder()).not.toContain('stars')
    expect(renderedOrder()).not.toContain('homework')
    expect(renderedOrder().indexOf('races')).toBeLessThan(renderedOrder().indexOf('chores'))
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
    const saved = putBodies[0].order
    expect(saved[0]).toBe('greeting')
    expect(saved.indexOf('daylight')).toBe(saved.indexOf('weather') - 1)
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

describe('Dashboard – code-split widgets', () => {
  const REACT_LAZY = Symbol.for('react.lazy')

  const isLazyComponent = (component: unknown) =>
    typeof component === 'object' &&
    component !== null &&
    (component as { $$typeof?: symbol }).$$typeof === REACT_LAZY

  it('flags exactly the lazy components with lazy: true', () => {
    // The dashboard only wraps `lazy` entries in Suspense, so a React.lazy
    // component without the flag would suspend the whole page.
    for (const def of WIDGET_REGISTRY) {
      expect(isLazyComponent(def.component), `${def.id} lazy flag`).toBe(Boolean(def.lazy))
    }
  })

  it('code-splits the below-the-fold widgets', () => {
    const lazyIds = WIDGET_REGISTRY.filter((def) => def.lazy).map((def) => def.id)

    expect(lazyIds).toEqual(['trips', 'races', 'wordfeud', 'netatmo', 'infra', 'github'])
  })

  it('renders a code-split widget once its chunk resolves', async () => {
    authState.features = { infra: true }
    await renderDashboard()

    await waitFor(() => expect(renderedOrder()).toContain('infra'))
    expect(renderedOrder()).toContain('github')
  })
})

describe('Dashboard – widget titles', () => {
  const LOCALES: Array<[string, unknown]> = [
    ['en', enDashboard],
    ['nb', nbDashboard],
    ['th', thDashboard],
  ]

  it.each(LOCALES)('has a %s translation for every widget title key', (locale, messages) => {
    for (const def of WIDGET_REGISTRY) {
      const value = def.titleKey.split('.').reduce<unknown>((node, part) => {
        return node && typeof node === 'object' ? (node as Record<string, unknown>)[part] : undefined
      }, messages)
      expect(value, `${locale}: missing ${def.titleKey}`).toBeTypeOf('string')
    }
  })
})
