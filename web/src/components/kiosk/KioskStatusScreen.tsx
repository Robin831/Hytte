import { Loader, ShieldAlert, WifiOff } from 'lucide-react'
import type { ComponentType } from 'react'

type KioskStatusTone = 'loading' | 'rejected' | 'unreachable'

// Per-tone presentation, keyed the same way KioskBusDepartures keys LINE_COLORS.
// The test id is derived from the tone rather than passed in as a prop so the
// two cannot drift apart.
const TONE_STYLES: Record<
  KioskStatusTone,
  {
    Icon: ComponentType<{ size?: number; className?: string }>
    iconClass: string
    testId: string
    isError: boolean
  }
> = {
  loading: {
    Icon: Loader,
    iconClass: 'text-gray-500 animate-spin',
    testId: 'kiosk-loading',
    isError: false,
  },
  rejected: {
    Icon: ShieldAlert,
    iconClass: 'text-amber-400',
    testId: 'kiosk-token-rejected',
    isError: true,
  },
  unreachable: {
    Icon: WifiOff,
    iconClass: 'text-red-400',
    testId: 'kiosk-unreachable',
    isError: true,
  },
}

interface Props {
  tone: KioskStatusTone
  title: string
  body: string
}

// Full-screen status panel shown when a tokened kiosk has no data to display:
// before its first successful fetch, or after its token has been rejected.
// Strings arrive already translated from KioskPage so this component stays
// presentational. Note that the surrounding kiosk panels are *not* translated
// at all — KioskStaleBadge hardcodes English and KioskBusDepartures hardcodes
// Norwegian, deliberately avoiding i18n for old-browser safety — so the kiosk
// is only partly localised today.
export default function KioskStatusScreen({ tone, title, body }: Props) {
  const { Icon, iconClass, testId, isError } = TONE_STYLES[tone]

  return (
    <div
      data-testid={testId}
      role={isError ? 'alert' : 'status'}
      aria-live={isError ? 'assertive' : 'polite'}
      className="min-h-screen bg-gray-950 text-white flex flex-col items-center justify-center gap-6 px-6 text-center"
    >
      <Icon size={64} className={iconClass} />
      <h1 className={`text-3xl sm:text-5xl font-semibold ${isError ? 'text-white' : 'text-gray-300'}`}>
        {title}
      </h1>
      <p className="text-lg sm:text-2xl text-gray-400 max-w-2xl leading-relaxed">{body}</p>
    </div>
  )
}
