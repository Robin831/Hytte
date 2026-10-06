// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { SessionConflict, useWorkHoursApi } from './useWorkHoursApi'

const conflictSession = {
  id: 7, day_id: 1, start_time: '09:00', end_time: '10:00', sort_order: 0, is_internal: false, crosses_midnight: false,
}

const input = { day_id: 1, start_time: '09:30', end_time: '11:00', sort_order: 1, is_internal: false, crosses_midnight: false }

function stubResponse(status: number, json: () => Promise<unknown>) {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: status >= 200 && status < 300, status, json } as Response)))
}

function api() {
  return renderHook(() => useWorkHoursApi()).result.current
}

describe('useWorkHoursApi session conflicts', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('throws SessionConflict with the conflicting session on a 409 from addSession', async () => {
    stubResponse(409, () => Promise.resolve({ error: 'overlaps', conflict: conflictSession }))
    const err = await api().addSession(input).catch(e => e)
    expect(err).toBeInstanceOf(SessionConflict)
    expect(err.message).toBe('overlaps')
    expect(err.conflict).toEqual(conflictSession)
  })

  it('throws SessionConflict on a 409 from updateSession', async () => {
    stubResponse(409, () => Promise.resolve({ error: 'overlaps', conflict: conflictSession }))
    await expect(api().updateSession(3, { ...input })).rejects.toBeInstanceOf(SessionConflict)
  })

  it('returns false for a 409 with an unparseable body', async () => {
    stubResponse(409, () => Promise.reject(new SyntaxError('bad json')))
    await expect(api().addSession(input)).resolves.toBe(false)
  })

  it('returns false for a 409 without a conflict object', async () => {
    stubResponse(409, () => Promise.resolve({ error: 'overlaps', conflict: null }))
    await expect(api().addSession(input)).resolves.toBe(false)
  })

  it('returns false for a non-409 error without reading the body', async () => {
    const json = vi.fn(() => Promise.resolve({ error: 'boom' }))
    stubResponse(500, json)
    await expect(api().updateSession(3, { ...input })).resolves.toBe(false)
    expect(json).not.toHaveBeenCalled()
  })

  it('returns true on success', async () => {
    stubResponse(201, () => Promise.resolve({}))
    await expect(api().addSession(input)).resolves.toBe(true)
  })
})
