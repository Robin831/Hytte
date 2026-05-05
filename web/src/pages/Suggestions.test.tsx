// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Suggestions from './Suggestions'
import enCommon from '../../public/locales/en/common.json'
import type { Suggestion } from '../components/suggestions/SuggestionCard'

vi.mock('react-markdown', () => ({
  default: ({ children }: { children: string }) => <span data-testid="markdown">{children}</span>,
}))
vi.mock('remark-gfm', () => ({ default: () => {} }))

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

function makeSuggestion(overrides: Partial<Suggestion> & { id: number }): Suggestion {
  return {
    user_id: 1,
    generated_at: '2026-05-01T00:00:00Z',
    page_slug: 'dashboard',
    source: 'claude',
    type: 'addition',
    size: 's',
    title: `Suggestion ${overrides.id}`,
    body: 'Body text',
    status: 'pending',
    ...overrides,
  }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <Suggestions />
    </MemoryRouter>,
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('Suggestions – data fetch', () => {
  it('shows loading skeleton, then renders tab counts and pending cards', async () => {
    const list = {
      pending: [makeSuggestion({ id: 1, title: 'First pending' })],
      planned: [
        makeSuggestion({ id: 2, status: 'planned', title: 'A planned' }),
        makeSuggestion({ id: 3, status: 'planned', title: 'B planned' }),
      ],
      rejected: [],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Pending \(1\)/ })).toBeInTheDocument()
    })
    expect(screen.getByRole('tab', { name: /Planned \(2\)/ })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Rejected \(0\)/ })).toBeInTheDocument()
    expect(screen.getByText('First pending')).toBeInTheDocument()
  })

  it('shows load error and hides panels when initial fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) }),
    ))

    renderPage()

    await waitFor(() => {
      expect(screen.getByTestId('load-error')).toBeInTheDocument()
    })
    expect(screen.getByTestId('load-error')).toHaveTextContent('Failed to load suggestions')
  })

  it('switches to a different tab on click and shows its empty state', async () => {
    const list = {
      pending: [makeSuggestion({ id: 1, title: 'P1' })],
      planned: [],
      rejected: [],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('P1')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))

    await waitFor(() => {
      const activePanel = screen.getByRole('tabpanel')
      expect(
        within(activePanel).getByText('Nothing planned. Plan a pending suggestion to see it here.'),
      ).toBeVisible()
    })
  })

  it('renders the per-tab pending empty state when list is empty', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ pending: [], planned: [], rejected: [] }),
      }),
    ))

    renderPage()

    await waitFor(() => {
      expect(
        screen.getByText('No pending suggestions yet — try Run now.'),
      ).toBeInTheDocument()
    })
  })
})

describe('Suggestions – Run now flow', () => {
  it('refetches list on successful Run now', async () => {
    const initial = {
      pending: [makeSuggestion({ id: 1, title: 'Old item' })],
      planned: [],
      rejected: [],
    }
    const refreshed = {
      pending: [
        makeSuggestion({ id: 1, title: 'Old item' }),
        makeSuggestion({ id: 2, title: 'Fresh item' }),
      ],
      planned: [],
      rejected: [],
    }

    let listCalls = 0
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/run' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
      }
      if (url === '/api/suggestions') {
        listCalls += 1
        const body = listCalls === 1 ? initial : refreshed
        return Promise.resolve({ ok: true, json: () => Promise.resolve(body) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Old item')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /Run now/ }))

    await waitFor(() => {
      expect(screen.getByText('Fresh item')).toBeInTheDocument()
    })
    expect(listCalls).toBe(2)
    expect(screen.queryByTestId('run-error')).not.toBeInTheDocument()
  })

  it('keeps loaded data visible and shows banner when Run now fails', async () => {
    const initial = {
      pending: [makeSuggestion({ id: 1, title: 'Stays visible' })],
      planned: [],
      rejected: [],
    }
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/run' && init?.method === 'POST') {
        return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })
      }
      if (url === '/api/suggestions') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(initial) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Stays visible')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /Run now/ }))

    await waitFor(() => {
      expect(screen.getByTestId('run-error')).toBeInTheDocument()
    })
    expect(screen.getByTestId('run-error')).toHaveTextContent('Failed to run suggestions')
    // Critical: data must remain visible after a Run-now failure.
    expect(screen.getByText('Stays visible')).toBeInTheDocument()
    expect(screen.queryByTestId('load-error')).not.toBeInTheDocument()
  })

  it('keeps loaded data visible and shows load-error banner when refetch GET fails after successful Run now', async () => {
    const initial = {
      pending: [makeSuggestion({ id: 1, title: 'Stays visible' })],
      planned: [],
      rejected: [],
    }
    let listCalls = 0
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/run' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
      }
      if (url === '/api/suggestions') {
        listCalls += 1
        if (listCalls === 1) {
          return Promise.resolve({ ok: true, json: () => Promise.resolve(initial) })
        }
        return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Stays visible')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /Run now/ }))

    await waitFor(() => {
      expect(screen.getByTestId('load-error')).toBeInTheDocument()
    })
    // Data must remain visible after a refetch failure
    expect(screen.getByText('Stays visible')).toBeInTheDocument()
    // POST succeeded so no run-error
    expect(screen.queryByTestId('run-error')).not.toBeInTheDocument()
  })

  it('disables Run now button while in flight', async () => {
    const initial = { pending: [], planned: [], rejected: [] }
    let resolveRun: ((v: { ok: boolean; json: () => Promise<unknown> }) => void) | null = null
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/run' && init?.method === 'POST') {
        return new Promise<{ ok: boolean; json: () => Promise<unknown> }>(resolve => {
          resolveRun = resolve
        })
      }
      if (url === '/api/suggestions') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(initial) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    const runBtn = await screen.findByRole('button', { name: /Run now/ })
    fireEvent.click(runBtn)

    await waitFor(() => {
      const inFlightBtn = screen.getByRole('button', { name: /Running…/ })
      expect(inFlightBtn).toBeDisabled()
    })

    resolveRun!({ ok: true, json: () => Promise.resolve({}) })

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Run now/ })).not.toBeDisabled()
    })
  })
})

