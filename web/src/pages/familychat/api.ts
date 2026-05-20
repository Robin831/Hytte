// Shared types and helpers for Family Chat API calls. Kept narrowly scoped to
// the reactions feature for now; if other pages start hitting the API
// directly we can grow this into a fuller wrapper.

export interface ReactionBucket {
  count: number
  users: number[]
  extra_count?: number
  me: boolean
}

export type ReactionMap = Record<string, ReactionBucket>

// addReaction POSTs a reaction. Resolves on success (204). Throws on any
// non-success response so callers can roll back optimistic UI updates.
export async function addReaction(convID: number, msgID: number, emoji: string): Promise<void> {
  const res = await fetch(
    `/api/familychat/conversations/${convID}/messages/${msgID}/reactions`,
    {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ emoji }),
    },
  )
  if (!res.ok) throw new Error(`add reaction failed: ${res.status}`)
}

// removeReaction DELETEs a reaction. Resolves on success (204). Throws on
// any non-success response.
export async function removeReaction(convID: number, msgID: number, emoji: string): Promise<void> {
  const res = await fetch(
    `/api/familychat/conversations/${convID}/messages/${msgID}/reactions?emoji=${encodeURIComponent(emoji)}`,
    {
      method: 'DELETE',
      credentials: 'include',
    },
  )
  if (!res.ok) throw new Error(`remove reaction failed: ${res.status}`)
}

// applyReactionEvent mutates a reaction map in place to reflect an
// add/remove SSE event. Returns a NEW object so React state updates trigger
// re-renders even when the bucket fields didn't change identity.
export function applyReactionEvent(
  current: ReactionMap | undefined,
  evt: { user_id: number; emoji: string; count: number },
  meUserID: number | undefined,
  removed: boolean,
): ReactionMap {
  const next: ReactionMap = { ...(current ?? {}) }
  const existing = next[evt.emoji]
  const isMe = meUserID !== undefined && evt.user_id === meUserID
  if (removed) {
    if (!existing) return next
    const users = existing.users.filter(u => u !== evt.user_id)
    const newCount = evt.count
    if (newCount <= 0) {
      delete next[evt.emoji]
      return next
    }
    next[evt.emoji] = {
      ...existing,
      count: newCount,
      users,
      me: isMe ? false : existing.me,
    }
    return next
  }
  // added
  const users = existing?.users ?? []
  const alreadyHasUser = users.includes(evt.user_id)
  next[evt.emoji] = {
    count: evt.count,
    users: alreadyHasUser ? users : [...users, evt.user_id],
    extra_count: existing?.extra_count,
    me: isMe ? true : (existing?.me ?? false),
  }
  return next
}
