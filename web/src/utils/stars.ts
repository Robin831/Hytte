export function xpForLevel(n: number): number {
  if (n <= 0) return 0
  return Math.round(50 * Math.pow(n, 1.6))
}

export function xpProgressPercent(level: number, xp: number): number {
  const currentThreshold = xpForLevel(level - 1)
  const nextThreshold = xpForLevel(level)
  if (nextThreshold <= currentThreshold) return 100
  return Math.min(100, Math.max(0, ((xp - currentThreshold) / (nextThreshold - currentThreshold)) * 100))
}

export function getFlameVariant(count: number): string {
  if (count === 0) return 'flame-grey'
  if (count <= 2) return 'flame-small'
  if (count <= 6) return 'flame-medium'
  if (count <= 13) return 'flame-large'
  if (count <= 29) return 'flame-blue'
  return 'flame-rainbow'
}

export const REASON_EMOJI: Record<string, string> = {
  showed_up: '💪',
  duration_bonus: '⏱️',
  effort_bonus: '❤️',
  distance_milestone: '🏃',
  first_kilometer: '🏃',
  '5k_finisher': '🏃',
  '10k_hero': '🏃',
  half_marathon_legend: '🏃',
  century_club: '🏃',
  explorer_500k: '🏃',
  titan_1000k: '🏃',
  streak: '🔥',
  weekly_bonus: '📅',
  personal_record: '🏆',
  pr_longest_run: '🏆',
  pr_calorie_burn: '🏆',
  pr_elevation: '🏆',
  pr_fastest_5k: '🏆',
  pr_fastest_pace: '🏆',
  badge: '🏅',
  zone_commander: '🏅',
  zone_explorer: '🏅',
  easy_day_hero: '🏅',
  threshold_trainer: '🏅',
  waypoint_reached: '🗺️',
  savings_deposit: '🐷',
  savings_withdrawal: '🐷',
  savings_interest: '📈',
  bingo_line: '🎱',
  bingo_jackpot: '🎉',
}

export function formatRelativeTime(dateStr: string, locale: string): string {
  const date = new Date(dateStr)
  const now = Date.now()
  const diffMs = now - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  if (diffMins < 60) return rtf.format(-diffMins, 'minute')
  if (diffHours < 24) return rtf.format(-diffHours, 'hour')
  return rtf.format(-diffDays, 'day')
}
