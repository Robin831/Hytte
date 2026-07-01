// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import ChatView from './ChatView'

// Mock voicePlayer so VoiceBubble renders without a real HTMLAudioElement.
// stopAll is called by ChatView's cleanup effect on every unmount.
vi.mock('./voice/voicePlayer', () => ({
  getState: vi.fn(() => ({ currentId: null, playing: false, positionMs: 0, durationMs: 0 })),
  subscribe: vi.fn((listener: (s: object) => void) => {
    listener({ currentId: null, playing: false, positionMs: 0, durationMs: 0 })
    return () => {}
  }),
  play: vi.fn().mockResolvedValue(undefined),
  pause: vi.fn(),
  seek: vi.fn(),
  stop: vi.fn(),
  stopAll: vi.fn(),
  getCurrentId: vi.fn(() => null),
  setAudioFactory: vi.fn(),
}))

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
  'composer.sending': 'Sending…',
  'composer.failedRetry': 'Failed — tap to retry',
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
  'chat.connection.live': 'Connected',
  'chat.connection.reconnecting': 'Reconnecting…',
  'chat.connection.offline': 'Offline',
  'newModal.parent': 'Parent',
  'reactions.pickerLabel': 'Add reaction',
  'reactions.add': 'React with {{emoji}}',
  'reactions.remove': 'Remove {{emoji}} reaction',
  'edit.edit': 'Edit',
  'edit.delete': 'Delete',
  'edit.save': 'Save',
  'edit.cancel': 'Cancel',
  'edit.confirmDelete': 'Delete this message?',
  'edit.tombstone': 'Message deleted by {{name}}',
  'edit.tombstoneSelf': 'You deleted this message',
  'edit.editedTag': 'edited',
  'edit.menuLabel': 'Message actions',
  'edit.saving': 'Saving…',
  'edit.saveError': 'Failed to save changes',
  'edit.deleteError': 'Failed to delete message',
  'voice.bubble.play': 'Play voice note',
  'voice.bubble.pause': 'Pause voice note',
  'voice.bubble.seek': 'Voice note position',
  'call.start': 'Start voice call',
  'call.startVideo': 'Start video call',
  'call.incomingLabel': 'Incoming call',
  'call.incomingVideoLabel': 'Incoming video call',
  'call.ringing': 'Ringing…',
  'call.accept': 'Accept',
  'call.decline': 'Decline',
  'call.hangup': 'Hang up',
  'call.mute': 'Mute',
  'call.unmute': 'Unmute',
  'call.speakerOn': 'Speaker on',
  'call.speakerOff': 'Speaker off',
  'call.cameraOn': 'Turn camera on',
  'call.cameraOff': 'Turn camera off',
  'call.switchCamera': 'Switch camera',
  'call.remoteCameraOff': 'Camera off',
  'call.localPreview': 'Your camera preview',
  'call.remoteVideo': 'Remote video',
  'call.ended': 'Call ended — {{duration}}',
  'call.missedFrom': 'Missed call from {{name}}',
  'call.callBack': 'Call back',
  'call.dismiss': 'Dismiss',
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

