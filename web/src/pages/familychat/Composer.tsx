interface ComposerProps {
  conversationId: number
  onMessageSent?: () => void
}

export default function Composer(_props: ComposerProps) {
  return <div data-testid="family-chat-composer" />
}
