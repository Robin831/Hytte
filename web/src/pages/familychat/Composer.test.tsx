// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import Composer from './Composer'
import type { ChatMessage } from './ChatView'

// ── Translation mock ──────────────────────────────────────────────────────────

const TRANSLATIONS: Record<string, string> = {
  'composer.placeholder': 'Write a message…',
  'composer.send': 'Send message',
  'composer.errors.send': 'Failed to send message',
  'composer.errors.tooLong': 'Message is too long',
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

function renderComposer(conversationId = 1, onMessageCreated = vi.fn()) {
  return render(
    <Composer conversationId={conversationId} onMessageCreated={onMessageCreated} />,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('Composer – keyboard behavior', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('submits when Enter is pressed without Shift', async () => {
    const msg = makeMessage({ body: 'Hello world' })
    vi.stubGlobal('fetch', vi.fn(() => sendOk(msg)))
    const onMessageCreated = vi.fn()
    renderComposer(1, onMessageCreated)

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Hello world' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })

    await waitFor(() => {
      expect(onMessageCreated).toHaveBeenCalledWith(msg)
    })
  })

  it('does not submit when Shift+Enter is pressed', async () => {
    vi.stubGlobal('fetch', vi.fn())
    renderComposer()

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Hello world' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })

    // Give async effects a chance to run
    await new Promise(r => setTimeout(r, 50))
    expect(vi.mocked(fetch)).not.toHaveBeenCalled()
  })
})

describe('Composer – successful send', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

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

  it('calls onMessageCreated with the returned message', async () => {
    const msg = makeMessage({ id: 99, body: 'Ping' })
    vi.stubGlobal('fetch', vi.fn(() => sendOk(msg)))
    const onMessageCreated = vi.fn()
    renderComposer(1, onMessageCreated)

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Ping' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(onMessageCreated).toHaveBeenCalledWith(msg)
    })
  })
})

describe('Composer – error handling', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows error message when server responds with non-2xx', async () => {
    vi.stubGlobal('fetch', vi.fn(() => ({
      ok: false,
      json: () => Promise.resolve({ error: 'Server unavailable' }),
    })))
    renderComposer()

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Test message' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Server unavailable')
    })
  })

  it('shows fallback error when server error has no message', async () => {
    vi.stubGlobal('fetch', vi.fn(() => ({
      ok: false,
      json: () => Promise.resolve({}),
    })))
    renderComposer()

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'Test message' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to send message')
    })
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
