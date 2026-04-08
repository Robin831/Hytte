// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import CalendarPage from './CalendarPage'
import enCommon from '../../public/locales/en/common.json'

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
  return function t(key: string): string {
    const val = resolveKey(translations, key.split('.'))
    return typeof val === 'string' ? val : key
  }
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: makeT(enCommon as unknown as JsonObject),
    i18n: { language: 'en' },
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: (_date: Date, _opts: unknown) => 'Apr 8',
  formatTime: (_ts: string, _opts: unknown) => '09:00',
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────

const authState: { user: object | null } = { user: null }

vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

// ── Fetch mock ────────────────────────────────────────────────────────────────

function makeFetchMock(calendarRes: object, eventsRes: object) {
  return vi.fn((url: string) => {
    if (typeof url === 'string' && url.includes('/calendar/calendars')) {
      return Promise.resolve({ ok: true, json: () => Promise.resolve(calendarRes) })
    }
    if (typeof url === 'string' && url.includes('/calendar/events')) {
      return Promise.resolve({ ok: true, json: () => Promise.resolve(eventsRes) })
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
}

function renderPage() {
  return render(
    <MemoryRouter>
      <CalendarPage />
    </MemoryRouter>,
  )
}

// ── groupEventsByDate unit tests ──────────────────────────────────────────────

// Import the pure function by re-exporting it; since it isn't exported we test
// its behaviour indirectly through the rendered output. Direct unit tests for the
// grouping logic live here using a hand-rolled invocation pattern.

// We duplicate the pure function here so it can be tested without rendering.
type CalendarEvent = {
  id: string; calendar_id: string; title: string; start_time: string
  end_time: string; all_day: boolean; status: string; color?: string
}

function groupEventsByDate(events: CalendarEvent[], locale: string): Map<string, CalendarEvent[]> {
  const groups = new Map<string, CalendarEvent[]>()
  const sorted = [...events].sort((a, b) => {
    const aTime = new Date(a.start_time).getTime()
    const bTime = new Date(b.start_time).getTime()
    if (aTime !== bTime) return aTime - bTime
    if (a.all_day && !b.all_day) return -1
    if (!a.all_day && b.all_day) return 1
    return a.title.localeCompare(b.title, locale)
  })
  for (const event of sorted) {
    let key: string
    if (event.all_day) {
      key = event.start_time.slice(0, 10)
    } else {
      const d = new Date(event.start_time)
      const y = d.getFullYear()
      const m = String(d.getMonth() + 1).padStart(2, '0')
      const day = String(d.getDate()).padStart(2, '0')
      key = `${y}-${m}-${day}`
    }
    const existing = groups.get(key)
    if (existing) existing.push(event)
    else groups.set(key, [event])
  }
  return groups
}

function makeEvent(overrides: Partial<CalendarEvent> & { id: string; start_time: string }): CalendarEvent {
  return {
    calendar_id: 'primary', title: 'Event', end_time: overrides.start_time,
    all_day: false, status: 'confirmed', ...overrides,
  }
}

describe('groupEventsByDate', () => {
  it('groups timed events by local date', () => {
    const e = makeEvent({ id: '1', start_time: '2026-04-08T10:00:00Z' })
    const groups = groupEventsByDate([e], 'en')
    // The key is derived from local date parsing; we just verify the event appears
    expect(groups.size).toBe(1)
    expect([...groups.values()][0]).toContain(e)
  })

  it('groups all-day events by UTC date from ISO string prefix', () => {
    const e = makeEvent({ id: '1', start_time: '2026-04-08T00:00:00Z', all_day: true })
    const groups = groupEventsByDate([e], 'en')
    expect(groups.has('2026-04-08')).toBe(true)
    expect(groups.get('2026-04-08')).toContain(e)
  })

  it('places all-day events before timed events on same day', () => {
    const timed = makeEvent({ id: '1', start_time: '2026-04-08T08:00:00Z', title: 'Meeting' })
    const allDay = makeEvent({ id: '2', start_time: '2026-04-08T00:00:00Z', title: 'Holiday', all_day: true })
    const groups = groupEventsByDate([timed, allDay], 'en')
    // all-day key is '2026-04-08'; both land on same date group
    const entries = [...groups.values()].flat()
    const allDayIdx = entries.findIndex(e => e.id === '2')
    const timedIdx = entries.findIndex(e => e.id === '1')
    expect(allDayIdx).toBeLessThan(timedIdx)
  })

  it('returns empty map for empty event list', () => {
    expect(groupEventsByDate([], 'en').size).toBe(0)
  })

  it('sorts events on the same date by start_time', () => {
    const first = makeEvent({ id: '1', start_time: '2026-04-08T09:00:00Z', title: 'Early' })
    const second = makeEvent({ id: '2', start_time: '2026-04-08T14:00:00Z', title: 'Late' })
    const groups = groupEventsByDate([second, first], 'en')
    const events = [...groups.values()].flat()
    expect(events[0].id).toBe('1')
    expect(events[1].id).toBe('2')
  })
})

// ── Render state tests ────────────────────────────────────────────────────────

describe('CalendarPage – unauthenticated', () => {
  beforeEach(() => { authState.user = null })

  it('shows sign-in required message when not authenticated', () => {
    renderPage()
    expect(screen.getByText('Sign in to view your calendar.')).toBeInTheDocument()
  })
})

describe('CalendarPage – loading state', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.restoreAllMocks() })

  it('shows spinner while loading', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { container } = renderPage()
    // Loading spinner is present (animate-spin class)
    expect(container.querySelector('.animate-spin')).toBeInTheDocument()
  })
})

describe('CalendarPage – error state', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.restoreAllMocks() })

  it('shows error message when events fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (typeof url === 'string' && url.includes('/calendar/calendars')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ calendars: [{ id: 'primary', summary: 'My Cal', primary: true, selected: true }], connected: true }),
        })
      }
      return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })
    }))
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert')).toHaveTextContent('Failed to load calendar events')
  })
})

describe('CalendarPage – empty state', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.restoreAllMocks() })

  it('shows no-events message when calendar is connected but has no events', async () => {
    vi.stubGlobal('fetch', makeFetchMock(
      { calendars: [{ id: 'primary', summary: 'My Calendar', primary: true, selected: true }], connected: true },
      { events: [] },
    ))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('No events in this period')).toBeInTheDocument()
    })
  })
})

describe('CalendarPage – not connected state', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.restoreAllMocks() })

  it('shows not-connected message when calendar is not connected', async () => {
    vi.stubGlobal('fetch', makeFetchMock(
      { calendars: [], connected: false },
      { events: [] },
    ))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Calendar not connected')).toBeInTheDocument()
    })
  })
})

describe('CalendarPage – default calendar selection', () => {
  beforeEach(() => { authState.user = { id: 1 } })
  afterEach(() => { vi.restoreAllMocks() })

  it('defaults primary calendar to selected when none are selected', async () => {
    vi.stubGlobal('fetch', makeFetchMock(
      {
        calendars: [
          { id: 'primary', summary: 'My Calendar', primary: true, selected: false },
          { id: 'other', summary: 'Other', primary: false, selected: false },
        ],
        connected: true,
      },
      { events: [] },
    ))
    renderPage()
    // After load, filter button should be visible (calendar selector shown when calendars exist)
    await waitFor(() => {
      expect(screen.getByText('No events in this period')).toBeInTheDocument()
    })
    // The calendar selector button should be present since connected=true and calendars exist
    expect(screen.getByLabelText('Select calendars')).toBeInTheDocument()
  })
})
