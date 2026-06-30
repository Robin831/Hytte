import { useState, type CSSProperties } from 'react'
import ConversationList from './familychat/ConversationList'
import ChatView from './familychat/ChatView'
import NewConversationModal from './familychat/NewConversationModal'
import { FamilyChatProvider, useFamilyChat } from './familychat/FamilyChatContext'
import { useKeyboardInset } from '../hooks/useKeyboardInset'

function FamilyChatInner() {
  const { refreshConversations } = useFamilyChat()
  const [selectedConversationId, setSelectedConversationId] = useState<number | null>(null)
  const [newConversationOpen, setNewConversationOpen] = useState(false)
  // When the mobile on-screen keyboard opens it covers the bottom of the
  // viewport. We shrink the chat shell by that amount (and use dvh instead of
  // vh) so the composer stays pinned above the keyboard and the message list
  // keeps the newest messages visible instead of scrolling off the top.
  const keyboardInset = useKeyboardInset()

  const handleSelectConversation = (id: number) => {
    setSelectedConversationId(id)
  }

  const handleOpenNewConversation = () => {
    setNewConversationOpen(true)
  }

  const handleCloseNewConversation = () => {
    setNewConversationOpen(false)
  }

  const handleConversationCreated = (id: number) => {
    setNewConversationOpen(false)
    setSelectedConversationId(id)
    refreshConversations()
  }

  const handleBackToList = () => {
    setSelectedConversationId(null)
  }

  return (
    <div
      className="flex h-[calc(100dvh-3.5rem-var(--kb,0px))] md:h-[calc(100dvh-var(--kb,0px))]"
      style={{ '--kb': `${keyboardInset}px` } as CSSProperties}
    >
      {/* Left column: conversation list. Hidden on mobile when a conversation is selected. */}
      <aside
        className={`${
          selectedConversationId !== null ? 'hidden md:flex' : 'flex'
        } flex-col w-full md:w-80 md:shrink-0 border-r border-gray-800 bg-gray-950`}
      >
        <ConversationList
          selectedConversationId={selectedConversationId}
          onSelectConversation={handleSelectConversation}
          onNewConversation={handleOpenNewConversation}
        />
      </aside>

      {/* Right column: chat view. Hidden on mobile when no conversation is selected. */}
      <section
        className={`${
          selectedConversationId !== null ? 'flex' : 'hidden md:flex'
        } flex-1 min-w-0 flex-col`}
      >
        <ChatView
          key={selectedConversationId ?? 'none'}
          conversationId={selectedConversationId}
          onBack={handleBackToList}
        />
      </section>

      <NewConversationModal
        open={newConversationOpen}
        onClose={handleCloseNewConversation}
        onCreated={handleConversationCreated}
      />
    </div>
  )
}

export default function FamilyChat() {
  return (
    <FamilyChatProvider>
      <FamilyChatInner />
    </FamilyChatProvider>
  )
}
