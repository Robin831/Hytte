// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Chat from './Chat'

// stableT must be a stable reference — Chat's load-messages useEffect lists `t`
// in its dependency array, so returning a new closure per render would cause
// the effect to re-run and burn through the mocked fetch sequence.
const TRANSLATIONS: Record<string, string> = {
  title: 'Chats',
  newChat: 'New chat',
  newConversation: 'New conversation',
  yesterday: 'Yesterday',
  thinking: 'Thinking...',
  streamingIndicator: 'Streaming…',
  emptyMessages: 'Send a message to start the conversation',
  'empty.noConversations': 'No conversations yet',
  'empty.startNew': 'Start a new chat to begin',
  'conversation.rename': 'Rename',
  'conversation.delete': 'Delete',
  'conversation.renameLabel': 'Rename conversation',
  'conversation.confirmDelete': 'Delete?',
  'conversation.backLabel': 'Back to conversations',
  'header.selectOrStart': 'Select or start a conversation',
  'header.modelLabel': 'Model',
  'models.opus': 'Opus',
  'models.sonnet': 'Sonnet',
  'models.haiku': 'Haiku',
  'welcome.title': 'Hytte Chat',
  'welcome.subtitle': 'Start a new conversation or pick one from the sidebar',
  'input.placeholder': 'Type a message...',
  'input.sendLabel': 'Send message',
  'input.stopStreaming': 'Stop generating',
  'input.dismissError': 'Dismiss error',
  'input.copyMessage': 'Copy message',
  'input.regenerate': 'Regenerate response',
  'input.editMessage': 'Edit message',
  'input.cancelEdit': 'Cancel edit',
  'input.editingNotice': 'Editing message',
  'errors.failedToLoad': 'Failed to load conversations',
  'errors.failedToLoadMessages': 'Failed to load messages',
  'errors.failedToCreate': 'Failed to create conversation',
  'errors.failedToDelete': 'Failed to delete conversation',
  'errors.failedToRename': 'Failed to rename conversation',
  'errors.failedToUpdateModel': 'Failed to update model',
  'errors.failedToSend': 'Failed to send message',
  'errors.streamError': 'The response stream was interrupted. Please try again.',
  'errors.failedToTruncate': 'Failed to update the conversation',
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

interface ConversationFixture {
  id: number
  user_id: number
  title: string
  model: string
  created_at: string
  updated_at: string
}

function makeConv(overrides: Partial<ConversationFixture> = {}): ConversationFixture {
  return {
    id: 1,
    user_id: 1,
    title: 'Existing chat',
    model: 'claude-sonnet-4-6',
    created_at: '2026-05-01T00:00:00Z',
    updated_at: '2026-05-01T00:00:00Z',
    ...overrides,
  }
}

// manualSSEResponse exposes push/close/error so tests can interleave assertions
// between events. The returned Response object's body is a ReadableStream that
// emits the frames pushed via the helper.
function manualSSEResponse() {
  const encoder = new TextEncoder()
  let controllerRef: ReadableStreamDefaultController<Uint8Array> | null = null
  // Capture signals that cancel the underlying stream so we can drive cancel
  // assertions even when the controller's enqueue would otherwise block.
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controllerRef = controller
    },
    cancel() {
      controllerRef = null
    },
  })
  return {
    response: { ok: true, status: 200, body: stream } as unknown as Response,
    push(frame: string) {
      controllerRef?.enqueue(encoder.encode(frame))
    },
    close() {
      controllerRef?.close()
      controllerRef = null
    },
  }
}

