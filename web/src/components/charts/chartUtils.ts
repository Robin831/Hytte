/** Compute a rolling (moving) average over an array of numbers. */
export function rollingAvg(values: number[], windowSize: number): number[] {
  const result: number[] = []
  let sum = 0
  for (let i = 0; i < values.length; i++) {
    sum += values[i]
    if (i >= windowSize) {
      sum -= values[i - windowSize]
    }
    const count = Math.min(i + 1, windowSize)
    result.push(sum / count)
  }
  return result
}

/** Compute the mean of values, ignoring nulls/undefineds/zeros. */
export function computeAverage(values: number[]): number {
  const valid = values.filter((v) => v != null && v > 0)
  if (valid.length === 0) return 0
  return valid.reduce((a, b) => a + b, 0) / valid.length
}
