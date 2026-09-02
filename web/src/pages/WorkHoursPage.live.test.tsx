// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import WorkHoursPage from './WorkHoursPage'
import enWorkhours from '../../public/locales/en/workhours.json'

// ── Translation helper ────────────────────────────────────────────────────────

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
  return function t(key: string, vars?: Record<string, string>): string {
    const dotKey = key.includes(':') ? key.split(':').slice(1).join('.') : key
    const val = resolveKey(translations, dotKey.split('.'))
    if (typeof val !== 'string') return key
    if (!vars) return val
    return val.replace(/\{\{(\w+)\}\}/g, (_, k) => vars[k] ?? '')
  }
}

// t is cached so it stays referentially stable across renders like real
// react-i18next's — components may use it in effect dependency arrays.
const stableT = makeT(enWorkhours as unknown as JsonObject)

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: stableT,
    i18n: { language: 'en' },
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// Stand-in for locale formatting. It renders dd.mm.yyyy so a date that reached
// the DOM without going through formatDate is distinguishable from one that did.
vi.mock('../utils/formatDate', () => ({
  formatDate: (value: string) => {
    const [y, m, d] = String(value).split('T')[0].split('-')
    return `${d}.${m}.${y}`
  },
  formatTime: () => '14:00',
  toLocalDateString: () => '2026-04-17',
}))

// ── Fetch mock ────────────────────────────────────────────────────────────────

function buildFetch(overrides: Record<string, unknown> = {}) {
  const defaults: Record<string, unknown> = {
    '/api/workhours/presets': { presets: [] },
    '/api/workhours/punch-session': { session: null },
    '/api/settings/preferences': { preferences: {} },
    '/api/workhours/flex': { flex: { total_minutes: 0, to_next_interval: 0 }, reset_date: '2026-01-01', days_in_pool: 0 },
    '/api/workhours/day': { day: null, summary: null },
    '/api/workhours/leave': { leave_days: [], balance: { total: 0, used: 0, remaining: 0 } },
  }
  return vi.fn((url: string) => {
    const path = url.toString().split('?')[0]
    const body = overrides[path] ?? defaults[path] ?? null
    return Promise.resolve({
      ok: body !== null,
      json: () => Promise.resolve(body),
    } as Response)
  })
}

