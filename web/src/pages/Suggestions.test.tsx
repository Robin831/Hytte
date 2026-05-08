// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Suggestions from './Suggestions'
import { nextRunHintKey } from './suggestionsUtils'
import enCommon from '../../public/locales/en/common.json'
import enSuggestions from '../../public/locales/en/suggestions.json'
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
  return function t(key: string, vars?: Record<string, string | number>): string {
    const val = resolveKey(translations, key.split('.'))
    let str = typeof val === 'string' ? val : key
    if (vars) {
      for (const [name, value] of Object.entries(vars)) {
        str = str.replace(new RegExp(`{{\\s*${name}\\s*}}`, 'g'), String(value))
      }
    }
    return str
  }
}

const namespaces: Record<string, JsonObject> = {
  common: enCommon as unknown as JsonObject,
  suggestions: enSuggestions as unknown as JsonObject,
}

vi.mock('react-i18next', () => ({
  useTranslation: (ns?: string) => ({
    t: makeT(namespaces[ns ?? 'common'] ?? namespaces.common),
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

function expandCard(id: number) {
  fireEvent.click(screen.getByTestId(`suggestion-card-header-${id}`))
}

// Pending tab page-group sections start collapsed (Hytte-2d7p), so tests that
// need to interact with cards or assert on card content must expand the
// surrounding group first.
async function expandPendingGroup(slug: string = 'dashboard') {
  const header = await screen.findByTestId(`suggestion-group-header-${slug}`)
  if (header.getAttribute('aria-expanded') === 'false') {
    fireEvent.click(header)
  }
}

interface SSEFrame {
  event: string
  data: unknown
}

// Build a minimal Response-shaped object whose `body` is a ReadableStream
// emitting the supplied SSE frames. The Suggestions page only reads
// res.status, res.ok, and res.body, so the structural shape is sufficient.
function sseResponse(frames: SSEFrame[]): Response {
  const encoder = new TextEncoder()
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const f of frames) {
        controller.enqueue(
          encoder.encode(`event: ${f.event}\ndata: ${JSON.stringify(f.data)}\n\n`),
        )
      }
      controller.close()
    },
  })
  return { ok: true, status: 200, body: stream } as unknown as Response
}

// Build a streaming SSE response whose frames are pushed manually by the
// test. Returns helpers to enqueue more frames or close the stream — useful
// for asserting incremental UI updates between events.
function manualSSEResponse() {
  const encoder = new TextEncoder()
  let controllerRef: ReadableStreamDefaultController<Uint8Array> | null = null
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controllerRef = controller
    },
  })
  return {
    response: { ok: true, status: 200, body: stream } as unknown as Response,
    push(frame: SSEFrame) {
      controllerRef?.enqueue(
        encoder.encode(
          `event: ${frame.event}\ndata: ${JSON.stringify(frame.data)}\n\n`,
        ),
      )
    },
    close() {
      controllerRef?.close()
    },
    error(err: Error) {
      controllerRef?.error(err)
    },
  }
}

beforeEach(() => {
  // Pin Date so the header next-run helper renders deterministically. We only
  // fake Date — leaving setTimeout/queueMicrotask alive — because React/RTL
  // rely on real timers to flush updates. 2026-05-06T20:00:00Z = 22:00
  // Europe/Oslo (CEST), before 03:00 → header should say "tonight".
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-05-06T20:00:00Z'))
})

