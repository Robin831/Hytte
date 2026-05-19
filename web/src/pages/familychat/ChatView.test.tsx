// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import ChatView from './ChatView'

// ── Translation mock ──────────────────────────────────────────────────────────

const TRANSLATIONS: Record<string, string> = {
  'loading': 'Loading…',
  'unnamedConversation': 'Untitled chat',
  'errors.failedToLoadMessages': 'Failed to load messages',
  'errors.failedToLoadConversation': 'Failed to load conversation',
  'time.justNow': 'just now',
  'chat.back': 'Back to conversations',
  'chat.membersLabel': 'Members',
  'chat.memberFallback': 'Member #{{id}}',
  'chat.you': 'You',
  'chat.emptyMessages': 'No messages yet. Say hello!',
  'chat.noSelectionTitle': 'Pick a conversation',
  'chat.noSelectionHint': 'Choose a chat from the list to start reading.',
  'composer.placeholder': 'Write a message…',
  'composer.send': 'Send message',
  'composer.attach': 'Attach a file',
  'composer.removeAttachment': 'Remove attachment',
  'composer.errors.send': 'Failed to send message',
  'composer.errors.tooLong': 'Message is too long',
  'composer.errors.upload': 'Failed to upload file',
  'composer.errors.fileTooLarge': 'File is too large (max 10 MB)',
  'composer.errors.unsupportedType': 'File type is not supported',
  'chat.attachmentImageAlt': 'Attached image',
  'chat.attachmentAudioAlt': 'Attached audio',
  'chat.attachmentFileLabel': 'Download attachment ({{mime}})',
  'chat.lightboxTitle': 'Image preview',
  'chat.lightboxClose': 'Close preview',
  'chat.connection.reconnecting': 'Reconnecting…',
  'newModal.parent': 'Parent',
}

function stableT(key: string, opts?: Record<string, string | number>): string {
  const val = TRANSLATIONS[key] ?? key
  if (opts) return val.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? ''))
  return val
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────

const authState = {
  user: { id: 1, name: 'Alice', email: 'alice@example.com' },
  familyStatus: null as null | { is_parent: boolean; is_child: boolean },
}

vi.mock('../../auth', () => ({
  useAuth: () => authState,
}))

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeConversation(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    name: 'Family Chat',
    owner_user_id: 1,
    created_at: '2026-05-01T00:00:00Z',
    last_message_at: '2026-05-01T10:00:00Z',
    unread_count: 0,
    member_ids: [1],
    ...overrides,
  }
}

function makeMessage(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    conversation_id: 1,
    sender_user_id: 1,
    body: 'Hello!',
    created_at: '2026-05-01T10:00:00Z',
    ...overrides,
  }
}

function convOk(conv = makeConversation()) {
  return { ok: true, json: () => Promise.resolve({ conversation: conv }) }
}

function msgsOk(messages: ReturnType<typeof makeMessage>[] = []) {
  return { ok: true, json: () => Promise.resolve({ messages }) }
}

function sendOk(msg: ReturnType<typeof makeMessage>) {
  return { ok: true, json: () => Promise.resolve({ message: msg }) }
}

// streamOk returns a Response-shaped object whose body is a never-completing
// ReadableStream. The SSE subscription opens this stream after the initial
// load and reads until the test unmounts (which cancels the reader). Callers
// who need to push frames into the live stream can use streamWithController
// instead.
function streamOk() {
  const stream = new ReadableStream<Uint8Array>({
    start() { /* hold open; cancel() will close it */ },
  })
  return { ok: true, body: stream }
}

