// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useState } from 'react'
import { act, renderHook } from '@testing-library/react'
import { useMessageActions } from './useMessageActions'
import type { ChatMessage } from './useFamilyChatStream'

// ── i18n mock ─────────────────────────────────────────────────────────────────
// `t` echoes the key back, so assertions read as the key the UI would render.
// Kept stable across renders so the hook's callbacks don't churn identity.
const stableT = (key: string) => key

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

const ME = 1

function makeMessage(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 1,
    conversation_id: 1,
    sender_user_id: ME,
    body: 'Hello!',
    created_at: '2026-05-01T10:00:00Z',
    ...overrides,
  }
}

function optimistic(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return makeMessage({
    id: -1,
    body: 'typed text',
    client_id: 'c-1',
    status: 'sending',
    created_at: '2026-05-01T10:05:00Z',
    ...overrides,
  })
}

interface HarnessOptions {
  conversationId?: number | null
  initial?: ChatMessage[]
  userId?: number | undefined
}

// useHarness owns the messages/setMessages pair the way useFamilyChatStream
// does in production, so the hook reads a genuinely up-to-date list when it
// snapshots for rollback.
function useHarness(opts: HarnessOptions = {}) {
  const [messages, setMessages] = useState<ChatMessage[]>(opts.initial ?? [])
  const actions = useMessageActions({
    conversationId: opts.conversationId === undefined ? 1 : opts.conversationId,
    messages,
    setMessages,
    userId: 'userId' in opts ? opts.userId : ME,
  })
  return { messages, actions }
}

function renderActions(opts: HarnessOptions = {}) {
  return renderHook((props: HarnessOptions) => useHarness(props), {
    initialProps: opts,
  })
}

// ── fetch mock ────────────────────────────────────────────────────────────────

let fetchMock: ReturnType<typeof vi.fn>

function okResponse(body: unknown = {}) {
  return { ok: true, status: 200, json: () => Promise.resolve(body) }
}

function errResponse(status = 500) {
  return { ok: false, status, json: () => Promise.resolve({ error: 'nope' }) }
}

