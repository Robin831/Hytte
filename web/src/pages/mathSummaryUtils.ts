type Op = '*' | '/'
type Level = 'unseen' | 'red' | 'yellow' | 'green'

export interface LeaderboardEntry {
  user_id: number
  rank: number | null
}

export interface StatsCell {
  a: number
  b: number
  op: Op
  count: number
  accuracy_pct: number
  level: Level
}

export interface StatsResponse {
  multiplication: StatsCell[][]
  division: StatsCell[][]
}

const LEVEL_WEAKNESS: Record<Level, number> = { red: 0, yellow: 1, green: 2, unseen: 3 }

export function computeWeakestFacts(stats: StatsResponse, limit: number): StatsCell[] {
  const cells: StatsCell[] = []
  for (const grid of [stats.multiplication, stats.division]) {
    if (!grid) continue
    for (const row of grid) {
      for (const cell of row) {
        if (cell.count > 0) cells.push(cell)
      }
    }
  }
  cells.sort((x, y) => {
    if (x.accuracy_pct !== y.accuracy_pct) return x.accuracy_pct - y.accuracy_pct
    const lx = LEVEL_WEAKNESS[x.level] ?? 3
    const ly = LEVEL_WEAKNESS[y.level] ?? 3
    if (lx !== ly) return lx - ly
    return y.count - x.count
  })
  return cells.slice(0, limit)
}

export function findUserRank(entries: LeaderboardEntry[], userId: number | undefined): number | null {
  if (userId == null) return null
  const entry = entries.find(e => e.user_id === userId)
  return entry?.rank ?? null
}
