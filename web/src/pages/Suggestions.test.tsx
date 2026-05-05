// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Suggestions from './Suggestions'
import enCommon from '../../public/locales/en/common.json'
import type { Suggestion } from '../components/suggestions/SuggestionCard'

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
    id: overrides.id,
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
        return new Promise(resolve => {
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

    resolveRun?.({ ok: true, json: () => Promise.resolve({}) })

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Run now/ })).not.toBeDisabled()
    })
  })
})

describe('Suggestions – header', () => {
  it('renders title, next-run hint and the New suggestion stub', async () => {
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
})
