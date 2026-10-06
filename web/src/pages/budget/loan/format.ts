export function fmt(n: number): string {
  return new Intl.NumberFormat('nb-NO', {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(n)
}

export function fmtPct(n: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'percent',
    minimumFractionDigits: 1,
    maximumFractionDigits: 2,
  }).format(n)
}

/** Effective annual rate from nominal rate compounded monthly: (1 + r/12)^12 - 1 */
export function effectiveRate(nominalAnnual: number): number {
  return Math.pow(1 + nominalAnnual / 12, 12) - 1
}

export { localDateString } from '../../../lib/dates'