describe('ChatView – optimistic send + reconciliation', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  // postBody pulls the parsed JSON body of the first POST to the messages
  // endpoint so a test can read the client_id the Composer generated.
  function postClientId(fetchMock: ReturnType<typeof vi.fn>): string {
    const call = fetchMock.mock.calls.find(
      c => /\/messages$/.test(String(c[0])) && (c[1] as RequestInit | undefined)?.method === 'POST',
    )
    return JSON.parse((call![1] as RequestInit).body as string).client_id
  }

  it('renders a typed message instantly in a sending state before any network response', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(streamOk())
      .mockImplementationOnce(() => new Promise(() => {})) // POST never resolves
    vi.stubGlobal('fetch', fetchMock)
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Instant render' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })

    // Visible immediately, with the sending indicator, before the POST resolves.
    expect(screen.getByText('Instant render')).toBeInTheDocument()
    expect(screen.getByText('Sending…')).toBeInTheDocument()
  })

  it('reconciles the optimistic bubble when the SSE message_new arrives first (no duplicate)', async () => {
    const sse = streamWithController()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(sse.response)
      .mockResolvedValueOnce({ ok: true }) // typing indicator POST
      .mockImplementationOnce(() => new Promise(() => {})) // message POST never resolves
    vi.stubGlobal('fetch', fetchMock)
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'SSE first' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })
    await waitFor(() => screen.getByText('SSE first'))
    expect(screen.getByText('Sending…')).toBeInTheDocument()

    const clientId = postClientId(fetchMock)

    await act(async () => {
      sse.push('message_new', {
        message: makeMessage({ id: 77, body: 'SSE first', sender_user_id: 1, client_id: clientId }),
      })
      await Promise.resolve()
    })

    // The authoritative row replaces the optimistic bubble in place.
    expect(screen.getAllByText('SSE first')).toHaveLength(1)
    expect(screen.queryByText('Sending…')).not.toBeInTheDocument()
  })

  it('shows a failed affordance on POST error and reconciles after a tap-to-retry', async () => {
    const okMsg = makeMessage({ id: 88, body: 'Flaky', sender_user_id: 1 })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(streamOk())
      .mockResolvedValueOnce({ ok: true }) // typing indicator POST
      .mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'down' }) }) // message POST fails
      .mockResolvedValueOnce(sendOk(okMsg)) // retry succeeds
    vi.stubGlobal('fetch', fetchMock)
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Flaky' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })

    const retryBtn = await screen.findByText('Failed — tap to retry')

    await act(async () => {
      fireEvent.click(retryBtn)
    })

    await waitFor(() => {
      expect(screen.queryByText('Failed — tap to retry')).not.toBeInTheDocument()
    })
    expect(screen.getAllByText('Flaky')).toHaveLength(1)
  })

  it('does not leak an optimistic bubble into another conversation when switching mid-send', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk(makeConversation({ id: 1, name: 'One' })))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(streamOk())
      .mockResolvedValueOnce({ ok: true }) // typing indicator POST
      .mockImplementationOnce(() => new Promise(() => {})) // conv 1 message POST never resolves
      .mockResolvedValueOnce(convOk(makeConversation({ id: 2, name: 'Two' })))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(streamOk())
    vi.stubGlobal('fetch', fetchMock)

    const { rerender } = renderChatView(1)
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Leaky' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })
    await waitFor(() => screen.getByText('Leaky'))

    await act(async () => {
      rerender(<ChatView conversationId={2} onBack={vi.fn()} />)
    })

    await waitFor(() => screen.getByText('No messages yet. Say hello!'))
    expect(screen.queryByText('Leaky')).not.toBeInTheDocument()
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
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(sse.response)
      .mockResolvedValueOnce({ ok: true }) // typing indicator POST
      .mockResolvedValueOnce(sendOk(newMsg))
    vi.stubGlobal('fetch', fetchMock)
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Dedup me' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })

    await waitFor(() => screen.getByText('Dedup me'))

    // The server echoes the sender's client_id on the message_new broadcast
    // (see internal/familychat/handlers.go), so model that here. Reconciling by
    // client_id collapses the optimistic bubble and the SSE row into one
    // regardless of whether the POST response or the SSE event lands first —
    // which is what makes this dedup deterministic instead of timing-dependent.
    const postCall = fetchMock.mock.calls.find(
      c => /\/messages$/.test(String(c[0])) && (c[1] as RequestInit | undefined)?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const clientId = JSON.parse((postCall![1] as RequestInit).body as string).client_id
    expect(clientId).toBeTruthy()

    await act(async () => {
      sse.push('message_new', { message: { ...newMsg, client_id: clientId } })
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

  it('returns to a live indicator and re-fetches recent messages on successful reconnect', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    let closeStream: (() => void) | null = null
    const firstStream = new ReadableStream<Uint8Array>({
      start(c) { closeStream = () => c.close() },
    })

    const initialMsg = makeMessage({ id: 10, body: 'Before disconnect', conversation_id: 1 })
    const gapMsg = makeMessage({ id: 11, body: 'Filled on reconnect', conversation_id: 1, created_at: '2026-05-01T10:11:00Z' })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([initialMsg]))
      .mockResolvedValueOnce({ ok: true, body: firstStream })
      .mockResolvedValueOnce(msgsOk([gapMsg]))   // gap-fill on reconnect
      .mockResolvedValueOnce(streamOk())          // reconnect stream opens cleanly
    vi.stubGlobal('fetch', fetchMock)

    renderChatView()
    await waitFor(() => expect(screen.getByText('Before disconnect')).toBeInTheDocument())

    // Drop the stream → the Reconnecting badge appears.
    await act(async () => {
      closeStream!()
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(screen.getByTestId('family-chat-reconnecting')).toBeInTheDocument()
    })

    // Fast-forward past the backoff so the reconnect lands.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2500)
    })

    // The Reconnecting badge clears, a brief "Connected" confirmation shows,
    // and the gap-fill backfilled the message that arrived during the drop.
    await waitFor(() => {
      expect(screen.queryByTestId('family-chat-reconnecting')).not.toBeInTheDocument()
      expect(screen.getByTestId('family-chat-connected')).toBeInTheDocument()
      expect(screen.getByText('Filled on reconnect')).toBeInTheDocument()
    })
    const fetchedUrls = fetchMock.mock.calls.map(([url]) => url as string)
    expect(fetchedUrls.some(url => url.includes('/messages?since=10'))).toBe(true)
  }, 15000)

  it('clears the reconnecting indicator when the view unmounts mid-backoff', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    let closeStream: (() => void) | null = null
    const firstStream = new ReadableStream<Uint8Array>({
      start(c) { closeStream = () => c.close() },
    })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce({ ok: true, body: firstStream })
      .mockResolvedValue(streamOk())
    vi.stubGlobal('fetch', fetchMock)

    const { unmount } = renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    await act(async () => {
      closeStream!()
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(screen.getByTestId('family-chat-reconnecting')).toBeInTheDocument()
    })

    // Unmounting while the backoff is pending must tear down cleanly without
    // leaving the indicator stuck.
    unmount()
    expect(screen.queryByTestId('family-chat-reconnecting')).not.toBeInTheDocument()
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

describe('ChatView – offline / online transitions', () => {
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows the Offline indicator when the browser goes offline after being live', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    let closeStream: (() => void) | null = null
    const firstStream = new ReadableStream<Uint8Array>({
      start(c) { closeStream = () => c.close() },
    })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce({ ok: true, body: firstStream })
    vi.stubGlobal('fetch', fetchMock)

    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))
    expect(screen.getByTestId('family-chat-connected')).toBeInTheDocument()

    // Simulate going offline: close the stream, then fire the offline event.
    Object.defineProperty(navigator, 'onLine', { value: false, writable: true, configurable: true })
    await act(async () => {
      closeStream!()
      window.dispatchEvent(new Event('offline'))
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(screen.getByTestId('family-chat-offline')).toBeInTheDocument()
    })

    // Clean up
    Object.defineProperty(navigator, 'onLine', { value: true, writable: true, configurable: true })
  }, 15000)

  it('cancels pending backoff timer when going offline', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    let closeStream: (() => void) | null = null
    const firstStream = new ReadableStream<Uint8Array>({
      start(c) { closeStream = () => c.close() },
    })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce({ ok: true, body: firstStream })
      .mockResolvedValue(streamOk())
    vi.stubGlobal('fetch', fetchMock)

    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    // Drop the stream to trigger a backoff timer.
    await act(async () => {
      closeStream!()
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(screen.getByTestId('family-chat-reconnecting')).toBeInTheDocument()
    })

    // Go offline before the timer fires — no reconnect attempt should happen.
    const callsBefore = fetchMock.mock.calls.length
    Object.defineProperty(navigator, 'onLine', { value: false, writable: true, configurable: true })
    await act(async () => {
      window.dispatchEvent(new Event('offline'))
      await Promise.resolve()
    })

    // Advance past what the backoff delay would have been.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })

    // No additional fetch calls should have been made (the timer was cleared).
    expect(fetchMock.mock.calls.length).toBe(callsBefore)
    expect(screen.getByTestId('family-chat-offline')).toBeInTheDocument()

    // Clean up
    Object.defineProperty(navigator, 'onLine', { value: true, writable: true, configurable: true })
  }, 15000)

  it('retries immediately when coming back online from offline', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    let closeStream: (() => void) | null = null
    const firstStream = new ReadableStream<Uint8Array>({
      start(c) { closeStream = () => c.close() },
    })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce({ ok: true, body: firstStream })
      .mockResolvedValueOnce(msgsOk([]))   // gap-fill on reconnect
      .mockResolvedValueOnce(streamOk())   // reconnect stream
    vi.stubGlobal('fetch', fetchMock)

    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    // Drop the stream then go offline.
    Object.defineProperty(navigator, 'onLine', { value: false, writable: true, configurable: true })
    await act(async () => {
      closeStream!()
      window.dispatchEvent(new Event('offline'))
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(screen.getByTestId('family-chat-offline')).toBeInTheDocument()
    })

    // Come back online — should retry immediately without waiting for backoff.
    Object.defineProperty(navigator, 'onLine', { value: true, writable: true, configurable: true })
    await act(async () => {
      window.dispatchEvent(new Event('online'))
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(screen.getByTestId('family-chat-connected')).toBeInTheDocument()
    })
  }, 15000)
})

