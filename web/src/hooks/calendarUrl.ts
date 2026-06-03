import {
  type ViewMode,
  startOfDay,
  endOfDay,
  addDays,
  startOfWeekMonday,
} from '../components/calendar/types'

/** Number of days shown in the agenda view (and per agenda navigation step). */
export const AGENDA_DAYS = 14

/** Format a Date as RFC3339 without fractional seconds. */
function toRFC3339(date: Date): string {
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

function getStartOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1)
}

/** Calculate the date range to fetch based on view mode and rangeStart. */
export function getViewRange(view: ViewMode, rangeStart: Date): { start: Date; end: Date } {
  switch (view) {
    case 'month': {
      // Fetch from start of first visible week to end of last visible week
      const firstOfMonth = getStartOfMonth(rangeStart)
      const lastOfMonth = new Date(rangeStart.getFullYear(), rangeStart.getMonth() + 1, 0)
      const gridStart = startOfWeekMonday(firstOfMonth)
      const gridEnd = endOfDay(addDays(startOfWeekMonday(addDays(lastOfMonth, 7)), -1))
      // Extend end to cover full 6th row if needed, using date-based arithmetic
      // to avoid DST-sensitive millisecond day calculations.
      const sixWeekEnd = endOfDay(addDays(gridStart, 41))
      const endDate = gridEnd.getTime() < sixWeekEnd.getTime() ? sixWeekEnd : gridEnd
      return { start: gridStart, end: endDate }
    }
    case 'week': {
      const ws = startOfWeekMonday(rangeStart)
      return { start: ws, end: endOfDay(addDays(ws, 6)) }
    }
    case 'day':
      return { start: startOfDay(rangeStart), end: endOfDay(rangeStart) }
    case 'agenda':
    default:
      return { start: startOfDay(rangeStart), end: endOfDay(addDays(rangeStart, AGENDA_DAYS - 1)) }
  }
}

/** Build the events fetch URL for a given view mode and range start. */
export function buildEventsUrl(view: ViewMode, rangeStart: Date, sync = false): string {
  const { start, end } = getViewRange(view, rangeStart)
  return `/api/calendar/events?start=${encodeURIComponent(toRFC3339(start))}&end=${encodeURIComponent(toRFC3339(end))}${sync ? '&sync=true' : ''}`
}
