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
  'composer.errors.send': 'Failed to send message',
  'composer.errors.tooLong': 'Message is too long',
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
      .mockResolvedValueOnce(msgsOk()),
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
      .mockResolvedValueOnce(msgsOk([])),
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
      .mockResolvedValueOnce(msgsOk(messages)),
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
      .mockResolvedValueOnce(convOk(makeConversation({ id: 2, name: 'Chat Two' })))
      .mockResolvedValueOnce(msgsOk(conv2msgs)),
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