afterEach(() => {
  vi.useRealTimers()
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
    expect(screen.getByRole('tab', { name: /Created \(0\)/ })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Rejected \(0\)/ })).toBeInTheDocument()
    await expandPendingGroup()
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

    await screen.findByTestId('suggestion-group-header-dashboard')

    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))

    await waitFor(() => {
      const activePanel = screen.getByRole('tabpanel')
      expect(
        within(activePanel).getByText('Nothing planned. Plan a pending suggestion to see it here.'),
      ).toBeVisible()
    })
  })

  it('switching tabs swaps which cards are visible', async () => {
    const list = {
      pending: [makeSuggestion({ id: 1, title: 'Only pending card' })],
      planned: [
        makeSuggestion({ id: 2, status: 'planned', title: 'Only planned card', plan: 'A plan' }),
      ],
      rejected: [
        makeSuggestion({ id: 3, status: 'rejected', title: 'Only rejected card' }),
      ],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    // Initially on the Pending tab — only the pending card is in the active
    // panel (after expanding the page-group section).
    await expandPendingGroup()
    {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).getByText('Only pending card')).toBeInTheDocument()
      expect(within(panel).queryByText('Only planned card')).not.toBeInTheDocument()
      expect(within(panel).queryByText('Only rejected card')).not.toBeInTheDocument()
    }

    // Switch to Planned — only the planned card is now visible in the active panel.
    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))
    await waitFor(() => {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).getByText('Only planned card')).toBeInTheDocument()
    })
    {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).queryByText('Only pending card')).not.toBeInTheDocument()
      expect(within(panel).queryByText('Only rejected card')).not.toBeInTheDocument()
    }

    // Switch to Rejected — only the rejected card is now visible in the active panel.
    fireEvent.click(screen.getByRole('tab', { name: /Rejected/ }))
    await waitFor(() => {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).getByText('Only rejected card')).toBeInTheDocument()
    })
    {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).queryByText('Only pending card')).not.toBeInTheDocument()
      expect(within(panel).queryByText('Only planned card')).not.toBeInTheDocument()
    }
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
  it('streams SSE progress and refetches the list on done', async () => {
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
        return Promise.resolve(
          sseResponse([
            {
              event: 'started',
              data: { run_id: 1, total_pages: 1, page_slugs: ['weather'] },
            },
            {
              event: 'page_complete',
              data: {
                page_slug: 'weather',
                generated: 3,
                errors: 0,
                cost_usd: 0.04,
                elapsed_ms: 1200,
                status: 'ok',
              },
            },
            {
              event: 'done',
              data: { run_id: 1, generated: 3, errors: 0, cost_usd: 0.04 },
            },
          ]),
        )
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
      expect(screen.getByRole('tab', { name: /Pending \(1\)/ })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /Run now/ }))

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Pending \(2\)/ })).toBeInTheDocument()
    })
    // page_complete and done both call refetch — at least 3 list loads total.
    expect(listCalls).toBeGreaterThanOrEqual(2)
    expect(screen.queryByTestId('run-error')).not.toBeInTheDocument()
    // Progress UI cleared after `done`.
    expect(screen.queryByTestId('run-progress')).not.toBeInTheDocument()
  })

  it('shows progress pill and log entries as page_complete events arrive', async () => {
    const initial = { pending: [], planned: [], rejected: [] }

    const stream = manualSSEResponse()
    let resolveRun: ((v: Response) => void) | null = null
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/run' && init?.method === 'POST') {
        return new Promise<Response>(resolve => {
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

    fireEvent.click(await screen.findByRole('button', { name: /Run now/ }))
    resolveRun!(stream.response)

    // started: pill should read 0 / 2.
    stream.push({
      event: 'started',
      data: { run_id: 7, total_pages: 2, page_slugs: ['weather', 'webhooks'] },
    })

    await waitFor(() => {
      expect(screen.getByTestId('run-progress-pill')).toHaveTextContent('0 / 2')
    })

    // First page completes successfully.
    stream.push({
      event: 'page_complete',
      data: {
        page_slug: 'weather',
        generated: 3,
        errors: 0,
        cost_usd: 0.04,
        elapsed_ms: 1200,
        status: 'ok',
      },
    })

    await waitFor(() => {
      expect(screen.getByTestId('run-progress-pill')).toHaveTextContent('1 / 2')
    })
    expect(screen.getByTestId('run-progress-log-weather')).toHaveTextContent(
      'weather (3 ideas, $0.04)',
    )

    // Second page errors out.
    stream.push({
      event: 'page_complete',
      data: {
        page_slug: 'webhooks',
        generated: 0,
        errors: 1,
        cost_usd: 0,
        elapsed_ms: 240_000,
        status: 'error',
        error: 'timeout',
      },
    })

    await waitFor(() => {
      expect(screen.getByTestId('run-progress-pill')).toHaveTextContent('2 / 2')
    })
    expect(screen.getByTestId('run-progress-log-webhooks')).toHaveTextContent(
      'webhooks (timeout, 240s)',
    )

    // done clears the progress UI.
    stream.push({
      event: 'done',
      data: { run_id: 7, generated: 3, errors: 1, cost_usd: 0.04 },
    })
    stream.close()

    await waitFor(() => {
      expect(screen.queryByTestId('run-progress')).not.toBeInTheDocument()
    })
  })

  it('new_page_complete does not increment done counter beyond total', async () => {
    const initial = { pending: [], planned: [], rejected: [] }

    const stream = manualSSEResponse()
    let resolveRun: ((v: Response) => void) | null = null
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/run' && init?.method === 'POST') {
        return new Promise<Response>(resolve => {
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

    fireEvent.click(await screen.findByRole('button', { name: /Run now/ }))
    resolveRun!(stream.response)

    // started: total_pages=1 (backend excludes the new-page pass)
    stream.push({
      event: 'started',
      data: { run_id: 5, total_pages: 1, page_slugs: ['weather'] },
    })

    await waitFor(() => {
      expect(screen.getByTestId('run-progress-pill')).toHaveTextContent('0 / 1')
    })

    // Regular page completes — counter goes to 1/1.
    stream.push({
      event: 'page_complete',
      data: {
        page_slug: 'weather',
        generated: 2,
        errors: 0,
        cost_usd: 0.02,
        elapsed_ms: 800,
        status: 'ok',
      },
    })

    await waitFor(() => {
      expect(screen.getByTestId('run-progress-pill')).toHaveTextContent('1 / 1')
    })

    // new_page_complete fires — counter must stay at 1/1 (not exceed total).
    stream.push({
      event: 'new_page_complete',
      data: {
        page_slug: '__new_page__',
        generated: 1,
        errors: 0,
        cost_usd: 0.01,
        elapsed_ms: 500,
        status: 'ok',
      },
    })

    await waitFor(() => {
      // Counter must not go to 2/1 — it stays at 1/1.
      expect(screen.getByTestId('run-progress-pill')).toHaveTextContent('1 / 1')
    })
    // The new_page_complete entry does appear in the log.
    expect(screen.getByTestId('run-progress-log-__new_page__')).toBeInTheDocument()

    // Progressbar aria attributes must be valid (valuenow <= valuemax).
    const progressbar = screen.getByRole('progressbar')
    const valueNow = Number(progressbar.getAttribute('aria-valuenow'))
    const valueMax = Number(progressbar.getAttribute('aria-valuemax'))
    expect(valueNow).toBeLessThanOrEqual(valueMax)

    stream.push({ event: 'done', data: { run_id: 5, generated: 3, errors: 0, cost_usd: 0.03 } })
    stream.close()

    await waitFor(() => {
      expect(screen.queryByTestId('run-progress')).not.toBeInTheDocument()
    })
  })

  it('shows already-running banner on 409 and does not enter progress state', async () => {
    const initial = { pending: [], planned: [], rejected: [] }
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/run' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: () => Promise.resolve({ error: 'in progress', run_id: 42 }),
        })
      }
      if (url === '/api/suggestions') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(initial) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /Run now/ }))

    await waitFor(() => {
      expect(screen.getByTestId('already-running-banner')).toBeInTheDocument()
    })
    expect(screen.getByTestId('already-running-banner')).toHaveTextContent(
      'A run is already in progress',
    )
    // Banner has a link/anchor to the recent runs panel.
    const link = within(screen.getByTestId('already-running-banner')).getByRole(
      'link',
    )
    expect(link).toHaveAttribute('href', '#recent-runs')
    // No progress state and no run-error banner.
    expect(screen.queryByTestId('run-progress')).not.toBeInTheDocument()
    expect(screen.queryByTestId('run-error')).not.toBeInTheDocument()
  })

  it('shows Reconnect button when stream errors mid-run', async () => {
    const initial = { pending: [], planned: [], rejected: [] }
    const stream = manualSSEResponse()
    let resolveRun: ((v: Response) => void) | null = null
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/run' && init?.method === 'POST') {
        return new Promise<Response>(resolve => {
          resolveRun = resolve
        })
      }
      if (url === '/api/suggestions') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(initial) })
      }
      if (url === '/api/suggestions/runs?limit=1') {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve([
              { id: 9, finished_at: '2026-05-06T20:01:00Z' },
            ]),
        })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /Run now/ }))
    resolveRun!(stream.response)
    stream.push({
      event: 'started',
      data: { run_id: 9, total_pages: 2, page_slugs: ['weather', 'notes'] },
    })

    await waitFor(() => {
      expect(screen.getByTestId('run-progress-pill')).toHaveTextContent('0 / 2')
    })

    // Network drop mid-run.
    stream.error(new Error('connection reset'))

    await waitFor(() => {
      expect(screen.getByTestId('run-reconnect')).toBeInTheDocument()
    })
    expect(screen.getByTestId('run-stream-error')).toBeInTheDocument()
    // Progress UI should still be visible so the user keeps the context.
    expect(screen.getByTestId('run-progress')).toBeInTheDocument()

    // Clicking Reconnect polls /runs?limit=1; the latest is finished so
    // progress UI is cleared.
    fireEvent.click(screen.getByTestId('run-reconnect'))

    await waitFor(() => {
      expect(screen.queryByTestId('run-progress')).not.toBeInTheDocument()
    })
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

    await expandPendingGroup()
    expect(screen.getByText('Stays visible')).toBeInTheDocument()

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
        return Promise.resolve(
          sseResponse([
            {
              event: 'done',
              data: { run_id: 1, generated: 0, errors: 0, cost_usd: 0 },
            },
          ]),
        )
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

    await expandPendingGroup()
    expect(screen.getByText('Stays visible')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /Run now/ }))

    await waitFor(() => {
      expect(screen.getByTestId('load-error')).toBeInTheDocument()
    })
    // Data must remain visible after a refetch failure
    expect(screen.getByText('Stays visible')).toBeInTheDocument()
    // POST succeeded so no run-error
    expect(screen.queryByTestId('run-error')).not.toBeInTheDocument()
  })

  it('disables Run now button while a run is in flight', async () => {
    const initial = { pending: [], planned: [], rejected: [] }
    let resolveRun: ((v: Response) => void) | null = null
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/run' && init?.method === 'POST') {
        return new Promise<Response>(resolve => {
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

    // Resolve with a stream that emits a `done` event and closes — the
    // button should re-enable once the stream is fully consumed.
    resolveRun!(
      sseResponse([
        {
          event: 'done',
          data: { run_id: 1, generated: 0, errors: 0, cost_usd: 0 },
        },
      ]),
    )

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
    expect(screen.getByText('Next run: tonight at 03:00')).toBeInTheDocument()
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

describe('Suggestions – New suggestion happy path', () => {
  it('loads pages, posts the form, and refreshes the list', async () => {
    const initial = { pending: [], planned: [], rejected: [] }
    const created: Suggestion = makeSuggestion({
      id: 99,
      title: 'My new idea',
      body: 'Some body content',
      page_slug: 'weather',
      source: 'user',
      type: 'addition',
      size: 'm',
    })
    const refreshed = {
      pending: [created],
      planned: [],
      rejected: [],
    }

    let listCalls = 0
    let postCalls = 0
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/pages') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve([
            { slug: 'weather', title: 'Weather' },
            { slug: '__new_page__', title: 'New page' },
          ]),
        })
      }
      if (url === '/api/suggestions' && init?.method === 'POST') {
        postCalls += 1
        return Promise.resolve({ ok: true, json: () => Promise.resolve(created) })
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

    // Wait for initial list load.
    await waitFor(() => {
      expect(
        screen.getByText('No pending suggestions yet — try Run now.'),
      ).toBeInTheDocument()
    })

    // Open the form.
    fireEvent.click(screen.getByRole('button', { name: /New suggestion/ }))

    // The dialog appears and pages have loaded into the page <select>.
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
    await waitFor(() => {
      const pageSelect = screen.getByLabelText('Page') as HTMLSelectElement
      expect(within(pageSelect).getByRole('option', { name: 'Weather' })).toBeInTheDocument()
    })

    // Fill the form.
    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'addition' } })
    fireEvent.change(screen.getByLabelText('Size'), { target: { value: 'm' } })
    fireEvent.change(screen.getByLabelText('Page'), { target: { value: 'weather' } })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'My new idea' } })
    fireEvent.change(screen.getByLabelText('Body'), {
      target: { value: 'Some body content' },
    })

    // Submit.
    fireEvent.click(screen.getByRole('button', { name: /^Create suggestion$/ }))

    // POST happened with the right payload.
    await waitFor(() => {
      expect(postCalls).toBe(1)
    })
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => url === '/api/suggestions' && (init as RequestInit | undefined)?.method === 'POST',
    )
    expect(postCall).toBeTruthy()
    const init = postCall![1] as RequestInit
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body as string)
    expect(body).toEqual({
      type: 'addition',
      size: 'm',
      page_slug: 'weather',
      title: 'My new idea',
      body: 'Some body content',
    })

    // Dialog closes and the list refreshes — the new suggestion appears.
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByText('My new idea')).toBeInTheDocument()
    })
    expect(listCalls).toBeGreaterThanOrEqual(2)
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

    await expandPendingGroup()
    expect(screen.getByText('Plan me')).toBeInTheDocument()

    expandCard(1)
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

  it('plan it loading state: shows spinner, disables button, then re-enables', async () => {
    const pendingSuggestion = makeSuggestion({ id: 1, title: 'Plan me' })
    const updatedSuggestion: Suggestion = {
      ...pendingSuggestion,
      status: 'planned',
      plan: 'Plan body',
    }
    const initial = { pending: [pendingSuggestion], planned: [], rejected: [] }
    const refreshed = { pending: [], planned: [updatedSuggestion], rejected: [] }

    let resolvePlan: ((v: { ok: boolean; json: () => Promise<unknown> }) => void) | null = null
    let listCalls = 0
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/1/plan' && init?.method === 'POST') {
        return new Promise<{ ok: boolean; json: () => Promise<unknown> }>(resolve => {
          resolvePlan = resolve
        })
      }
      if (url === '/api/suggestions') {
        listCalls += 1
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(listCalls === 1 ? initial : refreshed),
        })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    const { container } = renderPage()

    await expandPendingGroup()
    expect(screen.getByText('Plan me')).toBeInTheDocument()

    expandCard(1)
    // Idle state — Plan it button is enabled and there's no spinner inside it.
    const idleBtn = screen.getByRole('button', { name: /Plan it/ })
    expect(idleBtn).not.toBeDisabled()
    expect(idleBtn.querySelector('.animate-spin')).toBeNull()

    fireEvent.click(idleBtn)

    // In-flight: button now reads "Planning…", is disabled, and shows a spinner.
    await waitFor(() => {
      const inFlightBtn = screen.getByRole('button', { name: /Planning…/ })
      expect(inFlightBtn).toBeDisabled()
      expect(inFlightBtn.querySelector('.animate-spin')).not.toBeNull()
    })
    // The Reject button is also disabled while plan is in flight.
    expect(screen.getByRole('button', { name: /^Reject$/ })).toBeDisabled()

    // Resolve the request; the optimistic update kicks in and the card is removed.
    resolvePlan!({ ok: true, json: () => Promise.resolve(updatedSuggestion) })

    await waitFor(() => {
      expect(
        screen.getByText('No pending suggestions yet — try Run now.'),
      ).toBeInTheDocument()
    })
    // Sanity check: no spinner is rendering in the page anymore for plan.
    expect(container.querySelectorAll('.animate-spin').length).toBe(0)
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

    await expandPendingGroup()
    expect(screen.getByText('Plan me')).toBeInTheDocument()

    expandCard(1)
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

    await expandPendingGroup()
    expect(screen.getByText('Plan me')).toBeInTheDocument()

    expandCard(1)
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

    await expandPendingGroup()
    expect(screen.getByText('Reject me')).toBeInTheDocument()

    expandCard(1)
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

    await expandPendingGroup()
    expect(screen.getByText('Reject me')).toBeInTheDocument()

    expandCard(1)
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
      expect(within(panel).getByText('My planned item')).toBeInTheDocument()
    })

    expandCard(2)

    const planEl = await screen.findByTestId('suggestion-2-plan')
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
      expect(screen.getByText('No plan yet')).toBeInTheDocument()
    })
    expandCard(3)
    expect(await screen.findByText('No plan saved yet.')).toBeInTheDocument()
  })

  it('Create bead button is enabled on planned suggestions', async () => {
    const plannedSuggestion = makeSuggestion({ id: 4, title: 'Has plan', status: 'planned', plan: 'A plan' })
    const list = { pending: [], planned: [plannedSuggestion], rejected: [], bead_created: [] }

    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Planned/ })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))

    await waitFor(() => {
      expect(screen.getByText('Has plan')).toBeInTheDocument()
    })

    expandCard(4)

    const btn = await screen.findByRole('button', { name: /^Create bead$/ })
    expect(btn).not.toBeDisabled()
    expect(btn).not.toHaveAttribute('aria-disabled', 'true')
  })
})