function frame(event: string, data: unknown): string {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`
}

function renderChat() {
  return render(
    <MemoryRouter>
      <Chat />
    </MemoryRouter>,
  )
}

async function selectExistingConversation(messages: unknown[] = []) {
  const conv = makeConv()
  const convListRes = { ok: true, json: () => Promise.resolve({ conversations: [conv] }) }
  const convDetailRes = {
    ok: true,
    json: () => Promise.resolve({ conversation: conv, messages }),
  }
  return { conv, convListRes, convDetailRes }
}

describe('Chat – streaming send', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('renders streamed tokens progressively into the assistant bubble', async () => {
    const { conv, convListRes, convDetailRes } = await selectExistingConversation([])
    const stream = manualSSEResponse()

    const refreshRes = {
      ok: true,
      json: () => Promise.resolve({ conversations: [makeConv()] }),
    }

    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(convListRes)
      .mockResolvedValueOnce(convDetailRes)
      .mockResolvedValueOnce(stream.response)
      .mockResolvedValue(refreshRes)
    vi.stubGlobal('fetch', fetchMock)

    renderChat()

    // Open the conversation.
    await waitFor(() => screen.getByText('Existing chat'))
    fireEvent.click(screen.getByText('Existing chat'))

    await waitFor(() => screen.getByPlaceholderText('Type a message...'))

    const textarea = screen.getByPlaceholderText('Type a message...') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Hi Claude' } })
    fireEvent.click(screen.getByTestId('chat-send-button'))

    // After clicking send, the placeholder bubble shows the streaming
    // indicator until the first token arrives.
    await screen.findByText('Streaming…')

    await act(async () => {
      stream.push(frame('user_message', { id: 100, conversation_id: conv.id, role: 'user', content: 'Hi Claude', created_at: '2026-05-01T01:00:00Z' }))
      stream.push(frame('token', { text: 'Hello, ' }))
    })
    // RTL's default normalizer trims trailing whitespace, so "Hello, " (the
    // first token, which ends with a space) normalises to "Hello," in the DOM.
    // Use a regex that does not require a trailing space.
    await waitFor(() => expect(screen.getByText(/Hello,/)).toBeInTheDocument())

    await act(async () => {
      stream.push(frame('token', { text: 'world!' }))
    })
    await waitFor(() => expect(screen.getByText(/Hello, world!/)).toBeInTheDocument())

    await act(async () => {
      stream.push(frame('done', {
        assistant_message: {
          id: 101,
          conversation_id: conv.id,
          role: 'assistant',
          content: 'Hello, world!',
          created_at: '2026-05-01T01:00:01Z',
        },
      }))
      stream.close()
    })

    // After close, the send button comes back (placeholder swapped for canonical row).
    await waitFor(() => expect(screen.queryByTestId('chat-stop-button')).not.toBeInTheDocument())
    expect(screen.getByText('Hello, world!')).toBeInTheDocument()
  })

  it('shows an error and removes the placeholder when the stream emits an error event', async () => {
    const { convListRes, convDetailRes } = await selectExistingConversation([])
    const stream = manualSSEResponse()

    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(convListRes)
      .mockResolvedValueOnce(convDetailRes)
      .mockResolvedValueOnce(stream.response)
    vi.stubGlobal('fetch', fetchMock)

    renderChat()

    await waitFor(() => screen.getByText('Existing chat'))
    fireEvent.click(screen.getByText('Existing chat'))
    await waitFor(() => screen.getByPlaceholderText('Type a message...'))

    const textarea = screen.getByPlaceholderText('Type a message...') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Bad prompt' } })
    fireEvent.click(screen.getByTestId('chat-send-button'))

    await screen.findByText('Streaming…')

    await act(async () => {
      stream.push(frame('error', { error: 'Claude exploded' }))
      stream.close()
    })

    await waitFor(() => {
      expect(screen.getByText('Claude exploded')).toBeInTheDocument()
    })
    // Placeholder bubble removed.
    expect(screen.queryByText('Streaming…')).not.toBeInTheDocument()
  })

  it('clicking Stop aborts the fetch and keeps the partial text on screen', async () => {
    const { conv, convListRes, convDetailRes } = await selectExistingConversation([])
    const stream = manualSSEResponse()

    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(convListRes)
      .mockResolvedValueOnce(convDetailRes)
      .mockImplementationOnce((_: string, init?: RequestInit) => {
        // Wire abort: when the test clicks Stop, the AbortController fires
        // and we resolve the stream with an AbortError.
        const signal = init?.signal as AbortSignal | undefined
        if (signal) {
          signal.addEventListener('abort', () => {
            try {
              stream.close()
            } catch {
              // already closed
            }
          })
        }
        return Promise.resolve(stream.response)
      })
    vi.stubGlobal('fetch', fetchMock)

    renderChat()

    await waitFor(() => screen.getByText('Existing chat'))
    fireEvent.click(screen.getByText('Existing chat'))
    await waitFor(() => screen.getByPlaceholderText('Type a message...'))

    const textarea = screen.getByPlaceholderText('Type a message...') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Long question' } })
    fireEvent.click(screen.getByTestId('chat-send-button'))

    await screen.findByText('Streaming…')

    await act(async () => {
      stream.push(frame('user_message', { id: 200, conversation_id: conv.id, role: 'user', content: 'Long question', created_at: '2026-05-01T02:00:00Z' }))
      stream.push(frame('token', { text: 'Partial answer so far' }))
    })

    await waitFor(() => expect(screen.getByText(/Partial answer so far/)).toBeInTheDocument())

    // Click Stop.
    const stopBtn = await screen.findByTestId('chat-stop-button')
    fireEvent.click(stopBtn)

    // Partial text is still visible after abort.
    await waitFor(() => expect(screen.queryByTestId('chat-stop-button')).not.toBeInTheDocument())
    expect(screen.getByText(/Partial answer so far/)).toBeInTheDocument()
  })

  it('clicking Stop before any tokens arrive removes the empty assistant placeholder', async () => {
    const { convListRes, convDetailRes } = await selectExistingConversation([])
    const stream = manualSSEResponse()

    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(convListRes)
      .mockResolvedValueOnce(convDetailRes)
      .mockImplementationOnce((_: string, init?: RequestInit) => {
        const signal = init?.signal as AbortSignal | undefined
        signal?.addEventListener('abort', () => {
          try {
            stream.close()
          } catch {
            // already closed
          }
        })
        return Promise.resolve(stream.response)
      })
    vi.stubGlobal('fetch', fetchMock)

    renderChat()

    await waitFor(() => screen.getByText('Existing chat'))
    fireEvent.click(screen.getByText('Existing chat'))
    await waitFor(() => screen.getByPlaceholderText('Type a message...'))

    const textarea = screen.getByPlaceholderText('Type a message...') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Long question' } })
    fireEvent.click(screen.getByTestId('chat-send-button'))

    // Wait until the streaming indicator (empty assistant placeholder) is on
    // screen, but do not push any token.
    await screen.findByText('Streaming…')

    // Click Stop while the bubble is still empty.
    const stopBtn = await screen.findByTestId('chat-stop-button')
    fireEvent.click(stopBtn)

    // The streaming indicator should disappear with the placeholder.
    await waitFor(() => expect(screen.queryByText('Streaming…')).not.toBeInTheDocument())
    expect(screen.queryByTestId('chat-stop-button')).not.toBeInTheDocument()
  })

  it('switching conversations aborts the in-flight stream', async () => {
    const convA = makeConv({ id: 1, title: 'Chat A' })
    const convB = makeConv({ id: 2, title: 'Chat B' })

    const stream = manualSSEResponse()

    // Route fetches by URL so post-abort cleanup fetches don't disturb the
    // mock chain. We rely on URL pattern matching rather than ordering.
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/chat/conversations') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ conversations: [convA, convB] }),
        })
      }
      if (url === `/api/chat/conversations/${convA.id}`) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ conversation: convA, messages: [] }),
        })
      }
      if (url === `/api/chat/conversations/${convB.id}`) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ conversation: convB, messages: [] }),
        })
      }
      if (url.endsWith('/messages/stream')) {
        const signal = init?.signal as AbortSignal | undefined
        signal?.addEventListener('abort', () => {
          try {
            stream.close()
          } catch {
            // already closed
          }
        })
        return Promise.resolve(stream.response)
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
    })
    vi.stubGlobal('fetch', fetchMock)

    renderChat()

    await waitFor(() => screen.getByText('Chat A'))
    fireEvent.click(screen.getByText('Chat A'))
    await waitFor(() => screen.getByPlaceholderText('Type a message...'))

    const textarea = screen.getByPlaceholderText('Type a message...') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Question for A' } })
    fireEvent.click(screen.getByTestId('chat-send-button'))

    await screen.findByText('Streaming…')
    await act(async () => {
      stream.push(frame('token', { text: 'Streaming A reply' }))
    })

    // Switch to B via the sidebar (mobile: re-open via back; here desktop
    // — the conversation row is still clickable).
    fireEvent.click(screen.getByText('Chat B'))

    // The stream should be aborted and the partial text should NOT leak into
    // conversation B's view.
    await waitFor(() => {
      // The empty-messages placeholder belongs to the newly-loaded conv B.
      expect(screen.getByText('Send a message to start the conversation')).toBeInTheDocument()
    })
    expect(screen.queryByText('Streaming A reply')).not.toBeInTheDocument()
  })
})

describe('Chat – model selector', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('renders the model dropdown listing the four supported models', async () => {
    const { convListRes, convDetailRes } = await selectExistingConversation([])
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(convListRes)
      .mockResolvedValueOnce(convDetailRes)
    vi.stubGlobal('fetch', fetchMock)

    renderChat()

    await waitFor(() => screen.getByText('Existing chat'))
    fireEvent.click(screen.getByText('Existing chat'))
    await waitFor(() => screen.getByPlaceholderText('Type a message...'))

    const select = screen.getByLabelText('Model') as HTMLSelectElement
    expect(select).toBeInTheDocument()
    // The active conversation's model is reflected as the selected value.
    expect(select.value).toBe('claude-sonnet-4-6')

    const optionValues = Array.from(select.options).map(o => o.value)
    expect(optionValues).toContain('claude-fable-5')
    expect(optionValues).toContain('claude-opus-5')
    expect(optionValues).toContain('claude-sonnet-4-6')
    expect(optionValues).toContain('claude-haiku-4-5')
    expect(optionValues).toHaveLength(4)
  })

  it('selecting a model on an existing conversation issues a PUT with the model', async () => {
    const { conv, convListRes, convDetailRes } = await selectExistingConversation([])
    const putRes = {
      ok: true,
      json: () => Promise.resolve({ conversation: { ...conv, model: 'claude-opus-5' } }),
    }
    // Dispatch on URL+method rather than call order: an occasional extra
    // list/detail refetch otherwise shifts the queue so the PUT consumes the
    // detail response (old model) and the select silently reverts — the CI
    // flake this replaces.
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === `/api/chat/conversations/${conv.id}` && init?.method === 'PUT') {
        return Promise.resolve(putRes)
      }
      if (url === `/api/chat/conversations/${conv.id}`) {
        return Promise.resolve(convDetailRes)
      }
      if (url === '/api/chat/conversations') {
        return Promise.resolve(convListRes)
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderChat()

    await waitFor(() => screen.getByText('Existing chat'))
    fireEvent.click(screen.getByText('Existing chat'))
    await waitFor(() => screen.getByPlaceholderText('Type a message...'))

    const select = screen.getByLabelText('Model') as HTMLSelectElement
    await act(async () => {
      fireEvent.change(select, { target: { value: 'claude-opus-5' } })
    })

    await waitFor(() => {
      const putCall = fetchMock.mock.calls.find(
        ([url, init]) => url === `/api/chat/conversations/${conv.id}` && init?.method === 'PUT',
      )
      expect(putCall).toBeDefined()
      expect(JSON.parse((putCall![1] as RequestInit).body as string)).toEqual({
        model: 'claude-opus-5',
      })
    })
    // The select reflects the PUT response only after its json() resolves and
    // state applies — poll rather than assert synchronously (flaked in CI).
    await waitFor(() => {
      expect((screen.getByLabelText('Model') as HTMLSelectElement).value).toBe('claude-opus-5')
    })
  })

  it('selecting a model before creating a conversation passes it in the POST body', async () => {
    const created = makeConv({ id: 5, title: '', model: 'claude-haiku-4-5' })
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/chat/conversations' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ conversation: created }) })
      }
      if (url === '/api/chat/conversations') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ conversations: [] }) })
      }
      if (url === `/api/chat/conversations/${created.id}`) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ conversation: created, messages: [] }),
        })
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
    })
    vi.stubGlobal('fetch', fetchMock)

    renderChat()

    // With no active conversation, the dropdown selects the next model.
    await waitFor(() => screen.getByLabelText('Model'))
    const select = screen.getByLabelText('Model') as HTMLSelectElement
    await act(async () => {
      fireEvent.change(select, { target: { value: 'claude-haiku-4-5' } })
    })

    // Start a new conversation via the sidebar button.
    await act(async () => {
      fireEvent.click(screen.getByLabelText('New chat'))
    })

    await waitFor(() => {
      const postCall = fetchMock.mock.calls.find(
        ([url, init]) => url === '/api/chat/conversations' && init?.method === 'POST',
      )
      expect(postCall).toBeDefined()
      expect(JSON.parse((postCall![1] as RequestInit).body as string)).toEqual({
        model: 'claude-haiku-4-5',
      })
    })
  })
})

describe('Chat – regenerate and edit', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  const conv = makeConv()

  // A two-turn history: user 1 / assistant 2 / user 3 / assistant 4.
  function history() {
    return [
      { id: 1, conversation_id: conv.id, role: 'user', content: 'First question', created_at: '2026-05-01T01:00:00Z' },
      { id: 2, conversation_id: conv.id, role: 'assistant', content: 'First answer', created_at: '2026-05-01T01:00:01Z' },
      { id: 3, conversation_id: conv.id, role: 'user', content: 'Second question', created_at: '2026-05-01T01:00:02Z' },
      { id: 4, conversation_id: conv.id, role: 'assistant', content: 'Second answer', created_at: '2026-05-01T01:00:03Z' },
    ]
  }

  // Dispatches by URL + method so extra refetches can't shift a call queue.
  function mockFetch(handlers: {
    truncate?: (messageId: string) => unknown
    stream?: (init?: RequestInit) => unknown
  } = {}) {
    return vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/chat/conversations') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ conversations: [conv] }) })
      }
      if (url === `/api/chat/conversations/${conv.id}`) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ conversation: conv, messages: history() }),
        })
      }
      if (url.endsWith('/messages/stream')) {
        return Promise.resolve(handlers.stream?.(init) ?? { ok: true, body: null, json: () => Promise.resolve({}) })
      }
      const match = url.match(new RegExp(`^/api/chat/conversations/${conv.id}/messages/(\\d+)$`))
      if (match && init?.method === 'DELETE') {
        return Promise.resolve(handlers.truncate?.(match[1]) ?? { ok: false, json: () => Promise.resolve(null) })
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) })
    })
  }

  async function openConversation() {
    await waitFor(() => screen.getByText('Existing chat'))
    fireEvent.click(screen.getByText('Existing chat'))
    await waitFor(() => screen.getByPlaceholderText('Type a message...'))
  }

  it('shows Regenerate only on the last assistant message and Edit on every user message', async () => {
    vi.stubGlobal('fetch', mockFetch())
    renderChat()
    await openConversation()

    await waitFor(() => expect(screen.getByText('Second answer')).toBeInTheDocument())

    // Only the newest assistant reply is regenerable.
    expect(screen.getAllByLabelText('Regenerate response')).toHaveLength(1)
    // Both user turns are editable.
    expect(screen.getAllByLabelText('Edit message')).toHaveLength(2)
  })

  it('hides Regenerate while a stream is in flight', async () => {
    const stream = manualSSEResponse()
    vi.stubGlobal('fetch', mockFetch({ stream: () => stream.response }))
    renderChat()
    await openConversation()

    await waitFor(() => expect(screen.getAllByLabelText('Regenerate response')).toHaveLength(1))

    const textarea = screen.getByPlaceholderText('Type a message...') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Third question' } })
    fireEvent.click(screen.getByTestId('chat-send-button'))

    await screen.findByText('Streaming…')
    expect(screen.queryByLabelText('Regenerate response')).not.toBeInTheDocument()
  })

  it('Regenerate truncates from the preceding user turn and re-streams it once', async () => {
    const stream = manualSSEResponse()
    const fetchMock = mockFetch({
      truncate: messageId => ({
        ok: true,
        json: () =>
          Promise.resolve({
            message: {
              id: Number(messageId),
              conversation_id: conv.id,
              role: 'user',
              content: 'Second question',
              created_at: '2026-05-01T01:00:02Z',
            },
          }),
      }),
      stream: () => stream.response,
    })
    vi.stubGlobal('fetch', fetchMock)
    renderChat()
    await openConversation()

    await waitFor(() => expect(screen.getByText('Second answer')).toBeInTheDocument())

    await act(async () => {
      fireEvent.click(screen.getByLabelText('Regenerate response'))
    })

    // The preceding user message (id 3) is the truncation point.
    await waitFor(() => {
      const del = fetchMock.mock.calls.find(([, init]) => (init as RequestInit | undefined)?.method === 'DELETE')
      expect(del).toBeDefined()
      expect(del![0]).toBe(`/api/chat/conversations/${conv.id}/messages/3`)
    })

    // The same text is re-sent through the normal streaming path.
    await waitFor(() => {
      const post = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/messages/stream'))
      expect(post).toBeDefined()
      expect(JSON.parse((post![1] as RequestInit).body as string)).toEqual({ content: 'Second question' })
    })

    // The stale reply is gone and exactly one user bubble remains for that turn.
    await waitFor(() => expect(screen.queryByText('Second answer')).not.toBeInTheDocument())
    expect(screen.getAllByText('Second question')).toHaveLength(1)
    expect(screen.getByText('First answer')).toBeInTheDocument()

    await act(async () => {
      stream.push(frame('token', { text: 'A better answer' }))
    })
    await waitFor(() => expect(screen.getByText(/A better answer/)).toBeInTheDocument())
    expect(screen.getAllByText('Second question')).toHaveLength(1)
  })

  it('Edit truncates the conversation and recalls the text into a focused composer', async () => {
    const fetchMock = mockFetch({
      truncate: messageId => ({
        ok: true,
        json: () =>
          Promise.resolve({
            message: {
              id: Number(messageId),
              conversation_id: conv.id,
              role: 'user',
              content: 'Second question',
              created_at: '2026-05-01T01:00:02Z',
            },
          }),
      }),
    })
    vi.stubGlobal('fetch', fetchMock)
    renderChat()
    await openConversation()

    await waitFor(() => expect(screen.getByText('Second answer')).toBeInTheDocument())

    // Edit the second user turn.
    await act(async () => {
      fireEvent.click(screen.getAllByLabelText('Edit message')[1])
    })

    await waitFor(() => {
      const del = fetchMock.mock.calls.find(([, init]) => (init as RequestInit | undefined)?.method === 'DELETE')
      expect(del![0]).toBe(`/api/chat/conversations/${conv.id}/messages/3`)
    })

    const textarea = screen.getByPlaceholderText('Type a message...') as HTMLTextAreaElement
    await waitFor(() => expect(textarea.value).toBe('Second question'))
    expect(document.activeElement).toBe(textarea)

    // The message and everything after it are gone; earlier turns survive.
    expect(screen.queryByText('Second answer')).not.toBeInTheDocument()
    expect(screen.getByText('First question')).toBeInTheDocument()
    expect(screen.getByText('First answer')).toBeInTheDocument()
    expect(screen.getByTestId('chat-cancel-edit-button')).toBeInTheDocument()
  })

  it('cancelling an edit clears the composer without restoring the deleted messages', async () => {
    const fetchMock = mockFetch({
      truncate: messageId => ({
        ok: true,
        json: () =>
          Promise.resolve({
            message: {
              id: Number(messageId),
              conversation_id: conv.id,
              role: 'user',
              content: 'Second question',
              created_at: '2026-05-01T01:00:02Z',
            },
          }),
      }),
    })
    vi.stubGlobal('fetch', fetchMock)
    renderChat()
    await openConversation()

    await waitFor(() => expect(screen.getByText('Second answer')).toBeInTheDocument())

    await act(async () => {
      fireEvent.click(screen.getAllByLabelText('Edit message')[1])
    })
    await waitFor(() => screen.getByTestId('chat-cancel-edit-button'))

    fireEvent.click(screen.getByTestId('chat-cancel-edit-button'))

    await waitFor(() =>
      expect(screen.queryByTestId('chat-cancel-edit-button')).not.toBeInTheDocument(),
    )
    const textarea = screen.getByPlaceholderText('Type a message...') as HTMLTextAreaElement
    expect(textarea.value).toBe('')
    // Cancelling does not resurrect the truncated messages.
    expect(screen.queryByText('Second question')).not.toBeInTheDocument()
    expect(screen.queryByText('Second answer')).not.toBeInTheDocument()
    expect(screen.getByText('First question')).toBeInTheDocument()
  })

  it('surfaces an error when truncation fails and keeps the messages on screen', async () => {
    const fetchMock = mockFetch({
      truncate: () => ({ ok: false, json: () => Promise.resolve({ error: 'message not found' }) }),
    })
    vi.stubGlobal('fetch', fetchMock)
    renderChat()
    await openConversation()

    await waitFor(() => expect(screen.getByText('Second answer')).toBeInTheDocument())

    await act(async () => {
      fireEvent.click(screen.getByLabelText('Regenerate response'))
    })

    await waitFor(() => expect(screen.getByText('message not found')).toBeInTheDocument())
    expect(screen.getByText('Second answer')).toBeInTheDocument()
    expect(screen.getAllByText('Second question')).toHaveLength(1)
  })
})
