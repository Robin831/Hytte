import { useTranslation } from 'react-i18next'

/**
 * Pulsing placeholder that mirrors the watch-face header (time + date lines)
 * so the frame stays stable while auth resolves.
 */
export function TodayHeaderSkeleton() {
  const { t } = useTranslation('today')
  return (
    <header
      className="text-center mb-4 sm:mb-6 shrink-0"
      aria-busy="true"
      aria-label={t('loading')}
    >
      {/* Time line */}
      <div className="mx-auto h-10 sm:h-12 w-40 sm:w-48 rounded bg-gray-800 animate-pulse" />
      {/* Date line */}
      <div className="mx-auto mt-2 h-4 sm:h-5 w-48 sm:w-56 rounded bg-gray-800 animate-pulse" />
      {/* Ambient line */}
      <div className="mx-auto mt-2 h-3 w-32 rounded bg-gray-800 animate-pulse" />
      {/* Role line */}
      <div className="mx-auto mt-2 h-3 w-16 rounded bg-gray-800 animate-pulse" />
    </header>
  )
}

/**
 * Pulsing 2-column grid that mirrors the real widget grid layout so there is
 * no layout shift when the lazy role chunk finishes downloading.
 */
export function TodayGridSkeleton() {
  const { t } = useTranslation('today')
  return (
    <div
      className="grid grid-cols-2 gap-3 sm:gap-4 flex-1 min-h-0 auto-rows-fr"
      aria-busy="true"
      aria-label={t('loadingWidgets')}
    >
      {Array.from({ length: 4 }).map((_, i) => (
        <div key={i} className="rounded-xl bg-gray-800 animate-pulse" />
      ))}
    </div>
  )
}
