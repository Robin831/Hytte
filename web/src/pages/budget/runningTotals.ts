export interface RunningTotalTransaction {
  id: number
  account_id: number | null | undefined
  amount: number
  date: string
}

function dateToUTC(date: string): number {
  const [year, month, day] = date.split('-').map(Number)
  return Date.UTC(year, month - 1, day)
}

// computeAccountMonthToDate returns, for each transaction id, the cumulative
// sum of that transaction's account up to and including it. Transactions are
// walked oldest→newest by date with id as the tiebreak, and each account keeps
// its own accumulator so amounts in different accounts (and currencies) never
// mix. Transactions without an account_id share a single fallback bucket.
export function computeAccountMonthToDate(txns: RunningTotalTransaction[]): Map<number, number> {
  const sorted = [...txns].sort((a, b) => {
    const dateDiff = dateToUTC(a.date) - dateToUTC(b.date)
    return dateDiff !== 0 ? dateDiff : a.id - b.id
  })
  const perAccount = new Map<number | 'unknown', number>()
  const result = new Map<number, number>()
  for (const txn of sorted) {
    const key = txn.account_id ?? 'unknown'
    const total = (perAccount.get(key) ?? 0) + txn.amount
    perAccount.set(key, total)
    result.set(txn.id, total)
  }
  return result
}
