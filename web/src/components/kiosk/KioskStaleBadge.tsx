interface KioskStaleBadgeProps {
  isStale: boolean
  lastSuccessAt: number | null
  now: number
}

function formatAge(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000))
  if (seconds < 60) return `${seconds} sec ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.floor(minutes / 60)
  return `${hours} hr ago`
}

export default function KioskStaleBadge({ isStale, lastSuccessAt, now }: KioskStaleBadgeProps) {
  if (!isStale || lastSuccessAt === null) return null

  const ageMs = now - lastSuccessAt
  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="kiosk-stale-badge"
      className="fixed top-2 right-2 px-2 py-1 rounded bg-gray-900/80 text-gray-400 text-xs font-mono opacity-70"
    >
      Updated {formatAge(ageMs)}
    </div>
  )
}