describe('Suggestions – create bead action', () => {
  it('create bead succeeds: surfaces bead id in a Created beads section', async () => {
    const plannedSuggestion = makeSuggestion({
      id: 5,
      title: 'Plan ready',
      status: 'planned',
      plan: '## Plan\n\nDo it',
    })
    const updatedSuggestion: Suggestion = {
      ...plannedSuggestion,
      status: 'bead_created',
      bead_id: 'Hytte-abcd',
      bead_created_at: '2026-05-06T10:00:00Z',
    }
    const initial = { pending: [], planned: [plannedSuggestion], rejected: [], bead_created: [] }
    const refreshed = { pending: [], planned: [], rejected: [], bead_created: [updatedSuggestion] }

    let listCalls = 0
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/5/bead' && init?.method === 'POST') {
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
      expect(screen.getByRole('tab', { name: /Planned \(1\)/ })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))

    await waitFor(() => {
      expect(screen.getByText('Plan ready')).toBeInTheDocument()
    })

    expandCard(5)
    fireEvent.click(await screen.findByRole('button', { name: /^Create bead$/ }))

    // After Create bead succeeds, the card moves into the new Created tab.
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Created \(1\)/ })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole('tab', { name: /Created/ }))

    await waitFor(() => {
      expect(screen.getByTestId('suggestion-5-bead-id')).toBeInTheDocument()
    })
    expect(screen.getByTestId('suggestion-5-bead-id')).toHaveTextContent('Hytte-abcd')
    expect(listCalls).toBeGreaterThanOrEqual(2)
  })

  it('create bead loading state: shows spinner and disables button', async () => {
    const plannedSuggestion = makeSuggestion({
      id: 6,
      title: 'Plan ready',
      status: 'planned',
      plan: 'A plan',
    })
    const updatedSuggestion: Suggestion = {
      ...plannedSuggestion,
      status: 'bead_created',
      bead_id: 'Hytte-xyz9',
    }
    const initial = { pending: [], planned: [plannedSuggestion], rejected: [], bead_created: [] }
    const refreshed = { pending: [], planned: [], rejected: [], bead_created: [updatedSuggestion] }

    let resolveBead: ((v: { ok: boolean; json: () => Promise<unknown> }) => void) | null = null
    let listCalls = 0
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/6/bead' && init?.method === 'POST') {
        return new Promise<{ ok: boolean; json: () => Promise<unknown> }>(resolve => {
          resolveBead = resolve
        })
      }
      if (url === '/api/suggestions') {
        listCalls += 1
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(listCalls === 1 ? initial : refreshed),
        })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Planned/ })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))

    await waitFor(() => {
      expect(screen.getByText('Plan ready')).toBeInTheDocument()
    })
    expandCard(6)

    const idleBtn = await screen.findByRole('button', { name: /^Create bead$/ })
    expect(idleBtn).not.toBeDisabled()
    fireEvent.click(idleBtn)

    await waitFor(() => {
      const inFlight = screen.getByRole('button', { name: /Creating bead…/ })
      expect(inFlight).toBeDisabled()
      expect(inFlight.querySelector('.animate-spin')).not.toBeNull()
    })

    resolveBead!({ ok: true, json: () => Promise.resolve(updatedSuggestion) })

    // Card moves into the Created tab; switch over to verify the bead-id chip renders.
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Created \(1\)/ })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole('tab', { name: /Created/ }))
    await waitFor(() => {
      expect(screen.getByTestId('suggestion-6-bead-id')).toBeInTheDocument()
    })
  })

  it('create bead fails: shows error and keeps card in Planned', async () => {
    const plannedSuggestion = makeSuggestion({
      id: 7,
      title: 'Stays planned',
      status: 'planned',
      plan: 'A plan',
    })
    const initial = { pending: [], planned: [plannedSuggestion], rejected: [], bead_created: [] }

    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/7/bead' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 500,
          json: () => Promise.resolve({ error: 'bd create failed: database locked' }),
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
      expect(screen.getByRole('tab', { name: /Planned/ })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))

    await waitFor(() => {
      expect(screen.getByText('Stays planned')).toBeInTheDocument()
    })
    expandCard(7)

    const btn = await screen.findByRole('button', { name: /^Create bead$/ })
    fireEvent.click(btn)

    await waitFor(() => {
      expect(screen.getByTestId('suggestion-7-bead-error')).toBeInTheDocument()
    })
    expect(screen.getByTestId('suggestion-7-bead-error')).toHaveTextContent('database locked')
    // Card must remain in Planned — the planned-card title is still visible.
    expect(screen.getByText('Stays planned')).toBeInTheDocument()
    // Created tab is empty because nothing has been created yet.
    expect(screen.getByRole('tab', { name: /Created \(0\)/ })).toBeInTheDocument()
    // Button label flips to retry.
    expect(screen.getByRole('button', { name: /Retry create bead/ })).toBeInTheDocument()
  })
})

