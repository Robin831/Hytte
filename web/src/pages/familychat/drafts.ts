// Per-conversation composer drafts for Family Chat.
//
// ChatView is remounted on every conversation switch (see the
// key={selectedConversationId} in FamilyChat.tsx), which used to discard a
// half-typed message. The composer now saves its text here when the
// conversation changes (or on unmount) and restores it on mount.
//
// localStorage is the persistence layer so a draft survives a full reload; an
// in-memory Map mirrors every write so private-mode/quota failures degrade
// silently instead of breaking the composer. The Map is read first — within a
// session it is always at least as fresh as storage.

export const DRAFT_KEY_PREFIX = 'hytte.familychat.draft.'

const memoryDrafts = new Map<string, string>()

export function draftKey(conversationId: number | string): string {
  return `${DRAFT_KEY_PREFIX}${conversationId}`
}

// Conversation ids are positive row ids, so any falsy value (0, NaN, '')
// means "no conversation selected" and is not worth a storage round-trip.

// getDraft returns the stored draft for a conversation, or '' when there is
// none. Never throws — storage failures fall back to the in-memory mirror.
export function getDraft(conversationId: number | string): string {
  if (!conversationId) return ''
  const key = draftKey(conversationId)
  const cached = memoryDrafts.get(key)
  if (cached !== undefined) return cached
  if (typeof window === 'undefined') return ''
  try {
    return window.localStorage?.getItem(key) ?? ''
  } catch {
    // Storage disabled (private mode, blocked cookies) — nothing cached.
    return ''
  }
}

// setDraft persists the composer text for a conversation. A blank (or
// whitespace-only) body removes any previously stored draft instead of
// writing an empty one, so stale keys don't accumulate.
export function setDraft(conversationId: number | string, body: string): void {
  if (!conversationId) return
  if (!body.trim()) {
    clearDraft(conversationId)
    return
  }
  const key = draftKey(conversationId)
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
export function clearDraft(conversationId: number | string): void {
  if (!conversationId) return
  const key = draftKey(conversationId)
  memoryDrafts.delete(key)
  if (typeof window === 'undefined') return
  try {
    window.localStorage?.removeItem(key)
  } catch {
    // Ignore storage errors.
  }
}

// __resetDrafts wipes every draft from both layers. Test-only helper — the app
// never needs it, since drafts are cleared per conversation on send.
export function __resetDrafts(): void {
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
