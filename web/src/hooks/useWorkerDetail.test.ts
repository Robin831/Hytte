// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useWorkerDetail } from './useWorkerDetail'

// useForgeEvents opens an EventSource and SSE connections — stub it so tests
// stay self-contained and don't fail on missing SSE support in happy-dom.
vi.mock('./useForgeEvents', () => ({
  useForgeEvents: () => ({ events: [] }),
}))

function makeWorker(overrides: Record<string, unknown> = {}) {
  return {
    id: 'worker-1',
    bead_id: 'Hytte-test',
    status: 'running',
    phase: 'smith',
    anvil: 'hytte',
    branch: 'forge/Hytte-test',
    pr_number: 0,
    started_at: new Date().toISOString(),
    completed_at: undefined,
    updated_at: undefined,
    title: 'Test bead',
    ...overrides,
  }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useWorkerDetail', () => {
  it('starts in loading state', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { result } = renderHook(() => useWorkerDetail('worker-1'))
    expect(result.current.loading).toBe(true)
    expect(result.current.worker).toBeNull()
    expect(result.current.error).toBeNull()
  })

  it('sets worker when found in response', async () => {
    const worker = makeWorker()
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (url.includes('/api/forge/workers') && !url.includes('/log')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve([worker]) })
      }
      return Promise.resolve({ ok: false })
    }))

    const { result } = renderHook(() => useWorkerDetail('worker-1'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.worker?.id).toBe('worker-1')
    expect(result.current.error).toBeNull()
  })

  it('sets worker to null when worker not found', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (url.includes('/api/forge/workers') && !url.includes('/log')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve([]) })
      }
      return Promise.resolve({ ok: false })
    }))

    const { result } = renderHook(() => useWorkerDetail('worker-1'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.worker).toBeNull()
  })

  it('does not schedule another poll when worker is not found', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = vi.fn((url: string) => {
      if (url.includes('/api/forge/workers') && !url.includes('/log')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve([]) })
      }
      return Promise.resolve({ ok: false })
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWorkerDetail('worker-1'))
    await waitFor(() => expect(result.current.loading).toBe(false))

    const workerFetchCount = fetchMock.mock.calls.filter(
      ([url]: [string]) => url.includes('/api/forge/workers') && !url.includes('/log'),
    ).length

    // Advance past the 5s polling interval — no new poll should fire
    await act(async () => { vi.advanceTimersByTime(6000) })
    const workerFetchCountAfter = fetchMock.mock.calls.filter(
      ([url]: [string]) => url.includes('/api/forge/workers') && !url.includes('/log'),
    ).length

    expect(workerFetchCountAfter).toBe(workerFetchCount)
    vi.useRealTimers()
  })

  it('does not poll again when worker status is completed', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const worker = makeWorker({ status: 'completed' })
    const fetchMock = vi.fn((url: string) => {
      if (url.includes('/api/forge/workers') && !url.includes('/log')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve([worker]) })
      }
      return Promise.resolve({ ok: false })
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWorkerDetail('worker-1'))
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.worker?.status).toBe('completed')
    const countAfterLoad = fetchMock.mock.calls.filter(
      ([url]: [string]) => url.includes('/api/forge/workers') && !url.includes('/log'),
    ).length

    await act(async () => { vi.advanceTimersByTime(6000) })
    const countAfterTimer = fetchMock.mock.calls.filter(
      ([url]: [string]) => url.includes('/api/forge/workers') && !url.includes('/log'),
    ).length

    expect(countAfterTimer).toBe(countAfterLoad)
    vi.useRealTimers()
  })

  it('sets error on HTTP failure', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({ ok: false, status: 503 }),
    ))

    const { result } = renderHook(() => useWorkerDetail('worker-1'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('HTTP 503')
  })
})