describe('Suggestions – collapsed cards', () => {
  it('cards render collapsed by default and expand when the header is clicked', async () => {
    const list = {
      pending: [makeSuggestion({ id: 11, title: 'Collapsed by default', body: 'Hidden body content' })],
      planned: [],
      rejected: [],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    // Title is in the always-visible card header (after expanding the
    // surrounding page-group section, which is collapsed by default).
    await expandPendingGroup()
    expect(screen.getByText('Collapsed by default')).toBeInTheDocument()

    // Body, action buttons, and feedback textarea live under the collapsed
    // body and should not be in the DOM yet.
    expect(screen.queryByText('Hidden body content')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Plan it/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Reject$/ })).not.toBeInTheDocument()

    const header = screen.getByTestId('suggestion-card-header-11')
    expect(header).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(header)

    await waitFor(() => {
      expect(screen.getByText('Hidden body content')).toBeInTheDocument()
    })
    expect(header).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('button', { name: /Plan it/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^Reject$/ })).toBeInTheDocument()
  })

  it('header keyboard activation toggles expansion', async () => {
    const list = {
      pending: [makeSuggestion({ id: 12, title: 'Press to expand', body: 'Hidden body' })],
      planned: [],
      rejected: [],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    await expandPendingGroup()
    const header = await screen.findByTestId('suggestion-card-header-12')
    expect(header).toHaveAttribute('aria-expanded', 'false')

    fireEvent.keyDown(header, { key: 'Enter' })
    await waitFor(() => {
      expect(header).toHaveAttribute('aria-expanded', 'true')
    })
    expect(screen.getByText('Hidden body')).toBeInTheDocument()

    fireEvent.keyDown(header, { key: ' ' })
    await waitFor(() => {
      expect(header).toHaveAttribute('aria-expanded', 'false')
    })
    expect(screen.queryByText('Hidden body')).not.toBeInTheDocument()
  })

  it('expansion state survives tab switches', async () => {
    const list = {
      pending: [makeSuggestion({ id: 13, title: 'Persisted toggle', body: 'Persisted body' })],
      planned: [],
      rejected: [],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    await expandPendingGroup()
    expect(screen.getByText('Persisted toggle')).toBeInTheDocument()
    expandCard(13)
    await waitFor(() => {
      expect(screen.getByText('Persisted body')).toBeInTheDocument()
    })

    // Switch away and back — the card should still be expanded (the page
    // group also remains expanded across tab switches).
    fireEvent.click(screen.getByRole('tab', { name: /Rejected/ }))
    await waitFor(() => {
      expect(screen.queryByText('Persisted body')).not.toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole('tab', { name: /Pending/ }))
    await waitFor(() => {
      expect(screen.getByText('Persisted body')).toBeInTheDocument()
    })
  })
})

describe('Suggestions – page-grouped sections', () => {
  it('renders sections sorted alphabetically with __new_page__ last', async () => {
    const list = {
      pending: [
        makeSuggestion({ id: 21, title: 'New page idea', page_slug: '__new_page__', type: 'new_page' }),
        makeSuggestion({ id: 22, title: 'Weather one', page_slug: 'weather' }),
        makeSuggestion({ id: 23, title: 'Budget one', page_slug: 'budget' }),
      ],
      planned: [],
      rejected: [],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    await waitFor(() => {
      expect(screen.getByTestId('suggestion-group-budget')).toBeInTheDocument()
    })

    const groups = screen.getAllByRole('button', { name: /Toggle .* section/ })
    // Order should be: budget, weather, __new_page__ (rendered as 'New page ideas')
    expect(groups).toHaveLength(3)
    expect(groups[0]).toHaveAttribute('aria-label', 'Toggle budget section')
    expect(groups[1]).toHaveAttribute('aria-label', 'Toggle weather section')
    expect(groups[2]).toHaveAttribute('aria-label', 'Toggle New page ideas section')
  })

  it('sections that have zero items in the active tab are omitted', async () => {
    const list = {
      pending: [makeSuggestion({ id: 31, title: 'P1', page_slug: 'budget' })],
      planned: [makeSuggestion({ id: 32, status: 'planned', title: 'PL1', page_slug: 'weather' })],
      rejected: [],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    // Pending tab: only the budget group should render — weather is empty here.
    await waitFor(() => {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).getByTestId('suggestion-group-budget')).toBeInTheDocument()
    })
    {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).queryByTestId('suggestion-group-weather')).not.toBeInTheDocument()
    }
  })

  it('Pending tab sections start collapsed; clicking the header expands them', async () => {
    const list = {
      pending: [makeSuggestion({ id: 41, title: 'Hideable', page_slug: 'budget' })],
      planned: [],
      rejected: [],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    const groupHeader = await screen.findByTestId('suggestion-group-header-budget')
    expect(groupHeader).toHaveAttribute('aria-expanded', 'false')
    // Card body is not rendered while the section is collapsed.
    expect(screen.queryByText('Hideable')).not.toBeInTheDocument()
    // Header still surfaces the page title and count even when collapsed.
    expect(within(groupHeader).getByText('budget')).toBeInTheDocument()
    expect(within(groupHeader).getByText('1 suggestion')).toBeInTheDocument()

    fireEvent.click(groupHeader)

    await waitFor(() => {
      expect(groupHeader).toHaveAttribute('aria-expanded', 'true')
    })
    expect(screen.getByText('Hideable')).toBeInTheDocument()

    fireEvent.click(groupHeader)

    await waitFor(() => {
      expect(groupHeader).toHaveAttribute('aria-expanded', 'false')
    })
    expect(screen.queryByText('Hideable')).not.toBeInTheDocument()
  })

  it('Planned, Created and Rejected tab sections start expanded', async () => {
    const list = {
      pending: [],
      planned: [makeSuggestion({ id: 71, status: 'planned', title: 'Planned card', page_slug: 'budget', plan: 'A plan' })],
      rejected: [makeSuggestion({ id: 72, status: 'rejected', title: 'Rejected card', page_slug: 'weather' })],
      bead_created: [makeSuggestion({ id: 73, status: 'bead_created', title: 'Created card', page_slug: 'notes', bead_id: 'Hytte-aaaa' })],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    fireEvent.click(await screen.findByRole('tab', { name: /Planned/ }))
    await waitFor(() => {
      expect(screen.getByTestId('suggestion-group-header-budget')).toHaveAttribute('aria-expanded', 'true')
    })
    expect(screen.getByText('Planned card')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /Created/ }))
    await waitFor(() => {
      expect(screen.getByTestId('suggestion-group-header-notes')).toHaveAttribute('aria-expanded', 'true')
    })
    expect(screen.getByText('Created card')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: /Rejected/ }))
    await waitFor(() => {
      expect(screen.getByTestId('suggestion-group-header-weather')).toHaveAttribute('aria-expanded', 'true')
    })
    expect(screen.getByText('Rejected card')).toBeInTheDocument()
  })
})

