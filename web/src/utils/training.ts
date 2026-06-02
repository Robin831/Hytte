import type { TFunction } from 'i18next'
import { formatNumber } from './formatDate'

// Shared formatting helpers for the Training pages. These were previously
// re-implemented (with subtle divergences) in Training, TrainingDetail,
// TrainingCompare and TrainingTrends. Consolidating them here keeps unit
// suffixes localized and the formatting rules consistent across pages.

/**
 * Format a distance given in meters.
 *
 * Distances below 1 km are shown in whole meters; longer distances are shown
 * in kilometers. The kilometer fraction-digit count is fixed (default 2) so it
 * no longer diverges between the list and detail views.
 */
export function formatDistance(
  meters: number,
  t: TFunction<'training'>,
  opts?: { fractionDigits?: number },
): string {
  if (meters < 1000) return `${Math.round(meters)} ${t('units.m')}`
  const digits = opts?.fractionDigits ?? 2
  return `${formatNumber(meters / 1000, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  })} ${t('units.km')}`
}

/**
 * Format a running pace given in seconds per kilometer as `M:SS`.
 *
 * Non-positive paces render as `--:--`. Seconds are zero-padded and rolled over
 * to the next minute when rounding lands on 60. The localized `/km` suffix is
 * appended unless `withUnit` is false (e.g. for chart axes that already label
 * the unit).
 */
export function formatPace(
  secPerKm: number,
  t: TFunction<'training'>,
  opts?: { withUnit?: boolean },
): string {
  if (secPerKm <= 0) return '--:--'
  let mins = Math.floor(secPerKm / 60)
  let secs = Math.round(secPerKm % 60)
  if (secs === 60) {
    mins++
    secs = 0
  }
  const value = `${mins}:${secs.toString().padStart(2, '0')}`
  return opts?.withUnit === false ? value : `${value} ${t('units.pace')}`
}

/**
 * Format a duration given in seconds.
 *
 * Supports three styles:
 * - `clock` (default): `H:MM:SS` with hours, otherwise `M:SS` — for precise
 *   workout and lap durations.
 * - `human`: localized `Xh Ym` / `Ym` — minute granularity for summaries.
 * - `decimal`: localized hours with one fraction digit (e.g. `1.5h`) — for
 *   chart axes and tooltips.
 */
export function formatDuration(
  seconds: number,
  t: TFunction<'training'>,
  opts?: { style?: 'clock' | 'human' | 'decimal' },
): string {
  const style = opts?.style ?? 'clock'

  if (style === 'human') {
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (h > 0) return t('units.hours_minutes', { h, m })
    return t('units.minutes', { m })
  }

  if (style === 'decimal') {
    return `${(seconds / 3600).toFixed(1)}${t('units.h')}`
  }

  const total = Math.round(seconds)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
  }
  return `${m}:${s.toString().padStart(2, '0')}`
}