describe('ChatView – reactions', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders reaction chips when a message includes reaction data', async () => {
    const msg = makeMessage({
      id: 5,
      body: 'Hello!',
      reactions: { '👍': { count: 2, users: [1, 2], me: true } },
    })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([msg]))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    const chip = await screen.findByTestId('reaction-chip-👍')
    expect(chip).toBeInTheDocument()
    expect(chip.textContent).toContain('2')
  })

  it('opens the reaction picker when the trigger button is clicked', async () => {
    const msg = makeMessage({ id: 5, body: 'Hello!' })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([msg]))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    await waitFor(() => screen.getByTestId(`reaction-trigger-${msg.id}`))

    fireEvent.click(screen.getByTestId(`reaction-trigger-${msg.id}`))
    expect(screen.getByTestId('reaction-picker')).toBeInTheDocument()
  })

  it('closes the picker on Escape key', async () => {
    const msg = makeMessage({ id: 5, body: 'Hello!' })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([msg]))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    await waitFor(() => screen.getByTestId(`reaction-trigger-${msg.id}`))

    fireEvent.click(screen.getByTestId(`reaction-trigger-${msg.id}`))
    expect(screen.getByTestId('reaction-picker')).toBeInTheDocument()

    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByTestId('reaction-picker')).not.toBeInTheDocument())
  })

  it('optimistically increments count on chip click and rolls back on network failure', async () => {
    const msg = makeMessage({
      id: 5,
      body: 'Hello!',
      reactions: { '👍': { count: 1, users: [2], me: false } },
    })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([msg]))
      .mockResolvedValueOnce(streamOk())
      .mockResolvedValueOnce({ ok: false, status: 500 }),
    )
    renderChatView()
    const chip = await screen.findByTestId('reaction-chip-👍')
    expect(chip.textContent).toContain('1')

    fireEvent.click(chip)
    await waitFor(() => expect(screen.getByTestId('reaction-chip-👍').textContent).toContain('2'))

    // Network failed → rollback to original count
    await waitFor(() => expect(screen.getByTestId('reaction-chip-👍').textContent).toContain('1'))
  })

  it('applies a reaction_added SSE event to the message chips', async () => {
    const sse = streamWithController()
    const msg = makeMessage({ id: 5, body: 'Hello!' })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([msg]))
      .mockResolvedValueOnce(sse.response),
    )
    renderChatView()
    await waitFor(() => screen.getByText('Hello!'))

    await act(async () => {
      sse.push('reaction_added', {
        message_id: 5,
        conversation_id: 1,
        user_id: 2,
        emoji: '👍',
        count: 1,
      })
      await Promise.resolve()
    })

    await waitFor(() => expect(screen.getByTestId('reaction-chip-👍')).toBeInTheDocument())
  })
})

