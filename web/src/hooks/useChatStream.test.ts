// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import type { Dispatch, RefObject, SetStateAction } from 'react'
import { useChatStream } from './useChatStream'
import type { Conversation, Message } from './useChatStream'

// ── i18n mock ─────────────────────────────────────────────────────────────────
// Return the key verbatim so error assertions are deterministic and `t` is a
// stable reference (matching real react-i18next) so useCallback deps that
// include `t` don't invalidate every render.
vi.mock('react-i18next', () => {
  const t = (key: string) => key
  const i18n = { language: 'en' }
  return { useTranslation: () => ({ t, i18n }) }
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

// ── SSE frame + controllable stream helpers ─────────────────────────────────────
// Mirrors the backend wire format: `event: <name>\ndata: <json>\n\n`.
const frame = (event: string, data: unknown) =>
  `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`

// A hand-driven ReadableStream-ish body: tests push SSE chunks and close it, and
// aborting the attached signal rejects the pending read with an AbortError —
// matching the real fetch contract the hook's read loop relies on.
function makeSSEStream() {
  const encoder = new TextEncoder()
  const queue: Array<{ done: boolean; value?: Uint8Array }> = []
  let pending: { resolve: (r: { done: boolean; value?: Uint8Array }) => void; reject: (e: unknown) => void } | null = null
  let signal: AbortSignal | undefined

  const reader = {
    read() {
      return new Promise<{ done: boolean; value?: Uint8Array }>((resolve, reject) => {
        if (signal?.aborted) {
          reject(new DOMException('Aborted', 'AbortError'))
          return
        }
        const next = queue.shift()
        if (next) {
          resolve(next)
          return
        }
        pending = { resolve, reject }
      })
    },
    cancel() {},
  }

  return {
    body: { getReader: () => reader },
    attachSignal(s: AbortSignal | undefined) {
      signal = s
      s?.addEventListener(
        'abort',
        () => {
          if (pending) {
            pending.reject(new DOMException('Aborted', 'AbortError'))
            pending = null
          }
        },
        { once: true },
      )
    },
    push(text: string) {
      const value = encoder.encode(text)
      if (pending) {
        const p = pending
        pending = null
        p.resolve({ done: false, value })
      } else {
        queue.push({ done: false, value })
      }
    },
    close() {
      if (pending) {
        const p = pending
        pending = null
        p.resolve({ done: true })
      } else {
        queue.push({ done: true })
      }
    },
  }
}

type SSEStream = ReturnType<typeof makeSSEStream>

interface FetchOpts {
  // When set, the stream POST responds non-OK with this error body.
  streamError?: string
  // Conversations returned by the post-stream refetch.
  conversations?: Conversation[]
}

function installFetch(stream: SSEStream, opts: FetchOpts = {}) {
  const fetchMock = vi.fn((url: string, options?: { signal?: AbortSignal }) => {
    if (url.includes('/messages/stream')) {
      if (opts.streamError) {
        return Promise.resolve({
          ok: false,
          body: null,
          json: () => Promise.resolve({ error: opts.streamError }),
        })
      }
      stream.attachSignal(options?.signal)
      return Promise.resolve({ ok: true, body: stream.body })
    }
    // Post-stream conversation-list refetch.
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve({ conversations: opts.conversations ?? [] }),
    })
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

// ── Harness ─────────────────────────────────────────────────────────────────
const CONV: Conversation = {
  id: 1,
  user_id: 1,
  title: 'Chat',
  model: 'claude',
  created_at: '2026-06-03T00:00:00Z',
  updated_at: '2026-06-03T00:00:00Z',
}

interface HarnessState {
  messages: Message[]
  conversations: Conversation[]
  active: Conversation | null
  input: string
  sending: Set<number>
  deleted: RefObject<Set<number>>
}

function makeHarness(initial?: { sending?: Set<number> }) {
  const state: HarnessState = {
    messages: [],
    conversations: [],
    active: CONV,
    input: '',
    sending: initial?.sending ?? new Set<number>(),
    deleted: { current: new Set<number>() },
  }

  const setMessages: Dispatch<SetStateAction<Message[]>> = u => {
    state.messages = typeof u === 'function' ? (u as (p: Message[]) => Message[])(state.messages) : u
  }
  const setConversations: Dispatch<SetStateAction<Conversation[]>> = u => {
    state.conversations =
      typeof u === 'function' ? (u as (p: Conversation[]) => Conversation[])(state.conversations) : u
  }
  const setActiveConversation: Dispatch<SetStateAction<Conversation | null>> = u => {
    state.active =
      typeof u === 'function' ? (u as (p: Conversation | null) => Conversation | null)(state.active) : u
  }

  const params = {
    activeConversation: state.active,
    setActiveConversation,
    setMessages,
    setConversations,
    sendingConversationIds: state.sending,
    addSendingConversation: (id: number) => state.sending.add(id),
    removeSendingConversation: (id: number) => state.sending.delete(id),
    deletedConversationIds: state.deleted,
    setInput: (v: string) => {
      state.input = v
    },
    pinToBottom: () => {},
    focusInput: () => {},
  }

  return { state, params }
}

const userFrame = (overrides?: Partial<Message>) =>
  frame('user_message', {
    id: 10,
    conversation_id: 1,
    role: 'user',
    content: 'hi',
    created_at: '2026-06-03T00:00:01Z',
    ...overrides,
  })

const doneFrame = (content: string) =>
  frame('done', {
    assistant_message: {
      id: 11,
      conversation_id: 1,
      role: 'assistant',
      content,
      created_at: '2026-06-03T00:00:02Z',
    },
  })

describe('useChatStream', () => {
  it('success: swaps placeholders for canonical rows and streams tokens', async () => {
    const stream = makeSSEStream()
    installFetch(stream, { conversations: [{ ...CONV, title: 'Auto title' }] })
    const { state, params } = makeHarness()
    const { result } = renderHook(() => useChatStream(params))

    await act(async () => {
      const p = result.current.send('hi')
      stream.push(userFrame())
      stream.push(frame('token', { text: 'Hel' }))
      stream.push(frame('token', { text: 'lo' }))
      stream.push(doneFrame('Hello'))
      stream.close()
      await p
    })

    // Canonical rows (positive ids) replaced the optimistic placeholders.
    expect(state.messages).toHaveLength(2)
    expect(state.messages[0]).toMatchObject({ id: 10, role: 'user', content: 'hi' })
    expect(state.messages[1]).toMatchObject({ id: 11, role: 'assistant', content: 'Hello' })
    expect(result.current.error).toBe('')
    expect(result.current.streamingId).toBeNull()
    expect(state.sending.has(1)).toBe(false)
    // Post-stream refetch updated the conversation list.
    expect(state.conversations).toEqual([{ ...CONV, title: 'Auto title' }])
  })

  it('server error: surfaces error and keeps the persisted user message', async () => {
    const stream = makeSSEStream()
    installFetch(stream)
    const { state, params } = makeHarness()
    const { result } = renderHook(() => useChatStream(params))

    await act(async () => {
      const p = result.current.send('hi')
      stream.push(userFrame())
      stream.push(frame('error', { error: 'rate limited' }))
      stream.close()
      await p
    })

    expect(result.current.error).toBe('rate limited')
    // The user message persisted by the backend stays visible (canonical row),
    // the empty assistant placeholder is dropped, and the draft is restored.
    expect(state.messages).toHaveLength(1)
    expect(state.messages[0]).toMatchObject({ id: 10, role: 'user', content: 'hi' })
    expect(state.input).toBe('hi')
    expect(state.sending.has(1)).toBe(false)
  })

  it('abort before user_message: removes both optimistic placeholders', async () => {
    const stream = makeSSEStream()
    installFetch(stream)
    const { state, params } = makeHarness()
    const { result } = renderHook(() => useChatStream(params))

    await act(async () => {
      const p = result.current.send('hi')
      // Both placeholders exist synchronously before any frame arrives.
      expect(state.messages).toHaveLength(2)
      result.current.stop()
      await p
    })

    expect(state.messages).toHaveLength(0)
    // Draft restored so the user can re-send.
    expect(state.input).toBe('hi')
    expect(result.current.error).toBe('')
    expect(state.sending.has(1)).toBe(false)
  })

  it('abort after tokens: keeps the partial assistant message', async () => {
    const stream = makeSSEStream()
    installFetch(stream)
    const { state, params } = makeHarness()
    const { result } = renderHook(() => useChatStream(params))

    let pending!: Promise<void>
    await act(async () => {
      pending = result.current.send('hi')
      stream.push(userFrame())
      stream.push(frame('token', { text: 'Partial' }))
    })
    // Guard: the token frame was consumed and applied to the placeholder before
    // we abort — this is what distinguishes the "after tokens" branch.
    expect(state.messages[1]).toMatchObject({ role: 'assistant', content: 'Partial' })

    // The read loop is now awaiting the next read; aborting hits the
    // sawToken branch which keeps the partial assistant text on screen.
    await act(async () => {
      result.current.stop()
      await pending
    })

    expect(state.messages).toHaveLength(2)
    // Assistant placeholder retains the partial streamed text.
    expect(state.messages[1]).toMatchObject({ role: 'assistant', content: 'Partial' })
    // No error and the draft is NOT restored (Stop is intentional, not a failure).
    expect(result.current.error).toBe('')
    expect(state.input).toBe('')
    expect(state.sending.has(1)).toBe(false)
  })

  it('preserves a concurrent send on a different conversation', async () => {
    const stream = makeSSEStream()
    installFetch(stream, { conversations: [CONV] })
    // Conversation 2 already has an in-flight send when we send in conversation 1.
    const { state, params } = makeHarness({ sending: new Set<number>([2]) })
    const { result } = renderHook(() => useChatStream(params))

    await act(async () => {
      const p = result.current.send('hi')
      stream.push(userFrame())
      stream.push(doneFrame('Hello'))
      stream.close()
      await p
    })

    // The send/reconcile lifecycle only touched conversation 1's membership;
    // the concurrent send on conversation 2 is untouched.
    expect(state.sending.has(1)).toBe(false)
    expect(state.sending.has(2)).toBe(true)
  })
})
