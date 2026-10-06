import { describe, it, expect } from 'vitest'
import { computeAccountMonthToDate } from './runningTotals'

describe('computeAccountMonthToDate', () => {
  it('keeps separate totals per account', () => {
    const txns = [
      { id: 1, account_id: 10, amount: 1000, date: '2026-10-01' }, // NOK
      { id: 2, account_id: 20, amount: 50, date: '2026-10-02' }, // USD
      { id: 3, account_id: 10, amount: -200, date: '2026-10-03' },
      { id: 4, account_id: 20, amount: -20, date: '2026-10-04' },
    ]
    const totals = computeAccountMonthToDate(txns)
    expect(totals.get(1)).toBe(1000)
    expect(totals.get(2)).toBe(50)
    expect(totals.get(3)).toBe(800)
    expect(totals.get(4)).toBe(30)
  })

  it('accumulates by date then id regardless of input order', () => {
    const txns = [
      { id: 7, account_id: 1, amount: 3, date: '2026-10-05' },
      { id: 5, account_id: 1, amount: 2, date: '2026-10-05' },
      { id: 9, account_id: 1, amount: 1, date: '2026-10-01' },
    ]
    const totals = computeAccountMonthToDate(txns)
    expect(totals.get(9)).toBe(1)
    expect(totals.get(5)).toBe(3)
    expect(totals.get(7)).toBe(6)
  })

  it('does not reorder the input array', () => {
    const txns = [
      { id: 2, account_id: 1, amount: 1, date: '2026-10-02' },
      { id: 1, account_id: 1, amount: 1, date: '2026-10-01' },
    ]
    computeAccountMonthToDate(txns)
    expect(txns.map(t => t.id)).toEqual([2, 1])
  })

  it('returns the own amount for a single-transaction account', () => {
    const txns = [
      { id: 1, account_id: 1, amount: 100, date: '2026-10-01' },
      { id: 2, account_id: 2, amount: -42.5, date: '2026-10-02' },
    ]
    expect(computeAccountMonthToDate(txns).get(2)).toBe(-42.5)
  })

  it('handles transactions with an unknown or missing account', () => {
    const txns = [
      { id: 1, account_id: 999, amount: 10, date: '2026-10-01' },
      { id: 2, account_id: null, amount: 5, date: '2026-10-02' },
      { id: 3, account_id: undefined, amount: 7, date: '2026-10-03' },
    ]
    const totals = computeAccountMonthToDate(txns)
    expect(totals.get(1)).toBe(10)
    expect(totals.get(2)).toBe(5)
    expect(totals.get(3)).toBe(12)
  })

  it('returns an empty map for no transactions', () => {
    expect(computeAccountMonthToDate([]).size).toBe(0)
  })
})