describe('ChatView – edit + delete', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('opens the actions menu only for the current user\'s messages', async () => {
    const own = makeMessage({ id: 1, sender_user_id: 1, body: 'My message' })
    const other = makeMessage({ id: 2, sender_user_id: 2, body: 'Their message' })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([other, own])) // newest-first
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    await waitFor(() => screen.getByText('My message'))

    expect(screen.getByTestId(`chat-actions-trigger-${own.id}`)).toBeInTheDocument()
    expect(screen.queryByTestId(`chat-actions-trigger-${other.id}`)).not.toBeInTheDocument()
  })

  it('edits the message body via PATCH and shows the edited tag', async () => {
    const own = makeMessage({ id: 5, sender_user_id: 1, body: 'Before edit' })
    const editedAt = '2026-05-01T10:30:00Z'
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([own]))
      .mockResolvedValueOnce(streamOk())
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          message: { ...own, body: 'After edit', edited_at: editedAt },
        }),
      }),
    )
    renderChatView()
    await waitFor(() => screen.getByText('Before edit'))

    fireEvent.click(screen.getByTestId(`chat-actions-trigger-${own.id}`))
    fireEvent.click(screen.getByTestId(`chat-edit-action-${own.id}`))

    const input = await screen.findByTestId(`chat-edit-input-${own.id}`) as HTMLTextAreaElement
    fireEvent.change(input, { target: { value: 'After edit' } })
    fireEvent.click(screen.getByTestId(`chat-edit-save-${own.id}`))

    await waitFor(() => expect(screen.getByText('After edit')).toBeInTheDocument())
    await waitFor(() => expect(screen.getByTestId(`chat-edited-tag-${own.id}`)).toBeInTheDocument())
  })

  it('confirms before deleting and renders a tombstone bubble on success', async () => {
    const own = makeMessage({ id: 7, sender_user_id: 1, body: 'Goodbye' })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([own]))
      .mockResolvedValueOnce(streamOk())
      .mockResolvedValueOnce({ ok: true, status: 204 }),
    )
    renderChatView()
    await waitFor(() => screen.getByText('Goodbye'))

    fireEvent.click(screen.getByTestId(`chat-actions-trigger-${own.id}`))
    fireEvent.click(screen.getByTestId(`chat-delete-action-${own.id}`))

    expect(screen.getByTestId('chat-delete-confirm')).toBeInTheDocument()
    fireEvent.click(screen.getByTestId('chat-delete-confirm-button'))

    await waitFor(() => expect(screen.getByTestId(`chat-tombstone-${own.id}`)).toBeInTheDocument())
    expect(screen.queryByText('Goodbye')).not.toBeInTheDocument()
  })

  it('applies a message_edited SSE event to the open bubble', async () => {
    const sse = streamWithController()
    const msg = makeMessage({ id: 9, body: 'Original', sender_user_id: 2 })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([msg]))
      .mockResolvedValueOnce(sse.response),
    )
    renderChatView()
    await waitFor(() => screen.getByText('Original'))

    await act(async () => {
      sse.push('message_edited', {
        message_id: 9,
        conversation_id: 1,
        body: 'Live edit',
        edited_at: '2026-05-01T11:00:00Z',
      })
      await Promise.resolve()
    })

    await waitFor(() => expect(screen.getByText('Live edit')).toBeInTheDocument())
    expect(screen.queryByText('Original')).not.toBeInTheDocument()
    expect(screen.getByTestId(`chat-edited-tag-${msg.id}`)).toBeInTheDocument()
  })

  it('applies a message_deleted SSE event to convert the bubble into a tombstone', async () => {
    const sse = streamWithController()
    const msg = makeMessage({ id: 11, body: 'Will be deleted', sender_user_id: 2 })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([msg]))
      .mockResolvedValueOnce(sse.response),
    )
    renderChatView()
    await waitFor(() => screen.getByText('Will be deleted'))

    await act(async () => {
      sse.push('message_deleted', {
        message_id: 11,
        conversation_id: 1,
        deleted_by: 2,
      })
      await Promise.resolve()
    })

    await waitFor(() => expect(screen.getByTestId(`chat-tombstone-${msg.id}`)).toBeInTheDocument())
    expect(screen.queryByText('Will be deleted')).not.toBeInTheDocument()
  })
})