function streamWithController() {
  let streamController: ReadableStreamDefaultController<Uint8Array> | null = null
  const stream = new ReadableStream<Uint8Array>({
    start(c) { streamController = c },
  })
  return {
    response: { ok: true, body: stream },
    push(event: string, data: unknown) {
      const encoder = new TextEncoder()
      streamController?.enqueue(
        encoder.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`),
      )
    },
    close() {
      streamController?.close()
    },
  }
}

function renderChatView(conversationId: number | null = 1, onBack = vi.fn()) {
  return render(<ChatView conversationId={conversationId} onBack={onBack} />)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('ChatView – no selection', () => {
  it('shows no-selection state when conversationId is null', () => {
    renderChatView(null)
    expect(screen.getByText('Pick a conversation')).toBeInTheDocument()
  })
})

describe('ChatView – loading and error states', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows loading skeleton while fetching', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { container } = renderChatView()
    expect(container.querySelector('[aria-busy="true"]')).toBeInTheDocument()
  })

  it('renders conversation name after successful load', async () => {
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk())
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    await waitFor(() => {
      expect(screen.getByText('Family Chat')).toBeInTheDocument()
    })
  })

  it('shows messages-error when messages request fails', async () => {
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce({ ok: false }),
    )
    renderChatView()
    await waitFor(() => {
      expect(screen.getByText('Failed to load messages')).toBeInTheDocument()
    })
  })

  it('shows conversation-error when conversation request fails', async () => {
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce({ ok: false })
      .mockResolvedValueOnce(msgsOk()),
    )
    renderChatView()
    await waitFor(() => {
      expect(screen.getByText('Failed to load conversation')).toBeInTheDocument()
    })
  })

  it('shows empty state when there are no messages', async () => {
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    await waitFor(() => {
      expect(screen.getByText('No messages yet. Say hello!')).toBeInTheDocument()
    })
  })
})

describe('ChatView – message rendering order', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders messages oldest-first (API returns newest-first, view reverses)', async () => {
    // API returns newest-first: [third, second, first]
    const messages = [
      makeMessage({ id: 3, body: 'Third message', created_at: '2026-05-01T12:00:00Z' }),
      makeMessage({ id: 2, body: 'Second message', created_at: '2026-05-01T11:00:00Z' }),
      makeMessage({ id: 1, body: 'First message', created_at: '2026-05-01T10:00:00Z' }),
    ]
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk(messages))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    await waitFor(() => expect(screen.getByText('First message')).toBeInTheDocument())

    const log = screen.getByRole('log')
    const bubbles = Array.from(log.querySelectorAll('[class*="rounded-2xl"]'))
    expect(bubbles[0].textContent).toBe('First message')
    expect(bubbles[1].textContent).toBe('Second message')
    expect(bubbles[2].textContent).toBe('Third message')
  })
})

describe('ChatView – optimistic message append', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('appends a newly sent message to the list', async () => {
    const newMsg = makeMessage({ id: 42, body: 'New message from me', sender_user_id: 1 })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(streamOk())
      .mockResolvedValueOnce(sendOk(newMsg)),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'New message from me' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })

    await waitFor(() => {
      expect(screen.getByText('New message from me')).toBeInTheDocument()
    })
  })

  it('does not append a message whose conversation_id does not match the current view', async () => {
    // Render conversation 1, then switch to conversation 2.
    // Messages from conversation 1 must not appear in conversation 2.
    const conv1msgs = [makeMessage({ id: 1, body: 'Conv1 message', conversation_id: 1 })]
    const conv2msgs = [makeMessage({ id: 2, body: 'Conv2 message', conversation_id: 2, sender_user_id: 2 })]
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk(makeConversation({ id: 1, name: 'Chat One' })))
      .mockResolvedValueOnce(msgsOk(conv1msgs))
      .mockResolvedValueOnce(streamOk())
      .mockResolvedValueOnce(convOk(makeConversation({ id: 2, name: 'Chat Two' })))
      .mockResolvedValueOnce(msgsOk(conv2msgs))
      .mockResolvedValueOnce(streamOk()),
    )

    const { rerender } = renderChatView(1)
    await waitFor(() => expect(screen.getByText('Conv1 message')).toBeInTheDocument())

    await act(async () => {
      rerender(<ChatView conversationId={2} onBack={vi.fn()} />)
    })

    await waitFor(() => expect(screen.getByText('Conv2 message')).toBeInTheDocument())
    expect(screen.queryByText('Conv1 message')).not.toBeInTheDocument()
  })
})

describe('ChatView – attachment rendering', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders an image attachment with a thumbnail pointing at the attachments endpoint', async () => {
    const imgMsg = makeMessage({
      id: 7,
      body: '',
      sender_user_id: 2,
      attachment_path: 'aabbccddeeff00112233445566778899',
      attachment_mime: 'image/png',
    })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([imgMsg]))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    const img = await screen.findByAltText('Attached image') as HTMLImageElement
    expect(img.src).toContain(`/api/familychat/conversations/${imgMsg.conversation_id}/attachments/${imgMsg.id}`)
  })

  it('renders an audio attachment with controls', async () => {
    const audioMsg = makeMessage({
      id: 8,
      body: '',
      sender_user_id: 2,
      attachment_path: 'aabbccddeeff00112233445566778899',
      attachment_mime: 'audio/mpeg',
    })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([audioMsg]))
      .mockResolvedValueOnce(streamOk()),
    )
    const { container } = renderChatView()
    await waitFor(() => {
      expect(container.querySelector('audio[controls]')).not.toBeNull()
    })
  })

  it('renders a non-image, non-audio attachment as a download link', async () => {
    const pdfMsg = makeMessage({
      id: 9,
      body: 'see attached',
      sender_user_id: 2,
      attachment_path: 'aabbccddeeff00112233445566778899',
      attachment_mime: 'application/pdf',
    })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([pdfMsg]))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    const link = await screen.findByText(/Download attachment/) as HTMLAnchorElement
    expect(link.closest('a')?.getAttribute('href')).toContain(`/attachments/${pdfMsg.id}`)
  })
})

describe('ChatView – SSE live updates', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('appends a message_new event delivered via the SSE stream', async () => {
    const sse = streamWithController()
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(sse.response),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    await act(async () => {
      sse.push('message_new', {
        message: makeMessage({
          id: 50,
          body: 'Live from SSE',
          conversation_id: 1,
          sender_user_id: 2,
          created_at: '2026-05-01T11:00:00Z',
        }),
      })
      // Yield so the read loop can process the enqueued frame.
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(screen.getByText('Live from SSE')).toBeInTheDocument()
    })
  })

  it('deduplicates a message delivered via both POST response and SSE', async () => {
    const sse = streamWithController()
    const newMsg = makeMessage({ id: 7, body: 'Dedup me', sender_user_id: 1 })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(sse.response)
      .mockResolvedValueOnce(sendOk(newMsg)),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Dedup me' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })

    await waitFor(() => screen.getByText('Dedup me'))

    await act(async () => {
      sse.push('message_new', { message: newMsg })
      await Promise.resolve()
    })

    // Still exactly one bubble for "Dedup me" after the duplicate SSE event.
    const matches = screen.getAllByText('Dedup me')
    expect(matches).toHaveLength(1)
  })

  it('ignores message_new events whose conversation_id does not match the view', async () => {
    const sse = streamWithController()
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(sse.response),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    await act(async () => {
      sse.push('message_new', {
        message: makeMessage({
          id: 99,
          body: 'Wrong conversation',
          conversation_id: 999,
          sender_user_id: 2,
        }),
      })
      await Promise.resolve()
    })

    // The misrouted message must not bleed into the open view.
    expect(screen.queryByText('Wrong conversation')).not.toBeInTheDocument()
    expect(screen.getByText('No messages yet. Say hello!')).toBeInTheDocument()
  })
})

describe('ChatView – reconnect gap-fill', () => {
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('issues a gap-fill fetch after stream disconnect and appends messages in order', async () => {
    // shouldAdvanceTime lets real time flow so waitFor retries work, while
    // advanceTimersByTimeAsync can still fast-forward the reconnect delay.
    vi.useFakeTimers({ shouldAdvanceTime: true })

    let closeStream: (() => void) | null = null
    const firstStream = new ReadableStream<Uint8Array>({
      start(c) { closeStream = () => c.close() },
    })

    const initialMsg = makeMessage({ id: 10, body: 'Before disconnect', conversation_id: 1 })
    const gapMsg1 = makeMessage({ id: 11, body: 'Gap one', conversation_id: 1, created_at: '2026-05-01T10:11:00Z' })
    const gapMsg2 = makeMessage({ id: 12, body: 'Gap two', conversation_id: 1, created_at: '2026-05-01T10:12:00Z' })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([initialMsg]))
      .mockResolvedValueOnce({ ok: true, body: firstStream })
      .mockResolvedValueOnce(msgsOk([gapMsg2, gapMsg1]))  // API newest-first
      .mockResolvedValueOnce(streamOk())
    vi.stubGlobal('fetch', fetchMock)

    renderChatView()
    await waitFor(() => expect(screen.getByText('Before disconnect')).toBeInTheDocument())

    // Close the first stream to trigger scheduleReconnect
    await act(async () => {
      closeStream!()
      await Promise.resolve()
    })

    // Fast-forward past the first reconnect delay (2000 ms for attempt #1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2500)
    })

    // Verify that a gap-fill fetch with since=10 was issued
    const fetchedUrls = fetchMock.mock.calls.map(([url]) => url as string)
    expect(fetchedUrls.some(url => url.includes('/messages?since=10'))).toBe(true)

    // Both gap messages should appear in ascending order after the pre-disconnect message
    await waitFor(() => {
      expect(screen.getByText('Gap one')).toBeInTheDocument()
      expect(screen.getByText('Gap two')).toBeInTheDocument()
    })

    const log = screen.getByRole('log')
    const bubbles = Array.from(log.querySelectorAll('[class*="rounded-2xl"]'))
    const texts = bubbles.map(b => b.textContent)
    expect(texts.indexOf('Before disconnect')).toBeLessThan(texts.indexOf('Gap one'))
    expect(texts.indexOf('Gap one')).toBeLessThan(texts.indexOf('Gap two'))
  }, 15000)

  it('shows a Reconnecting badge in the header while the SSE stream is dropped', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    let closeStream: (() => void) | null = null
    const firstStream = new ReadableStream<Uint8Array>({
      start(c) { closeStream = () => c.close() },
    })
    // Park the second connect on a never-resolving promise so the badge stays
    // visible long enough to assert on. The test cleans up via unmount.
    const stuckStreamFetch = new Promise<never>(() => {})

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce({ ok: true, body: firstStream })
      .mockResolvedValueOnce(msgsOk([]))         // gap-fill
      .mockReturnValueOnce(stuckStreamFetch)     // second connect hangs
    vi.stubGlobal('fetch', fetchMock)

    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    expect(screen.queryByTestId('family-chat-reconnecting')).not.toBeInTheDocument()

    await act(async () => {
      closeStream!()
      await Promise.resolve()
    })
    // Advance past the first reconnect backoff (2s for attempt #1).
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2500)
    })

    await waitFor(() => {
      expect(screen.getByTestId('family-chat-reconnecting')).toBeInTheDocument()
    })
  }, 15000)

  it('gap-fills without a since param when the initial message list is empty', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    let closeStream: (() => void) | null = null
    const firstStream = new ReadableStream<Uint8Array>({
      start(c) { closeStream = () => c.close() },
    })

    const reconnectMsg = makeMessage({ id: 1, body: 'Arrived during disconnect', conversation_id: 1 })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce({ ok: true, body: firstStream })
      .mockResolvedValueOnce(msgsOk([reconnectMsg]))
      .mockResolvedValueOnce(streamOk())
    vi.stubGlobal('fetch', fetchMock)

    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    await act(async () => {
      closeStream!()
      await Promise.resolve()
    })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2500)
    })

    // Gap-fill URL must not include ?since when lastId was 0
    const fetchedUrls = fetchMock.mock.calls.map(([url]) => url as string)
    const msgUrls = fetchedUrls.filter(url => url.includes('/messages'))
    expect(msgUrls.some(url => !url.includes('?since'))).toBe(true)

    await waitFor(() => expect(screen.getByText('Arrived during disconnect')).toBeInTheDocument())
  }, 15000)
})
