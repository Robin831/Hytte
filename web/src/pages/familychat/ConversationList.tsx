interface ConversationListProps {
  selectedConversationId: number | null
  onSelectConversation: (id: number) => void
  onNewConversation: () => void
}

export default function ConversationList(_props: ConversationListProps) {
  return <div className="h-full" data-testid="family-chat-conversation-list" />
}
