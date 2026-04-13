// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import StrideChatDrawer from './StrideChatDrawer'

// ── Translation mock ──────────────────────────────────────────────────────────

const TRANSLATIONS: Record<string, string> = {
  'chat.title': 'Chat with your coach',
  'chat.placeholder': 'Ask about your plan, request changes...',
  'chat.send': 'Send',
  'chat.sending': 'Thinking...',
  'chat.planUpdated': 'Plan updated',
  'chat.sessionRetry': 'Session expired, retrying...',
  'chat.error': 'Failed to send message',
  'chat.empty': 'No messages yet. Ask your coach anything about this week\'s plan.',
}

function stableT(key: string): string {
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: stableT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('react-markdown', () => ({
  default: ({ children }: { children: string }) => <>{children}</>,
}))
vi.mock('remark-gfm', () => ({ default: () => {} }))
vi.mock('react-syntax-highlighter', () => ({
  Prism: ({ children }: { children: string }) => <code>{children}</code>,
}))
vi.mock('react-syntax-highlighter/dist/esm/styles/prism', () => ({
  vscDarkPlus: {},
}))

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeMessage(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    plan_id: 10,
    role: 'assistant',
    content: 'Try adding a tempo run on Wednesday.',
    plan_modified: false,
    created_at: '2026-04-13T00:00:00Z',
    ...overrides,
  }
}

function chatHistoryResponse(messages: ReturnType<typeof makeMessage>[] = []) {
  return { ok: true, json: () => Promise.resolve({ messages }) }
}

function makeSSEStream(events: string[]) {
  const encoder = new TextEncoder()
  let idx = 0
  return {
    ok: true,
    body: {
      getReader() {
        return {
          read(): Promise<{ done: boolean; value: Uint8Array | undefined }> {
            if (idx < events.length) {
              return Promise.resolve({ done: false, value: encoder.encode(events[idx++]) })
            }
            return Promise.resolve({ done: true, value: undefined })
          },
          cancel(): Promise<void> {
            return Promise.resolve()
          },
        }
      },
    },
    json: () => Promise.reject(new Error('not json')),
  }
}

const defaultProps = {
  planId: 10,
  onPlanUpdated: vi.fn(),
}

function renderDrawer(props = {}) {
  return render(<StrideChatDrawer {...defaultProps} {...props} />)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('StrideChatDrawer – collapsed/expanded state', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders collapsed by default with chat title', () => {
    renderDrawer()
    expect(screen.getByText('Chat with your coach')).toBeInTheDocument()
    // Should not show the input area when collapsed
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('expands on click and shows empty state', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(chatHistoryResponse())))
    renderDrawer()

    fireEvent.click(screen.getByText('Chat with your coach'))

    await waitFor(() => {
      expect(screen.getByText("No messages yet. Ask your coach anything about this week's plan.")).toBeInTheDocument()
    })
    expect(screen.getByRole('textbox')).toBeInTheDocument()
  })
})

describe('StrideChatDrawer – message history', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('loads and displays message history', async () => {
    const msgs = [
      makeMessage({ id: 1, role: 'user', content: 'Can I swap Tuesday and Thursday?' }),
      makeMessage({ id: 2, role: 'assistant', content: 'Sure, I can swap those days.' }),
    ]
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(chatHistoryResponse(msgs))))
    renderDrawer()

    fireEvent.click(screen.getByText('Chat with your coach'))

    await waitFor(() => {
      expect(screen.getByText('Can I swap Tuesday and Thursday?')).toBeInTheDocument()
      expect(screen.getByText('Sure, I can swap those days.')).toBeInTheDocument()
    })
  })

  it('shows plan updated badge on messages with plan_modified', async () => {
    const msgs = [
      makeMessage({ id: 1, role: 'assistant', content: 'I updated the plan.', plan_modified: true }),
    ]
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(chatHistoryResponse(msgs))))
    renderDrawer()

    fireEvent.click(screen.getByText('Chat with your coach'))

    await waitFor(() => {
      expect(screen.getByText('Plan updated')).toBeInTheDocument()
    })
  })
})

