// Pure commission-tier math shared by the month view's tier bars and the
// client-side what-if projection. No React, no I/O — everything here is a
// deterministic function of the numbers already present in the
// /api/salary/estimate response.

import type { CommissionTier } from './types'

/** Calculate how far into a tier the given revenue is, as a 0–100 percentage. */
export const getTierProgress = (tier: CommissionTier, billableRevenue: number): number => {
  if (billableRevenue <= tier.floor) return 0
  if (tier.ceiling === 0) return 100 // Unbounded — always full once reached
  const filled = Math.min(billableRevenue, tier.ceiling) - tier.floor
  const total = tier.ceiling - tier.floor
  return Math.min((filled / total) * 100, 100)
}

/** Commission earned inside a single tier at the given revenue. */
export const getTierEarnings = (tier: CommissionTier, billableRevenue: number): number => {
  if (billableRevenue <= tier.floor) return 0
  const high = tier.ceiling === 0 ? billableRevenue : Math.min(billableRevenue, tier.ceiling)
  return (high - tier.floor) * tier.rate
}

/** True when the given revenue currently sits inside this tier's band. */
export const isTierActive = (tier: CommissionTier, billableRevenue: number): boolean =>
  billableRevenue > tier.floor && (tier.ceiling === 0 || billableRevenue <= tier.ceiling)

/** Total commission across all tiers — mirrors the backend's CalculateCommission. */
export const totalCommission = (tiers: CommissionTier[], billableRevenue: number): number =>
  tiers.reduce((sum, tier) => sum + getTierEarnings(tier, billableRevenue), 0)

/**
 * Billable hours needed to reach the nearest tier floor strictly above the
 * given revenue, rounded up to the nearest 0.1 h so the displayed number
 * really does clear the floor.
 *
 * Returns null when there is no higher floor (already in the top tier) or when
 * the hourly rate is missing/zero.
 */
export const hoursToNextTier = (
  tiers: CommissionTier[],
  billableRevenue: number,
  hourlyRate: number,
): number | null => {
  if (hourlyRate <= 0) return null
  let nextFloor: number | null = null
  for (const tier of tiers) {
    if (tier.floor > billableRevenue && (nextFloor === null || tier.floor < nextFloor)) {
      nextFloor = tier.floor
    }
  }
  if (nextFloor === null) return null
  return Math.ceil(((nextFloor - billableRevenue) / hourlyRate) * 10) / 10
}

/**
 * Upper bound for the what-if slider: enough extra hours to clear the ceiling
 * of the highest bounded tier, rounded up to a whole 0.5 h step. Falls back to
 * a sensible default when every tier is unbounded or the ceiling is already
 * passed, so the slider always has a usable range.
 */
export const FALLBACK_MAX_EXTRA_HOURS = 40

export const maxExtraHours = (
  tiers: CommissionTier[],
  billableRevenue: number,
  hourlyRate: number,
): number => {
  if (hourlyRate <= 0) return 0
  const ceilings = tiers.filter(t => t.ceiling > 0).map(t => t.ceiling)
  if (ceilings.length === 0) return FALLBACK_MAX_EXTRA_HOURS
  const top = Math.max(...ceilings)
  const needed = Math.max(0, (top - billableRevenue) / hourlyRate)
  return Math.max(FALLBACK_MAX_EXTRA_HOURS, Math.ceil(needed / 0.5) * 0.5)
}

export interface ProjectionInput {
  /** Absence-adjusted tiers, as rendered by the tier bars. */
  tiers: CommissionTier[]
  /** Revenue the tier bars are drawn at (billable + internal). */
  baselineRevenue: number
  hourlyRate: number
  /** Gross currently shown in the hero card. */
  currentGross: number
  /** Net currently shown in the hero card. */
  currentNet: number
  /** Server-computed net gain from one extra billable hour. */
  extraHourNet: number
  extraHours: number
}

export interface Projection {
  revenue: number
  commission: number
  gross: number
  /** Linearised — see projectFromExtraHours. */
  net: number
  perTier: { progress: number; earnings: number }[]
}

/**
 * Project revenue, commission, gross and net for a number of additional
 * billable hours, using only data already on screen.
 *
 * Revenue and commission are exact: commission is recomputed from the tiers, so
 * crossing a tier boundary shows up. Gross follows the backend composition
 * (base + commission + sick addon + benefits), where only the commission term
 * moves with extra hours — hence gross = currentGross + Δcommission.
 *
 * Net is deliberately *linearised*: the server may replace the formula tax with
 * a trekktabell lookup table the frontend never receives, so tax cannot be
 * re-derived here. Instead the marginal net-to-gross ratio implied by
 * extra_hour_net is applied to the gross delta. Present the result as an
 * estimate.
 */
export const projectFromExtraHours = ({
  tiers,
  baselineRevenue,
  hourlyRate,
  currentGross,
  currentNet,
  extraHourNet,
  extraHours,
}: ProjectionInput): Projection => {
  const revenue = baselineRevenue + Math.max(0, extraHours) * Math.max(0, hourlyRate)
  const baselineCommission = totalCommission(tiers, baselineRevenue)
  const commission = totalCommission(tiers, revenue)
  const gross = currentGross + (commission - baselineCommission)

  // Marginal net-to-gross ratio from the server's one-extra-hour figure.
  const grossAtOneHour =
    currentGross + (totalCommission(tiers, baselineRevenue + hourlyRate) - baselineCommission)
  const oneHourGrossDelta = grossAtOneHour - currentGross
  let netRatio: number
  if (oneHourGrossDelta > 1e-9 && extraHourNet > 0) {
    netRatio = extraHourNet / oneHourGrossDelta
  } else if (currentGross > 0) {
    // No usable marginal figure (zero-rate tier, missing hourly rate) — fall
    // back to the average net-to-gross ratio of the current month.
    netRatio = currentNet / currentGross
  } else {
    netRatio = 1
  }
  netRatio = Math.min(Math.max(netRatio, 0), 1)

  return {
    revenue,
    commission,
    gross,
    net: currentNet + netRatio * (gross - currentGross),
    perTier: tiers.map(tier => ({
      progress: getTierProgress(tier, revenue),
      earnings: getTierEarnings(tier, revenue),
    })),
  }
}
