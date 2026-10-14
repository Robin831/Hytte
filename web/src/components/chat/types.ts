import type { RefObject } from 'react'
import type { Conversation, Message } from '../../hooks/useChatStream'

export type { Conversation, Message }

// Props for the memoized chat components. Everything is a primitive, a
// reference that only changes when its data does (a message / conversation
// object, the conversations array), or a callback the parent stabilises with
// useCallback — passing freshly built objects or inline closures would defeat
// React.memo and re-render every row on each streamed token.

export interface MessageRowProps {
  message: Message
  /** Render the "streaming…" placeholder instead of the (still empty) bubble. */
  pending?: boolean
  /** Show Regenerate — set only for the newest assistant reply when idle. */
  showRegenerate?: boolean
  /** Show Edit — set for persisted user messages. */
  showEdit?: boolean
  /** Disables the actions while a stream is in flight. */
  actionsDisabled?: boolean
  onRegenerate?: (message: Message) => void
  onEdit?: (message: Message) => void
}

export interface ConversationSidebarProps {
  /** Below md the list and the chat pane alternate; false hides the list. */
  visibleOnMobile: boolean
  conversations: Conversation[]
  activeId: number | null
  loading: boolean
  onSelect: (conversation: Conversation) => void
  onNew: () => void
  /** Resolves true when the rename settled and the inline editor should close. */
  onRename: (id: number, title: string) => Promise<boolean>
  /** Resolves true when the delete succeeded and the confirmation should close. */
  onDelete: (id: number) => Promise<boolean>
}

export interface ModelSelectProps {
  value: string
  disabled: boolean
  onChange: (model: string) => void
}

export interface ComposerProps {
  /** Whether a conversation is active — the input area only renders then. */
  hasConversation: boolean
  value: string
  onChange: (value: string) => void
  onSend: () => void
  onStop: () => void
  /** True while the active conversation has a stream in flight. */
  sending: boolean
  isEditing: boolean
  onCancelEdit: () => void
  error: string
  onDismissError: () => void
  textareaRef: RefObject<HTMLTextAreaElement | null>
}

export interface MessageListProps {
  messages: Message[]
  hasConversation: boolean
  loading: boolean
  /** True while the active conversation has a stream in flight. */
  sending: boolean
  onNew: () => void
  onRegenerate: (message: Message) => void
  onEdit: (message: Message) => void
  /** Sentinel after the last message that the parent scrolls into view. */
  endRef: RefObject<HTMLDivElement | null>
}

export interface ChatHeaderProps {
  title: string | null
  model: string
  modelDisabled: boolean
  onModelChange: (model: string) => void
  onBack: () => void
}
