import type { Suggestion, SuggestionSize } from '../components/suggestions/SuggestionCard'

export type SortMode = 'date' | 'size'

const SIZE_RANK: Record<SuggestionSize, number> = { l: 0, m: 1, s: 2 }

export function sortSuggestions(list: Suggestion[], mode: SortMode): Suggestion[] {
  const sorted = [...list]
  if (mode === 'date') {
    sorted.sort((a, b) => b.generated_at.localeCompare(a.generated_at))
  } else {
    sorted.sort((a, b) =>
      SIZE_RANK[a.size] - SIZE_RANK[b.size]
      || b.generated_at.localeCompare(a.generated_at)
      || a.id - b.id,
    )
  }
  return sorted
}

// A fixed Date at 03:00 Europe/Oslo in winter (UTC+1). Used only to format
// the scheduler's run time for display.
const OSLO_03H_UTC = new Date('2000-01-01T02:00:00Z')

// formatRunTime formats the scheduler's 03:00 Oslo run time using 24-hour
// notation. The locale parameter is accepted so callers can pass i18n.language,
// keeping the door open for locale-aware formatting in the future.
export function formatRunTime(locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    timeZone: 'Europe/Oslo',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(OSLO_03H_UTC)
}

// nextRunHintKey returns the i18n key for the header next-run text. The
// scheduler fires at 03:00 Europe/Oslo every day. If the next 03:00 is less
// than 12 hours away (i.e. evening/night through pre-03:00 the same morning)
// it is "tonight"; otherwise (daytime hours after the morning run) it is
// "tomorrow".
export function nextRunHintKey(now: Date): 'header.nextRunTonight' | 'header.nextRunTomorrow' {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Europe/Oslo',
    hour: 'numeric',
    minute: 'numeric',
    hour12: false,
  }).formatToParts(now)
  // Intl can return "24" for midnight in some runtimes — treat it as 0.
  const hour = parseInt(parts.find(p => p.type === 'hour')?.value ?? '0', 10) % 24
  const minute = parseInt(parts.find(p => p.type === 'minute')?.value ?? '0', 10)
  const minutesSinceMidnight = hour * 60 + minute
  // Minutes until the next 03:00 Oslo. At 03:00 exactly the next run is 24h away.
  const minutesUntil = ((3 * 60 - minutesSinceMidnight + 24 * 60) % (24 * 60)) || 24 * 60
  return minutesUntil < 12 * 60 ? 'header.nextRunTonight' : 'header.nextRunTomorrow'
}
