import { describe, it, expect } from 'vitest'
import {
  getAnvilFreshness,
  FRESH_THRESHOLD_MS,
  STALE_THRESHOLD_MS,
} from './anvilFreshness'

const NOW = new Date('2026-05-27T12:00:00Z')

function isoAgo(ms: number): string {
  return new Date(NOW.getTime() - ms).toISOString()
}

describe('getAnvilFreshness', () => {
  it('returns "never" for null', () => {
    expect(getAnvilFreshness(null, NOW)).toBe('never')
  })

  it('returns "never" for undefined', () => {
    expect(getAnvilFreshness(undefined, NOW)).toBe('never')
  })

  it('returns "never" for an invalid ISO string', () => {
    expect(getAnvilFreshness('not-a-date', NOW)).toBe('never')
  })

  it('returns "fresh" for a timestamp 29 minutes old', () => {
    expect(getAnvilFreshness(isoAgo(29 * 60 * 1000), NOW)).toBe('fresh')
  })

  it('returns "stale" exactly at the 30-minute boundary', () => {
    expect(getAnvilFreshness(isoAgo(FRESH_THRESHOLD_MS), NOW)).toBe('stale')
  })

  it('returns "stale" for a timestamp 23h59m old', () => {
    const ms = 23 * 60 * 60 * 1000 + 59 * 60 * 1000
    expect(getAnvilFreshness(isoAgo(ms), NOW)).toBe('stale')
  })

  it('returns "dead" exactly at the 24-hour boundary', () => {
    expect(getAnvilFreshness(isoAgo(STALE_THRESHOLD_MS), NOW)).toBe('dead')
  })

  it('returns "dead" for a timestamp 3 days old', () => {
    expect(getAnvilFreshness(isoAgo(3 * 24 * 60 * 60 * 1000), NOW)).toBe('dead')
  })

  it('returns "fresh" for a future timestamp (clock skew)', () => {
    const future = new Date(NOW.getTime() + 5 * 60 * 1000).toISOString()
    expect(getAnvilFreshness(future, NOW)).toBe('fresh')
  })

  it('defaults `now` to the current time when omitted', () => {
    const oneMinuteAgo = new Date(Date.now() - 60 * 1000).toISOString()
    expect(getAnvilFreshness(oneMinuteAgo)).toBe('fresh')
  })
})
