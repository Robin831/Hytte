// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act, within } from '@testing-library/react'
import FamilyChat from './FamilyChat'

// ── Translation mock ──────────────────────────────────────────────────────────
// stableT must be a stable reference — ConversationList / ChatView / the modal
// all keep `t` in effect deps, so a fresh function on every render would loop
// the fetch mocks out of order.

const TRANSLATIONS: Record<string, string> = {
  title: 'Family Chat',
  newConversation: 'New conversation',
  unnamedConversation: 'Untitled chat',
  loading: 'Loading…',
  'unreadCount_one': '{{count}} unread message',
  'unreadCount_other': '{{count}} unread messages',
  'empty.title': 'No conversations yet',
  'empty.hint': 'Start a new chat with your family.',
  'empty.noMessages': 'No messages yet',
  'errors.failedToLoad': 'Failed to load conversations',
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
  'newModal.title': 'New conversation',
  'newModal.nameLabel': 'Name',
  'newModal.namePlaceholder': 'e.g. Holiday planning',
  'newModal.membersLabel': 'Members',
  'newModal.parent': 'Parent',
  'newModal.noMembers':
    'No family members available. Add family on the Family page first.',
  'newModal.cancel': 'Cancel',
  'newModal.create': 'Create',
  'newModal.creating': 'Creating…',
  'newModal.errors.loadMembers': 'Failed to load family members',
  'newModal.errors.create': 'Failed to create conversation',
  'actions.close': 'Close',
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
  familyStatus: { is_parent: true, is_child: false } as {
    is_parent: boolean
    is_child: boolean
  } | null,
}

vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

function makeConversation(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    name: 'Family Chat',
    owner_user_id: 1,
    created_at: '2026-05-01T00:00:00Z',
    last_message_at: '2026-05-01T10:00:00Z',
    unread_count: 0,
    member_ids: [1],
    last_message_preview: 'Hello!',
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

// jsonOk wraps a payload in the minimal Response-shape the components consume.
function jsonOk(payload: unknown) {
  return { ok: true, json: () => Promise.resolve(payload) }
}

// streamWithController exposes the underlying controller so a test can push
// SSE frames into the live stream and close it on cleanup.
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

// neverEndingStream parks the SSE reader so it does not close on its own; the
// test cleans up via unmount, which aborts the controller and cancels the
// reader. Used when a test doesn't need to push live frames.
function neverEndingStream() {
  const stream = new ReadableStream<Uint8Array>({ start() { /* hold open */ } })
  return { ok: true, body: stream }
}

// ── Fetch router ──────────────────────────────────────────────────────────────
// FamilyChat mounts three children that each fire requests, so a strict ordered
// `mockResolvedValueOnce` queue is brittle. The router dispatches on URL +
// method, returning per-test handlers. Unknown calls throw so a missing handler
// is obvious in the failure output.

type FetchHandler = (init: RequestInit | undefined) => unknown

function makeFetch(routes: Record<string, FetchHandler>) {
  return vi.fn((url: string, init?: RequestInit) => {
    const method = (init?.method ?? 'GET').toUpperCase()
    const key = `${method} ${url}`
    const handler = routes[key]
    if (!handler) {
      // Surface unhandled routes loudly so the test author can wire them up.
      throw new Error(`Unhandled fetch: ${key}`)
    }
    return Promise.resolve(handler(init))
  })
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('FamilyChat – initial render', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders the conversation list and an empty chat hint on first load', async () => {
    const conv = makeConversation({
      id: 1,
      name: 'Family Chat',
      last_message_preview: 'Hello!',
    })
    vi.stubGlobal('fetch', makeFetch({
      'GET /api/familychat/conversations': () => jsonOk({ conversations: [conv] }),
      // ChatView's memberLookup effect fires unconditionally for parents.
      'GET /api/family/children': () => jsonOk({ children: [] }),
    }))

    render(<FamilyChat />)

    // Sidebar header + conversation row
    expect(await screen.findByRole('heading', { name: 'Family Chat' })).toBeInTheDocument()
    expect(await screen.findByText('Hello!')).toBeInTheDocument()

    // No conversation selected → ChatView shows the no-selection hint
    expect(screen.getByText('Pick a conversation')).toBeInTheDocument()
  })

  it('selecting a conversation loads its messages and shows the composer', async () => {
    const conv = makeConversation({ id: 1, name: 'Family Chat' })
    const messages = [
      makeMessage({ id: 1, body: 'First message', sender_user_id: 1 }),
      makeMessage({ id: 2, body: 'Second message', sender_user_id: 2 }),
    ]
    vi.stubGlobal('fetch', makeFetch({
      'GET /api/familychat/conversations': () => jsonOk({ conversations: [conv] }),
      'GET /api/family/children': () => jsonOk({ children: [] }),
      'GET /api/familychat/conversations/1': () => jsonOk({ conversation: conv }),
      // API returns newest-first; ChatView reverses for display.
      'GET /api/familychat/conversations/1/messages': () => jsonOk({
        messages: [...messages].reverse(),
      }),
      'GET /api/familychat/conversations/1/stream': () => neverEndingStream(),
    }))

    render(<FamilyChat />)

    // Pick the conversation row by clicking on the sidebar row (button).
    const row = await screen.findByRole('button', { name: /Family Chat/ })
    fireEvent.click(row)

    await waitFor(() => {
      expect(screen.getByText('First message')).toBeInTheDocument()
      expect(screen.getByText('Second message')).toBeInTheDocument()
    })
    // Composer textarea is mounted in the right column.
    expect(screen.getByPlaceholderText('Write a message…')).toBeInTheDocument()
  })
})