describe('ChatView – voice note rendering', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })
  beforeEach(() => { try { window.localStorage.clear() } catch { /* ignore */ } })

  it('renders VoiceBubble (not native audio) for audio/webm with empty body and meta_json waveform', async () => {
    const bars = Array.from({ length: 32 }, (_, i) => (i + 1) / 32)
    const voiceMsg = makeMessage({
      id: 20,
      body: '',
      sender_user_id: 2,
      attachment_path: 'aabbccddeeff00112233445566778899',
      attachment_mime: 'audio/webm',
      meta_json: JSON.stringify({ bars, durationMs: 3000 }),
    })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([voiceMsg]))
      .mockResolvedValueOnce(streamOk()),
    )
    const { container } = renderChatView()
    await waitFor(() => {
      expect(screen.getByTestId('voice-bubble-20')).toBeInTheDocument()
    })
    expect(container.querySelector('audio[controls]')).toBeNull()
  })

  it('renders VoiceBubble for audio/ogg with empty body (Firefox voice note)', async () => {
    const voiceMsg = makeMessage({
      id: 21,
      body: '',
      sender_user_id: 2,
      attachment_path: 'aabbccddeeff00112233445566778899',
      attachment_mime: 'audio/ogg',
    })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([voiceMsg]))
      .mockResolvedValueOnce(streamOk()),
    )
    const { container } = renderChatView()
    await waitFor(() => {
      expect(screen.getByTestId('voice-bubble-21')).toBeInTheDocument()
    })
    expect(container.querySelector('audio[controls]')).toBeNull()
  })

  it('renders VoiceBubble using the localStorage waveform when meta_json is absent', async () => {
    const bars = Array.from({ length: 32 }, (_, i) => i / 32)
    window.localStorage.setItem('voice-waveform:22', JSON.stringify({ bars, durationMs: 4500 }))

    const voiceMsg = makeMessage({
      id: 22,
      body: '',
      sender_user_id: 2,
      attachment_path: 'aabbccddeeff00112233445566778899',
      attachment_mime: 'audio/webm',
    })
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([voiceMsg]))
      .mockResolvedValueOnce(streamOk()),
    )
    const { container } = renderChatView()
    await waitFor(() => {
      expect(screen.getByTestId('voice-bubble-22')).toBeInTheDocument()
    })
    expect(container.querySelector('audio[controls]')).toBeNull()
  })

  it('renders native audio controls for audio/mpeg (not a voice note)', async () => {
    const audioMsg = makeMessage({
      id: 23,
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
    expect(screen.queryByTestId('voice-bubble-23')).toBeNull()
  })
})

