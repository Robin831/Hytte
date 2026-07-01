export function parseDecimal(value: string): number {
  const s = String(value).trim()
  if (s === '') return NaN
  return Number(s.replace(',', '.'))
}
