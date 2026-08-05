// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import {
  useFamilyChatStream,
  type ChatMessage,
  type Conversation,
  type UseFamilyChatStreamOptions,
} from './useFamilyChatStream'
import type { CallSignalPayload } from './voice/useVoiceCall'

// ── i18n mock ─────────────────────────────────────────────────────────────────
// `t` must keep a stable identity across renders: it is a dependency of the
// stream effect, so a fresh function per render would re-subscribe endlessly.
const stableT = (key: string) => key

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

const encoder = new TextEncoder()

function makeConversation(memberIds: number[] = [1, 2]): Conversation {
  return {
    id: 1,
    name: 'Family Chat',
    owner_user_id: 1,
    created_at: '2026-05-01T00:00:00Z',
    last_message_at: '2026-05-01T10:00:00Z',
    unread_count: 0,
    member_ids: memberIds,
  }
}

function makeMessage(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 1,
    conversation_id: 1,
    sender_user_id: 2,
    body: 'Hello!',
    created_at: '2026-05-01T10:00:00Z',
    ...overrides,
  }
}

function convOk(conv: Conversation = makeConversation()) {
  return { ok: true, json: () => Promise.resolve({ conversation: conv }) }
}

// msgsOk mirrors the API contract: newest-first.
function msgsOk(messages: ChatMessage[] = []) {
  return { ok: true, json: () => Promise.resolve({ messages }) }
}