function renderPage() {
  return render(
    <MemoryRouter>
      <WorkHoursPage />
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// Pin clock to 2026-04-17 12:00 so punch-start comparisons are deterministic
const FIXED_NOW = new Date('2026-04-17T12:00:00')

describe('WorkHoursPage live punch estimate UI', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('shows estimate section when punched in with a past start time', async () => {
    // '08:00' is before the pinned clock of 12:00
    vi.stubGlobal('fetch', buildFetch({
      '/api/workhours/punch-session': { session: { start_time: '08:00', date: '2026-04-17' } },
    }))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('If punched out now')).toBeInTheDocument()
    })
  })

  it('shows invalid-start message when punch start is in the future', async () => {
    // '14:00' is after the pinned clock of 12:00
    vi.stubGlobal('fetch', buildFetch({
      '/api/workhours/punch-session': { session: { start_time: '14:00', date: '2026-04-17' } },
    }))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Start time is in the future — cannot estimate')).toBeInTheDocument()
    })
  })

  it('applies green highlight styling when estimated reported hours meet or exceed the standard', async () => {
    // Use a 5-minute standard and punch start '08:00' so 4h elapsed (pinned to 12:00) exceeds it
    vi.stubGlobal('fetch', buildFetch({
      '/api/workhours/punch-session': { session: { start_time: '08:00', date: '2026-04-17' } },
      '/api/settings/preferences': {
        preferences: { work_hours_standard_day: '5', work_hours_lunch_minutes: '0', work_hours_rounding: '1' },
      },
    }))
    renderPage()
    await waitFor(() => expect(screen.getByText('If punched out now')).toBeInTheDocument())
    // atStandard=true applies bg-green-900/20 to the estimate section
    await waitFor(() => {
      const heading = screen.getByText('If punched out now')
      const section = heading.closest('section')
      expect(section?.className).toContain('bg-green-900')
    })
  })

  it('keeps estimating an open session that started before midnight', async () => {
    // Clock is 01:00 on the 17th; the punch-in was at 22:00 on the 16th.
    vi.setSystemTime(new Date('2026-04-17T01:00:00'))
    vi.stubGlobal('fetch', buildFetch({
      '/api/workhours/punch-session': { session: { start_time: '22:00', date: '2026-04-16' } },
      '/api/settings/preferences': {
        preferences: { work_hours_lunch_minutes: '0', work_hours_rounding: '30' },
      },
    }))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('If punched out now')).toBeInTheDocument()
    })
    expect(screen.queryByText('Start time is in the future — cannot estimate')).not.toBeInTheDocument()
    // 22:00 -> 01:00 is 3h of gross time
    await waitFor(() => {
      expect(screen.getAllByText('3:00').length).toBeGreaterThan(0)
    })
  })

  it('keeps estimating when the clock ticks past midnight on a punch-in made in this session', async () => {
    // The reload path seeds punchDate from the server; this is the live path —
    // punch in through the UI at 22:00 and let the 60s tick carry the page over
    // midnight without ever remounting. setInterval is faked so that tick can be
    // advanced by hand.
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-04-16T22:00:00'))
    vi.stubGlobal('fetch', buildFetch({
      '/api/workhours/punch-in': {},
      '/api/settings/preferences': {
        preferences: { work_hours_lunch_minutes: '0', work_hours_rounding: '30' },
      },
    }))
    renderPage()
    await waitFor(() => expect(screen.getByLabelText('Punch in')).toBeInTheDocument())

    await act(async () => {
      fireEvent.click(screen.getByLabelText('Punch in'))
    })
    await waitFor(() => expect(screen.getByText('If punched out now')).toBeInTheDocument())

    // Midnight passes while the page stays mounted.
    vi.setSystemTime(new Date('2026-04-17T01:00:00'))
    await act(async () => {
      vi.advanceTimersByTime(60_000)
    })

    expect(screen.queryByText('Start time is in the future — cannot estimate')).not.toBeInTheDocument()
    expect(screen.getByText('If punched out now')).toBeInTheDocument()
    // 22:00 -> 01:00 is 3h of gross time
    expect(screen.getAllByText('3:00').length).toBeGreaterThan(0)

    // Cancelling clears the punch date along with the punch itself.
    await act(async () => {
      fireEvent.click(screen.getByLabelText('Cancel'))
    })
    expect(screen.queryByText('If punched out now')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Punch in')).toBeInTheDocument()
  })

  it('hides the estimate when a different day than the punch-in is being viewed', async () => {
    // The estimate adds the viewed day's sessions and deductions to the punch's
    // elapsed time, so on another day it would describe neither day.
    vi.setSystemTime(new Date('2026-04-17T01:00:00'))
    vi.stubGlobal('fetch', buildFetch({
      '/api/workhours/punch-session': { session: { start_time: '22:00', date: '2026-04-16' } },
    }))
    renderPage()
    await waitFor(() => expect(screen.getByText('If punched out now')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('Next day'))
    await waitFor(() => expect(screen.queryByText('If punched out now')).not.toBeInTheDocument())
    expect(screen.queryByText('Start time is in the future — cannot estimate')).not.toBeInTheDocument()

    // Back on the punch-in's own day it returns.
    fireEvent.click(screen.getByLabelText('Previous day'))
    await waitFor(() => expect(screen.getByText('If punched out now')).toBeInTheDocument())
  })

  it('flags a punch-in left open for more than a day instead of estimating', async () => {
    // Clock is on the 17th; the punch-in is from the 15th — forgotten, not running.
    vi.setSystemTime(new Date('2026-04-17T10:00:00'))
    vi.stubGlobal('fetch', buildFetch({
      '/api/workhours/punch-session': { session: { start_time: '08:00', date: '2026-04-15' } },
    }))
    renderPage()
    await waitFor(() => {
      // The date goes through formatDate, so it reads 15.04.2026, not the raw ISO string.
      expect(
        screen.getByText('Punched in on 15.04.2026 and still open — punch out or cancel it to record the hours'),
      ).toBeInTheDocument()
    })
    expect(screen.queryByText('If punched out now')).not.toBeInTheDocument()
    expect(screen.queryByText('Start time is in the future — cannot estimate')).not.toBeInTheDocument()
  })

  it('registers a 60-second interval to refresh the live estimate when punched in', async () => {
    const intervalSpy = vi.spyOn(globalThis, 'setInterval')
    vi.stubGlobal('fetch', buildFetch({
      '/api/workhours/punch-session': { session: { start_time: '08:00', date: '2026-04-17' } },
    }))
    const { unmount } = renderPage()
    try {
      await waitFor(() => expect(screen.getByText('If punched out now')).toBeInTheDocument())

      // The interval is registered in a passive effect that may not have
      // flushed when the first waitFor resolves, so poll for it rather than
      // reading the spy once.
      await waitFor(() => {
        const tickCalls = intervalSpy.mock.calls.filter(([, ms]) => ms === 60_000)
        expect(tickCalls.length).toBeGreaterThan(0)
      })
    } finally {
      unmount()
      intervalSpy.mockRestore()
    }
  })
})

