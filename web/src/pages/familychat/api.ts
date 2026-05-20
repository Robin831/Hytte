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

// editMessage PATCHes a message body. Resolves with the server's view of the
// updated message (including the freshly stamped `edited_at`). Throws on any
// non-success response so callers can roll back optimistic UI updates.
export async function editMessage(
  convID: number,
  msgID: number,
  body: string,
): Promise<{
  id: number
  conversation_id: number
  sender_user_id: number
  body: string
  created_at: string
  edited_at: string | null
  deleted_at: string | null
  deleted_by: number | null
}> {
  const res = await fetch(
    `/api/familychat/conversations/${convID}/messages/${msgID}`,
    {
      method: 'PATCH',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ body }),
    },
  )
  if (!res.ok) throw new Error(`edit message failed: ${res.status}`)
  const data = await res.json()
  return data.message
}

export interface UploadResult {
  uploadId: string
  mime: string
  size: number
}

export class UploadError extends Error {
  status: number
  serverCode: string
  constructor(message: string, status: number, serverCode: string) {
    super(message)
    this.status = status
    this.serverCode = serverCode
  }
}

// uploadAttachment POSTs a binary blob (or File) to the conversation upload
// endpoint and returns the parsed {upload_id, mime, size} response. Shared by
// the paperclip file picker and the voice-note recorder; both flows then
// reference the upload_id in a follow-up POST /messages call. Throws
// UploadError with the HTTP status so the caller can map 413 / 400 codes to
// localized error strings.
export async function uploadAttachment(
  convID: number,
  file: Blob,
  filename: string,
  signal?: AbortSignal,
): Promise<UploadResult> {
  const form = new FormData()
  form.append('file', file, filename)
  const res = await fetch(`/api/familychat/conversations/${convID}/upload`, {
    method: 'POST',
    credentials: 'include',
    body: form,
    signal,
  })
  if (!res.ok) {
    let serverCode = ''
    try {
      const data = await res.json()
      serverCode = typeof data?.error === 'string' ? data.error : ''
    } catch {
      // Body wasn't JSON; fall back to status-only mapping.
    }
    throw new UploadError(serverCode || `upload failed: ${res.status}`, res.status, serverCode)
  }
  const data = await res.json()
  if (!data?.upload_id || typeof data.upload_id !== 'string') {
    throw new UploadError('upload returned no id', res.status, '')
  }
  return {
    uploadId: data.upload_id,
    mime: typeof data.mime === 'string' ? data.mime : (file.type || ''),
    size: typeof data.size === 'number' ? data.size : file.size,
  }
}

// deleteMessage DELETEs (soft-deletes) a message. Resolves on success (204).
// Throws on any non-success response.
export async function deleteMessage(convID: number, msgID: number): Promise<void> {
  const res = await fetch(
    `/api/familychat/conversations/${convID}/messages/${msgID}`,
    {
      method: 'DELETE',
      credentials: 'include',
    },
  )
  if (!res.ok) throw new Error(`delete message failed: ${res.status}`)
}

// applyReactionEvent returns a new reaction map reflecting an add/remove SSE
// event. The input map is never mutated; a shallow clone is always returned so
// React state updates trigger re-renders even when bucket fields are unchanged.
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
