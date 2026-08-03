// Per-conversation composer drafts for Family Chat.
//
// ChatView is remounted on every conversation switch (see the
// key={selectedConversationId} in FamilyChat.tsx), which used to discard a
// half-typed message. The composer now saves its text here when the
// conversation changes, on unmount, and on pagehide so a draft survives a full
// reload.
//
// localStorage is the persistence layer; an in-memory Map acts as a fallback
// when storage is unavailable (private mode, quota errors). Storage is read
// first — the mirror only serves when the read throws.
//
// Keys are namespaced by user id so drafts never leak across accounts on a
// shared device, and clearAllDrafts() is called on logout as a second line
// of defence.

export const DRAFT_KEY_PREFIX = 'familychat-draft:'

const memoryDrafts = new Map<string, string>()

export function draftKey(userId: number, conversationId: number | string): string {
  return `${DRAFT_KEY_PREFIX}${userId}:${conversationId}`
}

// Conversation ids are positive row ids, so any falsy value (0, NaN, '')
// means "no conversation selected" and is not worth a storage round-trip.

// getDraft returns the stored draft for a conversation, or '' when there is
// none. Never throws — reads storage first, then the in-memory mirror;
// storage errors fall back to the mirror.
export function getDraft(userId: number, conversationId: number | string): string {
  if (!conversationId || !userId) return ''
  const key = draftKey(userId, conversationId)
  if (typeof window !== 'undefined') {
    try {
      const stored = window.localStorage?.getItem(key)
      if (stored !== null) {
        memoryDrafts.set(key, stored)
        return stored
      }
    } catch {
      // Storage disabled — fall back to the in-memory mirror.
    }
  }
  return memoryDrafts.get(key) ?? ''
}

// setDraft persists the composer text for a conversation. A blank (or
// whitespace-only) body removes any previously stored draft instead of
// writing an empty one, so stale keys don't accumulate.
export function setDraft(userId: number, conversationId: number | string, body: string): void {
  if (!conversationId || !userId) return
  if (!body.trim()) {
    clearDraft(userId, conversationId)
    return
  }
  const key = draftKey(userId, conversationId)
  memoryDrafts.set(key, body)
  if (typeof window === 'undefined') return
  try {
    window.localStorage?.setItem(key, body)
  } catch {
    // Quota exhausted or storage disabled — the in-memory mirror still serves
    // this session's switches, the draft just won't survive a reload.
  }
}

// clearDraft drops a conversation's draft from both storage layers. Called
// after a successful send.
export function clearDraft(userId: number, conversationId: number | string): void {
  if (!conversationId || !userId) return
  const key = draftKey(userId, conversationId)
  memoryDrafts.delete(key)
  if (typeof window === 'undefined') return
  try {
    window.localStorage?.removeItem(key)
  } catch {
    // Ignore storage errors.
  }
}

// clearAllDrafts wipes every draft from both layers. Called on logout to
// prevent draft leakage across user sessions on shared devices.
export function clearAllDrafts(): void {
  memoryDrafts.clear()
  if (typeof window === 'undefined') return
  try {
    const storage = window.localStorage
    if (!storage) return
    const stale: string[] = []
    for (let i = 0; i < storage.length; i++) {
      const key = storage.key(i)
      if (key?.startsWith(DRAFT_KEY_PREFIX)) stale.push(key)
    }
    for (const key of stale) storage.removeItem(key)
  } catch {
    // Ignore storage errors.
  }
}

// __resetDrafts is a test-only alias for clearAllDrafts.
export const __resetDrafts = clearAllDrafts
