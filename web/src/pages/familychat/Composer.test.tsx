// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import Composer, { type RetryHandle } from './Composer'
import type { ChatMessage } from './ChatView'

// ── Translation mock ──────────────────────────────────────────────────────────

const TRANSLATIONS: Record<string, string> = {
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
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => TRANSLATIONS[key] ?? key,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeMessage(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 1,
    conversation_id: 1,
    sender_user_id: 1,
    body: 'Hello!',
    created_at: '2026-05-01T10:00:00Z',
    ...overrides,
  }
}

function sendOk(msg: ChatMessage) {
  return { ok: true, json: () => Promise.resolve({ message: msg }) }
}

interface ExtraProps {
  onOptimisticMessage?: (m: ChatMessage) => void
  onMessageFailed?: (clientId: string) => void
  retryRef?: { current: RetryHandle | null }
  currentUserId?: number
}

function renderComposer(conversationId = 1, onMessageCreated = vi.fn(), extra: ExtraProps = {}) {
  return render(
    <Composer
      conversationId={conversationId}
      currentUserId={extra.currentUserId ?? 1}
      onMessageCreated={onMessageCreated}
      onOptimisticMessage={extra.onOptimisticMessage ?? vi.fn()}
      onMessageFailed={extra.onMessageFailed ?? vi.fn()}
      retryRef={extra.retryRef}
    />,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('Composer – keyboard behavior', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('submits when Enter is pressed without Shift', async () => {
    const msg = makeMessage({ id: 7, body: 'Hello world' })
    vi.stubGlobal('fetch', vi.fn(() => sendOk(msg)))
    const onOptimisticMessage = vi.fn()
    const onMessageCreated = vi.fn()
    renderComposer(1, onMessageCreated, { onOptimisticMessage })

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Hello world' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })

    expect(onOptimisticMessage).toHaveBeenCalledTimes(1)
    await waitFor(() => {
      expect(onMessageCreated).toHaveBeenCalledTimes(1)
    })
  })

  it('does not submit when Shift+Enter is pressed', async () => {
    vi.stubGlobal('fetch', vi.fn())
    const onOptimisticMessage = vi.fn()
    renderComposer(1, vi.fn(), { onOptimisticMessage })

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Hello world' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })

    // Give async effects a chance to run
    await new Promise(r => setTimeout(r, 50))
    expect(vi.mocked(fetch)).not.toHaveBeenCalled()
    expect(onOptimisticMessage).not.toHaveBeenCalled()
  })
})

describe('Composer – optimistic text send', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('emits an optimistic sending message and clears the input before the network resolves', async () => {
    // A fetch that never resolves: the optimistic bubble and the cleared input
    // must be observable with no network response at all.
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const onOptimisticMessage = vi.fn()
    renderComposer(1, vi.fn(), { onOptimisticMessage, currentUserId: 5 })

    const textarea = screen.getByRole('textbox') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Instant!' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    expect(onOptimisticMessage).toHaveBeenCalledTimes(1)
    const optimistic = onOptimisticMessage.mock.calls[0][0] as ChatMessage
    expect(optimistic.body).toBe('Instant!')
    expect(optimistic.status).toBe('sending')
    expect(optimistic.sender_user_id).toBe(5)
    expect(typeof optimistic.client_id).toBe('string')
    expect(textarea.value).toBe('')
  })

  it('clears the textarea after a successful send', async () => {
    const msg = makeMessage({ body: 'My message' })
    vi.stubGlobal('fetch', vi.fn(() => sendOk(msg)))
    renderComposer()

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'My message' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect((textarea as HTMLTextAreaElement).value).toBe('')
    })
  })

  it('reconciles via onMessageCreated stamped with the optimistic client_id', async () => {
    const msg = makeMessage({ id: 99, body: 'Ping' })
    vi.stubGlobal('fetch', vi.fn(() => sendOk(msg)))
    const onOptimisticMessage = vi.fn()
    const onMessageCreated = vi.fn()
    renderComposer(1, onMessageCreated, { onOptimisticMessage })

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Ping' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    const clientId = (onOptimisticMessage.mock.calls[0][0] as ChatMessage).client_id

    await waitFor(() => {
      expect(onMessageCreated).toHaveBeenCalledTimes(1)
    })
    const reconciled = onMessageCreated.mock.calls[0][0] as ChatMessage
    expect(reconciled.id).toBe(99)
    expect(reconciled.client_id).toBe(clientId)

    // The POST body must carry the same client_id so the server can echo it.
    const postBody = JSON.parse(vi.mocked(fetch).mock.calls[0][1]!.body as string)
    expect(postBody.client_id).toBe(clientId)
  })
})