beforeEach(() => {
  fetchMock = vi.fn().mockResolvedValue(okResponse())
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

// lastCall returns the [url, init] pair of the most recent fetch.
function lastCall(): [string, RequestInit] {
  const call = fetchMock.mock.calls.at(-1)
  return [String(call?.[0]), (call?.[1] ?? {}) as RequestInit]
}

// ── Optimistic send lifecycle ─────────────────────────────────────────────────

describe('useMessageActions — optimistic send', () => {
  it('inserts an optimistic bubble and reconciles it with the server row', () => {
    const { result } = renderActions()

    act(() => { result.current.actions.send.onOptimisticMessage(optimistic()) })
    expect(result.current.messages).toHaveLength(1)
    expect(result.current.messages[0]).toMatchObject({
      id: -1,
      client_id: 'c-1',
      status: 'sending',
      body: 'typed text',
    })

    act(() => {
      result.current.actions.send.onMessageCreated(makeMessage({
        id: 42,
        body: 'typed text',
        client_id: 'c-1',
      }))
    })
    // The optimistic bubble is replaced in place — same list slot, real id, no
    // status, client_id kept so the React key stays stable.
    expect(result.current.messages).toHaveLength(1)
    expect(result.current.messages[0]).toMatchObject({ id: 42, client_id: 'c-1' })
    expect(result.current.messages[0].status).toBeUndefined()
  })

  it('appends a server message that has no optimistic counterpart', () => {
    const { result } = renderActions()

    act(() => {
      result.current.actions.send.onMessageCreated(makeMessage({ id: 7, sender_user_id: 2 }))
    })
    expect(result.current.messages).toHaveLength(1)

    // A duplicate frame for the same id must not add a second bubble.
    act(() => {
      result.current.actions.send.onMessageCreated(makeMessage({ id: 7, sender_user_id: 2 }))
    })
    expect(result.current.messages).toHaveLength(1)
  })

  it('drops messages that belong to a different conversation', () => {
    const { result } = renderActions({ conversationId: 1 })

    act(() => {
      result.current.actions.send.onOptimisticMessage(optimistic({ conversation_id: 99 }))
      result.current.actions.send.onMessageCreated(makeMessage({ id: 5, conversation_id: 99 }))
    })
    expect(result.current.messages).toEqual([])
  })

  it('flips a failed send to "failed" and back to "sending" on retry', () => {
    const { result } = renderActions()

    act(() => { result.current.actions.send.onOptimisticMessage(optimistic()) })
    act(() => { result.current.actions.send.onMessageFailed('c-1') })
    expect(result.current.messages[0].status).toBe('failed')

    const composerRetry = vi.fn()
    result.current.actions.send.retryRef.current = composerRetry

    act(() => { result.current.actions.retry(result.current.messages[0]) })
    expect(result.current.messages[0].status).toBe('sending')
    expect(composerRetry).toHaveBeenCalledWith('c-1', 'typed text', 1)

    // ...and a successful retry reconciles the same bubble.
    act(() => {
      result.current.actions.send.onMessageCreated(makeMessage({
        id: 43,
        body: 'typed text',
        client_id: 'c-1',
      }))
    })
    expect(result.current.messages).toHaveLength(1)
    expect(result.current.messages[0].id).toBe(43)
    expect(result.current.messages[0].status).toBeUndefined()
  })

  it('ignores a retry for a bubble with no client_id', () => {
    const { result } = renderActions({ initial: [makeMessage({ status: 'failed' })] })
    const composerRetry = vi.fn()
    result.current.actions.send.retryRef.current = composerRetry

    act(() => { result.current.actions.retry(result.current.messages[0]) })
    expect(composerRetry).not.toHaveBeenCalled()
  })
})

// ── Inline edit ───────────────────────────────────────────────────────────────

describe('useMessageActions — edit', () => {
  it('seeds the draft from the bubble body and clears it on cancel', () => {
    const { result } = renderActions({ initial: [makeMessage({ body: 'first' })] })

    act(() => { result.current.actions.beginEdit(result.current.messages[0]) })
    expect(result.current.actions.editDraft).toMatchObject({ msgId: 1, text: 'first', error: '' })

    act(() => { result.current.actions.setEditText('second') })
    expect(result.current.actions.editDraft.text).toBe('second')

    act(() => { result.current.actions.cancelEdit() })
    expect(result.current.actions.editDraft.msgId).toBeNull()
    expect(result.current.actions.editDraft.text).toBe('')
  })

  it('applies the edit optimistically then adopts the server row', async () => {
    fetchMock.mockResolvedValue(okResponse({
      message: {
        id: 1,
        conversation_id: 1,
        sender_user_id: ME,
        body: 'edited',
        created_at: '2026-05-01T10:00:00Z',
        edited_at: '2026-05-01T11:00:00Z',
        deleted_at: null,
        deleted_by: null,
      },
    }))
    const { result } = renderActions({ initial: [makeMessage({ body: 'first' })] })

    act(() => { result.current.actions.beginEdit(result.current.messages[0]) })
    act(() => { result.current.actions.setEditText('  edited  ') })
    await act(async () => { await result.current.actions.saveEdit(1) })

    const [url, init] = lastCall()
    expect(url).toBe('/api/familychat/conversations/1/messages/1')
    expect(init.method).toBe('PATCH')
    expect(JSON.parse(String(init.body))).toEqual({ body: 'edited' })

    expect(result.current.messages[0].body).toBe('edited')
    expect(result.current.messages[0].edited_at).toBe('2026-05-01T11:00:00Z')
    // Editor closes only after the server accepts.
    expect(result.current.actions.editDraft.msgId).toBeNull()
  })

  it('rolls the body back and surfaces an error when the save fails', async () => {
    fetchMock.mockResolvedValue(errResponse(500))
    const { result } = renderActions({
      initial: [makeMessage({ body: 'first', edited_at: null })],
    })

    act(() => { result.current.actions.beginEdit(result.current.messages[0]) })
    act(() => { result.current.actions.setEditText('edited') })
    await act(async () => { await result.current.actions.saveEdit(1) })

    expect(result.current.messages[0].body).toBe('first')
    expect(result.current.messages[0].edited_at).toBeNull()
    // The draft survives so the user can retry without retyping.
    expect(result.current.actions.editDraft).toMatchObject({
      msgId: 1,
      text: 'edited',
      saving: false,
      error: 'edit.saveError',
    })
  })

  it('refuses to save a blank draft without hitting the network', async () => {
    const { result } = renderActions({ initial: [makeMessage({ body: 'first' })] })

    act(() => { result.current.actions.beginEdit(result.current.messages[0]) })
    act(() => { result.current.actions.setEditText('   ') })
    await act(async () => { await result.current.actions.saveEdit(1) })

    expect(fetchMock).not.toHaveBeenCalled()
    expect(result.current.actions.editDraft.error).toBe('edit.saveError')
    expect(result.current.messages[0].body).toBe('first')
  })

  it('is a no-op when no conversation is open', async () => {
    const { result } = renderActions({
      conversationId: null,
      initial: [makeMessage({ body: 'first' })],
    })

    act(() => { result.current.actions.beginEdit(result.current.messages[0]) })
    act(() => { result.current.actions.setEditText('edited') })
    await act(async () => { await result.current.actions.saveEdit(1) })

    expect(fetchMock).not.toHaveBeenCalled()
    expect(result.current.messages[0].body).toBe('first')
  })
})

// ── Soft delete ───────────────────────────────────────────────────────────────

describe('useMessageActions — delete', () => {
  it('tombstones the message and closes the modal once the server accepts', async () => {
    const { result } = renderActions({
      initial: [makeMessage({ body: 'bye', attachment_path: '/a.png', attachment_mime: 'image/png' })],
    })

    act(() => { result.current.actions.confirmDelete(1) })
    expect(result.current.actions.deleteTarget).toEqual({ msgId: 1, error: '' })

    await act(async () => { await result.current.actions.doDelete() })

    const [url, init] = lastCall()
    expect(url).toBe('/api/familychat/conversations/1/messages/1')
    expect(init.method).toBe('DELETE')

    expect(result.current.messages[0]).toMatchObject({
      body: '',
      attachment_path: '',
      attachment_mime: '',
      deleted_by: ME,
    })
    expect(result.current.messages[0].deleted_at).toBeTruthy()
    expect(result.current.actions.deleteTarget.msgId).toBeNull()
  })

  it('restores the message and keeps the modal open when the delete fails', async () => {
    fetchMock.mockResolvedValue(errResponse(403))
    const before = makeMessage({ body: 'bye' })
    const { result } = renderActions({ initial: [before] })

    act(() => { result.current.actions.confirmDelete(1) })
    await act(async () => { await result.current.actions.doDelete() })

    expect(result.current.messages[0]).toEqual(before)
    expect(result.current.actions.deleteTarget).toEqual({
      msgId: 1,
      error: 'edit.deleteError',
    })
  })

  it('does nothing when no message is targeted', async () => {
    const { result } = renderActions({ initial: [makeMessage()] })

    await act(async () => { await result.current.actions.doDelete() })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('drops the target on cancel', () => {
    const { result } = renderActions({ initial: [makeMessage()] })

    act(() => { result.current.actions.confirmDelete(1) })
    act(() => { result.current.actions.cancelDelete() })
    expect(result.current.actions.deleteTarget.msgId).toBeNull()
  })
})

// ── Reactions ─────────────────────────────────────────────────────────────────

describe('useMessageActions — reactions', () => {
  it('adds a reaction optimistically and POSTs it', async () => {
    const { result } = renderActions({ initial: [makeMessage()] })

    await act(async () => { await result.current.actions.toggleReaction(1, '👍', false) })

    const [url, init] = lastCall()
    expect(url).toBe('/api/familychat/conversations/1/messages/1/reactions')
    expect(init.method).toBe('POST')
    expect(JSON.parse(String(init.body))).toEqual({ emoji: '👍' })

    expect(result.current.messages[0].reactions?.['👍']).toMatchObject({
      count: 1,
      users: [ME],
      me: true,
    })
  })

  it('removes an existing reaction with a DELETE', async () => {
    const { result } = renderActions({
      initial: [makeMessage({ reactions: { '👍': { count: 1, users: [ME], me: true } } })],
    })

    await act(async () => { await result.current.actions.toggleReaction(1, '👍', true) })

    const [url, init] = lastCall()
    expect(url).toContain('/messages/1/reactions?emoji=')
    expect(init.method).toBe('DELETE')
    // Count hit zero, so the bucket is dropped entirely.
    expect(result.current.messages[0].reactions?.['👍']).toBeUndefined()
  })

  it('rolls the reaction back when the add fails', async () => {
    fetchMock.mockResolvedValue(errResponse(500))
    const { result } = renderActions({
      initial: [makeMessage({ reactions: { '🎉': { count: 2, users: [2, 3], me: false } } })],
    })

    await act(async () => { await result.current.actions.toggleReaction(1, '👍', false) })

    // Only the reactions field is reverted, and it reverts to the exact
    // pre-toggle snapshot — the optimistic 👍 is gone, 🎉 is untouched.
    expect(result.current.messages[0].reactions).toEqual({
      '🎉': { count: 2, users: [2, 3], me: false },
    })
  })

  it('rolls the reaction back when the remove fails', async () => {
    fetchMock.mockResolvedValue(errResponse(500))
    const { result } = renderActions({
      initial: [makeMessage({ reactions: { '👍': { count: 2, users: [ME, 2], me: true } } })],
    })

    await act(async () => { await result.current.actions.toggleReaction(1, '👍', true) })

    expect(result.current.messages[0].reactions?.['👍']).toEqual({
      count: 2,
      users: [ME, 2],
      me: true,
    })
  })

  it('does nothing without a signed-in user', async () => {
    const { result } = renderActions({ initial: [makeMessage()], userId: undefined })

    await act(async () => { await result.current.actions.toggleReaction(1, '👍', false) })
    expect(fetchMock).not.toHaveBeenCalled()
    expect(result.current.messages[0].reactions).toBeUndefined()
  })
})

// ── Pending markers & conversation switches ───────────────────────────────────

describe('useMessageActions — pendingIds', () => {
  it('marks a message pending while its mutation is in flight', async () => {
    let release: (v: unknown) => void = () => {}
    fetchMock.mockImplementationOnce(() => new Promise(resolve => { release = resolve }))
    const { result } = renderActions({ initial: [makeMessage()] })

    let inFlight: Promise<void> = Promise.resolve()
    act(() => { inFlight = result.current.actions.doDelete(1) })
    expect(result.current.actions.pendingIds.has(1)).toBe(true)

    await act(async () => {
      release(okResponse())
      await inFlight
    })
    expect(result.current.actions.pendingIds.has(1)).toBe(false)
  })

  it('keeps the marker until the last concurrent mutation settles', async () => {
    const releases: Array<(v: unknown) => void> = []
    fetchMock.mockImplementation(() => new Promise(resolve => { releases.push(resolve) }))
    const { result } = renderActions({ initial: [makeMessage()] })

    let first: Promise<void> = Promise.resolve()
    let second: Promise<void> = Promise.resolve()
    act(() => { first = result.current.actions.toggleReaction(1, '👍', false) })
    act(() => { second = result.current.actions.toggleReaction(1, '🎉', false) })
    expect(result.current.actions.pendingIds.has(1)).toBe(true)

    await act(async () => {
      releases[0](okResponse())
      await first
    })
    expect(result.current.actions.pendingIds.has(1)).toBe(true)

    await act(async () => {
      releases[1](okResponse())
      await second
    })
    expect(result.current.actions.pendingIds.has(1)).toBe(false)
  })
})

describe('useMessageActions — conversation switches', () => {
  it('drops the edit draft, delete target and pending markers', () => {
    const { result, rerender } = renderActions({
      conversationId: 1,
      initial: [makeMessage()],
    })

    act(() => { result.current.actions.beginEdit(result.current.messages[0]) })
    act(() => { result.current.actions.confirmDelete(1) })
    expect(result.current.actions.editDraft.msgId).toBe(1)
    expect(result.current.actions.deleteTarget.msgId).toBe(1)

    rerender({ conversationId: 2, initial: [] })

    expect(result.current.actions.editDraft.msgId).toBeNull()
    expect(result.current.actions.editDraft.text).toBe('')
    expect(result.current.actions.deleteTarget.msgId).toBeNull()
    expect(result.current.actions.pendingIds.size).toBe(0)
  })
})