describe('Suggestions – header', () => {
  it('renders title, next-run hint and the New suggestion button', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ pending: [], planned: [], rejected: [] }),
      }),
    ))

    renderPage()

    expect(await screen.findByRole('heading', { level: 1, name: 'Suggestions' })).toBeInTheDocument()
    expect(screen.getByText('Next run: tonight 03:00')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /New suggestion/ })).toBeInTheDocument()
  })

  it('opens the new-suggestion form when the New suggestion button is clicked', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/suggestions/pages') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve([
            { slug: 'weather', title: 'Weather' },
            { slug: '__new_page__', title: 'New page' },
          ]),
        })
      }
      if (url === '/api/suggestions') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ pending: [], planned: [], rejected: [] }),
        })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    const button = await screen.findByRole('button', { name: /New suggestion/ })
    fireEvent.click(button)

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
    expect(screen.getByRole('heading', { name: 'New suggestion' })).toBeInTheDocument()
  })
})

describe('Suggestions – plan action', () => {
  it('plan it succeeds: moves card from pending to planned optimistically and refetches', async () => {
    const pendingSuggestion = makeSuggestion({ id: 1, title: 'Plan me' })
    const updatedSuggestion: Suggestion = { ...pendingSuggestion, status: 'planned', plan: '## Plan\n\nDo stuff' }
    const initial = { pending: [pendingSuggestion], planned: [], rejected: [] }
    const refreshed = { pending: [], planned: [updatedSuggestion], rejected: [] }

    let listCalls = 0
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/1/plan' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(updatedSuggestion) })
      }
      if (url === '/api/suggestions') {
        listCalls += 1
        return Promise.resolve({ ok: true, json: () => Promise.resolve(listCalls === 1 ? initial : refreshed) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Plan me')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /Plan it/ }))

    await waitFor(() => {
      // Card is optimistically removed from pending; empty state appears
      expect(
        screen.getByText('No pending suggestions yet — try Run now.'),
      ).toBeInTheDocument()
    })
    expect(listCalls).toBeGreaterThanOrEqual(2)
    expect(screen.queryByTestId('suggestion-1-action-error')).not.toBeInTheDocument()
  })

  it('plan it fails: surfaces actionable error from backend instead of generic message', async () => {
    const pendingSuggestion = makeSuggestion({ id: 1, title: 'Plan me' })
    const initial = { pending: [pendingSuggestion], planned: [], rejected: [] }

    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/1/plan' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 400,
          json: () => Promise.resolve({ error: 'Claude is not enabled' }),
        })
      }
      if (url === '/api/suggestions') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(initial) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Plan me')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /Plan it/ }))

    await waitFor(() => {
      expect(screen.getByTestId('suggestion-1-action-error')).toBeInTheDocument()
    })
    expect(screen.getByTestId('suggestion-1-action-error')).toHaveTextContent('Claude is not enabled')
    // Card must remain in the pending list
    expect(screen.getByText('Plan me')).toBeInTheDocument()
  })

  it('plan it fails without error body: shows generic fallback message', async () => {
    const pendingSuggestion = makeSuggestion({ id: 1, title: 'Plan me' })
    const initial = { pending: [pendingSuggestion], planned: [], rejected: [] }

    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/1/plan' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 500,
          json: () => Promise.reject(new Error('no body')),
        })
      }
      if (url === '/api/suggestions') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(initial) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Plan me')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /Plan it/ }))

    await waitFor(() => {
      expect(screen.getByTestId('suggestion-1-action-error')).toBeInTheDocument()
    })
    expect(screen.getByTestId('suggestion-1-action-error')).toHaveTextContent('Failed to plan suggestion')
  })
})

