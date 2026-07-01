// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest'
import { parseDecimal } from '../utils/parseDecimal'

describe('parseDecimal', () => {
  it('parses Norwegian comma decimals', () => {
    expect(parseDecimal('7,5')).toBe(7.5)
    expect(parseDecimal('1234,56')).toBe(1234.56)
  })

  it('parses dot decimals unchanged', () => {
    expect(parseDecimal('7.5')).toBe(7.5)
  })

  it('parses integers', () => {
    expect(parseDecimal('42')).toBe(42)
  })

  it('returns NaN for empty input', () => {
    expect(Number.isNaN(parseDecimal(''))).toBe(true)
    expect(Number.isNaN(parseDecimal('  '))).toBe(true)
  })

  it('returns NaN for unparseable input', () => {
    expect(Number.isNaN(parseDecimal('abc'))).toBe(true)
  })

  it('preserves explicit zero', () => {
    expect(parseDecimal('0')).toBe(0)
    expect(parseDecimal('0,0')).toBe(0)
  })
})
