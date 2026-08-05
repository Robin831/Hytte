import { describe, it, expect } from 'vitest'
import type { CommissionTier } from './types'
import {
  hoursToNextTier,
  maxExtraHours,
  projectFromExtraHours,
  totalCommission,
} from './tierMath'

const tier = (id: number, floor: number, ceiling: number, rate: number): CommissionTier => ({
  id,
  config_id: 1,
  floor,
  ceiling,
  rate,
})

// 0–100k at 0%, 100k–200k at 20%, 200k+ at 40% (unbounded top tier).
const TIERS: CommissionTier[] = [
  tier(1, 0, 100_000, 0),
  tier(2, 100_000, 200_000, 0.2),
  tier(3, 200_000, 0, 0.4),
]

const HOURLY = 1_000

describe('totalCommission', () => {
  it('sums earnings across tiers', () => {
    expect(totalCommission(TIERS, 50_000)).toBe(0)
    expect(totalCommission(TIERS, 150_000)).toBeCloseTo(10_000)
    expect(totalCommission(TIERS, 250_000)).toBeCloseTo(20_000 + 20_000)
  })
})

describe('hoursToNextTier', () => {
  it('targets the first floor when below it', () => {
    expect(hoursToNextTier(TIERS, 40_000, HOURLY)).toBeCloseTo(60)
  })

  it('targets the next floor from mid-tier', () => {
    expect(hoursToNextTier(TIERS, 150_000, HOURLY)).toBeCloseTo(50)
  })

  it('targets the next floor — not zero — when exactly on a floor', () => {
    expect(hoursToNextTier(TIERS, 100_000, HOURLY)).toBeCloseTo(100)
  })

  it('rounds up to the nearest 0.1 hour so the floor is actually cleared', () => {
    // 99 kr short at 1000 kr/h = 0.099 h → 0.1 h.
    expect(hoursToNextTier(TIERS, 99_901, HOURLY)).toBeCloseTo(0.1)
  })

  it('returns null in the unbounded top tier', () => {
    expect(hoursToNextTier(TIERS, 250_000, HOURLY)).toBeNull()
  })

  it('returns null when the hourly rate is zero', () => {
    expect(hoursToNextTier(TIERS, 40_000, 0)).toBeNull()
  })

  it('returns null when there are no tiers', () => {
    expect(hoursToNextTier([], 40_000, HOURLY)).toBeNull()
  })
})

describe('maxExtraHours', () => {
  it('covers the highest bounded ceiling', () => {
    // 200k ceiling − 100k revenue = 100k / 1000 = 100 h.
    expect(maxExtraHours(TIERS, 100_000, HOURLY)).toBe(100)
  })

  it('falls back to a usable range past the top ceiling', () => {
    expect(maxExtraHours(TIERS, 500_000, HOURLY)).toBe(40)
  })

  it('falls back when every tier is unbounded', () => {
    expect(maxExtraHours([tier(1, 0, 0, 0.3)], 100_000, HOURLY)).toBe(40)
  })

  it('returns 0 when the hourly rate is zero', () => {
    expect(maxExtraHours(TIERS, 100_000, 0)).toBe(0)
  })
})

describe('projectFromExtraHours', () => {
  const base = {
    tiers: TIERS,
    baselineRevenue: 150_000,
    hourlyRate: HOURLY,
    currentGross: 80_000,
    currentNet: 50_000,
    extraHourNet: 120, // 200 kr gross commission per hour, 60% net
  }

  it('reproduces the current figures at zero extra hours', () => {
    const p = projectFromExtraHours({ ...base, extraHours: 0 })
    expect(p.revenue).toBe(150_000)
    expect(p.commission).toBeCloseTo(10_000)
    expect(p.gross).toBe(80_000)
    expect(p.net).toBe(50_000)
  })

  it('projects mid-tier hours at the tier rate', () => {
    const p = projectFromExtraHours({ ...base, extraHours: 10 })
    expect(p.revenue).toBe(160_000)
    expect(p.commission).toBeCloseTo(12_000)
    expect(p.gross).toBeCloseTo(82_000)
    // 2000 gross × (120 / 200) marginal net ratio.
    expect(p.net).toBeCloseTo(51_200)
  })

  it('applies the higher rate after crossing a tier boundary', () => {
    const p = projectFromExtraHours({ ...base, extraHours: 100 })
    expect(p.revenue).toBe(250_000)
    // 50k left in the 20% tier + 50k in the 40% tier.
    expect(p.commission).toBeCloseTo(10_000 + 20_000)
    expect(p.gross).toBeCloseTo(80_000 + 20_000)
    expect(p.net).toBeCloseTo(50_000 + 12_000)
  })

  it('stays below the first floor without earning commission', () => {
    const p = projectFromExtraHours({
      ...base,
      baselineRevenue: 40_000,
      extraHours: 10,
      extraHourNet: 0, // server reports no marginal gain inside the 0% tier
    })
    expect(p.revenue).toBe(50_000)
    expect(p.commission).toBe(0)
    expect(p.gross).toBe(80_000)
    expect(p.net).toBe(50_000)
  })

  it('does not divide by zero when the hourly rate is zero', () => {
    const p = projectFromExtraHours({ ...base, hourlyRate: 0, extraHours: 10 })
    expect(p.revenue).toBe(150_000)
    expect(p.commission).toBeCloseTo(10_000)
    expect(p.gross).toBe(80_000)
    expect(p.net).toBe(50_000)
    expect(Number.isFinite(p.net)).toBe(true)
  })

  it('falls back to the average net ratio when no marginal figure is available', () => {
    // Zero-rate first tier: one extra hour adds no gross, so extra_hour_net is
    // unusable. Crossing into the 20% tier must still move net.
    const p = projectFromExtraHours({
      ...base,
      baselineRevenue: 99_000,
      extraHours: 10,
      extraHourNet: 0,
    })
    expect(p.revenue).toBe(109_000)
    expect(p.commission).toBeCloseTo(1_800)
    expect(p.gross).toBeCloseTo(81_800)
    // Average ratio 50_000 / 80_000 = 0.625 applied to the 1800 gross delta.
    expect(p.net).toBeCloseTo(50_000 + 1_125)
  })

  it('reports per-tier progress and earnings at the projected revenue', () => {
    const p = projectFromExtraHours({ ...base, extraHours: 100 })
    expect(p.perTier).toHaveLength(3)
    expect(p.perTier[0]).toEqual({ progress: 100, earnings: 0 })
    expect(p.perTier[1].progress).toBe(100)
    expect(p.perTier[1].earnings).toBeCloseTo(20_000)
    expect(p.perTier[2].progress).toBe(100)
    expect(p.perTier[2].earnings).toBeCloseTo(20_000)
  })

  it('treats negative extra hours as zero', () => {
    const p = projectFromExtraHours({ ...base, extraHours: -5 })
    expect(p.revenue).toBe(150_000)
    expect(p.gross).toBe(80_000)
  })
})