describe('Suggestions – reject action', () => {
  it('reject succeeds: removes card from pending and refetches', async () => {
    const pendingSuggestion = makeSuggestion({ id: 1, title: 'Reject me' })
    const initial = { pending: [pendingSuggestion], planned: [], rejected: [] }
    const refreshed = { pending: [], planned: [], rejected: [{ ...pendingSuggestion, status: 'rejected' as const }] }

    let listCalls = 0
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/1/reject' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
      }
      if (url === '/api/suggestions') {
        listCalls += 1
        return Promise.resolve({ ok: true, json: () => Promise.resolve(listCalls === 1 ? initial : refreshed) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Reject me')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /^Reject$/ }))

    await waitFor(() => {
      expect(
        screen.getByText('No pending suggestions yet — try Run now.'),
      ).toBeInTheDocument()
    })
    expect(listCalls).toBe(2)
  })

  it('reject fails: shows error message and keeps card visible', async () => {
    const pendingSuggestion = makeSuggestion({ id: 1, title: 'Reject me' })
    const initial = { pending: [pendingSuggestion], planned: [], rejected: [] }

    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/1/reject' && init?.method === 'POST') {
        return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })
      }
      if (url === '/api/suggestions') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(initial) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('Reject me')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /^Reject$/ }))

    await waitFor(() => {
      expect(screen.getByTestId('suggestion-1-action-error')).toBeInTheDocument()
    })
    expect(screen.getByTestId('suggestion-1-action-error')).toHaveTextContent('Failed to reject suggestion')
    expect(screen.getByText('Reject me')).toBeInTheDocument()
  })
})

describe('Suggestions – planned tab', () => {
  it('renders saved plan markdown for planned suggestions', async () => {
    const plannedSuggestion = makeSuggestion({
      id: 2,
      title: 'My planned item',
      status: 'planned',
      plan: '## Implementation plan\n\nStep 1: do the thing',
    })
    const list = { pending: [], planned: [plannedSuggestion], rejected: [] }

    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Planned \(1\)/ })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))

    await waitFor(() => {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).getByTestId('suggestion-2-plan')).toBeInTheDocument()
    })

    const planEl = screen.getByTestId('suggestion-2-plan')
    expect(planEl).toHaveTextContent('## Implementation plan')
    expect(planEl).toHaveTextContent('Step 1: do the thing')
  })

  it('shows "no plan yet" when planned suggestion has no plan text', async () => {
    const plannedSuggestion = makeSuggestion({ id: 3, title: 'No plan yet', status: 'planned' })
    const list = { pending: [], planned: [plannedSuggestion], rejected: [] }

    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Planned \(1\)/ })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))

    await waitFor(() => {
      expect(screen.getByText('No plan saved yet.')).toBeInTheDocument()
    })
  })

  it('Create bead button is keyboard-focusable and shows its description', async () => {
    const plannedSuggestion = makeSuggestion({ id: 4, title: 'Has plan', status: 'planned', plan: 'A plan' })
    const list = { pending: [], planned: [plannedSuggestion], rejected: [] }

    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Planned/ })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Create bead/ })).toBeInTheDocument()
    })

    const btn = screen.getByRole('button', { name: /Create bead/ })
    // Must not be natively disabled (so keyboard can reach it)
    expect(btn).not.toBeDisabled()
    expect(btn).toHaveAttribute('aria-disabled', 'true')
    // Visible description text must be present
    expect(screen.getByText('Bead creation lands in the next bead.')).toBeInTheDocument()
  })
})
