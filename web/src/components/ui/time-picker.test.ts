import { describe, it, expect } from 'vitest'
import { parseTimeInput, adjustTime } from './time-picker-utils'

describe('parseTimeInput', () => {
  it('parses HH:MM colon format', () => {
    expect(parseTimeInput('06:30')).toBe('06:30')
    expect(parseTimeInput('14:45')).toBe('14:45')
    expect(parseTimeInput('00:00')).toBe('00:00')
    expect(parseTimeInput('23:59')).toBe('23:59')
  })

  it('parses H:M short colon format', () => {
    expect(parseTimeInput('9:5')).toBe('09:05')
    expect(parseTimeInput('8:0')).toBe('08:00')
  })

  it('parses 4-digit compact format', () => {
    expect(parseTimeInput('0600')).toBe('06:00')
    expect(parseTimeInput('1430')).toBe('14:30')
    expect(parseTimeInput('2359')).toBe('23:59')
  })

  it('parses 3-digit compact format', () => {
    expect(parseTimeInput('630')).toBe('06:30')
    expect(parseTimeInput('930')).toBe('09:30')
    expect(parseTimeInput('900')).toBe('09:00')
  })

  it('parses 1-2 digit hour-only', () => {
    expect(parseTimeInput('9')).toBe('09:00')
    expect(parseTimeInput('09')).toBe('09:00')
    expect(parseTimeInput('12')).toBe('12:00')
  })

  it('trims whitespace', () => {
    expect(parseTimeInput('  06:30  ')).toBe('06:30')
  })

  it('returns null for empty input', () => {
    expect(parseTimeInput('')).toBeNull()
    expect(parseTimeInput('   ')).toBeNull()
  })

  it('returns null for out-of-range hours', () => {
    expect(parseTimeInput('24:00')).toBeNull()
    expect(parseTimeInput('99:00')).toBeNull()
  })

  it('returns null for out-of-range minutes', () => {
    expect(parseTimeInput('12:60')).toBeNull()
    expect(parseTimeInput('12:99')).toBeNull()
  })

  it('returns null for non-numeric input', () => {
    expect(parseTimeInput('abc')).toBeNull()
  })
})

describe('adjustTime', () => {
  it('adds minutes forward', () => {
    expect(adjustTime('06:00', 15)).toBe('06:15')
    expect(adjustTime('06:00', 60)).toBe('07:00')
  })

  it('subtracts minutes backward', () => {
    expect(adjustTime('06:15', -15)).toBe('06:00')
    expect(adjustTime('07:00', -60)).toBe('06:00')
  })

  it('wraps forward past midnight', () => {
    expect(adjustTime('23:45', 15)).toBe('00:00')
    expect(adjustTime('23:00', 90)).toBe('00:30')
  })

  it('wraps backward before midnight', () => {
    expect(adjustTime('00:00', -15)).toBe('23:45')
    expect(adjustTime('00:00', -60)).toBe('23:00')
  })

  it('handles zero delta', () => {
    expect(adjustTime('12:30', 0)).toBe('12:30')
  })
})