describe('Composer – failure and retry', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('marks the message failed on POST error, then a retry re-sends under the same client_id', async () => {
    const okMsg = makeMessage({ id: 9, body: 'Retry me' })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'nope' }) })
      .mockResolvedValueOnce(sendOk(okMsg))
    vi.stubGlobal('fetch', fetchMock)

    const onOptimisticMessage = vi.fn()
    const onMessageFailed = vi.fn()
    const onMessageCreated = vi.fn()
    const retryRef: { current: RetryHandle | null } = { current: null }
    renderComposer(1, onMessageCreated, { onOptimisticMessage, onMessageFailed, retryRef })

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Retry me' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })

    const clientId = (onOptimisticMessage.mock.calls[0][0] as ChatMessage).client_id!

    await waitFor(() => {
      expect(onMessageFailed).toHaveBeenCalledWith(clientId)
    })
    expect(typeof retryRef.current).toBe('function')

    await act(async () => {
      retryRef.current!(clientId, 'Retry me', 1)
    })

    await waitFor(() => {
      expect(onMessageCreated).toHaveBeenCalledTimes(1)
    })
    const reconciled = onMessageCreated.mock.calls[0][0] as ChatMessage
    expect(reconciled.id).toBe(9)
    expect(reconciled.client_id).toBe(clientId)

    // The retry reused the same client_id and preserved text.
    const retryBody = JSON.parse(fetchMock.mock.calls[1][1].body as string)
    expect(retryBody.client_id).toBe(clientId)
    expect(retryBody.body).toBe('Retry me')
  })

  it('does not surface an inline composer alert for a failed text send', async () => {
    vi.stubGlobal('fetch', vi.fn(() => ({
      ok: false,
      json: () => Promise.resolve({ error: 'Server unavailable' }),
    })))
    const onMessageFailed = vi.fn()
    renderComposer(1, vi.fn(), { onMessageFailed })

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Test message' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(onMessageFailed).toHaveBeenCalledTimes(1)
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('send button is disabled when textarea is empty', () => {
    vi.stubGlobal('fetch', vi.fn())
    renderComposer()
    expect(screen.getByRole('button', { name: 'Send message' })).toBeDisabled()
  })

  it('send button is disabled when only whitespace is entered', () => {
    vi.stubGlobal('fetch', vi.fn())
    renderComposer()
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '   ' } })
    expect(screen.getByRole('button', { name: 'Send message' })).toBeDisabled()
  })
})

describe('Composer – attachments', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('uploads a file then sends the message with the upload id', async () => {
    const newMsg = makeMessage({
      id: 100,
      body: '',
      attachment_path: 'aabbccddeeff00112233445566778899',
      attachment_mime: 'image/png',
    })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          upload_id: 'aabbccddeeff00112233445566778899',
          mime: 'image/png',
          size: 1024,
        }),
      })
      .mockResolvedValueOnce(sendOk(newMsg))
    vi.stubGlobal('fetch', fetchMock)

    const onMessageCreated = vi.fn()
    const { container } = renderComposer(1, onMessageCreated)

    const file = new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], 'pic.png', { type: 'image/png' })
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    expect(input).toBeTruthy()
    fireEvent.change(input, { target: { files: [file] } })

    // Wait for the chip to appear after upload settles.
    await waitFor(() => {
      expect(screen.getByTestId('family-chat-attachment-chip')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(onMessageCreated).toHaveBeenCalledWith(newMsg)
    })

    // The second fetch (POST /messages) must include the upload_id.
    const sendCall = fetchMock.mock.calls[1]
    expect(sendCall[0]).toMatch(/\/api\/familychat\/conversations\/1\/messages/)
    const body = JSON.parse(sendCall[1].body as string)
    expect(body.attachment_path).toBe('aabbccddeeff00112233445566778899')
    expect(body.attachment_mime).toBe('image/png')
  })

  it('surfaces a 413 from the upload as a clear error toast', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
      ok: false,
      status: 413,
      json: () => Promise.resolve({ error: 'file too large' }),
    })))
    const { container } = renderComposer()

    const big = new File([new Uint8Array([0x89, 0x50])], 'huge.png', { type: 'image/png' })
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [big] } })

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('File is too large (max 10 MB)')
    })
  })
})
