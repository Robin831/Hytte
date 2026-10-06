import { describe, it, expect } from 'vitest'
import {
  EMPTY_DIM_OVERRIDE,
  applyDimOverride,
  dimOverrideFromConfig,
  dimOverrideToRequest,
  validateDimOverride,
} from './dimOverride'

describe('dimOverrideFromConfig → dimOverrideToRequest round trip', () => {
  it('keeps "off" plus a window', () => {
    const dim = dimOverrideFromConfig({ dim: false, dim_start: '22:30', dim_end: '06:00', location: 'Oslo' })
    expect(dim).toEqual({ mode: 'off', start: '22:30', end: '06:00' })
    expect(dimOverrideToRequest(dim)).toEqual({ dim: false, dim_start: '22:30', dim_end: '06:00' })
  })

  it('maps an absent dim to auto, sent as null', () => {
    const dim = dimOverrideFromConfig({ dim_start: '21:00', dim_end: '07:00' })
    expect(dim).toEqual({ mode: 'auto', start: '21:00', end: '07:00' })
    expect(dimOverrideToRequest(dim)).toEqual({ dim: null, dim_start: '21:00', dim_end: '07:00' })
  })

  it('reads a stored dim:true as auto, since the kiosk treats them the same', () => {
    const dim = dimOverrideFromConfig({ dim: true })
    expect(dim).toEqual(EMPTY_DIM_OVERRIDE)
    expect(dimOverrideToRequest(dim)).toEqual({ dim: null, dim_start: '', dim_end: '' })
  })

  it('trims times in the request', () => {
    expect(dimOverrideToRequest({ mode: 'auto', start: ' 22:00 ', end: '06:00 ' })).toEqual({
      dim: null,
      dim_start: '22:00',
      dim_end: '06:00',
    })
  })
})

describe('dimOverrideFromConfig defaults', () => {
  it.each([
    ['null', null],
    ['undefined', undefined],
    ['string', 'dim'],
    ['number', 1],
    ['array', [{ dim: false }]],
  ])('returns the default for a %s config', (_, config) => {
    expect(dimOverrideFromConfig(config)).toEqual(EMPTY_DIM_OVERRIDE)
  })

  it('ignores a wrongly-typed dim and window', () => {
    expect(dimOverrideFromConfig({ dim: 'off', dim_start: 2200, dim_end: 600 })).toEqual(EMPTY_DIM_OVERRIDE)
  })

  it.each([
    ['unpadded', { dim_start: '7:5', dim_end: '06:00' }],
    ['out of range', { dim_start: '24:00', dim_end: '06:00' }],
    ['start only', { dim_start: '22:00' }],
    ['end only', { dim_end: '06:00' }],
  ])('drops a %s window but keeps the mode', (_, window) => {
    const dim = dimOverrideFromConfig({ dim: false, ...window })
    expect(dim).toEqual({ mode: 'off', start: '', end: '' })
    expect(validateDimOverride(dim)).toBeNull()
  })
})

describe('validateDimOverride', () => {
  it('accepts an empty window', () => {
    expect(validateDimOverride(EMPTY_DIM_OVERRIDE)).toBeNull()
  })

  it('accepts a full window, including whitespace-padded times', () => {
    expect(validateDimOverride({ mode: 'auto', start: '22:00', end: '06:00' })).toBeNull()
    expect(validateDimOverride({ mode: 'off', start: ' 23:59 ', end: '00:00 ' })).toBeNull()
  })

  it.each([
    ['start only', '22:00', ''],
    ['end only', '', '06:00'],
  ])('rejects a half window (%s)', (_, start, end) => {
    expect(validateDimOverride({ mode: 'auto', start, end })).toBe('windowIncomplete')
  })

  it.each(['7:05', '24:00', '12:60', '1200', 'ab:cd'])('rejects %s as an invalid time', (bad) => {
    expect(validateDimOverride({ mode: 'auto', start: bad, end: '06:00' })).toBe('invalidTime')
    expect(validateDimOverride({ mode: 'auto', start: '22:00', end: bad })).toBe('invalidTime')
  })
})

describe('applyDimOverride', () => {
  it('omits every dim key for the default', () => {
    const config: Record<string, unknown> = { location: 'Oslo' }
    applyDimOverride(config, EMPTY_DIM_OVERRIDE)
    expect(config).toEqual({ location: 'Oslo' })
  })

  it('writes dim:false and a trimmed window', () => {
    const config: Record<string, unknown> = {}
    applyDimOverride(config, { mode: 'off', start: ' 22:00', end: '06:00 ' })
    expect(config).toEqual({ dim: false, dim_start: '22:00', dim_end: '06:00' })
  })

  it('omits a half window', () => {
    const config: Record<string, unknown> = {}
    applyDimOverride(config, { mode: 'auto', start: '22:00', end: '' })
    expect(config).toEqual({})
  })
})
