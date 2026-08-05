// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import MessageItem, { type MessageItemProps } from './MessageItem'
import type { ChatMessage } from './useFamilyChatStream'

// Mock voicePlayer so VoiceBubble renders without a real HTMLAudioElement.
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

// The translation mock echoes the key (plus interpolation options) so the
// assertions below verify that a t() lookup happened rather than pinning
// English copy — the real strings live in the locale files.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, string | number>) => (
      opts ? `${key}:${JSON.stringify(opts)}` : key
    ),
  }),
}))

function makeMessage(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 7,
    conversation_id: 1,
    sender_user_id: 2,
    body: 'Hello there',
    created_at: '2026-05-01T10:00:00.000Z',
    ...overrides,
  }
}

// handlers collects every mutation prop so a test can assert on the one it
// cares about while the rest stay inert spies.
function makeHandlers() {
  return {
    onOpenLightbox: vi.fn(),
    onOpenPicker: vi.fn(),
    onClosePicker: vi.fn(),
    onToggleReaction: vi.fn(),
    onOpenMenu: vi.fn(),
    onBeginEdit: vi.fn(),
    onConfirmDelete: vi.fn(),
    onEditTextChange: vi.fn(),
    onSaveEdit: vi.fn(),
    onCancelEdit: vi.fn(),
    onRetry: vi.fn(),
  }
}

type Handlers = ReturnType<typeof makeHandlers>

function renderItem(props: Partial<MessageItemProps> = {}): { handlers: Handlers } {
  const handlers = makeHandlers()
  render(
    <MessageItem
      msg={makeMessage()}
      isOwn={false}
      senderLabel="Ada"
      deletedByLabel="deleted by Ada"
      relative="just now"
      editDraft={null}
      pickerOpen={false}
      menuOpen={false}
      {...handlers}
      {...props}
    />,
  )
  return { handlers }
}

