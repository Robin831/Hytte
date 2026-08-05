import { useTranslation } from 'react-i18next'
import { ChevronLeft, Phone, Video } from 'lucide-react'
import { Skeleton } from '../../components/ui/skeleton'
import ConnectionStatus from '../../components/ConnectionStatus'
import type { ChatConnectionState } from '../../components/ConnectionStatus'
import type { CallKind } from './voice/useVoiceCall'

// ChatHeader is the bar above the message log: the mobile back button, the
// conversation title, the member chips, the live-connection badge and the call
// buttons. Purely presentational — it reports presses upward and renders what
// it is told.

// MemberChip is one member of the conversation as rendered in the header.
export interface MemberChip {
  id: number
  label: string
  emoji: string
  isSelf: boolean
}

interface ChatHeaderProps {
  title: string | null
  loading: boolean
  memberChips: MemberChip[]
  connStatus: ChatConnectionState
  justReconnected: boolean
  onBack: () => void
  // canCall is true for 1:1 conversations, canGroupCall for 3+ member ones.
  // The two are mutually exclusive by member count.
  canCall: boolean
  canGroupCall: boolean
  // callBusy disables the 1:1 buttons while a call is being set up or running;
  // groupCallActive does the same for the group buttons.
  callBusy: boolean
  groupCallActive: boolean
  onStartCall: (kind: CallKind) => void
  onStartGroupCall: (kind: CallKind) => void
}

export default function ChatHeader({
  title,
  loading,
  memberChips,
  connStatus,
  justReconnected,
  onBack,
  canCall,
  canGroupCall,
  callBusy,
  groupCallActive,
  onStartCall,
  onStartGroupCall,
}: ChatHeaderProps) {
  const { t } = useTranslation('familyChat')

  return (
    <header className="flex items-center gap-2 px-3 sm:px-4 py-3 border-b border-gray-800 bg-gray-950 shrink-0">
      <button
        type="button"
        onClick={onBack}
        aria-label={t('chat.back')}
        className="md:hidden p-1.5 -ml-1 text-gray-300 hover:text-white rounded-md cursor-pointer"
      >
        <ChevronLeft size={20} aria-hidden="true" />
      </button>
      <div className="flex-1 min-w-0">
        <h2 className="text-base sm:text-lg font-semibold text-white truncate">
          {loading && title === null ? (
            <Skeleton className="h-5 w-40" />
          ) : (
            title || t('unnamedConversation')
          )}
        </h2>
        {memberChips.length > 0 && (
          <ul
            className="flex flex-wrap gap-1.5 mt-1.5"
            aria-label={t('chat.membersLabel')}
            role="list"
          >
            {memberChips.map(chip => (
              <li
                key={chip.id}
                className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs border ${
                  chip.isSelf
                    ? 'bg-blue-500/15 border-blue-500/40 text-blue-200'
                    : 'bg-gray-800 border-gray-700 text-gray-300'
                }`}
              >
                <span aria-hidden="true">{chip.emoji}</span>
                <span className="truncate max-w-[10rem]">{chip.label}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
      <ConnectionStatus state={connStatus} emphasizeLabel={connStatus === 'live' && justReconnected} />
      {canCall && (
        <>
          <button
            type="button"
            onClick={() => onStartCall('voice')}
            disabled={callBusy}
            aria-label={t('call.start')}
            title={t('call.start')}
            className="shrink-0 p-2 rounded-full text-green-300 hover:text-green-200 hover:bg-green-500/15 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
            data-testid="family-chat-call-button"
          >
            <Phone size={20} aria-hidden="true" />
          </button>
          <button
            type="button"
            onClick={() => onStartCall('video')}
            disabled={callBusy}
            aria-label={t('call.startVideo')}
            title={t('call.startVideo')}
            className="shrink-0 p-2 rounded-full text-blue-300 hover:text-blue-200 hover:bg-blue-500/15 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
            data-testid="family-chat-video-call-button"
          >
            <Video size={20} aria-hidden="true" />
          </button>
        </>
      )}
      {canGroupCall && (
        <>
          <button
            type="button"
            onClick={() => onStartGroupCall('voice')}
            disabled={groupCallActive}
            aria-label={t('call.group.start')}
            title={t('call.group.start')}
            className="shrink-0 p-2 rounded-full text-green-300 hover:text-green-200 hover:bg-green-500/15 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
            data-testid="family-chat-group-call-button"
          >
            <Phone size={20} aria-hidden="true" />
          </button>
          <button
            type="button"
            onClick={() => onStartGroupCall('video')}
            disabled={groupCallActive}
            aria-label={t('call.group.startVideo')}
            title={t('call.group.startVideo')}
            className="shrink-0 p-2 rounded-full text-blue-300 hover:text-blue-200 hover:bg-blue-500/15 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
            data-testid="family-chat-group-video-call-button"
          >
            <Video size={20} aria-hidden="true" />
          </button>
        </>
      )}
    </header>
  )
}
