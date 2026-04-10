// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import HomeworkChat from './HomeworkChat'

// ── Translation mock ──────────────────────────────────────────────────────────
// stableT must be a stable reference — HomeworkChat's useEffect has `t` as a
// dependency, so a new function on every render would cause an infinite re-run
// loop that burns through fetch mocks out of order.

const TRANSLATIONS: Record<string, string> = {
  'backToList': 'Back to conversations',
  'noSubject': 'New topic',
  'thinking': 'Thinking...',
  'dismissError': 'Dismiss error',
  'imageAttached': 'Image attached',
  'imagePreview': 'Selected image preview',
  'removeImage': 'Remove image',
  'cameraButton': 'Take photo or select image',
  'cameraLabel': 'Camera or image input',
  'welcome.title': 'Homework Helper',
  'welcome.subtitle': 'Ask a question or take a photo of your homework to get started',
  'helpLevel.label': 'How much help do you want?',
  'helpLevel.hint': 'Hint',
  'helpLevel.explain': 'Explain',
  'helpLevel.walkthrough': 'Walk me through',
  'helpLevel.answer': 'Show answer',
  'input.placeholder': 'Ask about your homework...',
  'input.sendLabel': 'Send message',
  'input.copyMessage': 'Copy message',
  'errors.failedToLoadMessages': 'Failed to load messages',
  'errors.failedToSend': 'Failed to send message',
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

// Lightweight markdown renderer to avoid heavy dependencies in tests
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

function makeConversation(overrides = {}) {
  return {
    id: 1,
    kid_id: 42,
    subject: 'Maths',
    created_at: '2026-04-09T00:00:00Z',
    updated_at: '2026-04-09T00:00:00Z',
    ...overrides,
  }
}

function makeMessage(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    conversation_id: 1,
    role: 'user',
    content: 'What is 2+2?',
    help_level: 'hint',
    image_path: '',
    created_at: '2026-04-09T00:00:00Z',
    ...overrides,
  }
}

function convDetailResponse(conversation = makeConversation(), messages: ReturnType<typeof makeMessage>[] = []) {
  return { ok: true, json: () => Promise.resolve({ conversation, messages }) }
}

// Creates a fake SSE stream from an array of pre-formatted SSE event strings.
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
        }
      },
    },
    json: () => Promise.reject(new Error('not json')),
  }
}

function renderChat(id = '1') {
  return render(
    <MemoryRouter initialEntries={[`/homework/${id}`]}>
      <Routes>
        <Route path="/homework/:id" element={<HomeworkChat />} />
      </Routes>
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('HomeworkChat – loading and error states', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows loading spinner on initial render', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { container } = renderChat()
    expect(container.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('shows welcome state when no messages', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(convDetailResponse())))
    renderChat()
    await waitFor(() => {
      expect(screen.getByText('Homework Helper')).toBeInTheDocument()
    })
  })

  it('shows error when load fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false })))
    renderChat()
    await waitFor(() => {
      expect(screen.getByText('Failed to load messages')).toBeInTheDocument()
    })
  })

  it('renders existing messages', async () => {
    const msgs = [
      makeMessage({ id: 1, role: 'user', content: 'What is 2+2?' }),
      makeMessage({ id: 2, role: 'assistant', content: 'It is 4.' }),
    ]
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(convDetailResponse(makeConversation(), msgs))))
    renderChat()
    await waitFor(() => {
      expect(screen.getByText('What is 2+2?')).toBeInTheDocument()
      expect(screen.getByText('It is 4.')).toBeInTheDocument()
    })
  })
})

describe('HomeworkChat – send button disabled state', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('send button is disabled when input is empty', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(convDetailResponse())))
    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    expect(screen.getByRole('button', { name: 'Send message' })).toBeDisabled()
  })

  it('send button is enabled when input has text', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(convDetailResponse())))
    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'Hello' } })

    expect(screen.getByRole('button', { name: 'Send message' })).not.toBeDisabled()
  })

  it('send button remains disabled when only whitespace is entered', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(convDetailResponse())))
    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: '   ' } })

    expect(screen.getByRole('button', { name: 'Send message' })).toBeDisabled()
  })
})

describe('HomeworkChat – send message flow', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows error banner when send fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convDetailResponse())
      .mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'Claude unavailable' }) })
    vi.stubGlobal('fetch', fetchMock)

    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'What is entropy?' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(screen.getByText('Claude unavailable')).toBeInTheDocument()
    })
  })

  it('appends assistant message after SSE stream completes', async () => {
    const userMsg = makeMessage({ id: 10, role: 'user', content: 'Explain photosynthesis' })
    const assistantMsg = makeMessage({
      id: 11,
      conversation_id: 1,
      role: 'assistant',
      content: 'Photosynthesis is the process...',
      subject: 'Biology',
      updated_at: '2026-04-10T00:00:00Z',
    })

    const sseEvents = [
      `event: user_message\ndata: ${JSON.stringify(userMsg)}\n\n`,
      `event: delta\ndata: ${JSON.stringify({ text: 'Photosynthesis is the process...' })}\n\n`,
      `event: done\ndata: ${JSON.stringify(assistantMsg)}\n\n`,
    ]

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convDetailResponse())
      .mockResolvedValueOnce(makeSSEStream(sseEvents))
    vi.stubGlobal('fetch', fetchMock)

    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'Explain photosynthesis' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(screen.getByText('Photosynthesis is the process...')).toBeInTheDocument()
    })
  })

  it('shows image attached chip when message has image_path', async () => {
    const msgs = [
      makeMessage({ id: 1, role: 'user', content: 'See this problem', image_path: '/data/hw-1-abc.jpg' }),
    ]
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(convDetailResponse(makeConversation(), msgs))))
    renderChat()
    await waitFor(() => {
      expect(screen.getByText('Image attached')).toBeInTheDocument()
    })
  })
})
