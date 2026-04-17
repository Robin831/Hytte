// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: makeT(enWorkhours as unknown as JsonObject),
    i18n: { language: 'en' },
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: () => '2026-04-17',
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

  it('registers a 60-second interval to refresh the live estimate when punched in', async () => {
    const intervalSpy = vi.spyOn(globalThis, 'setInterval')
    vi.stubGlobal('fetch', buildFetch({
      '/api/workhours/punch-session': { session: { start_time: '08:00', date: '2026-04-17' } },
    }))
    const { unmount } = renderPage()
    await waitFor(() => expect(screen.getByText('If punched out now')).toBeInTheDocument())

    // Verify the component registered a 60-second tick interval
    const tickCalls = intervalSpy.mock.calls.filter(([, ms]) => ms === 60_000)
    expect(tickCalls.length).toBeGreaterThan(0)
    unmount()
    intervalSpy.mockRestore()
  })
})
