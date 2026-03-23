import type { TFunction } from 'i18next'
import { formatDate } from './formatDate'

export function timeAgo(dateStr: string, t: TFunction<'common'>): string {
  const now = new Date()
  const date = new Date(dateStr)
  if (isNaN(date.getTime())) return '-'
  const diffMs = now.getTime() - date.getTime()
  const diffMin = Math.max(0, Math.floor(diffMs / 60000))
  if (diffMin < 1) return t('time.justNow')
  if (diffMin < 60) return t('time.minutesAgo', { count: diffMin })
  const diffHours = Math.floor(diffMin / 60)
  if (diffHours < 24) return t('time.hoursAgo', { count: diffHours })
  const diffDays = Math.floor(diffHours / 24)
  if (diffDays === 1) return t('time.yesterday')
  if (diffDays < 7) return t('time.daysAgo', { count: diffDays })
  if (diffDays < 30) return t('time.weeksAgo', { count: Math.floor(diffDays / 7) })
  return formatDate(dateStr, { month: 'short', day: 'numeric' })
}