describe('Suggestions – Created tab', () => {
  it('contains only bead_created cards; Planned tab no longer does', async () => {
    const list = {
      pending: [],
      planned: [
        makeSuggestion({ id: 51, status: 'planned', title: 'Still planned', plan: 'A plan' }),
      ],
      rejected: [],
      bead_created: [
        makeSuggestion({
          id: 52,
          status: 'bead_created',
          title: 'Already created',
          plan: 'A plan',
          bead_id: 'Hytte-zzzz',
        }),
      ],
    }
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(list) }),
    ))

    renderPage()

    // Tab counts reflect the new partitioning: planned is 1, created is 1.
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Planned \(1\)/ })).toBeInTheDocument()
    })
    expect(screen.getByRole('tab', { name: /Created \(1\)/ })).toBeInTheDocument()

    // Planned tab shows only the planned card, not the bead_created one.
    fireEvent.click(screen.getByRole('tab', { name: /Planned/ }))
    await waitFor(() => {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).getByText('Still planned')).toBeInTheDocument()
    })
    {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).queryByText('Already created')).not.toBeInTheDocument()
    }

    // Created tab shows only the bead_created card.
    fireEvent.click(screen.getByRole('tab', { name: /Created/ }))
    await waitFor(() => {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).getByText('Already created')).toBeInTheDocument()
    })
    {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).queryByText('Still planned')).not.toBeInTheDocument()
    }
  })

  it('shows the Created tab empty state when there are no bead-created suggestions', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ pending: [], planned: [], rejected: [], bead_created: [] }),
      }),
    ))

    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Created \(0\)/ })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('tab', { name: /Created/ }))

    await waitFor(() => {
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).getByText(/No beads created yet/)).toBeInTheDocument()
    })
  })
})