// ── Voice call helpers / fakes ────────────────────────────────────────────────

// Fake media + RTC fakes mirror the shapes used by useVoiceCall's own tests so
// the hook's real signalling code runs against a deterministic peer
// connection. The ChatView tests only need a couple of behavioural checks:
//   • An incoming SSE call_offer drives the hook into 'incoming-ringing' so
//     the overlay appears.
//   • Clicking Accept fires the TURN fetch + the /answer POST.
// Anything beyond that is exhaustively covered in useVoiceCall.test.tsx.

interface CallFetchEntry { url: string; method: string; body?: string }

class FakeAudioTrack {
  kind = 'audio' as const
  enabled = true
  stop = vi.fn()
}

function makeFakeAudioStream(): MediaStream {
  const track = new FakeAudioTrack()
  const tracks = [track]
  return {
    getTracks: () => tracks,
    getAudioTracks: () => tracks,
    getVideoTracks: () => [],
  } as unknown as MediaStream
}

class FakeRtcPeerConnection {
  static instances: FakeRtcPeerConnection[] = []
  localDescription: RTCSessionDescriptionInit | null = null
  remoteDescription: RTCSessionDescriptionInit | null = null
  ontrack: ((event: RTCTrackEvent) => void) | null = null
  onicecandidate: ((event: RTCPeerConnectionIceEvent) => void) | null = null
  oniceconnectionstatechange: (() => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  closed = false
  constructor() { FakeRtcPeerConnection.instances.push(this) }
  addTrack() { return {} as RTCRtpSender }
  async createOffer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'offer', sdp: 'v=0\r\nfake-offer' }
  }
  async createAnswer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'answer', sdp: 'v=0\r\nfake-answer' }
  }
  async setLocalDescription(d: RTCSessionDescriptionInit) { this.localDescription = d }
  async setRemoteDescription(d: RTCSessionDescriptionInit) { this.remoteDescription = d }
  async addIceCandidate() { /* no-op */ }
  close() { this.closed = true }
}