describe('StrideChatDrawer – send message flow', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('sends a message and shows it optimistically', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(chatHistoryResponse())
      .mockResolvedValueOnce(makeSSEStream([
        `event: user_message\ndata: ${JSON.stringify(makeMessage({ id: 5, role: 'user', content: 'Make Wednesday easier' }))}\n\n`,
        `event: delta\ndata: ${JSON.stringify({ text: 'I will reduce the intensity.' })}\n\n`,
        `event: done\ndata: ${JSON.stringify(makeMessage({ id: 6, role: 'assistant', content: 'I will reduce the intensity.' }))}\n\n`,
      ]))
    vi.stubGlobal('fetch', fetchMock)

    renderDrawer()
    fireEvent.click(screen.getByText('Chat with your coach'))
    await waitFor(() => screen.getByRole('textbox'))

    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'Make Wednesday easier' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))

    // Optimistic message appears immediately
    await waitFor(() => {
      expect(screen.getByText('Make Wednesday easier')).toBeInTheDocument()
    })

    // Final assistant message appears after stream completes
    await waitFor(() => {
      expect(screen.getByText('I will reduce the intensity.')).toBeInTheDocument()
    })
  })

  it('handles SSE delta events and shows streaming text', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(chatHistoryResponse())
      .mockResolvedValueOnce(makeSSEStream([
        `event: user_message\ndata: ${JSON.stringify(makeMessage({ id: 7, role: 'user', content: 'What about tempo?' }))}\n\n`,
        `event: delta\ndata: ${JSON.stringify({ text: 'Tempo runs ' })}\n\n`,
        `event: delta\ndata: ${JSON.stringify({ text: 'are great.' })}\n\n`,
        `event: done\ndata: ${JSON.stringify(makeMessage({ id: 8, role: 'assistant', content: 'Tempo runs are great.' }))}\n\n`,
      ]))
    vi.stubGlobal('fetch', fetchMock)

    renderDrawer()
    fireEvent.click(screen.getByText('Chat with your coach'))
    await waitFor(() => screen.getByRole('textbox'))

    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'What about tempo?' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => {
      expect(screen.getByText('Tempo runs are great.')).toBeInTheDocument()
    })
  })

  it('handles plan_updated event and calls onPlanUpdated', async () => {
    const onPlanUpdated = vi.fn()
    const updatedPlan = [
      { date: '2026-04-14', rest_day: false, session: { warmup: '10min', main_set: 'easy', cooldown: '5min', strides: '', target_hr_cap: 150, description: 'Easy run' } },
    ]

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(chatHistoryResponse())
      .mockResolvedValueOnce(makeSSEStream([
        `event: user_message\ndata: ${JSON.stringify(makeMessage({ id: 9, role: 'user', content: 'Change Monday' }))}\n\n`,
        `event: plan_updated\ndata: ${JSON.stringify({ plan: updatedPlan })}\n\n`,
        `event: done\ndata: ${JSON.stringify(makeMessage({ id: 10, role: 'assistant', content: 'Done, updated Monday.', plan_modified: true }))}\n\n`,
      ]))
    vi.stubGlobal('fetch', fetchMock)

    renderDrawer({ onPlanUpdated })
    fireEvent.click(screen.getByText('Chat with your coach'))
    await waitFor(() => screen.getByRole('textbox'))

    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'Change Monday' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => {
      expect(onPlanUpdated).toHaveBeenCalledWith(updatedPlan)
    })

    await waitFor(() => {
      expect(screen.getByText('Plan updated')).toBeInTheDocument()
    })
  })

  it('shows error on failed send', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(chatHistoryResponse())
      .mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'Server error' }) })
    vi.stubGlobal('fetch', fetchMock)

    renderDrawer()
    fireEvent.click(screen.getByText('Chat with your coach'))
    await waitFor(() => screen.getByRole('textbox'))

    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'Do something' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => {
      expect(screen.getByText('Server error')).toBeInTheDocument()
    })
  })

  it('send button is disabled when input is empty', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(chatHistoryResponse())))
    renderDrawer()
    fireEvent.click(screen.getByText('Chat with your coach'))
    await waitFor(() => screen.getByRole('textbox'))

    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
  })
})
