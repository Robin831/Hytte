export interface CalendarEvent {
  id: string
  calendar_id: string
  title: string
  description?: string
  location?: string
  start_time: string
  end_time: string
  all_day: boolean
  status: string
  color?: string
}

export interface CalendarInfo {
  id: string
  summary: string
  description?: string
  background_color?: string
  foreground_color?: string
  primary: boolean
  selected: boolean
}

export type ViewMode = 'month' | 'week' | 'day' | 'agenda'

export interface CalendarViewProps {
  events: CalendarEvent[]
  calendars: CalendarInfo[]
  rangeStart: Date
  locale: string
  onNavigateToDay: (date: Date) => void
}

export function getCalendarColorMap(calendars: CalendarInfo[]): Map<string, string> {
  return new Map(calendars.map(c => [c.id, c.background_color || '#4285f4']))
}

export function getEventColor(event: CalendarEvent, colorMap: Map<string, string>): string {
  return event.color || colorMap.get(event.calendar_id) || '#4285f4'
}

export function formatDateKey(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

/** Get Monday-based start of week — shared across all calendar views. */
export function startOfWeekMonday(date: Date): Date {
  const d = new Date(date)
  const day = d.getDay()
  // Monday = 1, Sunday = 0 → shift Sunday to position 7
  const diff = day === 0 ? 6 : day - 1
  d.setDate(d.getDate() - diff)
  d.setHours(0, 0, 0, 0)
  return d
}

export function addDays(date: Date, days: number): Date {
  const d = new Date(date)
  d.setDate(d.getDate() + days)
  return d
}

export function startOfDay(date: Date): Date {
  const d = new Date(date)
  d.setHours(0, 0, 0, 0)
  return d
}

export function endOfDay(date: Date): Date {
  const d = new Date(date)
  d.setHours(23, 59, 59, 0)
  return d
}

export function isSameDay(a: Date, b: Date): boolean {
  return formatDateKey(a) === formatDateKey(b)
}

export function isToday(date: Date): boolean {
  return isSameDay(date, new Date())
}
