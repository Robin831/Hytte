// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useTabFetch } from './useTabFetch'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useTabFetch', () => {
  it('reports loading=true synchronously on first activation, before the fetch resolves', () => {
    // Fetcher that never resolves so the request stays in-flight.
    const fetcher = vi.fn(() => new Promise<{ value: number }>(() => {}))
    const onSuccess = vi.fn()

    const { result } = renderHook(() =>
      useTabFetch(true, fetcher, onSuccess, 'failed'),
    )

    // Skeleton must show immediately on first entry — never a blank list.
    expect(result.current.loading).toBe(true)
    expect(result.current.error).toBe('')
    expect(onSuccess).not.toHaveBeenCalled()
  })

  it('is not loading while the tab is inactive', () => {
    const fetcher = vi.fn(() => new Promise<{ value: number }>(() => {}))
    const onSuccess = vi.fn()

    const { result } = renderHook(() =>
      useTabFetch(false, fetcher, onSuccess, 'failed'),
    )

    expect(result.current.loading).toBe(false)
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('clears loading and delivers data after the fetch resolves', async () => {
    const fetcher = vi.fn(() => Promise.resolve({ value: 42 }))
    const onSuccess = vi.fn()

    const { result } = renderHook(() =>
      useTabFetch(true, fetcher, onSuccess, 'failed'),
    )

    expect(result.current.loading).toBe(true)
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(onSuccess).toHaveBeenCalledWith({ value: 42 })
    expect(result.current.error).toBe('')
  })

  it('does not re-show the skeleton when re-entering an already-loaded tab', async () => {
    const fetcher = vi.fn(() => Promise.resolve({ value: 1 }))
    const onSuccess = vi.fn()

    const { result, rerender } = renderHook(
      ({ active }) => useTabFetch(active, fetcher, onSuccess, 'failed'),
      { initialProps: { active: true } },
    )

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetcher).toHaveBeenCalledTimes(1)

    // Switch away from the tab, then back. The cached `loaded` flag must keep
    // loading false so no skeleton flashes on re-entry, and no refetch fires.
    rerender({ active: false })
    expect(result.current.loading).toBe(false)

    rerender({ active: true })
    expect(result.current.loading).toBe(false)
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it('surfaces the error message and stops loading on a failed fetch', async () => {
    const fetcher = vi.fn(() => Promise.reject(new Error('boom')))
    const onSuccess = vi.fn()

    const { result } = renderHook(() =>
      useTabFetch(true, fetcher, onSuccess, 'failed'),
    )

    await waitFor(() => expect(result.current.error).toBe('failed'))
    expect(result.current.loading).toBe(false)
    expect(onSuccess).not.toHaveBeenCalled()
  })

  it('reload() re-runs the fetcher and shows loading again', async () => {
    const fetcher = vi.fn(() => Promise.resolve({ value: 1 }))
    const onSuccess = vi.fn()

    const { result } = renderHook(() =>
      useTabFetch(true, fetcher, onSuccess, 'failed'),
    )

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetcher).toHaveBeenCalledTimes(1)

    act(() => result.current.reload())
    expect(result.current.loading).toBe(true)
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetcher).toHaveBeenCalledTimes(2)
  })
})
