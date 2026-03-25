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