describe('FamilyChat – sending a message', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('clears the textarea and POSTs the message after a successful send', async () => {
    const conv = makeConversation({ id: 1, name: 'Family Chat' })
    const newMsg = makeMessage({ id: 42, body: 'Sent via composer', sender_user_id: 1 })
    let postCalled = false
    let postBody: unknown = null

    vi.stubGlobal('fetch', makeFetch({
      'GET /api/familychat/conversations': () => jsonOk({ conversations: [conv] }),
      'GET /api/family/children': () => jsonOk({ children: [] }),
      'GET /api/familychat/conversations/1': () => jsonOk({ conversation: conv }),
      'GET /api/familychat/conversations/1/messages': () => jsonOk({ messages: [] }),
      'GET /api/familychat/conversations/1/stream': () => neverEndingStream(),
      'POST /api/familychat/conversations/1/messages': (init) => {
        postCalled = true
        postBody = JSON.parse(init?.body as string)
        return jsonOk({ message: newMsg })
      },
    }))

    render(<FamilyChat />)
    const row = await screen.findByRole('button', { name: /Family Chat/ })
    fireEvent.click(row)
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    const textarea = screen.getByPlaceholderText('Write a message…') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Sent via composer' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(postCalled).toBe(true)
      expect(textarea.value).toBe('')
    })
    expect(postBody).toEqual({ body: 'Sent via composer' })
    expect(screen.getByText('Sent via composer')).toBeInTheDocument()
  })
})

describe('FamilyChat – SSE live updates', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('appends a message_new event delivered over the SSE stream', async () => {
    const conv = makeConversation({ id: 1, name: 'Family Chat' })
    const sse = streamWithController()
    vi.stubGlobal('fetch', makeFetch({
      'GET /api/familychat/conversations': () => jsonOk({ conversations: [conv] }),
      'GET /api/family/children': () => jsonOk({ children: [] }),
      'GET /api/familychat/conversations/1': () => jsonOk({ conversation: conv }),
      'GET /api/familychat/conversations/1/messages': () => jsonOk({ messages: [] }),
      'GET /api/familychat/conversations/1/stream': () => sse.response,
    }))

    render(<FamilyChat />)
    const row = await screen.findByRole('button', { name: /Family Chat/ })
    fireEvent.click(row)
    await waitFor(() => screen.getByText('No messages yet. Say hello!'))

    await act(async () => {
      sse.push('message_new', {
        message: makeMessage({
          id: 77,
          body: 'Live from SSE',
          conversation_id: 1,
          sender_user_id: 2,
          created_at: '2026-05-01T11:00:00Z',
        }),
      })
      // Yield so the read loop processes the enqueued frame.
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(screen.getByText('Live from SSE')).toBeInTheDocument()
    })
  })
})

