// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useFlex } from './useFlex'

const stableT = (key: string) => key

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
}))

const DATE = '2026-04-17'

type Handler = (init?: RequestInit) => Promise<Response>

function jsonResponse(body: unknown, ok = true): Response {
  return { ok, json: () => Promise.resolve(body) } as Response
}

// Routes fetch by "METHOD path"; unknown routes answer 404.
function stubFetch(routes: Record<string, Handler>) {
  const fetchMock = vi.fn((url: string, init?: RequestInit) => {
    const path = url.toString().split('?')[0]
    const method = (init?.method ?? 'GET').toUpperCase()
    const handler = routes[`${method} ${path}`]
    return handler ? handler(init) : Promise.resolve(jsonResponse(null, false))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function calls(fetchMock: ReturnType<typeof stubFetch>, method: string, path: string) {
  return fetchMock.mock.calls.filter(([url, init]) =>
    url.toString().split('?')[0] === path && ((init?.method ?? 'GET').toUpperCase() === method),
  )
}

const flexBody = (total: number) => ({ flex: { total_minutes: total, to_next_interval: 0 }, reset_date: '2026-01-01', days_in_pool: 0 })

describe('useFlex', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('loads the pool on mount', async () => {
    stubFetch({ 'GET /api/workhours/flex': () => Promise.resolve(jsonResponse(flexBody(45))) })
    const { result } = renderHook(() => useFlex())
    await waitFor(() => expect(result.current.flex?.flex.total_minutes).toBe(45))
  })

  it('redeem posts the date, reloads the pool and resolves true', async () => {
    let total = 45
    const fetchMock = stubFetch({
      'GET /api/workhours/flex': () => Promise.resolve(jsonResponse(flexBody(total))),
      'POST /api/workhours/flex/redeem': () => {
        total = 15
        return Promise.resolve(jsonResponse({}))
      },
    })
    const alertSpy = vi.fn()
    vi.stubGlobal('alert', alertSpy)
    const { result } = renderHook(() => useFlex())
    await waitFor(() => expect(result.current.flex?.flex.total_minutes).toBe(45))

    let ok: boolean | undefined
    await act(async () => { ok = await result.current.redeem(DATE) })

    expect(ok).toBe(true)
    const posts = calls(fetchMock, 'POST', '/api/workhours/flex/redeem')
    expect(posts).toHaveLength(1)
    expect(JSON.parse(posts[0][1]?.body as string)).toEqual({ date: DATE })
    await waitFor(() => expect(result.current.flex?.flex.total_minutes).toBe(15))
    expect(calls(fetchMock, 'GET', '/api/workhours/flex')).toHaveLength(2)
    expect(result.current.redeeming).toBe(false)
    expect(alertSpy).not.toHaveBeenCalled()
  })

  it('redeem alerts the server error, skips the reload and resolves false', async () => {
    const fetchMock = stubFetch({
      'GET /api/workhours/flex': () => Promise.resolve(jsonResponse(flexBody(45))),
      'POST /api/workhours/flex/redeem': () => Promise.resolve(jsonResponse({ error: 'not enough flex' }, false)),
    })
    const alertSpy = vi.fn()
    vi.stubGlobal('alert', alertSpy)
    const { result } = renderHook(() => useFlex())
    await waitFor(() => expect(result.current.flex).not.toBeNull())

    let ok: boolean | undefined
    await act(async () => { ok = await result.current.redeem(DATE) })

    expect(ok).toBe(false)
    expect(alertSpy).toHaveBeenCalledWith('not enough flex')
    expect(calls(fetchMock, 'GET', '/api/workhours/flex')).toHaveLength(1)
    expect(result.current.redeeming).toBe(false)
  })

  it('redeem falls back to the generic error when the request throws', async () => {
    stubFetch({
      'GET /api/workhours/flex': () => Promise.resolve(jsonResponse(flexBody(45))),
      'POST /api/workhours/flex/redeem': () => Promise.reject(new Error('offline')),
    })
    const alertSpy = vi.fn()
    vi.stubGlobal('alert', alertSpy)
    const { result } = renderHook(() => useFlex())
    await waitFor(() => expect(result.current.flex).not.toBeNull())

    let ok: boolean | undefined
    await act(async () => { ok = await result.current.redeem(DATE) })

    expect(ok).toBe(false)
    expect(alertSpy).toHaveBeenCalledWith('workhours:redeemFlexError')
  })

  it('redeeming is true while the request is in flight', async () => {
    let resolveRedeem: (r: Response) => void = () => {}
    stubFetch({
      'GET /api/workhours/flex': () => Promise.resolve(jsonResponse(flexBody(45))),
      'POST /api/workhours/flex/redeem': () => new Promise<Response>(r => { resolveRedeem = r }),
    })
    const { result } = renderHook(() => useFlex())
    await waitFor(() => expect(result.current.flex).not.toBeNull())

    let pending: Promise<boolean> = Promise.resolve(false)
    act(() => { pending = result.current.redeem(DATE) })
    await waitFor(() => expect(result.current.redeeming).toBe(true))

    await act(async () => {
      resolveRedeem(jsonResponse({}))
      await pending
    })
    expect(result.current.redeeming).toBe(false)
  })
})