describe('nextRunHintKey', () => {
  it('returns the tonight key before 03:00 Europe/Oslo', () => {
    // 2026-05-06T20:00:00Z = 22:00 Europe/Oslo (CEST), before 03:00.
    expect(nextRunHintKey(new Date('2026-05-06T20:00:00Z'))).toBe('header.nextRunTonight')
  })

  it('returns the tomorrow key at or after 03:00 Europe/Oslo', () => {
    // 2026-05-06T05:00:00Z = 07:00 Europe/Oslo (CEST), already past 03:00.
    expect(nextRunHintKey(new Date('2026-05-06T05:00:00Z'))).toBe('header.nextRunTomorrow')
  })

  it('handles the boundary: exactly 03:00 Europe/Oslo is "tomorrow"', () => {
    // 2026-05-06T01:00:00Z = 03:00 Europe/Oslo (CEST). The next 03:00 is the
    // following day, so the helper must already say "tomorrow".
    expect(nextRunHintKey(new Date('2026-05-06T01:00:00Z'))).toBe('header.nextRunTomorrow')
  })

  it('uses minute accuracy near the 12-hour boundary: 15:59 Oslo (11h01m away) is tonight', () => {
    // 2026-05-06T13:59:00Z = 15:59 Europe/Oslo (CEST, UTC+2).
    // minutesSinceMidnight = 15*60+59 = 959; minutesUntil = (180-959+1440)%1440 = 661 < 720 → tonight.
    // Hour-only math yields hoursUntil=12 → "tomorrow" (wrong). Minute-accurate gives "tonight" (correct).
    expect(nextRunHintKey(new Date('2026-05-06T13:59:00Z'))).toBe('header.nextRunTonight')
  })

  it('uses minute accuracy near the 12-hour boundary: 15:01 Oslo (11h59m away) is tonight', () => {
    // 2026-05-06T13:01:00Z = 15:01 Europe/Oslo (CEST, UTC+2).
    // minutesSinceMidnight = 901; minutesUntil = (180-901+1440)%1440 = 719 < 720 → tonight.
    // Hour-only math yields hoursUntil=12 → "tomorrow" (wrong).
    expect(nextRunHintKey(new Date('2026-05-06T13:01:00Z'))).toBe('header.nextRunTonight')
  })

  it('uses minute accuracy near the 12-hour boundary: exactly 15:00 Oslo (12h00m away) is tomorrow', () => {
    // 2026-05-06T13:00:00Z = 15:00 Europe/Oslo (CEST, UTC+2).
    // minutesSinceMidnight = 900; minutesUntil = (180-900+1440)%1440 = 720, not < 720 → tomorrow.
    expect(nextRunHintKey(new Date('2026-05-06T13:00:00Z'))).toBe('header.nextRunTomorrow')
  })
})

