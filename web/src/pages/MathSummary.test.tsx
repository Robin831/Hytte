// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest'
import { computeWeakestFacts, findUserRank } from './MathSummary'

type Op = '*' | '/'
type Level = 'unseen' | 'red' | 'yellow' | 'green'

interface Cell {
  a: number
  b: number
  op: Op
  count: number
  accuracy_pct: number
  level: Level
}

function makeGrid(op: Op, overrides: Partial<Cell>[][] = []): Cell[][] {
  return Array.from({ length: 10 }, (_, row) =>
    Array.from({ length: 10 }, (_, col) => ({
      a: row + 1,
      b: col + 1,
      op,
      count: 0,
      accuracy_pct: 0,
      level: 'unseen' as Level,
      ...(overrides[row]?.[col] ?? {}),
    }))
  )
}

describe('computeWeakestFacts', () => {
  it('returns only attempted cells, weakest accuracy first, capped at the limit', () => {
    const mult = makeGrid('*')
    // Three attempted multiplication cells with varying accuracy.
    mult[0][0] = { a: 1, b: 1, op: '*', count: 5, accuracy_pct: 80, level: 'green' }
    mult[1][2] = { a: 2, b: 3, op: '*', count: 4, accuracy_pct: 20, level: 'red' }
    mult[6][7] = { a: 7, b: 8, op: '*', count: 6, accuracy_pct: 50, level: 'yellow' }
    const div = makeGrid('/')
    div[3][1] = { a: 4, b: 2, op: '/', count: 3, accuracy_pct: 10, level: 'red' }

    const weakest = computeWeakestFacts({ multiplication: mult, division: div }, 3)

    expect(weakest).toHaveLength(3)
    expect(weakest.map(c => c.accuracy_pct)).toEqual([10, 20, 50])
    // The 80%/green cell is the strongest and falls outside the top 3.
    expect(weakest.some(c => c.accuracy_pct === 80)).toBe(false)
  })

  it('ignores cells that have never been attempted', () => {
    const mult = makeGrid('*')
    const div = makeGrid('/')
    expect(computeWeakestFacts({ multiplication: mult, division: div }, 3)).toEqual([])
  })

  it('breaks accuracy ties by mastery level (red is weaker than yellow)', () => {
    const mult = makeGrid('*')
    mult[0][0] = { a: 1, b: 1, op: '*', count: 5, accuracy_pct: 50, level: 'yellow' }
    mult[0][1] = { a: 1, b: 2, op: '*', count: 5, accuracy_pct: 50, level: 'red' }
    const weakest = computeWeakestFacts({ multiplication: mult, division: makeGrid('/') }, 2)
    expect(weakest[0].level).toBe('red')
    expect(weakest[1].level).toBe('yellow')
  })
})

describe('findUserRank', () => {
  const entries = [
    { user_id: 1, rank: 1 },
    { user_id: 7, rank: 2 },
    { user_id: 9, rank: null },
  ]

  it('returns the rank of the matching user', () => {
    expect(findUserRank(entries, 7)).toBe(2)
  })

  it('returns null when the user has no entry', () => {
    expect(findUserRank(entries, 42)).toBeNull()
  })

  it('returns null when the user id is undefined', () => {
    expect(findUserRank(entries, undefined)).toBeNull()
  })

  it('returns null when the matching entry has no rank', () => {
    expect(findUserRank(entries, 9)).toBeNull()
  })
})
