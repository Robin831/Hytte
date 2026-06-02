// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useCurrentTime } from './useCurrentTime'

function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', {
    configurable: true,
    get: () => hidden,
  })
}

describe('useCurrentTime', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    setHidden(false)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('returns a Date', () => {
    vi.setSystemTime(new Date('2026-06-02T10:30:15.000Z'))
    const { result } = renderHook(() => useCurrentTime())
    expect(result.current).toBeInstanceOf(Date)
  })

  it('re-renders at the start of the next wall-clock minute', () => {
    vi.setSystemTime(new Date('2026-06-02T10:30:15.000Z'))
    const { result } = renderHook(() => useCurrentTime())

    const initial = result.current
    expect(initial.getSeconds()).toBe(15)

    // Advance to just before the minute boundary — no tick yet.
    act(() => {
      vi.advanceTimersByTime(44_999)
    })
    expect(result.current).toBe(initial)

    // Cross the :00 boundary — the hook should snap to the new minute.
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(result.current).not.toBe(initial)
    expect(result.current.getMinutes()).toBe(31)
    expect(result.current.getSeconds()).toBe(0)
  })

  it('keeps ticking every minute after the first aligned fire', () => {
    vi.setSystemTime(new Date('2026-06-02T10:30:15.000Z'))
    const { result } = renderHook(() => useCurrentTime())

    act(() => {
      vi.advanceTimersByTime(45_000) // -> 10:31:00
    })
    const afterFirst = result.current
    expect(afterFirst.getMinutes()).toBe(31)

    act(() => {
      vi.advanceTimersByTime(60_000) // -> 10:32:00
    })
    expect(result.current).not.toBe(afterFirst)
    expect(result.current.getMinutes()).toBe(32)
  })

  it('clears all timers on unmount with no leaks', () => {
    vi.setSystemTime(new Date('2026-06-02T10:30:15.000Z'))
    const { unmount } = renderHook(() => useCurrentTime())

    // The initial alignment timeout is pending.
    expect(vi.getTimerCount()).toBeGreaterThan(0)

    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('clears the interval timer on unmount after the boundary fired', () => {
    vi.setSystemTime(new Date('2026-06-02T10:30:15.000Z'))
    const { unmount } = renderHook(() => useCurrentTime())

    act(() => {
      vi.advanceTimersByTime(45_000) // alignment timeout fires, interval armed
    })
    expect(vi.getTimerCount()).toBeGreaterThan(0)

    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('snaps to the current time on window focus (wake/resume)', () => {
    vi.setSystemTime(new Date('2026-06-02T10:30:15.000Z'))
    const { result } = renderHook(() => useCurrentTime())
    const initial = result.current

    // The device sleeps/resumes without a visibility change; wall-clock time
    // jumps forward while no timer fired.
    act(() => {
      vi.setSystemTime(new Date('2026-06-02T11:05:42.000Z'))
    })
    expect(result.current).toBe(initial)

    // Focus resyncs immediately to the real current time.
    act(() => {
      window.dispatchEvent(new Event('focus'))
    })
    expect(result.current).not.toBe(initial)
    expect(result.current.getMinutes()).toBe(5)
    expect(result.current.getSeconds()).toBe(42)

    // The next tick stays aligned to the top of the minute (18s away), not
    // drifted to the resume offset.
    act(() => {
      vi.advanceTimersByTime(17_999)
    })
    expect(result.current.getSeconds()).toBe(42)
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(result.current.getMinutes()).toBe(6)
    expect(result.current.getSeconds()).toBe(0)
  })

  it('ignores focus while hidden so the hook stays paused', () => {
    vi.setSystemTime(new Date('2026-06-02T10:30:15.000Z'))
    const { result } = renderHook(() => useCurrentTime())
    const initial = result.current

    setHidden(true)
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(vi.getTimerCount()).toBe(0)

    // A focus event while hidden must not re-arm timers or update the time.
    act(() => {
      vi.setSystemTime(new Date('2026-06-02T10:50:00.000Z'))
      window.dispatchEvent(new Event('focus'))
    })
    expect(result.current).toBe(initial)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('removes the focus listener on unmount (no resync after unmount)', () => {
    vi.setSystemTime(new Date('2026-06-02T10:30:15.000Z'))
    const { result, unmount } = renderHook(() => useCurrentTime())
    const initial = result.current

    unmount()
    expect(vi.getTimerCount()).toBe(0)

    // Late focus event must not revive the hook.
    act(() => {
      vi.setSystemTime(new Date('2026-06-02T10:50:00.000Z'))
      window.dispatchEvent(new Event('focus'))
    })
    expect(result.current).toBe(initial)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('pauses while hidden and snaps to the current time when visible again', () => {
    vi.setSystemTime(new Date('2026-06-02T10:30:15.000Z'))
    const { result } = renderHook(() => useCurrentTime())
    const initial = result.current

    // Tab becomes hidden -> timers are cleared, hook pauses.
    setHidden(true)
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(vi.getTimerCount()).toBe(0)

    // Time moves on while hidden; the paused hook does not update.
    act(() => {
      vi.setSystemTime(new Date('2026-06-02T10:45:30.000Z'))
    })
    expect(result.current).toBe(initial)

    // Becoming visible snaps to the current time and re-arms the timer.
    setHidden(false)
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(result.current).not.toBe(initial)
    expect(result.current.getMinutes()).toBe(45)
    expect(result.current.getSeconds()).toBe(30)
    expect(vi.getTimerCount()).toBeGreaterThan(0)
  })
})
