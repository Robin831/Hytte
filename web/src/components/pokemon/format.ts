import { formatDate, formatNumber } from '../../utils/formatDate'

// 0 is treated the same as missing — Cardmarket never quotes a card at
// exactly €0,00 (the floor is €0,01), so amount===0 always means upstream
// hasn't priced this card yet (common for cards from the new Mega Evolution
// series whose Cardmarket scraper bridge isn't wired to pokemontcg.io's API
// yet). Showing "kr 0" makes the card look free; "—" reads as unknown.
export function formatNok(amount: number | null | undefined): string {
  if (amount == null || amount === 0) return '—'
  return formatNumber(amount, {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

export function formatReleaseDate(raw: string): string {
  // pokemontcg.io releases dates as "YYYY/MM/DD". `new Date('YYYY-MM-DD')`
  // parses as UTC midnight, which becomes the previous day in any negative-UTC
  // timezone, so we construct the Date from local components instead. Fall
  // back to the raw string when parsing fails so we never render
  // "Invalid Date".
  const m = raw.match(/^(\d{4})[/-](\d{1,2})[/-](\d{1,2})$/)
  let d: Date
  if (m) {
    d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  } else {
    d = new Date(raw)
  }
  if (Number.isNaN(d.getTime())) return raw
  try {
    return formatDate(d, { dateStyle: 'medium' })
  } catch {
    return raw
  }
}