describe('MessageItem', () => {
  afterEach(() => { cleanup(); vi.clearAllMocks() })

  it('renders a peer message with its sender label and relative time', () => {
    renderItem()
    expect(screen.getByText('Hello there')).toBeInTheDocument()
    expect(screen.getByText('Ada')).toBeInTheDocument()
    expect(screen.getByText('just now')).toBeInTheDocument()
  })

  it('omits the sender label on own messages', () => {
    renderItem({ isOwn: true })
    expect(screen.queryByText('Ada')).not.toBeInTheDocument()
    expect(screen.getByText('Hello there')).toBeInTheDocument()
  })

  it('renders a tombstone instead of the body for a deleted message', () => {
    renderItem({ msg: makeMessage({ deleted_at: '2026-05-01T11:00:00.000Z', body: '' }) })
    expect(screen.getByTestId('chat-tombstone-7').textContent).toBe('deleted by Ada')
    expect(screen.queryByTestId('reaction-trigger-7')).not.toBeInTheDocument()
  })

  it('marks an edited message with the edited tag', () => {
    renderItem({ msg: makeMessage({ edited_at: '2026-05-01T10:05:00.000Z' }) })
    expect(screen.getByTestId('chat-edited-tag-7')).toBeInTheDocument()
  })

  it('shows a sending marker and no reaction affordance while optimistic', () => {
    renderItem({ msg: makeMessage({ status: 'sending', client_id: 'c1' }), isOwn: true })
    expect(screen.getByTestId('chat-sending-7')).toBeInTheDocument()
    expect(screen.queryByTestId('reaction-trigger-7')).not.toBeInTheDocument()
    expect(screen.queryByTestId('chat-actions-trigger-7')).not.toBeInTheDocument()
  })

  it('retries a failed message with the message itself', () => {
    const msg = makeMessage({ status: 'failed', client_id: 'c1' })
    const { handlers } = renderItem({ msg, isOwn: true })
    fireEvent.click(screen.getByTestId('chat-failed-7'))
    expect(handlers.onRetry).toHaveBeenCalledWith(msg)
  })

  describe('attachments', () => {
    it('opens the lightbox for an image attachment', () => {
      const { handlers } = renderItem({
        msg: makeMessage({ attachment_path: 'a.png', attachment_mime: 'image/png', body: '' }),
      })
      fireEvent.click(screen.getByRole('button', { name: 'chat.attachmentImageAlt' }))
      expect(handlers.onOpenLightbox).toHaveBeenCalledWith({
        url: '/api/familychat/conversations/1/attachments/7',
        alt: 'chat.attachmentImageAlt',
      })
    })

    it('renders a voice bubble for an empty-bodied audio/webm attachment', () => {
      renderItem({
        msg: makeMessage({ attachment_path: 'v.webm', attachment_mime: 'audio/webm', body: '' }),
      })
      expect(screen.getByTestId('voice-bubble-7')).toBeInTheDocument()
    })

    it('renders a native audio player for audio that carries a body', () => {
      renderItem({
        msg: makeMessage({ attachment_path: 'v.webm', attachment_mime: 'audio/webm', body: 'listen' }),
      })
      expect(screen.queryByTestId('voice-bubble-7')).not.toBeInTheDocument()
      expect(document.querySelector('audio')).toBeInTheDocument()
    })

    it('renders a download link for a non-image non-audio attachment', () => {
      renderItem({
        msg: makeMessage({ attachment_path: 'doc.pdf', attachment_mime: 'application/pdf', body: '' }),
      })
      const link = screen.getByRole('link')
      expect(link).toHaveAttribute('href', '/api/familychat/conversations/1/attachments/7')
      expect(link.textContent).toContain('chat.attachmentFileLabel')
    })

    it('suppresses the attachment once the message is deleted', () => {
      renderItem({
        msg: makeMessage({
          attachment_path: 'a.png',
          attachment_mime: 'image/png',
          body: '',
          deleted_at: '2026-05-01T11:00:00.000Z',
        }),
      })
      expect(screen.queryByRole('img')).not.toBeInTheDocument()
    })
  })

  describe('reactions', () => {
    it('opens the picker from the hover trigger and closes it on a second press', () => {
      const { handlers } = renderItem()
      fireEvent.click(screen.getByTestId('reaction-trigger-7'))
      expect(handlers.onOpenPicker).toHaveBeenCalledWith(7)

      cleanup()
      const second = renderItem({ pickerOpen: true })
      fireEvent.click(screen.getByTestId('reaction-trigger-7'))
      expect(second.handlers.onClosePicker).toHaveBeenCalled()
      expect(second.handlers.onOpenPicker).not.toHaveBeenCalled()
    })

    it('opens the picker on a touch long-press but not on a mouse right-click', () => {
      const { handlers } = renderItem()
      const bubble = screen.getByText('Hello there').parentElement!

      fireEvent.pointerDown(bubble, { pointerType: 'mouse' })
      fireEvent.contextMenu(bubble)
      expect(handlers.onOpenPicker).not.toHaveBeenCalled()

      fireEvent.pointerDown(bubble, { pointerType: 'touch' })
      fireEvent.contextMenu(bubble)
      expect(handlers.onOpenPicker).toHaveBeenCalledWith(7)
    })

    it('toggles a reaction from a chip with the current me-flag', () => {
      const { handlers } = renderItem({
        msg: makeMessage({ reactions: { '👍': { count: 2, users: [1, 2], me: true } } }),
      })
      fireEvent.click(screen.getByTestId('reaction-chip-👍'))
      expect(handlers.onToggleReaction).toHaveBeenCalledWith(7, '👍', true)
    })

    it('closes the picker and toggles the picked emoji as a new reaction', () => {
      const { handlers } = renderItem({ pickerOpen: true })
      fireEvent.click(screen.getByRole('button', { name: '🎉' }))
      expect(handlers.onClosePicker).toHaveBeenCalled()
      expect(handlers.onToggleReaction).toHaveBeenCalledWith(7, '🎉', false)
    })
  })

  describe('action menu', () => {
    const ownMsg = makeMessage({ sender_user_id: 1 })

    it('is offered only on own messages', () => {
      renderItem({ msg: ownMsg, isOwn: false })
      expect(screen.queryByTestId('chat-actions-trigger-7')).not.toBeInTheDocument()
      cleanup()
      renderItem({ msg: ownMsg, isOwn: true })
      expect(screen.getByTestId('chat-actions-trigger-7')).toBeInTheDocument()
    })

    it('starts an edit and closes the menu', () => {
      const { handlers } = renderItem({ msg: ownMsg, isOwn: true, menuOpen: true })
      fireEvent.click(screen.getByTestId('chat-edit-action-7'))
      expect(handlers.onOpenMenu).toHaveBeenCalledWith(null)
      expect(handlers.onBeginEdit).toHaveBeenCalledWith(ownMsg)
    })

    it('asks for delete confirmation and closes the menu', () => {
      const { handlers } = renderItem({ msg: ownMsg, isOwn: true, menuOpen: true })
      fireEvent.click(screen.getByTestId('chat-delete-action-7'))
      expect(handlers.onOpenMenu).toHaveBeenCalledWith(null)
      expect(handlers.onConfirmDelete).toHaveBeenCalledWith(7)
    })
  })

  describe('inline edit', () => {
    const draft = { msgId: 7, text: 'Hello there', saving: false, error: '' }

    it('renders the draft text and reports every keystroke', () => {
      const { handlers } = renderItem({ isOwn: true, editDraft: draft })
      const input = screen.getByTestId('chat-edit-input-7')
      expect(input).toHaveValue('Hello there')
      fireEvent.change(input, { target: { value: 'Hello again' } })
      expect(handlers.onEditTextChange).toHaveBeenCalledWith('Hello again')
    })

    it('saves on the save button and on Enter, cancels on Escape', () => {
      const { handlers } = renderItem({ isOwn: true, editDraft: draft })
      const input = screen.getByTestId('chat-edit-input-7')

      fireEvent.keyDown(input, { key: 'Enter' })
      expect(handlers.onSaveEdit).toHaveBeenCalledWith(7)

      fireEvent.keyDown(input, { key: 'Escape' })
      expect(handlers.onCancelEdit).toHaveBeenCalled()

      fireEvent.click(screen.getByTestId('chat-edit-save-7'))
      expect(handlers.onSaveEdit).toHaveBeenCalledTimes(2)
    })

    it('leaves Shift+Enter to the textarea for a newline', () => {
      const { handlers } = renderItem({ isOwn: true, editDraft: draft })
      fireEvent.keyDown(screen.getByTestId('chat-edit-input-7'), { key: 'Enter', shiftKey: true })
      expect(handlers.onSaveEdit).not.toHaveBeenCalled()
    })

    it('disables saving while a save is in flight and shows the saving label', () => {
      renderItem({ isOwn: true, editDraft: { ...draft, saving: true } })
      expect(screen.getByTestId('chat-edit-save-7')).toBeDisabled()
      expect(screen.getByTestId('chat-edit-save-7').textContent).toBe('edit.saving')
    })

    it('disables saving for a blank draft and surfaces a save error', () => {
      renderItem({ isOwn: true, editDraft: { msgId: 7, text: '   ', saving: false, error: 'boom' } })
      expect(screen.getByTestId('chat-edit-save-7')).toBeDisabled()
      expect(screen.getByText('boom')).toBeInTheDocument()
    })

    it('hides the reaction and action affordances while editing', () => {
      renderItem({ isOwn: true, editDraft: draft })
      expect(screen.queryByTestId('reaction-trigger-7')).not.toBeInTheDocument()
      expect(screen.queryByTestId('chat-actions-trigger-7')).not.toBeInTheDocument()
    })

    it('cancels from the cancel button', () => {
      const { handlers } = renderItem({ isOwn: true, editDraft: draft })
      fireEvent.click(screen.getByTestId('chat-edit-cancel-7'))
      expect(handlers.onCancelEdit).toHaveBeenCalled()
    })
  })
})
