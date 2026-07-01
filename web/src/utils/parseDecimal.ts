export function parseDecimal(value: string): number {
  return Number(String(value).replace(',', '.'))
}