describe('Suggestions – Pages settings tab', () => {
  function pagesFetch(initial: Array<{ slug: string; title: string; rotation_enabled: boolean | null }>) {
    let pagesCalls = 0
    const patchCalls: Array<{ slug: string; body: { rotation_enabled: boolean } }> = []
    let patchResponse: { ok: boolean; status?: number; json?: () => Promise<unknown> } = {
      ok: true,
      status: 200,
      json: () => Promise.resolve({}),
    }
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ pending: [], planned: [], rejected: [] }),
        })
      }
      if (url === '/api/suggestions/pages' && (!init || init.method === undefined || init.method === 'GET')) {
        pagesCalls += 1
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(initial),
        })
      }
      if (url.startsWith('/api/suggestions/pages/') && init?.method === 'PATCH') {
        const slug = decodeURIComponent(url.slice('/api/suggestions/pages/'.length))
        patchCalls.push({ slug, body: JSON.parse(init.body as string) })
        return Promise.resolve(patchResponse)
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    return {
      fetchMock,
      patchCalls,
      get pagesCalls() {
        return pagesCalls
      },
      setPatchResponse(r: typeof patchResponse) {
        patchResponse = r
      },
    }
  }

  it('toggle off: optimistic update flips switch and PATCH carries the new value', async () => {
    const { fetchMock, patchCalls } = pagesFetch([
      { slug: 'weather', title: 'Weather', rotation_enabled: null },
      { slug: 'notes', title: 'Notes', rotation_enabled: null },
      { slug: '__new_page__', title: 'New page', rotation_enabled: null },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    fireEvent.click(await screen.findByRole('tab', { name: /Pages/ }))

    const toggle = await screen.findByTestId('settings-toggle-weather')
    expect(toggle).toHaveAttribute('aria-checked', 'true')
    // The synthetic new-page sentinel must not appear — rotation is N/A there.
    expect(screen.queryByTestId('settings-toggle-__new_page__')).not.toBeInTheDocument()

    fireEvent.click(toggle)

    // Optimistic flip is immediate.
    expect(toggle).toHaveAttribute('aria-checked', 'false')

    await waitFor(() => {
      expect(patchCalls).toHaveLength(1)
    })
    expect(patchCalls[0]).toEqual({
      slug: 'weather',
      body: { rotation_enabled: false },
    })
    // Still off after PATCH succeeded — no revert.
    expect(toggle).toHaveAttribute('aria-checked', 'false')
    expect(screen.queryByTestId('settings-toggle-error')).not.toBeInTheDocument()
  })

  it('failed PATCH reverts the toggle and surfaces an error toast', async () => {
    const helpers = pagesFetch([
      { slug: 'weather', title: 'Weather', rotation_enabled: null },
    ])
    helpers.setPatchResponse({ ok: false, status: 500, json: () => Promise.resolve({}) })
    vi.stubGlobal('fetch', helpers.fetchMock)

    renderPage()

    fireEvent.click(await screen.findByRole('tab', { name: /Pages/ }))

    const toggle = await screen.findByTestId('settings-toggle-weather')
    expect(toggle).toHaveAttribute('aria-checked', 'true')

    fireEvent.click(toggle)

    // Optimistic update: immediately off.
    expect(toggle).toHaveAttribute('aria-checked', 'false')

    // After the failed PATCH the toggle reverts to its prior state and an
    // error toast appears.
    await waitFor(() => {
      expect(screen.getByTestId('settings-toggle-error')).toBeInTheDocument()
    })
    expect(toggle).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('settings-toggle-error')).toHaveTextContent('Failed to update page setting.')
  })

  it('respects the explicit rotation_enabled=false override on initial render', async () => {
    const { fetchMock } = pagesFetch([
      { slug: 'weather', title: 'Weather', rotation_enabled: false },
      { slug: 'notes', title: 'Notes', rotation_enabled: true },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    fireEvent.click(await screen.findByRole('tab', { name: /Pages/ }))

    const off = await screen.findByTestId('settings-toggle-weather')
    const on = await screen.findByTestId('settings-toggle-notes')
    expect(off).toHaveAttribute('aria-checked', 'false')
    expect(on).toHaveAttribute('aria-checked', 'true')
  })

  it('renders the explanatory note about user-authored suggestions', async () => {
    const { fetchMock } = pagesFetch([
      { slug: 'weather', title: 'Weather', rotation_enabled: null },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    fireEvent.click(await screen.findByRole('tab', { name: /Pages/ }))

    expect(
      await screen.findByText(/you can still write your own suggestions/i),
    ).toBeInTheDocument()
  })
})
