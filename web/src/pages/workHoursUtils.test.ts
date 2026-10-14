import { describe, it, expect } from 'vitest'
import { findSessionOverlaps, type WorkSession } from './workHoursUtils'

describe('findSessionOverlaps', () => {
  const session = (id: number, start: string, end: string, extra: Partial<WorkSession> = {}): WorkSession => ({
    id,
    day_id: 1,
    start_time: start,
    end_time: end,
    sort_order: id,
    is_internal: false,
    crosses_midnight: false,
    ...extra,
  })
  const pairIds = (pairs: Array<[WorkSession, WorkSession]>) => pairs.map(([a, b]) => [a.id, b.id])

  it('returns nothing for an empty list', () => {
    const result = findSessionOverlaps([])
    expect(result.ids.size).toBe(0)
    expect(result.pairs).toEqual([])
  })

  it('returns nothing when sessions do not overlap', () => {
    const result = findSessionOverlaps([session(1, '08:00', '10:00'), session(2, '11:00', '12:00')])
    expect(result.ids.size).toBe(0)
    expect(result.pairs).toEqual([])
  })

  it('treats adjacent sessions as not overlapping', () => {
    const result = findSessionOverlaps([session(1, '08:00', '10:00'), session(2, '10:00', '12:00')])
    expect(result.ids.size).toBe(0)
    expect(result.pairs).toEqual([])
  })

  it('detects a partial overlap', () => {
    const result = findSessionOverlaps([session(1, '08:00', '10:00'), session(2, '09:00', '11:00')])
    expect([...result.ids].sort()).toEqual([1, 2])
    expect(pairIds(result.pairs)).toEqual([[1, 2]])
  })

  it('detects containment', () => {
    const result = findSessionOverlaps([session(1, '08:00', '12:00'), session(2, '09:00', '10:00')])
    expect([...result.ids].sort()).toEqual([1, 2])
    expect(pairIds(result.pairs)).toEqual([[1, 2]])
  })

  it('reports every overlapping pair', () => {
    const result = findSessionOverlaps([
      session(1, '08:00', '12:00'),
      session(2, '09:00', '10:00'),
      session(3, '11:00', '13:00'),
      session(4, '14:00', '16:00'),
      session(5, '15:00', '17:00'),
      session(6, '18:00', '19:00'),
    ])
    expect([...result.ids].sort()).toEqual([1, 2, 3, 4, 5])
    expect(pairIds(result.pairs)).toEqual([[1, 2], [1, 3], [4, 5]])
  })

  it('ignores the internal flag', () => {
    const result = findSessionOverlaps([
      session(1, '08:00', '10:00', { is_internal: true }),
      session(2, '09:30', '11:00'),
    ])
    expect(pairIds(result.pairs)).toEqual([[1, 2]])
  })

  it('sorts unsorted input by start time', () => {
    const result = findSessionOverlaps([session(1, '13:00', '15:00'), session(2, '08:00', '14:00')])
    expect(pairIds(result.pairs)).toEqual([[2, 1]])
  })

  it('places a crosses-midnight session on the day it started', () => {
    // 22:00-02:00 runs into the next day, so it overlaps 23:00-23:30 but not
    // an early-morning session on the same day.
    const result = findSessionOverlaps([
      session(1, '22:00', '02:00', { crosses_midnight: true }),
      session(2, '23:00', '23:30'),
      session(3, '01:00', '03:00'),
    ])
    expect(pairIds(result.pairs)).toEqual([[1, 2]])
  })

  it('skips malformed and empty sessions', () => {
    const result = findSessionOverlaps([
      session(1, '08:00', '12:00'),
      session(2, 'nope', '10:00'),
      session(3, '10:00', '09:00'),
    ])
    expect(result.ids.size).toBe(0)
  })
})