// installCallEnv stubs the browser APIs the voice-call hook touches so the
// regular ChatView SSE fetch paths can drive the call state machine without
// poking at a real audio device.
let _originalMediaDevicesDescriptor: PropertyDescriptor | undefined

function installCallEnv() {
  vi.stubGlobal('RTCPeerConnection', FakeRtcPeerConnection as unknown as typeof RTCPeerConnection)
  _originalMediaDevicesDescriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices')
  const sharedStream = makeFakeAudioStream()
  const getUserMedia = vi.fn(async () => sharedStream)
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia },
  })
  return { getUserMedia }
}

function uninstallCallEnv() {
  if (_originalMediaDevicesDescriptor !== undefined) {
    Object.defineProperty(navigator, 'mediaDevices', _originalMediaDevicesDescriptor)
  } else {
    delete (navigator as unknown as Record<string, unknown>).mediaDevices
  }
  _originalMediaDevicesDescriptor = undefined
}

describe('ChatView – call UI', () => {
  beforeEach(() => { FakeRtcPeerConnection.instances = [] })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    uninstallCallEnv()
  })

  it('renders the phone button only for two-member conversations', async () => {
    const conv2 = makeConversation({ id: 1, name: 'One on One', member_ids: [1, 2] })
    installCallEnv()
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk(conv2))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))
    expect(screen.getByTestId('family-chat-call-button')).toBeInTheDocument()
  })

  it('hides the phone button for group conversations (>2 members)', async () => {
    const group = makeConversation({ id: 1, name: 'Group', member_ids: [1, 2, 3] })
    installCallEnv()
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk(group))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))
    expect(screen.queryByTestId('family-chat-call-button')).not.toBeInTheDocument()
  })

  it('opens the incoming-call overlay when a call_offer SSE event arrives', async () => {
    const conv2 = makeConversation({ id: 1, name: 'One on One', member_ids: [1, 2] })
    const sse = streamWithController()
    installCallEnv()
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk(conv2))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(sse.response),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    await act(async () => {
      sse.push('call_offer', {
        conversation_id: 1,
        call_id: 'inbound-A',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nremote-offer' },
      })
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(screen.getByTestId('family-chat-incoming-overlay')).toBeInTheDocument()
    })
    expect(screen.getByTestId('family-chat-call-accept')).toBeInTheDocument()
    expect(screen.getByTestId('family-chat-call-decline')).toBeInTheDocument()
  })

  it('accepting an incoming call fetches TURN config and POSTs an answer to the relay', async () => {
    const conv2 = makeConversation({ id: 1, name: 'One on One', member_ids: [1, 2] })
    const sse = streamWithController()
    installCallEnv()
    const callFetches: CallFetchEntry[] = []

    // Single fetch handler covers every URL the test cares about: the initial
    // conv/messages/stream triple plus the TURN + signalling POSTs that
    // useVoiceCall fires after Accept. Using a router rather than a chain of
    // mockResolvedValueOnce keeps the test resilient to small ordering
    // changes in the hook's implementation.
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString()
      const method = (init?.method ?? 'GET').toUpperCase()
      const body = typeof init?.body === 'string' ? init.body : undefined
      if (url.includes('/calls/') || url.includes('/api/familychat/turn')) {
        callFetches.push({ url, method, body })
      }
      if (url.endsWith('/api/familychat/conversations/1') && method === 'GET') {
        return new Response(JSON.stringify({ conversation: conv2 }), { status: 200 })
      }
      if (url.endsWith('/api/familychat/conversations/1/messages') && method === 'GET') {
        return new Response(JSON.stringify({ messages: [] }), { status: 200 })
      }
      if (url.includes('/conversations/1/stream')) {
        return sse.response as Response
      }
      if (url.endsWith('/api/familychat/turn')) {
        return new Response(JSON.stringify({
          iceServers: [{ urls: ['stun:stun.example.com:3478'] }],
          ttl: 3600,
        }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      }
      if (url.includes('/calls/')) {
        return new Response(null, { status: 204 })
      }
      throw new Error(`unexpected fetch ${method} ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    await act(async () => {
      sse.push('call_offer', {
        conversation_id: 1,
        call_id: 'inbound-accept',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nremote-offer' },
      })
      await Promise.resolve()
    })
    await waitFor(() => screen.getByTestId('family-chat-call-accept'))

    await act(async () => {
      fireEvent.click(screen.getByTestId('family-chat-call-accept'))
    })

    await waitFor(() => {
      const turnCall = callFetches.find(c =>
        c.method === 'GET' && c.url.endsWith('/api/familychat/turn'))
      expect(turnCall).toBeDefined()
    })
    await waitFor(() => {
      const answerCall = callFetches.find(c =>
        c.method === 'POST' && c.url.includes('/calls/inbound-accept/answer'))
      expect(answerCall).toBeDefined()
    })

    // After accept the UI transitions to the active overlay.
    await waitFor(() => {
      expect(screen.getByTestId('family-chat-active-overlay')).toBeInTheDocument()
    })
  })

  it('renders a missed-call tombstone row when a call_end with status=missed arrives', async () => {
    const conv2 = makeConversation({ id: 1, name: 'One on One', member_ids: [1, 2] })
    const sse = streamWithController()
    installCallEnv()
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk(conv2))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(sse.response),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    await act(async () => {
      sse.push('call_end', {
        conversation_id: 1,
        call_id: 'missed-1',
        from_user_id: 2,
        status: 'missed',
      })
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(screen.getByTestId('missed-call-missed-1')).toBeInTheDocument()
    })
    expect(screen.getByTestId('missed-call-back-missed-1')).toBeInTheDocument()
  })

  it('renders the video call button next to the phone button in 2-member conversations', async () => {
    const conv2 = makeConversation({ id: 1, name: 'One on One', member_ids: [1, 2] })
    installCallEnv()
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk(conv2))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(streamOk()),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))
    expect(screen.getByTestId('family-chat-video-call-button')).toBeInTheDocument()
  })

  it('shows the video-call label on the incoming overlay when the offer carries kind=video', async () => {
    const conv2 = makeConversation({ id: 1, name: 'One on One', member_ids: [1, 2] })
    const sse = streamWithController()
    installCallEnv()
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(convOk(conv2))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(sse.response),
    )
    renderChatView()
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    await act(async () => {
      sse.push('call_offer', {
        conversation_id: 1,
        call_id: 'inbound-video-1',
        from_user_id: 2,
        data: { type: 'offer', sdp: 'v=0\r\nremote-offer' },
        kind: 'video',
      })
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(screen.getByTestId('family-chat-incoming-overlay')).toBeInTheDocument()
    })
    expect(screen.getByTestId('family-chat-incoming-kind-label').textContent).toBe('Incoming video call')
  })
})
