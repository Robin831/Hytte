// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { addReaction, removeReaction, applyReactionEvent, type ReactionMap } from './api'

describe('addReaction', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('POSTs to the reactions endpoint with the emoji in the body', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)
    await addReaction(7, 42, '👍')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/familychat/conversations/7/messages/42/reactions')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ emoji: '👍' })
  })

  it('throws when the response is not ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 400 }))
    await expect(addReaction(1, 1, '👍')).rejects.toThrow()
  })
})

describe('removeReaction', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('DELETEs with the emoji url-encoded into the query string', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)
    await removeReaction(3, 9, '👍')
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toContain('/api/familychat/conversations/3/messages/9/reactions?emoji=')
    expect(url).toContain(encodeURIComponent('👍'))
    expect(init.method).toBe('DELETE')
  })
})

describe('applyReactionEvent', () => {
  it('adds a brand-new emoji bucket', () => {
    const out = applyReactionEvent(undefined, { user_id: 5, emoji: '🎉', count: 1 }, 5, false)
    expect(out).toEqual({ '🎉': { count: 1, users: [5], extra_count: undefined, me: true } })
  })

  it('increments an existing bucket and sets me when the viewer is the actor', () => {
    const prev: ReactionMap = { '👍': { count: 1, users: [3], me: false } }
    const out = applyReactionEvent(prev, { user_id: 7, emoji: '👍', count: 2 }, 7, false)
    expect(out['👍'].count).toBe(2)
    expect(out['👍'].users).toEqual([3, 7])
    expect(out['👍'].me).toBe(true)
  })

  it('does not flip me when another user adds a reaction', () => {
    const prev: ReactionMap = { '👍': { count: 1, users: [3], me: false } }
    const out = applyReactionEvent(prev, { user_id: 8, emoji: '👍', count: 2 }, 7, false)
    expect(out['👍'].me).toBe(false)
  })

  it('removes the bucket entirely when the count drops to zero', () => {
    const prev: ReactionMap = { '👍': { count: 1, users: [3], me: false } }
    const out = applyReactionEvent(prev, { user_id: 3, emoji: '👍', count: 0 }, 3, true)
    expect(out['👍']).toBeUndefined()
  })

  it('clears me when the viewer themselves removes', () => {
    const prev: ReactionMap = { '👍': { count: 2, users: [3, 7], me: true } }
    const out = applyReactionEvent(prev, { user_id: 7, emoji: '👍', count: 1 }, 7, true)
    expect(out['👍'].me).toBe(false)
    expect(out['👍'].users).toEqual([3])
  })
})
