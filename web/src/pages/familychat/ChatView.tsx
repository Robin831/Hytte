interface ChatViewProps {
  conversationId: number | null
  onBack: () => void
}

export default function ChatView(_props: ChatViewProps) {
  return <div className="flex flex-col h-full" data-testid="family-chat-view" />
}
