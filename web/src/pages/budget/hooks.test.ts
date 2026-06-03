// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { formatNOK, formatBudgetNumber, useBudgetResource } from './hooks'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('formatNOK', () => {
  it('defaults to NOK and renders no fraction digits', () => {
    const out = formatNOK(1234)
    // The exact symbol/placement is locale-dependent; assert the digits and
    // currency marker rather than a fixed string.
    expect(out).toMatch(/1\D?234/)
    expect(out).not.toMatch(/[.,]\d{2}\b/) // no decimals
    expect(out.toLowerCase()).toMatch(/kr|nok/)
  })

  it('honors an explicit currency argument', () => {
    const out = formatNOK(1000, 'USD')
    expect(out).toMatch(/\$|USD/)
  })
})

describe('formatBudgetNumber', () => {
  it('is symbol-free and defaults to whole numbers', () => {
    const out = formatBudgetNumber(1234.56)
    expect(out).not.toMatch(/kr|nok|\$/i)
    expect(out).toMatch(/1\D?235/) // rounded, grouped
  })

  it('honors fraction-digit overrides (e.g. CSV import preview)', () => {
    const out = formatBudgetNumber(12, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    expect(out).toMatch(/12[.,]00/)
  })
})

describe('useBudgetResource', () => {
  it('reports loading=true synchronously before the fetch resolves', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { result } = renderHook(() => useBudgetResource<{ value: number }>('/api/budget/test', 'failed'))
    expect(result.current.loading).toBe(true)
    expect(result.current.error).toBeNull()
    expect(result.current.data).toBeNull()
  })

  it('stores the parsed body and clears loading on success', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve({ value: 42 }) })))
    const { result } = renderHook(() => useBudgetResource<{ value: number }>('/api/budget/test', 'failed'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toEqual({ value: 42 })
    expect(result.current.error).toBeNull()
  })

  it('surfaces the error message on a non-ok response', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false, json: () => Promise.resolve({}) })))
    const { result } = renderHook(() => useBudgetResource('/api/budget/test', 'load failed'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('load failed')
    expect(result.current.data).toBeNull()
  })

  it('swallows AbortError without setting an error', async () => {
    const abortErr = new DOMException('aborted', 'AbortError')
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(abortErr)))
    const { result } = renderHook(() => useBudgetResource('/api/budget/test', 'failed'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBeNull()
  })

  it('aborts the in-flight request on unmount', () => {
    const abortSpy = vi.spyOn(AbortController.prototype, 'abort')
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { unmount } = renderHook(() => useBudgetResource('/api/budget/test', 'failed'))
    unmount()
    expect(abortSpy).toHaveBeenCalled()
  })

  it('refetches when reload() is called', async () => {
    const fetchMock = vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve({ value: 1 }) }))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useBudgetResource<{ value: number }>('/api/budget/test', 'failed'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledTimes(1)

    act(() => result.current.reload())
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
  })
})
