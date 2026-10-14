import type { Conversation, Message } from '../components/chat/types'

// Thin wrappers around the chat REST endpoints used by the Chat page. Each
// throws an Error carrying `fallbackError` (or, where `preferServerError` is
// set, the server's `{"error"}` message) when the response is not OK. The
// SSE send lives in useChatStream.

async function send(
  url: string,
  init: RequestInit,
  fallbackError: string,
  preferServerError: boolean,
): Promise<Response> {
  const res = await fetch(url, { credentials: 'include', ...init })
  if (!res.ok) {
    const data = preferServerError ? await res.json().catch(() => null) : null
    throw new Error(data?.error || fallbackError)
  }
  return res
}

async function request<T>(
  url: string,
  init: RequestInit,
  fallbackError: string,
  preferServerError = false,
): Promise<T> {
  const res = await send(url, init, fallbackError, preferServerError)
  return res.json()
}

function jsonInit(method: string, body: unknown): RequestInit {
  return {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  }
}

export async function listConversations(signal: AbortSignal, fallbackError: string): Promise<Conversation[]> {
  const data = await request<{ conversations?: Conversation[] }>('/api/chat/conversations', { signal }, fallbackError)
  return data.conversations ?? []
}

export async function listMessages(id: number, signal: AbortSignal, fallbackError: string): Promise<Message[]> {
  const data = await request<{ messages?: Message[] }>(`/api/chat/conversations/${id}`, { signal }, fallbackError)
  return data.messages ?? []
}

/** Creates a conversation; an empty `model` lets the backend pick. */
export async function createConversation(model: string, fallbackError: string): Promise<Conversation> {
  const data = await request<{ conversation: Conversation }>(
    '/api/chat/conversations',
    jsonInit('POST', model ? { model } : {}),
    fallbackError,
    true,
  )
  return data.conversation
}

export async function updateConversation(
  id: number,
  changes: { title?: string; model?: string },
  fallbackError: string,
): Promise<Conversation> {
  const data = await request<{ conversation: Conversation }>(
    `/api/chat/conversations/${id}`,
    jsonInit('PUT', changes),
    fallbackError,
  )
  return data.conversation
}

export async function deleteConversation(id: number, fallbackError: string): Promise<void> {
  await send(`/api/chat/conversations/${id}`, { method: 'DELETE' }, fallbackError, false)
}

/** Deletes `messageId` and every later message; returns the deleted message. */
export async function truncateMessages(
  conversationId: number,
  messageId: number,
  fallbackError: string,
): Promise<Message> {
  const data = await request<{ message: Message }>(
    `/api/chat/conversations/${conversationId}/messages/${messageId}`,
    { method: 'DELETE' },
    fallbackError,
    true,
  )
  return data.message
}
