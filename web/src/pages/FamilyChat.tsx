import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import ConversationList from './familychat/ConversationList'
import ChatView from './familychat/ChatView'
import NewConversationModal from './familychat/NewConversationModal'

export default function FamilyChat() {
  const { t } = useTranslation('common')
  const [selectedConversationId, setSelectedConversationId] = useState<number | null>(null)
  const [newConversationOpen, setNewConversationOpen] = useState(false)

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
  }

  const handleBackToList = () => {
    setSelectedConversationId(null)
  }

  return (
    <div className="flex h-[calc(100vh-3.5rem)] md:h-screen bg-gray-900 text-white">
      {/* Left column: conversation list. Hidden on mobile when a conversation is selected. */}
      <aside
        className={`${
          selectedConversationId !== null ? 'hidden md:flex' : 'flex'
        } flex-col w-full md:w-80 md:shrink-0 border-r border-gray-800 bg-gray-950`}
        aria-label={t('nav.familyChat')}
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
        <ChatView conversationId={selectedConversationId} onBack={handleBackToList} />
      </section>

      <NewConversationModal
        open={newConversationOpen}
        onClose={handleCloseNewConversation}
        onCreated={handleConversationCreated}
      />
    </div>
  )
}