describe('FamilyChat – new conversation modal', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('opens the modal, creates a conversation, and selects it', async () => {
    let convListCalls = 0
    let postBody: unknown = null
    const initialConv = makeConversation({
      id: 1,
      name: 'Existing chat',
      last_message_preview: 'Old',
    })
    const createdConv = makeConversation({
      id: 99,
      name: 'New Family Chat',
      member_ids: [1, 7],
      last_message_preview: '',
    })

    vi.stubGlobal('fetch', makeFetch({
      'GET /api/familychat/conversations': () => {
        convListCalls += 1
        // After the modal creates conversation 99, the second list fetch
        // includes it so the sidebar can render the newly selected item.
        return jsonOk({
          conversations: convListCalls === 1
            ? [initialConv]
            : [createdConv, initialConv],
        })
      },
      'GET /api/family/children': () => jsonOk({
        children: [
          { child_id: 7, nickname: 'Bobby', avatar_emoji: '🐢' },
        ],
      }),
      'POST /api/familychat/conversations': (init) => {
        postBody = JSON.parse(init?.body as string)
        return jsonOk({ conversation: createdConv })
      },
      'GET /api/familychat/conversations/99': () => jsonOk({ conversation: createdConv }),
      'GET /api/familychat/conversations/99/messages': () => jsonOk({ messages: [] }),
      'GET /api/familychat/conversations/99/stream': () => neverEndingStream(),
    }))

    render(<FamilyChat />)

    // Wait for the sidebar to render before clicking the "New conversation" button.
    await screen.findByText('Existing chat')

    // Open the modal via the header "New conversation" button.
    fireEvent.click(screen.getByRole('button', { name: 'New conversation' }))

    // Modal title appears. The Dialog renders a heading with the modal title.
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('New conversation')).toBeInTheDocument()

    // Wait for the members list to load (Bobby comes from /family/children).
    await within(dialog).findByText('Bobby')

    // Fill in the name and toggle Bobby. The checkbox is visually sr-only but
    // still accessible via its label text, so fireEvent.click on the input
    // toggles selection reliably (clicking a label child does not propagate
    // to the input under happy-dom).
    const nameInput = within(dialog).getByLabelText('Name')
    fireEvent.change(nameInput, { target: { value: 'New Family Chat' } })
    const bobbyCheckbox = within(dialog).getByRole('checkbox', { name: 'Bobby' })
    fireEvent.click(bobbyCheckbox)

    // Submit the form.
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() => {
      expect(postBody).toEqual({
        name: 'New Family Chat',
        member_user_ids: [7],
      })
    })

    // After create, ChatView loads conversation 99 — its name renders in the
    // chat header. The modal is also closed.
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(
      await screen.findByRole('heading', { name: 'New Family Chat' }),
    ).toBeInTheDocument()

    // Creating a conversation calls refreshConversations() via FamilyChatContext,
    // which re-fetches the list. The second fetch returns the created conversation
    // so the sidebar reflects the new chat — no refreshKey counter prop involved.
    await waitFor(() => {
      expect(convListCalls).toBeGreaterThanOrEqual(2)
    })
    const sidebar = screen.getByTestId('family-chat-conversation-list')
    expect(within(sidebar).getByText('New Family Chat')).toBeInTheDocument()
  })
})
