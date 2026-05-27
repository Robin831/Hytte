// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
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

describe('HomeworkChat – optimistic send recovery on failure', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('removes optimistic bubble and restores draft on network error', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convDetailResponse())
      .mockRejectedValueOnce(new Error('Network down'))
    vi.stubGlobal('fetch', fetchMock)

    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    const input = screen.getByRole('textbox') as HTMLTextAreaElement
    fireEvent.change(input, { target: { value: 'My question' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(screen.getByText('Network down')).toBeInTheDocument()
    })

    // Bubble removed from the transcript — scope to the message log so the
    // textarea value (which happy-dom exposes as textContent) doesn't match.
    const log = screen.getByRole('log')
    expect(within(log).queryByText('My question')).not.toBeInTheDocument()
    // Draft restored.
    expect(input.value).toBe('My question')
  })

  it('removes optimistic bubble and restores draft on non-OK response with server error text', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convDetailResponse())
      .mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'Server boom' }) })
    vi.stubGlobal('fetch', fetchMock)

    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    const input = screen.getByRole('textbox') as HTMLTextAreaElement
    fireEvent.change(input, { target: { value: 'A query' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(screen.getByText('Server boom')).toBeInTheDocument()
    })

    expect(within(screen.getByRole('log')).queryByText('A query')).not.toBeInTheDocument()
    expect(input.value).toBe('A query')
  })

  it('removes the swapped real-id bubble when SSE emits an error mid-stream', async () => {
    // The server emits user_message (so the optimistic temp id is replaced by id=999),
    // then emits an error event. Recovery must remove the now-real-id bubble.
    const realUserMsg = makeMessage({ id: 999, role: 'user', content: 'Original question' })

    const sseEvents = [
      `event: user_message\ndata: ${JSON.stringify(realUserMsg)}\n\n`,
      `event: error\ndata: ${JSON.stringify({ error: 'Stream failed' })}\n\n`,
    ]

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convDetailResponse())
      .mockResolvedValueOnce(makeSSEStream(sseEvents))
    vi.stubGlobal('fetch', fetchMock)

    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    const input = screen.getByRole('textbox') as HTMLTextAreaElement
    fireEvent.change(input, { target: { value: 'Original question' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(screen.getByText('Stream failed')).toBeInTheDocument()
    })

    // The user-message bubble (real id 999) must be gone from the transcript.
    expect(within(screen.getByRole('log')).queryByText('Original question')).not.toBeInTheDocument()
    // Streaming placeholder cleared, draft restored.
    expect(input.value).toBe('Original question')
  })

  it('does not clobber a newer draft typed during the in-flight send', async () => {
    // Deferred reject so we can control timing.
    let rejectFn: ((err: Error) => void) | null = null
    const inFlight = new Promise<Response>((_, reject) => { rejectFn = reject })

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convDetailResponse())
      .mockReturnValueOnce(inFlight)
    vi.stubGlobal('fetch', fetchMock)

    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    const input = screen.getByRole('textbox') as HTMLTextAreaElement
    fireEvent.change(input, { target: { value: 'Original' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    // Wait for the send fetch to be in-flight (and input to have been cleared).
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(input.value).toBe(''))

    // User types a new draft while the send is still pending.
    fireEvent.change(input, { target: { value: 'Newer draft' } })

    // Now fail the in-flight send.
    rejectFn!(new Error('boom'))

    await waitFor(() => {
      expect(screen.getByText('boom')).toBeInTheDocument()
    })

    // Newer draft is preserved; captured "Original" is NOT restored over it.
    expect(input.value).toBe('Newer draft')
    // Optimistic bubble still removed.
    expect(within(screen.getByRole('log')).queryByText('Original')).not.toBeInTheDocument()
  })

  it('short-circuits silently on AbortError without restoring draft or showing banner', async () => {
    const abortErr = new Error('aborted')
    abortErr.name = 'AbortError'

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convDetailResponse())
      .mockRejectedValueOnce(abortErr)
    vi.stubGlobal('fetch', fetchMock)

    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    const input = screen.getByRole('textbox') as HTMLTextAreaElement
    fireEvent.change(input, { target: { value: 'Question' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    // Wait for the send promise to settle (the textarea re-enables when sending=false).
    await waitFor(() => expect(input.disabled).toBe(false))

    // No banner shown, draft NOT restored (input stays cleared).
    expect(input.value).toBe('')
    expect(screen.queryByText('aborted')).not.toBeInTheDocument()
    expect(screen.queryByText('Failed to send message')).not.toBeInTheDocument()
  })

  it('restores selected image file and preview after a failed send', async () => {
    // Use a synchronous FileReader so imagePreview is set before send is clicked.
    const fakeDataUrl = 'data:image/png;base64,abc'
    class SyncFileReader {
      result = fakeDataUrl
      onload: (() => void) | null = null
      readAsDataURL(_file: File) { this.onload?.() }
    }
    vi.stubGlobal('FileReader', SyncFileReader)

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convDetailResponse())
      .mockRejectedValueOnce(new Error('Network down'))
    vi.stubGlobal('fetch', fetchMock)

    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    // Select an image — preview loads synchronously
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['dummy'], 'photo.png', { type: 'image/png' })
    fireEvent.change(fileInput, { target: { files: [file] } })

    await waitFor(() => screen.getByAltText('Selected image preview'))

    // Type text and send
    const input = screen.getByRole('textbox') as HTMLTextAreaElement
    fireEvent.change(input, { target: { value: 'Look at this problem' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => expect(screen.getByText('Network down')).toBeInTheDocument())

    // Both the image preview and the remove button must be back in the UI
    expect(screen.getByAltText('Selected image preview')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove image' })).toBeInTheDocument()
    // And the draft text is restored
    expect(input.value).toBe('Look at this problem')
  })

  it('rebuilds image preview in the catch block when FileReader had not fired before send', async () => {
    // Deferred FileReader — onload is stored but not called until we trigger it.
    const fakeDataUrl = 'data:image/png;base64,abc'
    const readers: Array<{ onload: (() => void) | null; result: string }> = []
    class DeferredFileReader {
      result = fakeDataUrl
      onload: (() => void) | null = null
      readAsDataURL(_file: File) {
        // Capture the instance (onload will have been assigned before this call)
        // eslint-disable-next-line @typescript-eslint/no-this-alias
        readers.push(this)
        // Does NOT call onload — simulates FileReader that hasn't fired yet
      }
    }
    vi.stubGlobal('FileReader', DeferredFileReader)

    const fetchMock = vi.fn()
      .mockResolvedValueOnce(convDetailResponse())
      .mockRejectedValueOnce(new Error('Network down'))
    vi.stubGlobal('fetch', fetchMock)

    renderChat()
    await waitFor(() => screen.getByText('Homework Helper'))

    // Select image — FileReader created but onload not fired yet
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['dummy'], 'photo.png', { type: 'image/png' })
    fireEvent.change(fileInput, { target: { files: [file] } })

    // No preview shown yet
    expect(screen.queryByAltText('Selected image preview')).not.toBeInTheDocument()

    // Type text and send immediately (previewSnapshot captured as null)
    const input = screen.getByRole('textbox') as HTMLTextAreaElement
    fireEvent.change(input, { target: { value: 'Quick send' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => expect(screen.getByText('Network down')).toBeInTheDocument())

    // The catch block created a second FileReader to rebuild the preview.
    // readers[0] = from handleImageSelect, readers[1] = from catch block.
    expect(readers.length).toBe(2)
    const rebuildReader = readers[readers.length - 1]
    expect(rebuildReader.onload).not.toBeNull()

    // Fire the catch-block reader's onload to deliver the preview
    rebuildReader.onload!()

    await waitFor(() => expect(screen.getByAltText('Selected image preview')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Remove image' })).toBeInTheDocument()
  })
})
