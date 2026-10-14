import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { usePunchClock } from './usePunchClock'
import { DEFAULT_WORK_SETTINGS } from '../calculateDay'

const stableT = (key: string) => key

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
}))

const TODAY = '2026-04-17'

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

const noPunch: Handler = () => Promise.resolve(jsonResponse({ session: null }))
const openPunchAt = (start: string, date = TODAY): Handler =>
  () => Promise.resolve(jsonResponse({ session: { start_time: start, date } }))

describe('usePunchClock', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(`${TODAY}T12:00:00`))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('restores an open punch from the server', async () => {
    stubFetch({ 'GET /api/workhours/punch-session': openPunchAt('08:00') })
    const { result } = renderHook(() => usePunchClock({ date: TODAY, setDate: vi.fn() }))
    await waitFor(() => expect(result.current.openPunch).toEqual({ start: '08:00', date: TODAY }))
  })

  it('toggle switches between punch-in and punch-out', async () => {
    const fetchMock = stubFetch({
      'GET /api/workhours/punch-session': noPunch,
      'POST /api/workhours/punch-in': () => Promise.resolve(jsonResponse({})),
      'POST /api/workhours/punch-out': () => Promise.resolve(jsonResponse({ day: null, summary: null, date: TODAY })),
    })
    const { result } = renderHook(() => usePunchClock({ date: TODAY, setDate: vi.fn() }))
    await waitFor(() => expect(calls(fetchMock, 'GET', '/api/workhours/punch-session')).toHaveLength(1))

    await act(async () => { result.current.toggle() })
    await waitFor(() => expect(result.current.openPunch).toEqual({ start: '12:00', date: TODAY }))
    expect(calls(fetchMock, 'POST', '/api/workhours/punch-in')).toHaveLength(1)

    vi.setSystemTime(new Date(`${TODAY}T13:00:00`))
    await act(async () => { result.current.toggle() })
    await waitFor(() => expect(result.current.openPunch).toBeNull())
    expect(calls(fetchMock, 'POST', '/api/workhours/punch-out')).toHaveLength(1)
  })

  it('toggle does nothing while saving', async () => {
    let resolvePunchIn: (r: Response) => void = () => {}
    const fetchMock = stubFetch({
      'GET /api/workhours/punch-session': noPunch,
      'POST /api/workhours/punch-in': () => new Promise<Response>(resolve => { resolvePunchIn = resolve }),
    })
    const { result } = renderHook(() => usePunchClock({ date: TODAY, setDate: vi.fn() }))
    await waitFor(() => expect(calls(fetchMock, 'GET', '/api/workhours/punch-session')).toHaveLength(1))

    act(() => { result.current.toggle() })
    expect(result.current.saving).toBe(true)
    // Both the same-tick toggle and one after the re-render are ignored.
    act(() => { result.current.toggle() })
    act(() => { result.current.toggle() })
    expect(calls(fetchMock, 'POST', '/api/workhours/punch-in')).toHaveLength(1)
    expect(calls(fetchMock, 'POST', '/api/workhours/punch-out')).toHaveLength(0)

    await act(async () => { resolvePunchIn(jsonResponse({})) })
    await waitFor(() => expect(result.current.saving).toBe(false))
    expect(result.current.openPunch).toEqual({ start: '12:00', date: TODAY })
  })

  it('calls onChanged after a punch-out and moves to the saved date', async () => {
    stubFetch({
      'GET /api/workhours/punch-session': openPunchAt('08:00'),
      'POST /api/workhours/punch-out': () => Promise.resolve(jsonResponse({ day: null, summary: null, date: '2026-04-16' })),
    })
    const onChanged = vi.fn()
    const setDate = vi.fn()
    const { result } = renderHook(() => usePunchClock({ date: TODAY, setDate, onChanged }))
    await waitFor(() => expect(result.current.openPunch).not.toBeNull())

    await act(async () => { await result.current.punchOut() })
    expect(onChanged).toHaveBeenCalledTimes(1)
    expect(setDate).toHaveBeenCalledWith('2026-04-16')
    expect(result.current.openPunch).toBeNull()
  })

  it('does not call onChanged when the punch is cancelled', async () => {
    stubFetch({
      'GET /api/workhours/punch-session': openPunchAt('08:00'),
      'DELETE /api/workhours/punch-session': () => Promise.resolve(jsonResponse({})),
    })
    const onChanged = vi.fn()
    const { result } = renderHook(() => usePunchClock({ date: TODAY, setDate: vi.fn(), onChanged }))
    await waitFor(() => expect(result.current.openPunch).not.toBeNull())

    await act(async () => { await result.current.cancelPunch() })
    expect(result.current.openPunch).toBeNull()
    expect(onChanged).not.toHaveBeenCalled()
  })

  it('liveEstimate reflects the open punch', async () => {
    stubFetch({ 'GET /api/workhours/punch-session': openPunchAt('08:00') })
    const { result } = renderHook(() => usePunchClock({ date: TODAY, setDate: vi.fn() }))
    const input = {
      sessions: [],
      deductions: [],
      settings: { ...DEFAULT_WORK_SETTINGS, lunch_minutes: 0 },
      lunch: false,
      redeemedMinutes: 0,
    }
    expect(result.current.liveEstimate(input)).toBeNull()

    await waitFor(() => expect(result.current.openPunch).not.toBeNull())
    const estimate = result.current.liveEstimate(input)
    expect(estimate?.kind).toBe('estimate')
    if (estimate?.kind === 'estimate') {
      // 08:00 -> 12:00
      expect(estimate.calc.gross).toBe(240)
    }
  })

  it('liveEstimate is null on a day other than the punch-in', async () => {
    stubFetch({ 'GET /api/workhours/punch-session': openPunchAt('08:00') })
    const { result } = renderHook(() => usePunchClock({ date: '2026-04-20', setDate: vi.fn() }))
    await waitFor(() => expect(result.current.openPunch).not.toBeNull())
    expect(result.current.liveEstimate({
      sessions: [], deductions: [], settings: DEFAULT_WORK_SETTINGS, lunch: false, redeemedMinutes: 0,
    })).toBeNull()
  })

  it('liveEstimate flags a punch-in left open for more than a day', async () => {
    stubFetch({ 'GET /api/workhours/punch-session': openPunchAt('08:00', '2026-04-15') })
    const { result } = renderHook(() => usePunchClock({ date: '2026-04-15', setDate: vi.fn() }))
    await waitFor(() => expect(result.current.openPunch).not.toBeNull())
    expect(result.current.liveEstimate({
      sessions: [], deductions: [], settings: DEFAULT_WORK_SETTINGS, lunch: false, redeemedMinutes: 0,
    })).toEqual({ kind: 'stale', punchDate: '2026-04-15' })
  })
})