describe('WorkHoursPage keyboard shortcuts', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(FIXED_NOW)
    vi.stubGlobal('fetch', buildFetch())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('opens and closes the shortcuts dialog with ?', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('Day')).toBeInTheDocument())

    fireEvent.keyDown(window, { key: '?' })
    await waitFor(() => expect(screen.getByText('Keyboard shortcuts')).toBeInTheDocument())

    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByText('Keyboard shortcuts')).not.toBeInTheDocument())
  })

  it('switches tabs with number keys', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('Day')).toBeInTheDocument())

    fireEvent.keyDown(window, { key: '2' })
    await waitFor(() => {
      const weekBtn = screen.getByText('Week')
      expect(weekBtn.className).toContain('bg-gray-700')
    })

    fireEvent.keyDown(window, { key: '3' })
    await waitFor(() => {
      const monthBtn = screen.getByText('Month')
      expect(monthBtn.className).toContain('bg-gray-700')
    })

    fireEvent.keyDown(window, { key: '1' })
    await waitFor(() => {
      const dayBtn = screen.getByText('Day')
      expect(dayBtn.className).toContain('bg-gray-700')
    })
  })

  it('ignores repeat keydown events for single-action shortcuts', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('Day')).toBeInTheDocument())

    fireEvent.keyDown(window, { key: '2', repeat: true })
    const dayBtn = screen.getByText('Day')
    expect(dayBtn.className).toContain('bg-gray-700')
  })

  it('suppresses shortcuts when an input is focused', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('Day')).toBeInTheDocument())

    const input = document.createElement('input')
    document.body.appendChild(input)
    input.focus()

    fireEvent.keyDown(window, { key: '2' })
    const dayBtn = screen.getByText('Day')
    expect(dayBtn.className).toContain('bg-gray-700')

    document.body.removeChild(input)
  })

  it('suppresses shortcuts when a combobox sibling button is focused', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('Day')).toBeInTheDocument())

    const container = document.createElement('div')
    const combobox = document.createElement('input')
    combobox.setAttribute('role', 'combobox')
    const button = document.createElement('button')
    container.appendChild(combobox)
    container.appendChild(button)
    document.body.appendChild(container)
    button.focus()

    fireEvent.keyDown(window, { key: '2' })
    const dayBtn = screen.getByText('Day')
    expect(dayBtn.className).toContain('bg-gray-700')

    document.body.removeChild(container)
  })

  it('suppresses shortcuts while a dialog is open', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByText('Day')).toBeInTheDocument())

    fireEvent.keyDown(window, { key: '?' })
    await waitFor(() => expect(screen.getByText('Keyboard shortcuts')).toBeInTheDocument())

    fireEvent.keyDown(window, { key: '2' })
    const dayBtn = screen.getByText('Day')
    expect(dayBtn.className).toContain('bg-gray-700')
  })
})