// makeStream builds a Response-shaped object whose body the hook reads as SSE.
// The handle lets a test push frames, end the stream cleanly, or fail it
// mid-read.
function makeStream() {
  let ctrl!: ReadableStreamDefaultController<Uint8Array>
  const body = new ReadableStream<Uint8Array>({ start(c) { ctrl = c } })
  return {
    response: { ok: true, body },
    push(event: string, data: unknown) {
      ctrl.enqueue(encoder.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`))
    },
    // pushRaw writes an unencoded frame so a test can send malformed data.
    pushRaw(text: string) { ctrl.enqueue(encoder.encode(text)) },
    close() { ctrl.close() },
    fail(err: Error) { ctrl.error(err) },
  }
}

// openStream is a stream that stays open until the hook cancels it on cleanup.
function openStream() {
  return { ok: true, body: new ReadableStream<Uint8Array>({ start() { /* held open */ } }) }
}

type Overrides = Partial<UseFamilyChatStreamOptions>

function renderStream(overrides: Overrides = {}) {
  const onSignal = vi.fn()
  const onGroupSignal = vi.fn()
  const onConversationOpen = vi.fn()
  const onConversationClose = vi.fn()
  const view = renderHook(() => useFamilyChatStream({
    conversationId: 1,
    userId: 1,
    callKind: 'voice',
    onSignal,
    onGroupSignal,
    onConversationOpen,
    onConversationClose,
    ...overrides,
  }))
  return { ...view, onSignal, onGroupSignal, onConversationOpen, onConversationClose }
}

function fetchedUrls(mock: ReturnType<typeof vi.fn>): string[] {
  return mock.mock.calls.map(call => String(call[0]))
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

// ── Initial load ──────────────────────────────────────────────────────────────

describe('useFamilyChatStream – initial load', () => {
  it('loads the conversation, sorts messages oldest-first and goes live', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([
        makeMessage({ id: 3, body: 'Third' }),
        makeMessage({ id: 2, body: 'Second' }),
        makeMessage({ id: 1, body: 'First' }),
      ]))
      .mockResolvedValueOnce(openStream())
    vi.stubGlobal('fetch', fetchMock)

    const { result, onConversationOpen } = renderStream()

    await waitFor(() => expect(result.current.connStatus).toBe('live'))
    expect(result.current.conversation?.name).toBe('Family Chat')
    expect(result.current.messages.map(m => m.body)).toEqual(['First', 'Second', 'Third'])
    expect(result.current.lastIdRef.current).toBe(3)
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBe('')
    expect(onConversationOpen).toHaveBeenCalled()
    // The first connect must not issue a gap-fill: the initial /messages fetch
    // already covered everything up to lastId.
    expect(fetchedUrls(fetchMock).filter(u => u.includes('since='))).toHaveLength(0)
  })

  it('surfaces a translated error when the conversation request fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false })
      .mockResolvedValueOnce(msgsOk())
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()

    await waitFor(() => expect(result.current.error).toBe('errors.failedToLoadConversation'))
    expect(result.current.loading).toBe(false)
  })

  it('does nothing when no conversation is selected', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream({ conversationId: null })

    expect(fetchMock).not.toHaveBeenCalled()
    expect(result.current.messages).toEqual([])
  })
})

// ── Reconnect ─────────────────────────────────────────────────────────────────

describe('useFamilyChatStream – reconnect', () => {
  it('retries after the backoff delay and flashes justReconnected once', async () => {
    // shouldAdvanceTime lets real time flow so waitFor retries work, while
    // advanceTimersByTimeAsync can still fast-forward the reconnect delay.
    vi.useFakeTimers({ shouldAdvanceTime: true })

    const first = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([makeMessage({ id: 10, body: 'Before drop' })]))
      .mockResolvedValueOnce(first.response)
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(openStream())
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))
    expect(result.current.justReconnected).toBe(false)

    await act(async () => {
      first.close()
      await Promise.resolve()
    })
    await waitFor(() => expect(result.current.connStatus).toBe('reconnecting'))

    // Attempt #1 waits 1000 * 2**1 = 2000 ms.
    await act(async () => { await vi.advanceTimersByTimeAsync(1500) })
    expect(fetchMock).toHaveBeenCalledTimes(3)

    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    await waitFor(() => expect(result.current.connStatus).toBe('live'))
    expect(result.current.justReconnected).toBe(true)

    // The confirmation clears itself after 3s.
    await act(async () => { await vi.advanceTimersByTimeAsync(3100) })
    await waitFor(() => expect(result.current.justReconnected).toBe(false))

    // The reconnected stream resumes from the pre-gap watermark.
    expect(fetchedUrls(fetchMock)).toContain(
      '/api/familychat/conversations/1/stream?since_message_id=10',
    )
  }, 15000)

  it('stays in connecting (not reconnecting) when the very first connect fails', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([makeMessage({ id: 7 })]))
      .mockResolvedValueOnce({ ok: false })
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(openStream())
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    expect(result.current.connStatus).toBe('connecting')

    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })
    await waitFor(() => expect(result.current.connStatus).toBe('live'))
  }, 15000)

  it('recovers from a mid-stream reader error', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    const first = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([makeMessage({ id: 4 })]))
      .mockResolvedValueOnce(first.response)
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(openStream())
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      first.fail(new Error('network blip'))
      await Promise.resolve()
    })
    await waitFor(() => expect(result.current.connStatus).toBe('reconnecting'))

    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })
    await waitFor(() => expect(result.current.connStatus).toBe('live'))
  }, 15000)

  it('cancels a pending reconnect on unmount', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    const first = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([makeMessage({ id: 2 })]))
      .mockResolvedValueOnce(first.response)
    vi.stubGlobal('fetch', fetchMock)

    const { result, unmount, onConversationClose } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      first.close()
      await Promise.resolve()
    })
    await waitFor(() => expect(result.current.connStatus).toBe('reconnecting'))

    unmount()
    expect(onConversationClose).toHaveBeenCalled()

    await act(async () => { await vi.advanceTimersByTimeAsync(10000) })
    // No gap-fill and no re-connect after teardown.
    expect(fetchMock).toHaveBeenCalledTimes(3)
  }, 15000)
})

// ── Backfill ──────────────────────────────────────────────────────────────────

describe('useFamilyChatStream – backfill', () => {
  it('gap-fills from lastId and drops messages already in the list', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    const first = makeStream()
    const seen = makeMessage({ id: 10, body: 'Before drop' })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([seen]))
      .mockResolvedValueOnce(first.response)
      // The backfill window overlaps the message we already rendered.
      .mockResolvedValueOnce(msgsOk([
        makeMessage({ id: 12, body: 'Gap two' }),
        makeMessage({ id: 11, body: 'Gap one' }),
        seen,
      ]))
      .mockResolvedValueOnce(openStream())
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      first.close()
      await Promise.resolve()
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })
    await waitFor(() => expect(result.current.messages).toHaveLength(3))

    expect(fetchedUrls(fetchMock)).toContain(
      '/api/familychat/conversations/1/messages?since=10',
    )
    // Ascending order, one row per id — the overlapping id 10 is not duplicated.
    expect(result.current.messages.map(m => m.id)).toEqual([10, 11, 12])
    expect(result.current.lastIdRef.current).toBe(12)
  }, 15000)

  it('gap-fills without a since param when nothing has been rendered yet', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    const first = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(first.response)
      .mockResolvedValueOnce(msgsOk([makeMessage({ id: 1, body: 'Late arrival' })]))
      .mockResolvedValueOnce(openStream())
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      first.close()
      await Promise.resolve()
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })
    await waitFor(() => expect(result.current.messages).toHaveLength(1))

    const gapUrls = fetchedUrls(fetchMock).filter(u => u.includes('/messages'))
    expect(gapUrls.every(u => !u.includes('since='))).toBe(true)
    // Without a watermark the stream must not carry a resume marker either.
    expect(fetchedUrls(fetchMock)).toContain('/api/familychat/conversations/1/stream')
  }, 15000)

  it('tolerates a failing gap-fill and still opens the stream', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    const first = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([makeMessage({ id: 5 })]))
      .mockResolvedValueOnce(first.response)
      .mockRejectedValueOnce(new Error('gap-fill exploded'))
      .mockResolvedValueOnce(openStream())
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      first.close()
      await Promise.resolve()
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })
    await waitFor(() => expect(result.current.connStatus).toBe('live'))
    expect(result.current.messages).toHaveLength(1)
  }, 15000)
})

// ── Live frames ───────────────────────────────────────────────────────────────

describe('useFamilyChatStream – live frames', () => {
  it('appends new messages once and advances the watermark', async () => {
    const stream = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(stream.response)
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    const msg = makeMessage({ id: 21, body: 'Live one' })
    await act(async () => {
      stream.push('message_new', { message: msg })
      // The same frame replayed after a resume must not duplicate the row.
      stream.push('message_new', { message: msg })
      await Promise.resolve()
    })

    await waitFor(() => expect(result.current.messages).toHaveLength(1))
    expect(result.current.messages[0].body).toBe('Live one')
    expect(result.current.lastIdRef.current).toBe(21)
  })

  it('ignores messages addressed to another conversation and malformed frames', async () => {
    const stream = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(stream.response)
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      stream.push('message_new', { message: makeMessage({ id: 99, conversation_id: 42 }) })
      // A malformed payload must not tear the stream down…
      stream.pushRaw('event: message_new\ndata: {not json\n\n')
      // …the heartbeat comment is ignored…
      stream.pushRaw(': keep-alive\n\n')
      // …and the next well-formed frame still lands.
      stream.push('message_new', { message: makeMessage({ id: 100 }) })
      await Promise.resolve()
    })

    await waitFor(() => expect(result.current.messages).toHaveLength(1))
    expect(result.current.messages[0].id).toBe(100)
    expect(result.current.connStatus).toBe('live')
  })
})

// ── Typing indicators ─────────────────────────────────────────────────────────

describe('useFamilyChatStream – typing indicators', () => {
  it('records a member typing and expires the entry after ~5s', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    const stream = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(stream.response)
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      // Our own echo is ignored; the other member's signal is recorded.
      stream.push('typing', { user_id: 1 })
      stream.push('typing', { user_id: 2 })
      await Promise.resolve()
    })

    await waitFor(() => expect(result.current.typingUsers.has(2)).toBe(true))
    expect(result.current.typingUsers.has(1)).toBe(false)

    await act(async () => { await vi.advanceTimersByTimeAsync(6000) })
    await waitFor(() => expect(result.current.typingUsers.size).toBe(0))
  }, 15000)

  it('clears a member typing state as soon as their message lands', async () => {
    const stream = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk())
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(stream.response)
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      stream.push('typing', { user_id: 2 })
      await Promise.resolve()
    })
    await waitFor(() => expect(result.current.typingUsers.has(2)).toBe(true))

    await act(async () => {
      stream.push('message_new', { message: makeMessage({ id: 31, sender_user_id: 2 }) })
      await Promise.resolve()
    })
    await waitFor(() => expect(result.current.typingUsers.size).toBe(0))
  })
})

// ── Call signalling ───────────────────────────────────────────────────────────

describe('useFamilyChatStream – call signalling', () => {
  it('forwards 1:1 frames to onSignal without handling them internally', async () => {
    const stream = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk(makeConversation([1, 2])))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(stream.response)
    vi.stubGlobal('fetch', fetchMock)

    const { result, onSignal, onGroupSignal } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    const offer: CallSignalPayload = {
      conversation_id: 1,
      call_id: 'call-1',
      from_user_id: 2,
      kind: 'video',
    }
    await act(async () => {
      stream.push('call_offer', offer)
      await Promise.resolve()
    })

    await waitFor(() => expect(onSignal).toHaveBeenCalledWith('call_offer', offer))
    expect(onGroupSignal).not.toHaveBeenCalled()
  })

  it('routes frames to onGroupSignal for 3+ member conversations', async () => {
    const stream = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk(makeConversation([1, 2, 3])))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(stream.response)
    vi.stubGlobal('fetch', fetchMock)

    const { result, onSignal, onGroupSignal } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      stream.push('call_join', { conversation_id: 1, call_id: 'g1', from_user_id: 3 })
      stream.push('call_offer', { conversation_id: 1, call_id: 'g1', from_user_id: 3 })
      await Promise.resolve()
    })

    await waitFor(() => expect(onGroupSignal).toHaveBeenCalledTimes(2))
    expect(onGroupSignal.mock.calls.map(c => c[0])).toEqual(['call_join', 'call_offer'])
    expect(onSignal).not.toHaveBeenCalled()
  })

  it('synthesises a missed-call entry with the offered kind, only for inbound calls', async () => {
    const stream = makeStream()
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convOk(makeConversation([1, 2])))
      .mockResolvedValueOnce(msgsOk([]))
      .mockResolvedValueOnce(stream.response)
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderStream()
    await waitFor(() => expect(result.current.connStatus).toBe('live'))

    await act(async () => {
      // A video offer followed immediately by a missed end — the kind must
      // survive even though no render happened in between.
      stream.push('call_offer', { conversation_id: 1, call_id: 'c1', from_user_id: 2, kind: 'video' })
      stream.push('call_end', { conversation_id: 1, call_id: 'c1', from_user_id: 2, status: 'missed' })
      // A call we placed ourselves is not our missed-call history, and the same
      // call_id must never be recorded twice.
      stream.push('call_end', { conversation_id: 1, call_id: 'c2', from_user_id: 1, status: 'missed' })
      stream.push('call_end', { conversation_id: 1, call_id: 'c1', from_user_id: 2, status: 'missed' })
      await Promise.resolve()
    })

    await waitFor(() => expect(result.current.missedCalls).toHaveLength(1))
    expect(result.current.missedCalls[0].callId).toBe('c1')
    expect(result.current.missedCalls[0].fromUserId).toBe(2)
    expect(result.current.missedCalls[0].kind).toBe('video')
  })
})
