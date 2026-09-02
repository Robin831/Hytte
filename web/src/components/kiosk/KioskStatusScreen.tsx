import { Loader, ShieldAlert, WifiOff } from 'lucide-react'

export type KioskStatusTone = 'loading' | 'rejected' | 'unreachable'

interface Props {
  tone: KioskStatusTone
  title: string
  body: string
  testId: string
}

// Full-screen status panel shown before a tokened kiosk has ever fetched
// successfully. Strings are passed in already translated so this component
// stays presentational (matching the other kiosk display components).
export default function KioskStatusScreen({ tone, title, body, testId }: Props) {
  const isError = tone !== 'loading'
  const Icon = tone === 'rejected' ? ShieldAlert : tone === 'unreachable' ? WifiOff : Loader

  return (
    <div
      data-testid={testId}
      role={isError ? 'alert' : 'status'}
      aria-live={isError ? 'assertive' : 'polite'}
      className="min-h-screen bg-gray-950 text-white flex flex-col items-center justify-center gap-6 px-6 text-center"
    >
      <Icon
        size={64}
        className={
          isError
            ? tone === 'rejected'
              ? 'text-amber-400'
              : 'text-red-400'
            : 'text-gray-500 animate-spin'
        }
      />
      <h1 className={`text-3xl sm:text-5xl font-semibold ${isError ? 'text-white' : 'text-gray-300'}`}>
        {title}
      </h1>
      <p className="text-lg sm:text-2xl text-gray-400 max-w-2xl leading-relaxed">{body}</p>
    </div>
  )
}
