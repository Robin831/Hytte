import { useTranslation } from 'react-i18next'

// The live state of the Family Chat SSE stream, driven entirely by the existing
// EventSource-style fetch reader lifecycle in ChatView:
//   'connecting'   — initial load, before the stream has ever opened
//   'live'         — stream is open and messages arrive in real time
//   'reconnecting' — the stream dropped after being live and a backoff retry is pending
//   'offline'      — the browser reports no network connectivity
export type ChatConnectionState = 'connecting' | 'live' | 'reconnecting' | 'offline'

// Per-state presentation: dot colour, badge colours, i18n key and test id. The
// 'connecting' state is intentionally absent — it renders nothing so the initial
// skeleton isn't shadowed by a premature status badge.
const STATE_META: Record<
  Exclude<ChatConnectionState, 'connecting'>,
  { dot: string; badge: string; labelKey: string; testId: string; pulse: boolean }
> = {
  live: {
    dot: 'bg-green-400',
    badge: 'bg-green-500/15 border-green-500/40 text-green-200',
    labelKey: 'chat.connection.live',
    testId: 'family-chat-connected',
    pulse: false,
  },
  reconnecting: {
    dot: 'bg-amber-400',
    badge: 'bg-amber-500/15 border-amber-500/40 text-amber-200',
    labelKey: 'chat.connection.reconnecting',
    testId: 'family-chat-reconnecting',
    pulse: true,
  },
  offline: {
    dot: 'bg-gray-400',
    badge: 'bg-gray-700/40 border-gray-600 text-gray-300',
    labelKey: 'chat.connection.offline',
    testId: 'family-chat-offline',
    pulse: false,
  },
}

interface ConnectionStatusProps {
  state: ChatConnectionState
  // When true, the text label is shown even at narrow widths. Used for the brief
  // "Connected" confirmation right after a reconnect so mobile users get a clear,
  // readable signal that live delivery has resumed.
  emphasizeLabel?: boolean
}

// Compact connection-status indicator for the ChatView header: a coloured dot
// plus a localized label. On narrow (<640px) screens the label collapses to just
// the dot so it never pushes or overflows the header layout; the accessible name
// is preserved via the title attribute and aria-live region.
export default function ConnectionStatus({ state, emphasizeLabel = false }: ConnectionStatusProps) {
  const { t } = useTranslation('familyChat')

  if (state === 'connecting') return null

  const meta = STATE_META[state]
  const label = t(meta.labelKey)

  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs border shrink-0 ${meta.badge}`}
      role="status"
      aria-live="polite"
      title={label}
      data-testid={meta.testId}
    >
      <span
        className={`w-2 h-2 rounded-full ${meta.dot} ${meta.pulse ? 'animate-pulse' : ''}`}
        aria-hidden="true"
      />
      <span className={`${emphasizeLabel ? 'inline' : 'hidden sm:inline'} truncate max-w-[8rem]`}>
        {label}
      </span>
    </span>
  )
}
