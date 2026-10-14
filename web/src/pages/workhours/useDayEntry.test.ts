// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useDayEntry } from './useDayEntry'

const stableT = (key: string) => key

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
}))

const TODAY = '2026-04-17'

describe('useDayEntry add-session prefill from the open punch', () => {
  beforeEach(() => {
    // Every call answers 404, so the day loads empty; only the drafts matter here.
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false, status: 404, json: () => Promise.resolve(null) } as Response)))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function renderEntry(initial: string | null) {
    return renderHook(
      ({ openPunchStart }: { openPunchStart: string | null }) =>
        useDayEntry({ date: TODAY, openPunchStart, lastPunchOut: null, onFlexChanged: () => {} }),
      { initialProps: { openPunchStart: initial } },
    )
  }

  it('starts empty without an open punch', () => {
    const { result } = renderEntry(null)
    expect(result.current.draft.start).toBe('')
    expect(result.current.draft.end).toBe('')
  })

  it('starts from the open punch when one is already running', () => {
    const { result } = renderEntry('08:00')
    expect(result.current.draft.start).toBe('08:00')
    expect(result.current.draft.end).toBe('')
  })

  it('punch-in fills the start and clears a typed end', () => {
    const { result, rerender } = renderEntry(null)
    act(() => result.current.changeDraft({ end: '16:00' }))
    expect(result.current.draft.end).toBe('16:00')

    rerender({ openPunchStart: '08:00' })
    expect(result.current.draft.start).toBe('08:00')
    expect(result.current.draft.end).toBe('')
  })

  it('editing the open punch start follows it and keeps a typed end', () => {
    const { result, rerender } = renderEntry('08:00')
    act(() => result.current.changeDraft({ end: '16:00' }))

    rerender({ openPunchStart: '07:45' })
    expect(result.current.draft.start).toBe('07:45')
    expect(result.current.draft.end).toBe('16:00')
  })

  it('punch-out clears both start and end', () => {
    const { result, rerender } = renderEntry('08:00')
    act(() => result.current.changeDraft({ end: '16:00' }))

    rerender({ openPunchStart: null })
    expect(result.current.draft.start).toBe('')
    expect(result.current.draft.end).toBe('')
  })

  it('leaves the draft alone while the open punch is unchanged', () => {
    const { result, rerender } = renderEntry('08:00')
    act(() => result.current.changeDraft({ start: '09:00', end: '16:00' }))

    rerender({ openPunchStart: '08:00' })
    expect(result.current.draft.start).toBe('09:00')
    expect(result.current.draft.end).toBe('16:00')
  })
})
